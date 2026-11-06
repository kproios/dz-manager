// Package players keeps player identities, sessions and combat history (#11).
// Sources, in priority order: @DZMBridge events (authoritative), MetricZ
// presence (fallback sessions when the bridge is not connected).
package players

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/woozymasta/dzid"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/bridge"
	"github.com/kproios/dz-manager/internal/caps"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/metricz"
	"github.com/kproios/dz-manager/internal/profiles"
	"github.com/kproios/dz-manager/internal/store"
)

// Service maintains player history.
type Service struct {
	DB       *store.DB
	Profiles *profiles.Store
	Caps     *caps.Registry
	Audit    *audit.Log
	Log      *slog.Logger

	mu       sync.Mutex
	lastSeen map[string]time.Time // throttle presence writes
}

// Init prepares the service.
func (s *Service) Init() { s.lastSeen = map[string]time.Time{} }

// Attach subscribes to bridge events.
func (s *Service) Attach(b *bridge.Bridge) {
	b.Subscribe(s.onConnect, "player.connect")
	b.Subscribe(s.onDisconnect, "player.disconnect")
	b.Subscribe(s.onDeath, "player.death")
	b.Subscribe(s.onServerStart, "server.start")
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func validSteam64(s string) bool {
	if len(s) != 17 || !strings.HasPrefix(s, "7656119") {
		return false
	}
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

// upsert records an identity. reportedGUID is validated against dzid(steam64).
func (s *Service) upsert(ctx context.Context, steam64, name, reportedGUID string, at time.Time) {
	if !validSteam64(steam64) {
		return
	}
	id, _ := strconv.ParseUint(steam64, 10, 64)
	guid := dzid.BattlEyeString(id)
	dz := dzid.DayzString(id)
	// Sources report either the BE GUID or the DayZ ID (PlayerIdentity.GetId());
	// only a value matching neither is a mismatch.
	mismatch := 0
	if reportedGUID != "" && !strings.EqualFold(reportedGUID, guid) && reportedGUID != dz {
		mismatch = 1
	}
	t := ts(at)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO players(steam64,be_guid,dayz_id,name,first_seen,last_seen,id_mismatch) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(steam64) DO UPDATE SET name=CASE WHEN excluded.name<>'' THEN excluded.name ELSE players.name END,
		last_seen=MAX(players.last_seen, excluded.last_seen), id_mismatch=MAX(players.id_mismatch, excluded.id_mismatch)`,
		steam64, guid, dz, name, t, t, mismatch)
	if err != nil {
		s.Log.Error("player upsert", "err", err)
		return
	}
	if name != "" {
		_, _ = s.DB.ExecContext(ctx, `INSERT INTO player_names(steam64,name,first_seen,last_seen) VALUES(?,?,?,?)
			ON CONFLICT(steam64,name) DO UPDATE SET last_seen=MAX(player_names.last_seen, excluded.last_seen)`, steam64, name, t, t)
	}
}

type connectData struct {
	Name   string `json:"name"`
	BEGUID string `json:"be_guid"`
	UID    string `json:"uid"` // PlayerIdentity.GetId()
}

func (s *Service) onConnect(ctx context.Context, ev bridge.Event) {
	var d connectData
	_ = json.Unmarshal(ev.Data, &d)
	steam := ev.Actor
	if !validSteam64(steam) {
		return
	}
	reported := d.BEGUID
	if reported == "" {
		reported = d.UID
	}
	s.upsert(ctx, steam, d.Name, reported, ev.Time)
	// Bridge sessions are authoritative: presence (MetricZ) sessions that began
	// after this connect are dropped, older ones end as presence_end (no playtime
	// is added for them, so nothing is double-counted).
	t := ts(ev.Time)
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM player_sessions WHERE server_id=? AND steam64=? AND end_kind='open' AND source='metricz' AND started_at >= ?`, ev.ServerID, steam, t)
	_, _ = s.DB.ExecContext(ctx, `UPDATE player_sessions SET ended_at=last_seen, end_kind='presence_end' WHERE server_id=? AND steam64=? AND end_kind='open' AND source='metricz'`, ev.ServerID, steam)
	// A still-open bridge session means we missed the disconnect: close it as inferred.
	s.closeOpen(ctx, ev.ServerID, steam, "inferred_end", nil)
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO player_sessions(server_id,steam64,started_at,last_seen,source) VALUES(?,?,?,?, 'bridge')`, ev.ServerID, steam, ts(ev.Time), ts(ev.Time))
}

func (s *Service) onDisconnect(ctx context.Context, ev bridge.Event) {
	if !validSteam64(ev.Actor) {
		return
	}
	s.upsert(ctx, ev.Actor, "", "", ev.Time)
	t := ev.Time
	s.closeOpen(ctx, ev.ServerID, ev.Actor, "disconnect", &t)
}

// closeOpen ends open sessions for a player (or all players when steam64 == "").
// With at == nil the session ends at its last_seen time.
func (s *Service) closeOpen(ctx context.Context, serverID, steam64, kind string, at *time.Time) {
	q := `SELECT id, started_at, last_seen, steam64 FROM player_sessions WHERE server_id=? AND end_kind='open'`
	args := []any{serverID}
	if steam64 != "" {
		q += ` AND steam64=?`
		args = append(args, steam64)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return
	}
	type open struct {
		id                   int64
		start, last, steam64 string
	}
	var opens []open
	for rows.Next() {
		var o open
		if rows.Scan(&o.id, &o.start, &o.last, &o.steam64) == nil {
			opens = append(opens, o)
		}
	}
	rows.Close()
	for _, o := range opens {
		start, _ := time.Parse(time.RFC3339Nano, o.start)
		end, _ := time.Parse(time.RFC3339Nano, o.last)
		if at != nil {
			end = *at
		}
		if end.Before(start) {
			end = start
		}
		_, _ = s.DB.ExecContext(ctx, `UPDATE player_sessions SET ended_at=?, end_kind=?, last_seen=? WHERE id=?`, ts(end), kind, ts(end), o.id)
		_, _ = s.DB.ExecContext(ctx, `UPDATE players SET playtime_s = playtime_s + ? WHERE steam64=?`, int64(end.Sub(start).Seconds()), o.steam64)
	}
}

type deathData struct {
	Killer    string   `json:"killer"`
	Weapon    string   `json:"weapon"`
	DistanceM *float64 `json:"distance_m"`
	Cause     string   `json:"cause"`
	KillerPos *struct {
		X float64 `json:"x"`
		Z float64 `json:"z"`
	} `json:"killer_pos"`
	Name string `json:"name"`
}

func (s *Service) onDeath(ctx context.Context, ev bridge.Event) {
	if !validSteam64(ev.Actor) {
		return
	}
	var d deathData
	_ = json.Unmarshal(ev.Data, &d)
	killer := ev.Target
	if killer == "" {
		killer = d.Killer
	}
	if !validSteam64(killer) || killer == ev.Actor {
		killer = "" // environment, AI, zombie or suicide: never invent an attacker
	}
	var vx, vz, kx, kz any
	if ev.Pos != nil {
		vx, vz = ev.Pos.X, ev.Pos.Z
	}
	if d.KillerPos != nil && killer != "" {
		kx, kz = d.KillerPos.X, d.KillerPos.Z
	}
	var dist any
	if d.DistanceM != nil && killer != "" && *d.DistanceM >= 0 {
		dist = *d.DistanceM
	}
	res, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO combat(server_id,event_id,ts,victim,killer,weapon,distance_m,cause,vx,vz,kx,kz) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		ev.ServerID, ev.ID, ts(ev.Time), ev.Actor, killer, trunc(d.Weapon, 64), dist, trunc(d.Cause, 64), vx, vz, kx, kz)
	if err != nil {
		s.Log.Error("combat insert", "err", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return
	}
	s.upsert(ctx, ev.Actor, d.Name, "", ev.Time)
	_, _ = s.DB.ExecContext(ctx, `UPDATE players SET deaths=deaths+1 WHERE steam64=?`, ev.Actor)
	if killer != "" {
		s.upsert(ctx, killer, "", "", ev.Time)
		_, _ = s.DB.ExecContext(ctx, `UPDATE players SET kills=kills+1 WHERE steam64=?`, killer)
	}
}

func (s *Service) onServerStart(ctx context.Context, ev bridge.Event) {
	s.closeOpen(ctx, ev.ServerID, "", "inferred_end", nil)
}

// Presence consumes MetricZ snapshots: updates last_seen, and when the bridge
// is not connected, maintains presence-based sessions.
func (s *Service) Presence(serverID string, ps []metricz.Player, at time.Time) {
	ctx := context.Background()
	bridgeOK := s.Caps.OK(serverID, caps.Bridge)
	present := map[string]bool{}
	for _, p := range ps {
		if !validSteam64(p.Steam64) {
			continue
		}
		present[p.Steam64] = true
		s.mu.Lock()
		last := s.lastSeen[p.Steam64]
		write := at.Sub(last) >= time.Minute
		if write {
			s.lastSeen[p.Steam64] = at
		}
		s.mu.Unlock()
		if !write {
			continue
		}
		s.upsert(ctx, p.Steam64, p.Name, p.BEGUID, at)
		_, _ = s.DB.ExecContext(ctx, `UPDATE player_sessions SET last_seen=? WHERE server_id=? AND steam64=? AND end_kind='open'`, ts(at), serverID, p.Steam64)
		if !bridgeOK {
			var n int
			_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM player_sessions WHERE server_id=? AND steam64=? AND end_kind='open'`, serverID, p.Steam64).Scan(&n)
			if n == 0 {
				_, _ = s.DB.ExecContext(ctx, `INSERT INTO player_sessions(server_id,steam64,started_at,last_seen,source) VALUES(?,?,?,?, 'metricz')`, serverID, p.Steam64, ts(at), ts(at))
			}
		}
	}
	// Presence sessions of players no longer present end at their last sighting.
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT steam64 FROM player_sessions WHERE server_id=? AND end_kind='open' AND source='metricz'`, serverID)
	if err != nil {
		return
	}
	var gone []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && !present[id] {
			gone = append(gone, id)
		}
	}
	rows.Close()
	for _, id := range gone {
		s.closeOpen(ctx, serverID, id, "presence_end", nil)
		s.mu.Lock()
		delete(s.lastSeen, id)
		s.mu.Unlock()
	}
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Player is the list/profile view.
type Player struct {
	Steam64    string `json:"steam64"`
	BEGUID     string `json:"be_guid"`
	DayzID     string `json:"dayz_id"`
	Name       string `json:"name"`
	FirstSeen  string `json:"first_seen"`
	LastSeen   string `json:"last_seen"`
	PlaytimeS  int64  `json:"playtime_seconds"`
	Kills      int64  `json:"kills"`
	Deaths     int64  `json:"deaths"`
	IDMismatch bool   `json:"id_mismatch"`
}

const playerCols = `steam64,be_guid,dayz_id,name,first_seen,last_seen,playtime_s,kills,deaths,id_mismatch`

func scanPlayer(r interface{ Scan(...any) error }) (Player, error) {
	var p Player
	var mm int
	err := r.Scan(&p.Steam64, &p.BEGUID, &p.DayzID, &p.Name, &p.FirstSeen, &p.LastSeen, &p.PlaytimeS, &p.Kills, &p.Deaths, &mm)
	p.IDMismatch = mm == 1
	return p, err
}

// Find resolves a player by Steam64, BE GUID or DayZ ID.
func (s *Service) Find(ctx context.Context, id string) (*Player, error) {
	p, err := scanPlayer(s.DB.QueryRowContext(ctx, `SELECT `+playerCols+` FROM players WHERE steam64=? OR be_guid=? OR dayz_id=? LIMIT 1`, id, strings.ToLower(id), id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return &p, err
}

// Register adds API routes.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/players", s.list)
	mux.HandleFunc("GET /api/players/{id}", s.profile)
}

func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := httpx.QueryInt(r, "limit", 100)
	if limit < 1 || limit > 500 {
		limit = 100
	}
	var rows *sql.Rows
	var err error
	if q == "" {
		rows, err = s.DB.QueryContext(r.Context(), `SELECT `+playerCols+` FROM players ORDER BY last_seen DESC LIMIT ?`, limit)
	} else {
		like := "%" + strings.NewReplacer("%", "", "_", "").Replace(q) + "%"
		rows, err = s.DB.QueryContext(r.Context(), `SELECT `+playerCols+` FROM players WHERE steam64=? OR be_guid=? OR dayz_id=? OR name LIKE ?
			OR steam64 IN (SELECT steam64 FROM player_names WHERE name LIKE ?) ORDER BY last_seen DESC LIMIT ?`, q, strings.ToLower(q), q, like, like, limit)
	}
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	defer rows.Close()
	out := []Player{}
	for rows.Next() {
		p, err := scanPlayer(rows)
		if err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		out = append(out, p)
	}
	httpx.JSON(w, 200, out)
}

// Session row.
type Session struct {
	ServerID  string  `json:"server_id"`
	StartedAt string  `json:"started_at"`
	EndedAt   *string `json:"ended_at"`
	EndKind   string  `json:"end_kind"`
	Source    string  `json:"source"`
	DurationS int64   `json:"duration_seconds"`
}

// Combat row.
type Combat struct {
	TS        string   `json:"ts"`
	Victim    string   `json:"victim"`
	Killer    string   `json:"killer,omitempty"`
	Weapon    string   `json:"weapon,omitempty"`
	DistanceM *float64 `json:"distance_m,omitempty"`
	Cause     string   `json:"cause,omitempty"`
	Role      string   `json:"role"` // kill | death
	// Where the victim fell, so an admin can go there long after the marker ended.
	X *float64 `json:"x,omitempty"`
	Z *float64 `json:"z,omitempty"`
}

// Profile is the full player page.
type Profile struct {
	Player   Player        `json:"player"`
	Names    []NameRow     `json:"names"`
	Sessions []Session     `json:"sessions"`
	Combat   []Combat      `json:"combat"`
	Audit    []audit.Entry `json:"audit"`
}

// NameRow is one observed alias.
type NameRow struct {
	Name      string `json:"name"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
}

// GetProfile builds a profile.
func (s *Service) GetProfile(ctx context.Context, id string) (*Profile, error) {
	p, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	pr := &Profile{Player: *p, Names: []NameRow{}, Sessions: []Session{}, Combat: []Combat{}, Audit: []audit.Entry{}}
	if rows, err := s.DB.QueryContext(ctx, `SELECT name,first_seen,last_seen FROM player_names WHERE steam64=? ORDER BY last_seen DESC`, p.Steam64); err == nil {
		for rows.Next() {
			var n NameRow
			if rows.Scan(&n.Name, &n.FirstSeen, &n.LastSeen) == nil {
				pr.Names = append(pr.Names, n)
			}
		}
		rows.Close()
	}
	if rows, err := s.DB.QueryContext(ctx, `SELECT server_id,started_at,ended_at,last_seen,end_kind,source FROM player_sessions WHERE steam64=? ORDER BY started_at DESC LIMIT 100`, p.Steam64); err == nil {
		for rows.Next() {
			var se Session
			var ended sql.NullString
			var last string
			if rows.Scan(&se.ServerID, &se.StartedAt, &ended, &last, &se.EndKind, &se.Source) == nil {
				st, _ := time.Parse(time.RFC3339Nano, se.StartedAt)
				end, _ := time.Parse(time.RFC3339Nano, last)
				if ended.Valid {
					se.EndedAt = &ended.String
					end, _ = time.Parse(time.RFC3339Nano, ended.String)
				} else {
					end = time.Now() // open session: duration so far
				}
				se.DurationS = int64(end.Sub(st).Seconds())
				pr.Sessions = append(pr.Sessions, se)
			}
		}
		rows.Close()
	}
	if rows, err := s.DB.QueryContext(ctx, `SELECT ts,victim,killer,weapon,distance_m,cause,vx,vz FROM combat WHERE victim=? OR killer=? ORDER BY ts DESC LIMIT 200`, p.Steam64, p.Steam64); err == nil {
		for rows.Next() {
			var c Combat
			var d, vx, vz sql.NullFloat64
			if rows.Scan(&c.TS, &c.Victim, &c.Killer, &c.Weapon, &d, &c.Cause, &vx, &vz) == nil {
				if d.Valid {
					c.DistanceM = &d.Float64
				}
				if vx.Valid && vz.Valid {
					c.X, c.Z = &vx.Float64, &vz.Float64
				}
				c.Role = "death"
				if c.Killer == p.Steam64 {
					c.Role = "kill"
				}
				pr.Combat = append(pr.Combat, c)
			}
		}
		rows.Close()
	}
	if rows, err := s.DB.QueryContext(ctx, `SELECT id,ts,actor_sub,actor_name,action,target,params,result,detail FROM audit_log WHERE target IN (?,?,?) ORDER BY id DESC LIMIT 100`, p.Steam64, p.BEGUID, p.DayzID); err == nil {
		for rows.Next() {
			var e audit.Entry
			var params string
			if rows.Scan(&e.ID, &e.TS, &e.ActorSub, &e.ActorName, &e.Action, &e.Target, &params, &e.Result, &e.Detail) == nil {
				e.Params = json.RawMessage(params)
				pr.Audit = append(pr.Audit, e)
			}
		}
		rows.Close()
	}
	return pr, nil
}

func (s *Service) profile(w http.ResponseWriter, r *http.Request) {
	pr, err := s.GetProfile(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, 404, "player not found")
		return
	}
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	httpx.JSON(w, 200, pr)
}
