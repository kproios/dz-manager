package restarts

import (
	"github.com/kproios/dz-manager/internal/httpx"
	"net/http"
	"time"
)

// apiNow requests a native graceful shutdown; the verified provider relaunches.
// It never invents success: the existing observer records down/up separately.
func (s *Service) apiNow(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirm string `json:"confirm"`
	}
	if err := httpx.Decode(r, 1024, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	if in.Confirm != "RESTART" {
		httpx.Error(w, 400, "type RESTART to confirm")
		return
	}
	p, _, err := s.Profiles.Active(r.Context())
	if err != nil {
		httpx.Error(w, 409, "no active server profile")
		return
	}
	if !s.driveVerified(r.Context(), p.ID) {
		httpx.Error(w, 409, "provider auto-restart has not been verified for this server")
		return
	}
	s.mu.Lock()
	online, known := s.online[p.ID]
	s.mu.Unlock()
	if !known || !online {
		httpx.Error(w, 409, "server must be online before requesting a restart")
		return
	}
	now := s.Now().UTC()
	claim, err := s.DB.ExecContext(r.Context(), `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value WHERE CAST(value AS INTEGER) < ?`, "restart_now_until:"+p.ID, now.Add(3*time.Minute).Unix(), now.Unix())
	if err != nil {
		httpx.Error(w, 500, "could not record restart request")
		return
	}
	n, _ := claim.RowsAffected()
	if n == 0 {
		httpx.Error(w, 409, "a restart was already requested; wait for the server to return")
		return
	}
	err = s.Exec(r.Context(), "#shutdown")
	if err != nil {
		_, _ = s.DB.ExecContext(r.Context(), `DELETE FROM settings WHERE key=?`, "restart_now_until:"+p.ID)
		_ = s.Audit.Write(r.Context(), actor(r), "restart.now", p.ID, nil, "error", err.Error())
		httpx.Error(w, 502, "restart command failed; check server status before retrying")
		return
	}
	_ = s.Audit.Write(r.Context(), actor(r), "restart.now", p.ID, nil, "ok", "Graceful shutdown requested; provider relaunch and readiness are observed separately")
	s.publish()
	httpx.JSON(w, 202, map[string]any{"requested": true, "message": "Restart requested. Waiting for the provider to bring the server back."})
}
