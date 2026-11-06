package server

import (
	"encoding/json"
	"net/http"

	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/httpx"
)

// UI preferences (map layers, hidden icon kinds, table sizes) are stored per
// account identity in the settings table, so they follow the owner across
// browsers, sign-outs and PCs. They are cosmetic: never secrets, never audited.
const maxPrefs = 64 << 10 // 100 vehicle rules alone can exceed 8 KiB, and then nothing saved

func prefsKey(u *auth.User) string { return "ui_prefs:" + u.Sub }

func (s *Server) getPrefs(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.Error(w, 401, "not signed in")
		return
	}
	var raw string
	if err := s.DB.QueryRowContext(r.Context(), `SELECT value FROM settings WHERE key=?`, prefsKey(u)).Scan(&raw); err != nil || !json.Valid([]byte(raw)) {
		raw = "{}"
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(raw))
}

func (s *Server) putPrefs(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.Error(w, 401, "not signed in")
		return
	}
	var in map[string]json.RawMessage
	if err := httpx.Decode(r, maxPrefs, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	if in == nil {
		httpx.Error(w, 400, "preferences must be a JSON object")
		return
	}
	b, _ := json.Marshal(in)
	if _, err := s.DB.ExecContext(r.Context(), `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, prefsKey(u), string(b)); err != nil {
		httpx.Error(w, 500, "could not save preferences")
		return
	}
	// A rule added on the phone must change the map already open on the PC.
	s.Hub.Publish("prefs", map[string]bool{"changed": true})
	httpx.JSON(w, 200, in)
}
