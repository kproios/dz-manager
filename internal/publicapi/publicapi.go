// Package publicapi is the read-only API for a Discord bot (or any other
// public integration): server status, who is online, kill feed, leaderboards,
// player stats by name, the ban list, world-event counts, the owner's server
// info text and a pollable feed of every hook event. It needs a bearer token
// created in Settings. It never exposes Steam IDs, IPs or exact positions of
// world events.
package publicapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/hooks"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/secret"
	"github.com/kproios/dz-manager/internal/store"
)

const settingsKey = "bot_api"

// Settings are stored as JSON (token hashed).
type Settings struct {
	TokenHash string `json:"token_hash,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	Info      string `json:"info"`
}

// API serves /public/v1 and the admin settings.
type API struct {
	DB    *store.DB
	Box   *secret.Box
	Audit *audit.Log
	Bus   *hooks.Bus
	// Status returns the public server status (name, players, next restart…).
	Status func(ctx context.Context) map[string]any
	// Online returns the names of players online now.
	Online func(ctx context.Context) []string
}

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

func actor(r *http.Request) audit.Actor {
	if u := auth.FromContext(r.Context()); u != nil {
		return u.Actor()
	}
	return audit.System
}

// RegisterAdmin adds the settings endpoints (session-authenticated mux).
func (a *API) RegisterAdmin(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/bot", func(w http.ResponseWriter, r *http.Request) {
		st := a.Load(r.Context())
		httpx.JSON(w, 200, map[string]any{"enabled": st.TokenHash != "", "created_at": st.CreatedAt, "info": st.Info, "base_path": "/public/v1"})
	})
	mux.HandleFunc("POST /api/settings/bot/token", func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		token := "dzm_" + hex.EncodeToString(b)
		st := a.Load(r.Context())
		st.TokenHash, st.CreatedAt = a.Box.Hash(token), time.Now().UTC().Format(time.RFC3339)
		if err := a.save(r.Context(), st); err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		_ = a.Audit.Write(r.Context(), actor(r), "settings.bot_token", "", nil, "ok", "new bot token; the previous one stops working")
		httpx.JSON(w, 200, map[string]string{"token": token})
	})
	mux.HandleFunc("POST /api/settings/bot/revoke", func(w http.ResponseWriter, r *http.Request) {
		st := a.Load(r.Context())
		st.TokenHash, st.CreatedAt = "", ""
		if err := a.save(r.Context(), st); err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		_ = a.Audit.Write(r.Context(), actor(r), "settings.bot_revoke", "", nil, "ok", "")
		httpx.JSON(w, 200, map[string]bool{"enabled": false})
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
		_ = a.Audit.Write(r.Context(), actor(r), "settings.bot_info", "", map[string]any{"characters": len(in.Info)}, "ok", "")
		httpx.JSON(w, 200, map[string]bool{"ok": true})
	})
}

// authorized checks the bearer token.
func (a *API) authorized(r *http.Request) bool {
	st := a.Load(r.Context())
	if st.TokenHash == "" {
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a.Box.Hash(token)), []byte(st.TokenHash)) == 1
}

func (a *API) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.authorized(r) {
			httpx.Error(w, 401, "missing or invalid bot token")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		h(w, r)
	}
}

func limit(r *http.Request, def, max int) int {
	n := httpx.QueryInt(r, "limit", def)
	if n < 1 || n > max {
		return def
	}
	return n
}

// RegisterPublic adds /public/v1 (token-authenticated mux).
func (a *API) RegisterPublic(mux *http.ServeMux) {
	mux.HandleFunc("GET /public/v1/status", a.guard(func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, a.Status(r.Context()))
	}))
	mux.HandleFunc("GET /public/v1/online", a.guard(func(w http.ResponseWriter, r *http.Request) {
		names := a.Online(r.Context())
		httpx.JSON(w, 200, map[string]any{"count": len(names), "players": names})
	}))
	mux.HandleFunc("GET /public/v1/info", a.guard(func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, map[string]string{"info": a.Load(r.Context()).Info})
	}))
	mux.HandleFunc("GET /public/v1/feed", a.guard(func(w http.ResponseWriter, r *http.Request) {
		after := int64(httpx.QueryInt(r, "after", 0))
		httpx.JSON(w, 200, a.Bus.After(after, limit(r, 100, 500)))
	}))
	mux.HandleFunc("GET /public/v1/killfeed", a.guard(a.killfeed))
	mux.HandleFunc("GET /public/v1/leaderboard", a.guard(a.leaderboard))
	mux.HandleFunc("GET /public/v1/player", a.guard(a.player))
	mux.HandleFunc("GET /public/v1/bans", a.guard(a.bans))
	mux.HandleFunc("GET /public/v1/events", a.guard(a.events))
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
	rows, err := a.DB.QueryContext(r.Context(), `SELECT COALESCE(name,''), reason, created_at, expires_at FROM bans WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?) ORDER BY created_at DESC LIMIT ?`, now, limit(r, 50, 200))
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
