// Package publicapi is the API for websites, Discord bots and other servers
// (/public/v1). Every route needs an API key (Settings → Integrations) holding
// the route's scope:
//
//   - public: server status, names online, kill feed, leaderboards, player
//     stats by name, ban list (names), world event counts, server info, and a
//     pollable feed of every public hook event. Never Steam IDs, IPs or
//     positions; raid events never say where.
//   - players:read, bans:read, priority:read, map:read: exports with Steam IDs
//     (ban lists to share between servers, priority queues) and live positions.
//   - actions:write: player and vehicle actions and messages, audited under
//     the key's name.
//   - server:restart: restart the game server (confirm RESTART), now or after
//     a countdown, through the same checks and audit as the panel.
//
// GET routes send CORS headers so a website can call them from the browser;
// only ever put a key with just the public scope in browser code.
package publicapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/kproios/dz-manager/internal/actions"
	"github.com/kproios/dz-manager/internal/apikeys"
	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/hooks"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/perm"
	"github.com/kproios/dz-manager/internal/store"
)

const settingsKey = "bot_api"

// Settings are stored as JSON.
type Settings struct {
	Info string `json:"info"`
}

// API serves /public/v1 and the admin settings.
type API struct {
	DB    *store.DB
	Audit *audit.Log
	Bus   *hooks.Bus
	Keys  *apikeys.Store
	// Status returns the public server status (name, players, next restart…).
	Status func(ctx context.Context) map[string]any
	// Online returns the names of players online now.
	Online func(ctx context.Context) []string
	// Map returns the live map (world, players, vehicles, active events).
	Map func(ctx context.Context) any
	// Actions runs player and vehicle actions.
	Actions *actions.API
	// Notify sends an on-screen message to everyone (steam64 "") or one player.
	Notify func(ctx context.Context, a audit.Actor, steam64, text string) error
	// Restart serves a restart request for the key's actor.
	Restart func(w http.ResponseWriter, r *http.Request, a audit.Actor)
	// Version is the dzm version, shown in the OpenAPI document.
	Version string
}

var reSteam = regexp.MustCompile(`^[0-9]{17}$`)

// Load returns the settings.
func (a *API) Load(ctx context.Context) Settings {
	var st Settings
	var raw string
	if err := a.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, settingsKey).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &st)
	}
	return st
}

func (a *API) save(ctx context.Context, st Settings) error {
	b, _ := json.Marshal(st)
	_, err := a.DB.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, settingsKey, string(b))
	return err
}

func sessionActor(r *http.Request) audit.Actor {
	if u := auth.FromContext(r.Context()); u != nil {
		return u.Actor()
	}
	return audit.System
}

// RegisterAdmin adds the settings endpoints (session-authenticated mux).
func (a *API) RegisterAdmin(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/bot", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, map[string]any{"info": a.Load(r.Context()).Info, "base_path": "/public/v1"})
	})
	mux.HandleFunc("PUT /api/settings/bot/info", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Info string `json:"info"`
		}
		if err := httpx.Decode(r, 64<<10, &in); err != nil {
			httpx.Error(w, 400, err.Error())
			return
		}
		if len(in.Info) > 20000 {
			httpx.Error(w, 400, "keep the server info under 20,000 characters")
			return
		}
		st := a.Load(r.Context())
		st.Info = in.Info
		if err := a.save(r.Context(), st); err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		_ = a.Audit.Write(r.Context(), sessionActor(r), "settings.bot_info", "", map[string]any{"characters": len(in.Info)}, "ok", "")
		httpx.JSON(w, 200, map[string]bool{"ok": true})
	})
}

func limit(r *http.Request, def, max int) int {
	n := httpx.QueryInt(r, "limit", def)
	if n < 1 || n > max {
		return def
	}
	return n
}

// RegisterPublic adds /public/v1 (CORS is added by the server).
func (a *API) RegisterPublic(mux *http.ServeMux) {
	k := a.Keys
	mux.HandleFunc("GET /public/v1/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=300")
		httpx.JSON(w, 200, OpenAPI(a.Version))
	})
	// public
	mux.HandleFunc("GET /public/v1/status", k.Require("public", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, a.Status(r.Context()))
	}))
	mux.HandleFunc("GET /public/v1/online", k.Require("public", func(w http.ResponseWriter, r *http.Request) {
		names := a.Online(r.Context())
		httpx.JSON(w, 200, map[string]any{"count": len(names), "players": names})
	}))
	mux.HandleFunc("GET /public/v1/info", k.Require("public", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, map[string]string{"info": a.Load(r.Context()).Info})
	}))
	mux.HandleFunc("GET /public/v1/feed", k.Require("public", func(w http.ResponseWriter, r *http.Request) {
		after := int64(httpx.QueryInt(r, "after", 0))
		httpx.JSON(w, 200, a.Bus.After(after, limit(r, 100, 500)))
	}))
	mux.HandleFunc("GET /public/v1/killfeed", k.Require("public", a.killfeed))
	mux.HandleFunc("GET /public/v1/leaderboard", k.Require("public", a.leaderboard))
	mux.HandleFunc("GET /public/v1/player", k.Require("public", a.player))
	mux.HandleFunc("GET /public/v1/bans", k.Require("public", a.bans))
	mux.HandleFunc("GET /public/v1/events", k.Require("public", a.events))
	// players:read
	mux.HandleFunc("GET /public/v1/players", k.Require("players:read", a.players))
	mux.HandleFunc("GET /public/v1/players/{steam64}", k.Require("players:read", a.playerByID))
	// bans:read
	mux.HandleFunc("GET /public/v1/banlist", k.Require("bans:read", a.banlist))
	// priority:read
	mux.HandleFunc("GET /public/v1/priority", k.Require("priority:read", a.priority))
	// map:read
	mux.HandleFunc("GET /public/v1/map", k.Require("map:read", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, a.Map(r.Context()))
	}))
	// actions:write
	mux.HandleFunc("POST /public/v1/players/{steam64}/actions/{action}", k.Require("actions:write", func(w http.ResponseWriter, r *http.Request) {
		a.runAction(w, r, "player", r.PathValue("steam64"))
	}))
	mux.HandleFunc("POST /public/v1/vehicles/{id}/actions/{action}", k.Require("actions:write", func(w http.ResponseWriter, r *http.Request) {
		a.runAction(w, r, "vehicle", r.PathValue("id"))
	}))
	mux.HandleFunc("POST /public/v1/message", k.Require("actions:write", a.message))
	// server:restart
	mux.HandleFunc("POST /public/v1/restart", k.Require("server:restart", func(w http.ResponseWriter, r *http.Request) {
		a.Restart(w, r, apikeys.FromContext(r.Context()).Actor())
	}))
}

func (a *API) runAction(w http.ResponseWriter, r *http.Request, target, id string) {
	if target == "player" && !reSteam.MatchString(id) {
		httpx.Error(w, 400, "use the player's Steam64 ID")
		return
	}
	key := apikeys.FromContext(r.Context())
	if slices.Contains(perm.StrongActions, r.PathValue("action")) && !key.Can(perm.ActionsStrong) {
		httpx.Error(w, 403, "the user this API key belongs to may not use strong actions (kill, strip, give items, delete vehicles)")
		return
	}
	var in actions.Request
	if err := httpx.Decode(r, 4<<10, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	o := a.Actions.Run(r.Context(), key.Actor(), target, id, r.PathValue("action"), in)
	httpx.JSON(w, o.Status, o)
}

func (a *API) message(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text    string `json:"text"`
		Steam64 string `json:"steam64"`
	}
	if err := httpx.Decode(r, 4096, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" || len(in.Text) > 200 {
		httpx.Error(w, 400, "text is required (up to 200 characters)")
		return
	}
	if in.Steam64 != "" && !reSteam.MatchString(in.Steam64) {
		httpx.Error(w, 400, "steam64 must be a Steam64 ID (or empty for everyone)")
		return
	}
	if err := a.Notify(r.Context(), apikeys.FromContext(r.Context()).Actor(), in.Steam64, in.Text); err != nil {
		httpx.Error(w, 502, err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]bool{"ok": true})
}

func (a *API) killfeed(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.QueryContext(r.Context(), `SELECT c.ts, COALESCE(k.name,''), COALESCE(v.name,''), c.weapon, COALESCE(c.distance_m,0) FROM combat c
		LEFT JOIN players k ON k.steam64=c.killer LEFT JOIN players v ON v.steam64=c.victim WHERE c.killer<>'' ORDER BY c.ts DESC LIMIT ?`, limit(r, 20, 100))
	if err != nil {
		httpx.Error(w, 500, "query failed")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var ts, killer, victim, weapon string
		var dist float64
		if rows.Scan(&ts, &killer, &victim, &weapon, &dist) == nil {
			out = append(out, map[string]any{"time": ts, "killer": killer, "victim": victim, "weapon": weapon, "distance_m": int(dist)})
		}
	}
	httpx.JSON(w, 200, out)
}

func (a *API) leaderboard(w http.ResponseWriter, r *http.Request) {
	order := "kills DESC, deaths ASC"
	where := "1"
	switch r.URL.Query().Get("by") {
	case "playtime":
		order = "playtime_s DESC"
	case "kd":
		order, where = "CAST(kills AS REAL)/MAX(deaths,1) DESC", "kills >= 5"
	}
	rows, err := a.DB.QueryContext(r.Context(), `SELECT name, kills, deaths, playtime_s FROM players WHERE name<>'' AND `+where+` ORDER BY `+order+` LIMIT ?`, limit(r, 10, 50))
	if err != nil {
		httpx.Error(w, 500, "query failed")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var name string
		var kills, deaths, play int64
		if rows.Scan(&name, &kills, &deaths, &play) == nil {
			out = append(out, map[string]any{"name": name, "kills": kills, "deaths": deaths, "playtime_hours": play / 3600})
		}
	}
	httpx.JSON(w, 200, out)
}

func (a *API) player(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" || len(name) > 64 {
		httpx.Error(w, 400, "name is required")
		return
	}
	var n, first, last string
	var kills, deaths, play int64
	err := a.DB.QueryRowContext(r.Context(), `SELECT name, kills, deaths, playtime_s, first_seen, last_seen FROM players WHERE name = ? COLLATE NOCASE ORDER BY last_seen DESC LIMIT 1`, name).
		Scan(&n, &kills, &deaths, &play, &first, &last)
	if err == sql.ErrNoRows {
		httpx.Error(w, 404, "no player with that name")
		return
	}
	if err != nil {
		httpx.Error(w, 500, "query failed")
		return
	}
	httpx.JSON(w, 200, map[string]any{"name": n, "kills": kills, "deaths": deaths, "playtime_hours": play / 3600, "first_seen": first, "last_seen": last})
}

func (a *API) bans(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := a.DB.QueryContext(r.Context(), `SELECT COALESCE(p.name,''), b.reason, b.created_at, b.expires_at FROM bans b LEFT JOIN players p ON p.steam64=b.steam64 AND b.steam64<>''
		WHERE b.revoked_at IS NULL AND (b.expires_at IS NULL OR b.expires_at > ?) ORDER BY b.created_at DESC LIMIT ?`, now, limit(r, 50, 200))
	if err != nil {
		httpx.Error(w, 500, "query failed")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var name, reason, created string
		var exp sql.NullString
		if rows.Scan(&name, &reason, &created, &exp) == nil {
			until := "permanent"
			if exp.Valid {
				until = exp.String
			}
			out = append(out, map[string]any{"name": name, "reason": reason, "since": created, "until": until})
		}
	}
	httpx.JSON(w, 200, out)
}

func (a *API) events(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.QueryContext(r.Context(), `SELECT category, COUNT(*) FROM map_events WHERE status='active' AND category IN ('heli_crash','plane_crash','airdrop','military_convoy','train') GROUP BY category`)
	if err != nil {
		httpx.Error(w, 500, "query failed")
		return
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var c string
		var n int
		if rows.Scan(&c, &n) == nil {
			out[c] = n
		}
	}
	httpx.JSON(w, 200, map[string]any{"active": out})
}

type playerRow struct {
	Steam64       string `json:"steam64"`
	Name          string `json:"name"`
	Kills         int64  `json:"kills"`
	Deaths        int64  `json:"deaths"`
	PlaytimeHours int64  `json:"playtime_hours"`
	FirstSeen     string `json:"first_seen"`
	LastSeen      string `json:"last_seen"`
	Banned        bool   `json:"banned"`
	Priority      bool   `json:"priority"`
}

const playerSelect = `SELECT p.steam64, p.name, p.kills, p.deaths, p.playtime_s/3600, p.first_seen, p.last_seen,
	EXISTS(SELECT 1 FROM bans b WHERE b.steam64=p.steam64 AND b.revoked_at IS NULL AND (b.expires_at IS NULL OR b.expires_at > ?)),
	EXISTS(SELECT 1 FROM priority q WHERE q.steam64=p.steam64 AND (q.expires_at IS NULL OR q.expires_at > ?))
	FROM players p`

func (a *API) players(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Format(time.RFC3339)
	order := "p.last_seen DESC"
	switch r.URL.Query().Get("sort") {
	case "kills":
		order = "p.kills DESC"
	case "playtime":
		order = "p.playtime_s DESC"
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	where, args := "", []any{now, now}
	if q != "" {
		if len(q) > 64 {
			httpx.Error(w, 400, "q is too long")
			return
		}
		where = ` WHERE p.name LIKE ? ESCAPE '\' OR p.steam64 = ?`
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
		args = append(args, like, q)
	}
	args = append(args, limit(r, 50, 500), httpx.QueryInt(r, "offset", 0))
	rows, err := a.DB.QueryContext(r.Context(), playerSelect+where+` ORDER BY `+order+` LIMIT ? OFFSET ?`, args...)
	if err != nil {
		httpx.Error(w, 500, "query failed")
		return
	}
	defer rows.Close()
	out := []playerRow{}
	for rows.Next() {
		var p playerRow
		if rows.Scan(&p.Steam64, &p.Name, &p.Kills, &p.Deaths, &p.PlaytimeHours, &p.FirstSeen, &p.LastSeen, &p.Banned, &p.Priority) == nil {
			out = append(out, p)
		}
	}
	httpx.JSON(w, 200, out)
}

func (a *API) playerByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("steam64")
	if !reSteam.MatchString(id) {
		httpx.Error(w, 400, "use a Steam64 ID")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var p playerRow
	err := a.DB.QueryRowContext(r.Context(), playerSelect+` WHERE p.steam64=?`, now, now, id).
		Scan(&p.Steam64, &p.Name, &p.Kills, &p.Deaths, &p.PlaytimeHours, &p.FirstSeen, &p.LastSeen, &p.Banned, &p.Priority)
	if err == sql.ErrNoRows {
		httpx.Error(w, 404, "unknown player")
		return
	}
	if err != nil {
		httpx.Error(w, 500, "query failed")
		return
	}
	httpx.JSON(w, 200, p)
}

// banlist exports active bans: format=json (default), txt (one Steam64 per
// line) or battleye (bans.txt lines: GUID minutes reason, -1 = permanent).
func (a *API) banlist(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	rows, err := a.DB.QueryContext(r.Context(), `SELECT b.steam64, b.be_guid, COALESCE(p.name,''), b.reason, b.created_at, b.expires_at FROM bans b
		LEFT JOIN players p ON p.steam64=b.steam64 AND b.steam64<>''
		WHERE b.revoked_at IS NULL AND (b.expires_at IS NULL OR b.expires_at > ?) ORDER BY b.created_at`, now.Format(time.RFC3339))
	if err != nil {
		httpx.Error(w, 500, "query failed")
		return
	}
	defer rows.Close()
	type ban struct {
		Steam64   string  `json:"steam64,omitempty"`
		BEGUID    string  `json:"be_guid"`
		Name      string  `json:"name,omitempty"`
		Reason    string  `json:"reason"`
		CreatedAt string  `json:"created_at"`
		ExpiresAt *string `json:"expires_at"`
	}
	list := []ban{}
	for rows.Next() {
		var b ban
		var exp sql.NullString
		if rows.Scan(&b.Steam64, &b.BEGUID, &b.Name, &b.Reason, &b.CreatedAt, &exp) == nil {
			if exp.Valid {
				b.ExpiresAt = &exp.String
			}
			list = append(list, b)
		}
	}
	switch r.URL.Query().Get("format") {
	case "txt":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		for _, b := range list {
			if b.Steam64 != "" {
				fmt.Fprintln(w, b.Steam64)
			}
		}
	case "battleye":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		for _, b := range list {
			minutes := -1
			if b.ExpiresAt != nil {
				if t, err := time.Parse(time.RFC3339, *b.ExpiresAt); err == nil {
					minutes = int(t.Sub(now).Minutes()) + 1
				}
			}
			reason := strings.Map(func(r rune) rune {
				if r == '\n' || r == '\r' {
					return ' '
				}
				return r
			}, b.Reason)
			fmt.Fprintf(w, "%s %d %s\n", b.BEGUID, minutes, reason)
		}
	default:
		httpx.JSON(w, 200, map[string]any{"generated_at": now.Format(time.RFC3339), "count": len(list), "bans": list})
	}
}

// priority exports queue priority: format=json (default) or txt (Steam64 per line).
func (a *API) priority(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := a.DB.QueryContext(r.Context(), `SELECT q.steam64, COALESCE(p.name,''), q.reason, q.expires_at, q.created_at FROM priority q LEFT JOIN players p ON p.steam64=q.steam64
		WHERE q.expires_at IS NULL OR q.expires_at > ? ORDER BY q.created_at`, now)
	if err != nil {
		httpx.Error(w, 500, "query failed")
		return
	}
	defer rows.Close()
	type entry struct {
		Steam64   string  `json:"steam64"`
		Name      string  `json:"name,omitempty"`
		Reason    string  `json:"reason,omitempty"`
		ExpiresAt *string `json:"expires_at"`
		Since     string  `json:"since"`
	}
	list := []entry{}
	for rows.Next() {
		var e entry
		var exp sql.NullString
		if rows.Scan(&e.Steam64, &e.Name, &e.Reason, &exp, &e.Since) == nil {
			if exp.Valid {
				e.ExpiresAt = &exp.String
			}
			list = append(list, e)
		}
	}
	if r.URL.Query().Get("format") == "txt" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		for _, e := range list {
			fmt.Fprintln(w, e.Steam64)
		}
		return
	}
	httpx.JSON(w, 200, map[string]any{"generated_at": now, "count": len(list), "entries": list})
}
