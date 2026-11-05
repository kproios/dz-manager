package restarts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kproios/dz-manager/internal/alert"
	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/caps"
	"github.com/kproios/dz-manager/internal/hooks"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/profiles"
	"github.com/kproios/dz-manager/internal/store"
)

// Event is one observed restart (or a missed planned one).
type Event struct {
	ID         int64      `json:"id"`
	ServerID   string     `json:"server_id"`
	DownAt     *time.Time `json:"down_at,omitempty"`
	UpAt       *time.Time `json:"up_at,omitempty"`
	PlannedFor *time.Time `json:"planned_for,omitempty"`
	Kind       string     `json:"kind"`    // planned | unplanned | missed
	Outcome    string     `json:"outcome"` // down | ok | not_back | recovered_late | missed
	DurationS  *int64     `json:"duration_seconds,omitempty"`
}

// Service runs the observer and scheduler.
type Service struct {
	DB       *store.DB
	Profiles *profiles.Store
	Caps     *caps.Registry
	Audit    *audit.Log
	Alert    *alert.Sender
	Log      *slog.Logger
	TZ       string
	// Exec sends an RCON command to the active server.
	Exec func(ctx context.Context, cmd string) error
	// Publish notifies the UI.
	Publish func()
	Now     func() time.Time
	// KickAll kicks every online player (optional; kick steps are skipped without it).
	KickAll func(ctx context.Context, reason string) (int, error)
	// OnLock records a lock state change applied by the scheduler (optional).
	OnLock func(locked bool)
	// Hooks receives public restart events (Discord, bot API); optional.
	Hooks *hooks.Bus

	mu        sync.Mutex
	online    map[string]bool
	offStreak map[string]int
	sent      map[string]bool
}

// Init prepares internal state.
func (s *Service) Init() {
	if s.Now == nil {
		s.Now = time.Now
	}
	s.online, s.offStreak, s.sent = map[string]bool{}, map[string]int{}, map[string]bool{}
}

func key(serverID string) string { return "restart_schedule:" + serverID }

// Load returns the stored schedule (defaults when none).
func (s *Service) Load(ctx context.Context, serverID string) Schedule {
	var raw string
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key(serverID)).Scan(&raw); err == nil {
		var sc Schedule
		if json.Unmarshal([]byte(raw), &sc) == nil {
			sc.ServerID = serverID
			return sc
		}
	}
	return Defaults(serverID, s.TZ)
}

// Save validates and stores a schedule.
func (s *Service) Save(ctx context.Context, sc Schedule) error {
	if err := sc.Validate(); err != nil {
		return err
	}
	b, _ := json.Marshal(sc)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key(sc.ServerID), string(b))
	s.refreshCaps(ctx, sc)
	return err
}

func (s *Service) driveVerified(ctx context.Context, serverID string) bool {
	var v string
	_ = s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, "restart_drive_verified:"+serverID).Scan(&v)
	return v == "true"
}

func (s *Service) refreshCaps(ctx context.Context, sc Schedule) {
	if !sc.Enabled {
		s.Caps.Set(sc.ServerID, caps.ProviderSchedule, caps.Unconfigured, "no restart schedule mirrored")
	} else {
		var n int
		_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM restart_events WHERE server_id=? AND kind='planned' AND outcome IN ('ok','recovered_late') AND down_at > ?`,
			sc.ServerID, s.Now().UTC().Add(-7*24*time.Hour).Format(time.RFC3339)).Scan(&n)
		if n > 0 {
			s.Caps.Set(sc.ServerID, caps.ProviderSchedule, caps.OK, fmt.Sprintf("%d planned restart(s) observed in the last 7 days", n))
		} else {
			s.Caps.Set(sc.ServerID, caps.ProviderSchedule, caps.Unverified, "schedule mirrored; no planned restart observed yet")
		}
	}
	if s.driveVerified(ctx, sc.ServerID) {
		s.Caps.Set(sc.ServerID, caps.RestartDrive, caps.OK, "owner verified the provider restarts a cleanly exited server")
	} else {
		s.Caps.Set(sc.ServerID, caps.RestartDrive, caps.Unsupported, "not verified: provider must auto-restart after a clean #shutdown")
	}
}

// Observe feeds server reachability (called on every status poll).
func (s *Service) Observe(ctx context.Context, serverID string, online bool) {
	if serverID == "" {
		return
	}
	now := s.Now().UTC()
	s.mu.Lock()
	was, known := s.online[serverID]
	if online {
		s.offStreak[serverID] = 0
	} else {
		s.offStreak[serverID]++
	}
	streak := s.offStreak[serverID]
	transitionDown := known && was && !online && streak >= 2
	transitionUp := known && !was && online
	if online || streak >= 2 || !known {
		s.online[serverID] = online
	}
	s.mu.Unlock()

	if transitionDown {
		sc := s.Load(ctx, serverID)
		kind := "unplanned"
		var planned any
		if sc.Enabled {
			if p, ok := sc.Nearest(now); ok {
				kind, planned = "planned", p.UTC().Format(time.RFC3339)
			}
		}
		var manualUntil int64
		_ = s.DB.QueryRowContext(ctx, `SELECT CAST(value AS INTEGER) FROM settings WHERE key=?`, "restart_now_until:"+serverID).Scan(&manualUntil)
		if manualUntil > now.Unix() {
			kind = "manual"
			planned = nil
		}
		_, err := s.DB.ExecContext(ctx, `INSERT INTO restart_events(server_id,down_at,planned_for,kind,outcome) VALUES(?,?,?,?, 'down')`, serverID, now.Format(time.RFC3339), planned, kind)
		if err != nil {
			s.Log.Error("restart event insert", "err", err)
		}
		if kind == "unplanned" {
			s.Alert.Send("server went down outside the restart schedule (crash or manual stop?)")
			s.hook(hooks.Server, "Server went down unexpectedly", "It should be back in a few minutes.")
		} else {
			s.hook(hooks.Restart, "Server is restarting", "Back in a few minutes.")
		}
		s.publish()
	}
	if transitionUp {
		var id int64
		var downAt, outcome string
		err := s.DB.QueryRowContext(ctx, `SELECT id, down_at, outcome FROM restart_events WHERE server_id=? AND up_at IS NULL AND down_at IS NOT NULL ORDER BY id DESC LIMIT 1`, serverID).Scan(&id, &downAt, &outcome)
		if err == nil {
			d, _ := time.Parse(time.RFC3339, downAt)
			res := "ok"
			if outcome == "not_back" {
				res = "recovered_late"
				s.Alert.Send(fmt.Sprintf("server is back after %s", now.Sub(d).Round(time.Second)))
			}
			_, _ = s.DB.ExecContext(ctx, `UPDATE restart_events SET up_at=?, duration_s=?, outcome=? WHERE id=?`, now.Format(time.RFC3339), int64(now.Sub(d).Seconds()), res, id)
			s.refreshCaps(ctx, s.Load(ctx, serverID))
			s.publish()
		}
		// A freshly started DayZ server accepts joins again.
		s.setLocked(false)
	}
}

func (s *Service) hook(kind, title, text string) {
	if s.Hooks != nil {
		s.Hooks.Publish(hooks.Event{Kind: kind, Title: title, Text: text})
	}
}

func (s *Service) setLocked(v bool) {
	if s.OnLock != nil {
		s.OnLock(v)
	}
}

func (s *Service) publish() {
	if s.Publish != nil {
		s.Publish()
	}
}

// Run is the scheduler loop.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick performs announcements, drive actions, missed and not-back detection.
func (s *Service) Tick(ctx context.Context) {
	p, _, err := s.Profiles.Active(ctx)
	if err != nil {
		return
	}
	sc := s.Load(ctx, p.ID)
	s.refreshCaps(ctx, sc)
	now := s.Now()
	if sc.Enabled {
		if next, ok := sc.Next(now.Add(-30 * time.Second)); ok && !s.skipped(ctx, sc.ServerID, next) {
			s.announce(ctx, sc, next, now)
			s.prepare(ctx, sc, next, now, time.Time{})
			s.drive(ctx, sc, next, now)
		}
		s.detectMissed(ctx, sc, now)
	}
	s.manualTick(ctx, sc, now)
	s.detectNotBack(ctx, sc, now)
	s.gcSent(now)
}

// due reports whether a step planned for at should run now (never catches up).
// A step planned before floor (a delayed manual restart requested late) runs at floor.
func due(at, floor, now time.Time) bool {
	if !floor.IsZero() && at.Before(floor) {
		at = floor
	}
	return !now.Before(at) && now.Sub(at) < slot
}

// prepare locks the server and kicks everyone before a restart at next, when
// configured. In drive mode with dry-run on it only records what it would do.
func (s *Service) prepare(ctx context.Context, sc Schedule, next, now, floor time.Time) {
	dry := sc.Mode == ModeDrive && sc.DryRun && floor.IsZero()
	planned := map[string]any{"planned_for": next.UTC()}
	if sc.LockMinutes > 0 && due(next.Add(-time.Duration(sc.LockMinutes)*time.Minute), floor, now) &&
		s.once(fmt.Sprintf("lock:%s:%d", sc.ServerID, next.Unix())) {
		if dry {
			_ = s.Audit.Write(ctx, audit.System, "restart.drive_dryrun", sc.ServerID, map[string]any{"cmd": "#lock", "planned_for": next.UTC()}, "ok", "dry-run")
		} else {
			err := s.Exec(ctx, "#lock")
			res, detail := "ok", "New joins are closed until the restart"
			if err != nil {
				res, detail = "error", err.Error()
			} else {
				s.setLocked(true)
				_ = s.Exec(ctx, "say -1 Server locked for the restart at "+localClock(sc, next)+". Please finish up and log out safely.")
			}
			_ = s.Audit.Write(ctx, audit.System, "restart.lock", sc.ServerID, planned, res, detail)
		}
	}
	if sc.KickMinutes > 0 && due(next.Add(-time.Duration(sc.KickMinutes)*time.Minute), floor, now) &&
		s.once(fmt.Sprintf("kick:%s:%d", sc.ServerID, next.Unix())) {
		if dry {
			_ = s.Audit.Write(ctx, audit.System, "restart.drive_dryrun", sc.ServerID, map[string]any{"cmd": "kick all", "planned_for": next.UTC()}, "ok", "dry-run")
			return
		}
		if s.KickAll == nil {
			return
		}
		n, err := s.KickAll(ctx, sc.KickMessage)
		res, detail := "ok", fmt.Sprintf("%d player(s) kicked", n)
		if err != nil {
			res, detail = "error", err.Error()
		}
		_ = s.Audit.Write(ctx, audit.System, "restart.kick_all", sc.ServerID, map[string]any{"planned_for": next.UTC(), "reason": sc.KickMessage, "kicked": n}, res, detail)
	}
}

func localClock(sc Schedule, t time.Time) string {
	if loc, err := time.LoadLocation(sc.Timezone); err == nil {
		t = t.In(loc)
	}
	return t.Format("15:04")
}

// unlockIfLocked releases a lock DZ Manager applied for a restart that did not happen.
func (s *Service) unlockIfLocked(ctx context.Context, serverID string, at time.Time, why string) {
	s.mu.Lock()
	locked := s.sent[fmt.Sprintf("lock:%s:%d", serverID, at.Unix())] || s.sent[fmt.Sprintf("drive:%s:%d:#lock", serverID, at.Unix())]
	s.mu.Unlock()
	if !locked {
		return
	}
	err := s.Exec(ctx, "#unlock")
	res, detail := "ok", why
	if err != nil {
		res, detail = "error", err.Error()
	} else {
		s.setLocked(false)
	}
	_ = s.Audit.Write(ctx, audit.System, "restart.unlock", serverID, map[string]any{"planned_for": at.UTC()}, res, detail)
}

const slot = 25 * time.Second

func (s *Service) once(k string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sent[k] {
		return false
	}
	s.sent[k] = true
	return true
}

func (s *Service) gcSent(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sent) > 500 {
		s.sent = map[string]bool{}
	}
}

func (s *Service) announce(ctx context.Context, sc Schedule, next, now time.Time) {
	if !sc.Announce {
		return
	}
	for _, m := range sc.AnnounceMinutes {
		at := next.Add(-time.Duration(m) * time.Minute)
		if now.Before(at) || now.Sub(at) >= slot {
			continue // never catch up on missed announcements
		}
		k := fmt.Sprintf("ann:%s:%d:%d", sc.ServerID, next.Unix(), m)
		if !s.once(k) {
			continue
		}
		msg := sc.Announcement(m, next)
		err := s.Exec(ctx, "say -1 "+msg)
		s.hook(hooks.Restart, fmt.Sprintf("Server restart in %d minute(s)", m), "Planned for "+localClock(sc, next)+".")
		res, detail := "ok", ""
		if err != nil {
			res, detail = "error", err.Error()
		}
		_ = s.Audit.Write(ctx, audit.System, "restart.announce", sc.ServerID, map[string]any{"minutes": m, "planned_for": next.UTC()}, res, detail)
	}
}

func (s *Service) drive(ctx context.Context, sc Schedule, next, now time.Time) {
	if sc.Mode != ModeDrive {
		return
	}
	type step struct {
		at  time.Time
		cmd string
	}
	steps := []step{{next, "#shutdown"}}
	if sc.LockMinutes == 0 { // prepare() owns the lock when a lock time is configured
		steps = []step{{next.Add(-time.Minute), "#lock"}, {next, "#shutdown"}}
	}
	for _, st := range steps {
		if now.Before(st.at) || now.Sub(st.at) >= slot {
			continue
		}
		if !s.once(fmt.Sprintf("drive:%s:%d:%s", sc.ServerID, next.Unix(), st.cmd)) {
			continue
		}
		if sc.DryRun || !s.Caps.OK(sc.ServerID, caps.RestartDrive) {
			s.Log.Info("restart drive (dry-run or not verified): would send", "cmd", st.cmd, "planned_for", next)
			_ = s.Audit.Write(ctx, audit.System, "restart.drive_dryrun", sc.ServerID, map[string]any{"cmd": st.cmd, "planned_for": next.UTC()}, "ok", "dry-run")
			continue
		}
		err := s.Exec(ctx, st.cmd)
		res, detail := "ok", ""
		if err != nil {
			res, detail = "error", err.Error()
			s.Alert.Send("restart command " + st.cmd + " failed: " + err.Error())
		} else if st.cmd == "#lock" {
			s.setLocked(true)
		}
		_ = s.Audit.Write(ctx, audit.System, "restart.drive", sc.ServerID, map[string]any{"cmd": st.cmd, "planned_for": next.UTC()}, res, detail)
	}
}

func (s *Service) detectMissed(ctx context.Context, sc Schedule, now time.Time) {
	w := time.Duration(sc.WindowMinutes) * time.Minute
	// Planned instants whose detection window closed within the last ~2 ticks.
	for _, o := range sc.Occurrences(now.Add(-w-30*time.Second), now.Add(-w)) {
		if !s.once(fmt.Sprintf("missed:%s:%d", sc.ServerID, o.Unix())) {
			continue
		}
		if s.skipped(ctx, sc.ServerID, o) {
			s.clearSkip(ctx, sc.ServerID) // the skipped restart has passed
			continue
		}
		var n int
		_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM restart_events WHERE server_id=? AND down_at BETWEEN ? AND ?`, sc.ServerID,
			o.Add(-w).UTC().Format(time.RFC3339), o.Add(w).UTC().Format(time.RFC3339)).Scan(&n)
		if n == 0 {
			_, _ = s.DB.ExecContext(ctx, `INSERT INTO restart_events(server_id,planned_for,kind,outcome) VALUES(?,?, 'missed','missed')`, sc.ServerID, o.UTC().Format(time.RFC3339))
			s.Alert.Send("planned restart at " + o.Format("15:04 MST") + " did not happen (provider schedule out of sync?)")
			// Never leave players locked out after a restart that did not happen.
			s.unlockIfLocked(ctx, sc.ServerID, o, "Restart did not happen; server unlocked again")
			s.publish()
		}
	}
}

func (s *Service) detectNotBack(ctx context.Context, sc Schedule, now time.Time) {
	var id int64
	var downAt string
	err := s.DB.QueryRowContext(ctx, `SELECT id, down_at FROM restart_events WHERE server_id=? AND up_at IS NULL AND outcome='down' ORDER BY id DESC LIMIT 1`, sc.ServerID).Scan(&id, &downAt)
	if err != nil {
		return
	}
	d, _ := time.Parse(time.RFC3339, downAt)
	if now.Sub(d) > time.Duration(sc.TimeoutMinutes)*time.Minute {
		_, _ = s.DB.ExecContext(ctx, `UPDATE restart_events SET outcome='not_back' WHERE id=?`, id)
		s.Alert.Send(fmt.Sprintf("server has not come back %d minutes after going down", sc.TimeoutMinutes))
		s.publish()
	}
}

// Events lists recent events.
func (s *Service) Events(ctx context.Context, serverID string, limit int) ([]Event, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,server_id,down_at,up_at,planned_for,kind,outcome,duration_s FROM restart_events WHERE server_id=? ORDER BY COALESCE(down_at, planned_for) DESC, id DESC LIMIT ?`, serverID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var down, up, planned sql.NullString
		var dur sql.NullInt64
		if err := rows.Scan(&e.ID, &e.ServerID, &down, &up, &planned, &e.Kind, &e.Outcome, &dur); err != nil {
			return nil, err
		}
		e.DownAt, e.UpAt, e.PlannedFor = ts(down), ts(up), ts(planned)
		if dur.Valid {
			e.DurationS = &dur.Int64
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func ts(n sql.NullString) *time.Time {
	if !n.Valid {
		return nil
	}
	t, err := time.Parse(time.RFC3339, n.String)
	if err != nil {
		return nil
	}
	return &t
}

// Register adds the API routes.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/restarts", s.apiGet)
	mux.HandleFunc("PUT /api/restarts/schedule", s.apiPut)
	mux.HandleFunc("POST /api/restarts/drive-verification", s.apiDriveVerify)
	mux.HandleFunc("POST /api/restarts/now", s.apiNow)
	mux.HandleFunc("POST /api/restarts/cancel", s.apiCancel)
	mux.HandleFunc("POST /api/restarts/skip", s.apiSkip)
}

func actor(r *http.Request) audit.Actor {
	if u := auth.FromContext(r.Context()); u != nil {
		return u.Actor()
	}
	return audit.System
}

func (s *Service) apiGet(w http.ResponseWriter, r *http.Request) {
	p, _, err := s.Profiles.Active(r.Context())
	if err != nil {
		httpx.Error(w, 409, "no active server profile")
		return
	}
	sc := s.Load(r.Context(), p.ID)
	evs, err := s.Events(r.Context(), p.ID, 50)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	out := map[string]any{"schedule": sc, "events": evs, "now": s.Now().UTC(),
		"provider_schedule": s.Caps.Get(p.ID, caps.ProviderSchedule), "restart_drive": s.Caps.Get(p.ID, caps.RestartDrive)}
	if next, ok := sc.Next(s.Now()); ok && sc.Enabled {
		out["next"] = next.UTC()
	}
	if m, ok := s.PendingManual(r.Context(), p.ID); ok {
		out["manual"] = m
	}
	if t, ok := s.SkippedAt(r.Context(), p.ID); ok {
		out["skipped"] = t
	}
	out["manual_delays"] = ManualDelays
	s.mu.Lock()
	if on, ok := s.online[p.ID]; ok {
		out["online"] = on
	}
	s.mu.Unlock()
	httpx.JSON(w, 200, out)
}

func (s *Service) apiPut(w http.ResponseWriter, r *http.Request) {
	p, _, err := s.Profiles.Active(r.Context())
	if err != nil {
		httpx.Error(w, 409, "no active server profile")
		return
	}
	var sc Schedule
	if err := httpx.Decode(r, 8<<10, &sc); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	sc.ServerID = p.ID
	if err := s.Save(r.Context(), sc); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	_ = s.Audit.Write(r.Context(), actor(r), "restart.schedule_update", p.ID, sc, "ok", "")
	s.publish()
	httpx.JSON(w, 200, s.Load(r.Context(), p.ID))
}

func (s *Service) apiDriveVerify(w http.ResponseWriter, r *http.Request) {
	p, _, err := s.Profiles.Active(r.Context())
	if err != nil {
		httpx.Error(w, 409, "no active server profile")
		return
	}
	var in struct {
		Verified bool   `json:"verified"`
		Evidence string `json:"evidence"`
	}
	if err := httpx.Decode(r, 4<<10, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	if in.Verified && strings.TrimSpace(in.Evidence) == "" {
		httpx.Error(w, 400, errors.New("describe the live test that proved the provider restarts after #shutdown").Error())
		return
	}
	v := "false"
	if in.Verified {
		v = "true"
	}
	_, _ = s.DB.ExecContext(r.Context(), `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "restart_drive_verified:"+p.ID, v)
	_ = s.Audit.Write(r.Context(), actor(r), "restart.drive_verification", p.ID, in, "ok", "")
	s.refreshCaps(r.Context(), s.Load(r.Context(), p.ID))
	httpx.JSON(w, 200, map[string]bool{"verified": in.Verified})
}
