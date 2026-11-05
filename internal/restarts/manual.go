package restarts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/hooks"
	"github.com/kproios/dz-manager/internal/httpx"
)

// ManualDelays are the only countdowns a manual restart accepts (minutes).
var ManualDelays = []int{0, 1, 2, 5, 10, 15, 30}

// Manual is a pending, owner-requested restart with a countdown.
type Manual struct {
	At        time.Time `json:"at"`
	Requested time.Time `json:"requested"`
	Delay     int       `json:"delay_minutes"`
	By        string    `json:"by"`
	BySub     string    `json:"by_sub,omitempty"`
}

type apiErr struct {
	code int
	msg  string
}

func (e apiErr) Error() string { return e.msg }

func manualKey(id string) string { return "restart_manual:" + id }

// PendingManual returns the countdown restart waiting for serverID, if any.
func (s *Service) PendingManual(ctx context.Context, serverID string) (Manual, bool) {
	var raw string
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, manualKey(serverID)).Scan(&raw); err != nil {
		return Manual{}, false
	}
	var m Manual
	if json.Unmarshal([]byte(raw), &m) != nil || m.At.IsZero() {
		return Manual{}, false
	}
	return m, true
}

func (s *Service) clearManual(ctx context.Context, serverID string) {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, manualKey(serverID))
}

// checkRestartable verifies a restart may be requested for serverID.
func (s *Service) checkRestartable(ctx context.Context, serverID string) error {
	if !s.driveVerified(ctx, serverID) {
		return apiErr{409, "provider auto-restart has not been verified for this server"}
	}
	s.mu.Lock()
	online, known := s.online[serverID]
	s.mu.Unlock()
	if !known || !online {
		return apiErr{409, "server must be online before requesting a restart"}
	}
	return nil
}

// shutdownNow requests a native graceful shutdown; the verified provider relaunches.
// It never invents success: the observer records down/up separately.
func (s *Service) shutdownNow(ctx context.Context, a audit.Actor, serverID string, params map[string]any) error {
	if err := s.checkRestartable(ctx, serverID); err != nil {
		return err
	}
	now := s.Now().UTC()
	claim, err := s.DB.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value WHERE CAST(value AS INTEGER) < ?`, "restart_now_until:"+serverID, now.Add(3*time.Minute).Unix(), now.Unix())
	if err != nil {
		return apiErr{500, "could not record restart request"}
	}
	if n, _ := claim.RowsAffected(); n == 0 {
		return apiErr{409, "a restart was already requested; wait for the server to return"}
	}
	if err := s.Exec(ctx, "#shutdown"); err != nil {
		_, _ = s.DB.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, "restart_now_until:"+serverID)
		_ = s.Audit.Write(ctx, a, "restart.now", serverID, params, "error", err.Error())
		return apiErr{502, "restart command failed; check server status before retrying"}
	}
	_ = s.Audit.Write(ctx, a, "restart.now", serverID, params, "ok", "Graceful shutdown requested; provider relaunch and readiness are observed separately")
	s.publish()
	return nil
}

func writeErr(w http.ResponseWriter, err error) {
	if e, ok := err.(apiErr); ok {
		httpx.Error(w, e.code, e.msg)
		return
	}
	httpx.Error(w, 500, err.Error())
}

// apiNow restarts immediately (delay 0) or starts a countdown restart that
// announces, optionally locks and kicks with the schedule's settings, then shuts down.
func (s *Service) apiNow(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirm string `json:"confirm"`
		Delay   int    `json:"delay_minutes"`
	}
	if err := httpx.Decode(r, 1024, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	if in.Confirm != "RESTART" {
		httpx.Error(w, 400, "type RESTART to confirm")
		return
	}
	allowed := false
	for _, d := range ManualDelays {
		allowed = allowed || d == in.Delay
	}
	if !allowed {
		httpx.Error(w, 400, fmt.Sprintf("delay_minutes must be one of %v", ManualDelays))
		return
	}
	p, _, err := s.Profiles.Active(r.Context())
	if err != nil {
		httpx.Error(w, 409, "no active server profile")
		return
	}
	if _, pending := s.PendingManual(r.Context(), p.ID); pending {
		httpx.Error(w, 409, "a countdown restart is already pending; cancel it first")
		return
	}
	a := actor(r)
	if in.Delay == 0 {
		if err := s.shutdownNow(r.Context(), a, p.ID, nil); err != nil {
			writeErr(w, err)
			return
		}
		httpx.JSON(w, 202, map[string]any{"requested": true, "message": "Restart requested. Waiting for the provider to bring the server back."})
		return
	}
	if err := s.checkRestartable(r.Context(), p.ID); err != nil {
		writeErr(w, err)
		return
	}
	now := s.Now().UTC().Truncate(time.Second)
	m := Manual{At: now.Add(time.Duration(in.Delay) * time.Minute), Requested: now, Delay: in.Delay, By: a.Name, BySub: a.Sub}
	b, _ := json.Marshal(m)
	if _, err := s.DB.ExecContext(r.Context(), `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, manualKey(p.ID), string(b)); err != nil {
		httpx.Error(w, 500, "could not record restart request")
		return
	}
	_ = s.Audit.Write(r.Context(), a, "restart.countdown", p.ID, map[string]any{"delay_minutes": in.Delay, "planned_for": m.At}, "ok", "")
	s.manualTick(r.Context(), s.Load(r.Context(), p.ID), s.Now()) // first announcement right away
	s.publish()
	httpx.JSON(w, 202, map[string]any{"requested": true, "manual": m, "message": fmt.Sprintf("Restart in %d minute(s). Players are being warned.", in.Delay)})
}

// apiCancel cancels a pending countdown restart and unlocks if it locked.
func (s *Service) apiCancel(w http.ResponseWriter, r *http.Request) {
	p, _, err := s.Profiles.Active(r.Context())
	if err != nil {
		httpx.Error(w, 409, "no active server profile")
		return
	}
	m, ok := s.PendingManual(r.Context(), p.ID)
	if !ok {
		httpx.Error(w, 409, "no countdown restart is pending")
		return
	}
	s.clearManual(r.Context(), p.ID)
	s.unlockIfLocked(r.Context(), p.ID, m.At, "Countdown restart cancelled; server unlocked again")
	_ = s.Exec(r.Context(), "say -1 The planned restart was cancelled. Carry on!")
	_ = s.Audit.Write(r.Context(), actor(r), "restart.cancel", p.ID, map[string]any{"planned_for": m.At}, "ok", "")
	s.publish()
	httpx.JSON(w, 200, map[string]bool{"cancelled": true})
}

// manualTick drives a pending countdown restart: announcements, lock, kick, shutdown.
func (s *Service) manualTick(ctx context.Context, sc Schedule, now time.Time) {
	m, ok := s.PendingManual(ctx, sc.ServerID)
	if !ok {
		return
	}
	if now.After(m.At.Add(2 * time.Minute)) {
		// Stale (DZ Manager was down at the planned moment): never restart late.
		s.clearManual(ctx, sc.ServerID)
		s.unlockIfLocked(ctx, sc.ServerID, m.At, "Countdown restart expired; server unlocked again")
		_ = s.Audit.Write(ctx, audit.System, "restart.countdown_expired", sc.ServerID, map[string]any{"planned_for": m.At}, "error", "DZ Manager missed the planned moment; no restart sent")
		s.publish()
		return
	}
	// The first warning names the full countdown; later ones follow the schedule's list.
	mins := []int{m.Delay}
	for _, x := range sc.AnnounceMinutes {
		if x < m.Delay {
			mins = append(mins, x)
		}
	}
	for _, x := range mins {
		at := m.At.Add(-time.Duration(x) * time.Minute)
		if !due(at, m.Requested, now) || !s.once(fmt.Sprintf("mann:%s:%d:%d", sc.ServerID, m.At.Unix(), x)) {
			continue
		}
		err := s.Exec(ctx, "say -1 "+sc.Announcement(x, m.At))
		s.hook(hooks.Restart, fmt.Sprintf("Server restart in %d minute(s)", x), "Restart requested by an admin.")
		res, detail := "ok", ""
		if err != nil {
			res, detail = "error", err.Error()
		}
		_ = s.Audit.Write(ctx, audit.System, "restart.announce", sc.ServerID, map[string]any{"minutes": x, "planned_for": m.At.UTC()}, res, detail)
	}
	s.prepare(ctx, sc, m.At, now, m.Requested)
	if now.Before(m.At) {
		return
	}
	s.clearManual(ctx, sc.ServerID)
	a := audit.Actor{Sub: m.BySub, Name: m.By}
	if a.Name == "" {
		a = audit.System
	}
	if err := s.shutdownNow(ctx, a, sc.ServerID, map[string]any{"delay_minutes": m.Delay, "planned_for": m.At}); err != nil {
		s.unlockIfLocked(ctx, sc.ServerID, m.At, "Countdown restart failed; server unlocked again")
		s.Alert.Send("countdown restart failed: " + err.Error())
		s.publish()
	}
}
