package restarts

import (
	"context"
	"net/http"
	"time"

	"github.com/kproios/dz-manager/internal/hooks"
	"github.com/kproios/dz-manager/internal/httpx"
)

// Skipping one planned restart: DZ Manager sends no warnings, lock, kick or
// shutdown for that occurrence and does not report it as missed. In provider
// mode the provider's own schedule still restarts the server.

func skipKey(id string) string { return "restart_skip:" + id }

func (s *Service) skipped(ctx context.Context, serverID string, at time.Time) bool {
	var v string
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, skipKey(serverID)).Scan(&v); err != nil {
		return false
	}
	t, err := time.Parse(time.RFC3339, v)
	return err == nil && t.Equal(at.UTC().Truncate(time.Second))
}

// SkippedAt returns the occurrence that will be skipped, if any.
func (s *Service) SkippedAt(ctx context.Context, serverID string) (time.Time, bool) {
	var v string
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, skipKey(serverID)).Scan(&v); err != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, v)
	return t, err == nil
}

func (s *Service) clearSkip(ctx context.Context, serverID string) {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, skipKey(serverID))
}

// apiSkip skips (skip=true) or restores (skip=false) the next planned restart.
func (s *Service) apiSkip(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Skip bool `json:"skip"`
	}
	if err := httpx.Decode(r, 1024, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	p, _, err := s.Profiles.Active(r.Context())
	if err != nil {
		httpx.Error(w, 409, "no active server profile")
		return
	}
	sc := s.Load(r.Context(), p.ID)
	next, ok := sc.Next(s.Now())
	if !sc.Enabled || !ok {
		httpx.Error(w, 409, "no planned restart to skip")
		return
	}
	if !in.Skip {
		s.clearSkip(r.Context(), p.ID)
		_ = s.Audit.Write(r.Context(), actor(r), "restart.unskip", p.ID, map[string]any{"planned_for": next.UTC()}, "ok", "")
		s.publish()
		httpx.JSON(w, 200, map[string]any{"skipped": false})
		return
	}
	v := next.UTC().Truncate(time.Second).Format(time.RFC3339)
	if _, err := s.DB.ExecContext(r.Context(), `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, skipKey(p.ID), v); err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	_ = s.Audit.Write(r.Context(), actor(r), "restart.skip", p.ID, map[string]any{"planned_for": next.UTC()}, "ok", "")
	s.hook(hooks.Restart, "The "+localClock(sc, next)+" restart is skipped", "The server keeps running until the following planned restart.")
	s.publish()
	httpx.JSON(w, 200, map[string]any{"skipped": true, "planned_for": next.UTC()})
}
