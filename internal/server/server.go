// Package server assembles the HTTP surface: health, auth, the authenticated
// API, the game ingest router and the embedded UI.
package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/caps"
	"github.com/kproios/dz-manager/internal/config"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/profiles"
	"github.com/kproios/dz-manager/internal/store"
	"github.com/kproios/dz-manager/internal/web"
)

// Server is the HTTP application.
type Server struct {
	Cfg      *config.Config
	DB       *store.DB
	Auth     *auth.Auth
	Audit    *audit.Log
	Caps     *caps.Registry
	Hub      *Hub
	Profiles *profiles.Store
	Log      *slog.Logger
	Version  string

	// API is the authenticated mux; features register "METHOD /api/..." patterns.
	API *http.ServeMux
	// Ingest is the unauthenticated-by-session mux for game traffic; each handler authenticates the server itself.
	Ingest *http.ServeMux

	ready   atomic.Bool
	started time.Time
	limiter *rateLimiter
}

// New builds a Server with core routes registered.
func New(s *Server) *Server {
	s.API = http.NewServeMux()
	s.Ingest = http.NewServeMux()
	s.started = time.Now()
	s.limiter = newRateLimiter(50, 100) // per server/IP: 50 req/s sustained, burst 100
	s.API.HandleFunc("GET /api/me", s.me)
	s.API.HandleFunc("GET /api/status", s.status)
	s.API.HandleFunc("GET /api/audit", s.auditList)
	s.API.HandleFunc("GET /api/audit/search", func(w http.ResponseWriter, r *http.Request) {
		page, err := s.Audit.Search(r.Context(), httpx.QueryInt(r, "limit", 25), int64(httpx.QueryInt(r, "before", 0)), r.URL.Query().Get("q"), r.URL.Query().Get("result"))
		if err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		httpx.JSON(w, 200, page)
	})
	s.API.HandleFunc("GET /api/capabilities", s.capabilities)
	s.API.HandleFunc("GET /api/prefs", s.getPrefs)
	s.API.HandleFunc("PUT /api/prefs", s.putPrefs)
	s.API.Handle("GET /api/stream", s.Hub)
	s.API.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { httpx.Error(w, 404, "not found") })
	s.Ingest.HandleFunc("POST /ingest/ping", s.ingestPing)
	s.Ingest.HandleFunc("/ingest/", func(w http.ResponseWriter, r *http.Request) { httpx.Error(w, 404, "not found") })
	return s
}

// SetReady flips /ready.
func (s *Server) SetReady(v bool) { s.ready.Store(v) }

// Handler returns the root handler.
func (s *Server) Handler() http.Handler {
	root := http.NewServeMux()
	root.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, map[string]string{"status": "ok"})
	})
	root.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		if !s.ready.Load() {
			httpx.JSON(w, 503, map[string]string{"status": "starting", "version": s.Version})
			return
		}
		if err := s.DB.PingContext(r.Context()); err != nil {
			httpx.JSON(w, 503, map[string]string{"status": "db_unavailable", "version": s.Version})
			return
		}
		httpx.JSON(w, 200, map[string]string{"status": "ready", "version": s.Version})
	})
	root.HandleFunc("GET /auth/login", s.Auth.Login)
	root.HandleFunc("GET /auth/callback", s.Auth.Callback)
	root.Handle("POST /auth/logout", s.Auth.OptionalUser(http.HandlerFunc(s.Auth.Logout)))
	root.Handle("/api/", s.Auth.RequireAPI(s.API))
	root.Handle("/ingest/", s.limiter.wrap(http.MaxBytesHandler(s.Ingest, 4<<20)))

	static := http.FileServerFS(web.FS())
	root.Handle("GET /assets/", static)
	root.Handle("GET /tiles/", s.Auth.RequireAPI(http.StripPrefix("/tiles/", http.FileServer(http.Dir(filepath.Join(s.Cfg.DataDir, "tiles"))))))
	root.Handle("/", s.Auth.RequirePage(http.HandlerFunc(s.spa)))
	return s.securityHeaders(s.logRequests(root))
}

func (s *Server) spa(w http.ResponseWriter, r *http.Request) {
	fsys := web.FS()
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p != "" && p != "index.html" {
		if st, err := fs.Stat(fsys, p); err == nil && !st.IsDir() {
			http.ServeFileFS(w, r, fsys, p)
			return
		}
	}
	if !web.Built() {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>DZ Manager</title><p>DZ Manager ` + s.Version + ` is running, but this build has no UI embedded. Use a CI release build.</p>`))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFileFS(w, r, fsys, "index.html")
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self' "+originOf(s.Cfg.OIDCIssuer))
		if s.Cfg.SecureCookies() {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func originOf(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		rest := u[i+3:]
		if j := strings.Index(rest, "/"); j >= 0 {
			rest = rest[:j]
		}
		return u[:i+3] + rest
	}
	return ""
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(c int) { r.code = c; r.ResponseWriter.WriteHeader(c) }
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, code: 200}
		next.ServeHTTP(rec, r)
		if r.URL.Path == "/health" || r.URL.Path == "/ready" {
			return
		}
		lvl := slog.LevelDebug
		if rec.code >= 500 {
			lvl = slog.LevelError
		} else if rec.code >= 400 {
			lvl = slog.LevelInfo
		}
		loggedPath := r.URL.Path
		if rest, ok := strings.CutPrefix(loggedPath, "/ingest/v1/"); ok {
			// DayZ's bridge credentials occupy this segment, including on errors.
			_, route, hasRoute := strings.Cut(rest, "/")
			loggedPath = "/ingest/v1/[redacted]"
			if hasRoute {
				loggedPath += "/" + route
			}
		}
		s.Log.Log(r.Context(), lvl, "http", "method", r.Method, "path", loggedPath, "status", rec.code, "dur_ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, 200, auth.FromContext(r.Context()))
}

func (s *Server) auditList(w http.ResponseWriter, r *http.Request) {
	es, err := s.Audit.List(r.Context(), httpx.QueryInt(r, "limit", 100), int64(httpx.QueryInt(r, "before", 0)))
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	httpx.JSON(w, 200, es)
}

func (s *Server) capabilities(w http.ResponseWriter, r *http.Request) {
	p, _, err := s.Profiles.Active(r.Context())
	if err != nil {
		httpx.JSON(w, 200, map[string]any{"server_id": nil, "capabilities": s.Caps.List("")})
		return
	}
	httpx.JSON(w, 200, map[string]any{"server_id": p.ID, "capabilities": s.Caps.List(p.ID)})
}

// UpdaterStatus mirrors docs/contracts/host-and-release.md §5.
type UpdaterStatus struct {
	CheckedAt       string   `json:"checked_at"`
	Installed       string   `json:"installed"`
	LatestSeen      string   `json:"latest_seen"`
	LastResult      string   `json:"last_result"`
	LastError       string   `json:"last_error"`
	BlockedVersions []string `json:"blocked_versions"`
	TokenExpiresAt  string   `json:"token_expires_at"`
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := map[string]any{
		"version":        s.Version,
		"started_at":     s.started.UTC().Format(time.RFC3339),
		"server_time":    time.Now().In(s.Cfg.Location).Format(time.RFC3339),
		"timezone":       s.Cfg.Location.String(),
		"schema_version": func() int { v, _ := s.DB.SchemaVersion(ctx); return v }(),
		"ui_built":       web.Built(),
		"sse_clients":    s.Hub.Clients(),
	}
	if st, err := os.Stat(s.DB.Path); err == nil {
		size := st.Size()
		if wal, err := os.Stat(s.DB.Path + "-wal"); err == nil {
			size += wal.Size()
		}
		out["db_bytes"] = size
	}
	var warnings []string
	if b, err := os.ReadFile(filepath.Join(s.Cfg.DataDir, "updater", "status.json")); err == nil {
		var us UpdaterStatus
		if json.Unmarshal(b, &us) == nil {
			out["updater"] = us
			if t, err := time.Parse(time.RFC3339, us.TokenExpiresAt); err == nil && time.Until(t) < 30*24*time.Hour {
				warnings = append(warnings, "GitHub updater token expires "+t.Format("2006-01-02")+": rotate it (runbook)")
			}
			if us.LastResult == "rolled_back" {
				warnings = append(warnings, "Last update was rolled back: "+us.LastError)
			}
		}
	} else {
		out["updater"] = nil
		warnings = append(warnings, "No updater status found (host updater not installed or never ran)")
	}
	if p, _, err := s.Profiles.Active(ctx); err == nil {
		out["active_server"] = map[string]any{"id": p.ID, "name": p.Name, "provider": p.ProviderKind}
	} else {
		out["active_server"] = nil
		warnings = append(warnings, "No game server configured yet (Settings → Servers)")
	}
	out["warnings"] = warnings
	httpx.JSON(w, 200, out)
}

// BridgeToken extracts a server token from Authorization: Bearer, the "token"
// query parameter, or the {token} path segment (DayZ RestApi cannot set
// arbitrary headers, so the URL path is the primary carrier for the mod).
func BridgeToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if t := r.PathValue("token"); t != "" {
		return t
	}
	return r.URL.Query().Get("token")
}

func (s *Server) ingestPing(w http.ResponseWriter, r *http.Request) {
	id, ok := s.Profiles.AuthBridge(r.Context(), BridgeToken(r))
	if !ok {
		httpx.Error(w, 401, "invalid server token")
		return
	}
	httpx.JSON(w, 200, map[string]string{"status": "ok", "server_id": id, "version": s.Version})
}

// Background runs fn until ctx ends, restarting it after a panic (logged).
func (s *Server) Background(ctx context.Context, name string, fn func(context.Context)) {
	go func() {
		for ctx.Err() == nil {
			func() {
				defer func() {
					if v := recover(); v != nil {
						s.Log.Error("background task panicked; restarting", "task", name, "panic", v)
					}
				}()
				fn(ctx)
			}()
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
		}
	}()
}
