// Package auth owns DZ Manager's accounts: built-in sign-in (password +
// TOTP), Steam sign-in and an optional OpenID Connect provider, all followed
// by a mandatory second factor; server-side sessions; CSRF/Origin checks;
// role-based permissions on every panel API route; invites and user
// management. Roles live in DZ Manager, never in an identity provider.
package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/config"
	"github.com/kproios/dz-manager/internal/perm"
	"github.com/kproios/dz-manager/internal/secret"
	"github.com/kproios/dz-manager/internal/store"
)

const (
	// CSRFHeader must echo the session's CSRF token on state-changing requests.
	CSRFHeader = "X-DZM-CSRF"

	absoluteTTL = 12 * time.Hour
	idleTTL     = 2 * time.Hour
	pendingTTL  = 15 * time.Minute // between the first factor and 2FA (setting up an app by hand takes a while)
	flowTTL     = 10 * time.Minute
	// StepUpTTL is how long a 2FA confirmation unlocks sensitive routes.
	StepUpTTL = 10 * time.Minute
)

// User is the authenticated principal attached to a request.
type User struct {
	ID          int64             `json:"id"`
	Sub         string            `json:"sub"`
	Username    string            `json:"username"`
	Email       string            `json:"email"`
	CSRF        string            `json:"csrf"`
	Role        perm.Role         `json:"role"`
	Permissions []perm.Permission `json:"permissions"`
	Method      string            `json:"method"`
	// StepUpUntil is when the last 2FA confirmation stops unlocking sensitive routes.
	StepUpUntil time.Time `json:"step_up_until"`

	sessionHash string
	mfaAt       int64
}

// Can reports whether the user holds a permission.
func (u *User) Can(p perm.Permission) bool { return u != nil && perm.Has(u.Role, p) }

// Actor converts a user to an audit actor.
func (u *User) Actor() audit.Actor { return audit.Actor{Sub: u.Sub, Name: u.Username} }

type ctxKey struct{}

// FromContext returns the session user (nil if none).
func FromContext(ctx context.Context) *User {
	u, _ := ctx.Value(ctxKey{}).(*User)
	return u
}

// WithUser attaches a user to a context (used by tests and internal callers).
func WithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// Auth wires sign-in methods, sessions and permissions.
type Auth struct {
	cfg   *config.Config
	db    *store.DB
	box   *secret.Box
	audit *audit.Log
	log   *slog.Logger
	now   func() time.Time

	oidc   *oidcProvider // nil when no OpenID Connect provider is configured
	oidcMu sync.Mutex
	steam  *steamLogin // nil when Steam sign-in is turned off

	ipFails *failWindow

	// HTTPClient is used for OIDC and Steam calls (tests inject one).
	HTTPClient *http.Client
	// SteamEndpoint overrides Steam's OpenID endpoint (tests).
	SteamEndpoint string
}

// New creates an Auth. Provider discovery is lazy so an identity provider
// being down never prevents the service from starting or reporting ready.
func New(cfg *config.Config, db *store.DB, box *secret.Box, al *audit.Log, log *slog.Logger) *Auth {
	a := &Auth{cfg: cfg, db: db, box: box, audit: al, log: log, now: time.Now, ipFails: newFailWindow(20, 15*time.Minute)}
	if cfg.OIDCIssuer != "" {
		a.oidc = &oidcProvider{a: a}
	}
	if cfg.SteamLogin {
		a.steam = &steamLogin{a: a}
	}
	return a
}

// SetClock replaces the clock (tests).
func (a *Auth) SetClock(now func() time.Time) { a.now = now }

func (a *Auth) client() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Methods describes the sign-in methods for the login page.
type Methods struct {
	Password  bool   `json:"password"`
	Steam     bool   `json:"steam"`
	OIDC      bool   `json:"oidc"`
	OIDCLabel string `json:"oidc_label,omitempty"`
}

// Methods returns the enabled sign-in methods.
func (a *Auth) Methods() Methods {
	m := Methods{Password: true, Steam: a.steam != nil, OIDC: a.oidc != nil}
	if m.OIDC {
		m.OIDCLabel = a.cfg.OIDCLabel
	}
	return m
}

// FormActionOrigin is the identity provider origin the logout form may
// redirect to (CSP form-action), or "".
func (a *Auth) FormActionOrigin() string {
	if a.oidc == nil {
		return ""
	}
	return originOf(a.cfg.OIDCIssuer)
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

// ---- cookies ----

func (a *Auth) sessionCookieName() string {
	if a.cfg.SecureCookies() {
		return "__Host-dzm_session"
	}
	return "dzm_session"
}

func (a *Auth) pendingCookieName() string {
	if a.cfg.SecureCookies() {
		return "__Host-dzm_pending"
	}
	return "dzm_pending"
}

const flowCookie = "dzm_flow"

func (a *Auth) setCookie(w http.ResponseWriter, name, value, path string, ttl time.Duration) {
	c := &http.Cookie{Name: name, Value: value, Path: path, HttpOnly: true, Secure: a.cfg.SecureCookies(), SameSite: http.SameSiteLaxMode, MaxAge: int(ttl.Seconds())}
	if value == "" {
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
}

// ---- sessions ----

// errNoSession is returned when no valid session exists.
var errNoSession = errors.New("no session")

const sessionUserCols = `u.id,u.sub,u.username,u.email,u.role,s.csrf,s.method,s.mfa_at,s.last_seen_at,s.expires_at`

// createSession signs the user in. Every sign-in has just passed 2FA, so the
// session starts with a fresh confirmation.
func (a *Auth) createSession(ctx context.Context, w http.ResponseWriter, r *http.Request, uid int64, method string) error {
	token := secret.Token(32)
	now := a.now().Unix()
	ua := r.UserAgent()
	if len(ua) > 200 {
		ua = ua[:200]
	}
	if _, err := a.db.ExecContext(ctx, `INSERT INTO sessions(id_hash,user_id,csrf,id_token,created_at,last_seen_at,expires_at,method,mfa_at,ip,user_agent) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		a.box.Hash(token), uid, secret.Token(24), "", now, now, now+int64(absoluteTTL.Seconds()), method, now, ClientIP(r), ua); err != nil {
		return err
	}
	_, _ = a.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ? OR last_seen_at < ?`, now, now-int64(idleTTL.Seconds()))
	_, _ = a.db.ExecContext(ctx, `UPDATE users SET last_login_at=?, failed_logins=0, locked_until=0 WHERE id=?`, store.Now(), uid)
	a.setCookie(w, a.sessionCookieName(), token, "/", absoluteTTL)
	a.setCookie(w, a.pendingCookieName(), "", "/", 0)
	return nil
}

// Session resolves the request's session, extending idle expiry. Disabled
// users and users whose role is unknown have no session.
func (a *Auth) Session(r *http.Request) (*User, error) {
	c, err := r.Cookie(a.sessionCookieName())
	if err != nil || c.Value == "" {
		return nil, errNoSession
	}
	h := a.box.Hash(c.Value)
	now := a.now().Unix()
	var u User
	var role string
	var lastSeen, expires int64
	err = a.db.QueryRowContext(r.Context(), `SELECT `+sessionUserCols+` FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.id_hash=? AND u.disabled_at IS NULL`, h).
		Scan(&u.ID, &u.Sub, &u.Username, &u.Email, &role, &u.CSRF, &u.Method, &u.mfaAt, &lastSeen, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoSession
	}
	if err != nil {
		return nil, err
	}
	if now > expires || now-lastSeen > int64(idleTTL.Seconds()) || !perm.ValidRole(role) {
		_, _ = a.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE id_hash=?`, h)
		return nil, errNoSession
	}
	if now-lastSeen > 60 {
		_, _ = a.db.ExecContext(r.Context(), `UPDATE sessions SET last_seen_at=? WHERE id_hash=?`, now, h)
	}
	u.Role = perm.Role(role)
	u.Permissions = slices.Clone(perm.Of(u.Role))
	u.sessionHash = h
	u.StepUpUntil = time.Unix(u.mfaAt, 0).Add(StepUpTTL).UTC()
	return &u, nil
}

// Logout ends the session (POST /auth/logout). OIDC sessions continue to the
// provider's end-session endpoint.
func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	method := ""
	if u, err := a.Session(r); err == nil {
		method = u.Method
		_, _ = a.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE id_hash=?`, u.sessionHash)
		_ = a.audit.Write(r.Context(), u.Actor(), "auth.logout", "", nil, "ok", "")
	}
	a.setCookie(w, a.sessionCookieName(), "", "/", 0)
	target := "/"
	if method == "oidc" && a.oidc != nil {
		target = a.oidc.endSessionURL(r.Context())
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (a *Auth) sameOrigin(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		return o == a.cfg.PublicOrigin()
	}
	// No Origin header: accept only if the browser says same-origin.
	return r.Header.Get("Sec-Fetch-Site") == "same-origin"
}

// RequireAPI protects JSON API routes: 401 without session, 403 on CSRF failure.
func (a *Auth) RequireAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.Session(r)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "authentication required"})
			return
		}
		if !safeMethod(r.Method) {
			if !a.sameOrigin(r) || !secret.Equal(r.Header.Get(CSRFHeader), u.CSRF) {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "CSRF check failed"})
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), u)))
	})
}

// Authorize checks the signed-in user's role against the permission of the
// mux route the request matches. Routes without a rule are refused for
// everyone. Sensitive routes also need a 2FA confirmation from the last
// StepUpTTL; the response then carries "step_up": true so the UI can ask.
func (a *Auth) Authorize(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := FromContext(r.Context())
		_, pattern := mux.Handler(r)
		p, stepUp, ok := perm.Resolve(pattern, r)
		if u == nil || !ok {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "this route has no permission rule"})
			return
		}
		if !u.Can(p) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "your role (" + string(u.Role) + ") does not allow this", "permission": p})
			return
		}
		if stepUp && a.now().Unix()-u.mfaAt > int64(StepUpTTL.Seconds()) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "confirm with your 2FA code to continue", "step_up": true})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// OptionalUser attaches the user if a session exists, without requiring one.
func (a *Auth) OptionalUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, err := a.Session(r); err == nil {
			r = r.WithContext(WithUser(r.Context(), u))
		}
		next.ServeHTTP(w, r)
	})
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

func safeReturn(s string) string {
	if s == "" || !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/\\") || strings.HasPrefix(s, "/auth/") {
		return "/"
	}
	return s
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ClientIP is the visitor's address: the Cloudflare or proxy header when the
// direct peer is a local proxy (loopback or private network), else the peer.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		if v := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); net.ParseIP(v) != nil {
			return v
		}
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			first := strings.TrimSpace(strings.Split(v, ",")[0])
			if net.ParseIP(first) != nil {
				return first
			}
		}
	}
	return host
}

// ---- throttling ----

// failWindow counts failed sign-ins per IP in a sliding window.
type failWindow struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func newFailWindow(max int, window time.Duration) *failWindow {
	return &failWindow{max: max, window: window, hits: map[string][]time.Time{}}
}

func (f *failWindow) blocked(key string, now time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prune(key, now)
	return len(f.hits[key]) >= f.max
}

func (f *failWindow) add(key string, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.hits) > 10000 {
		f.hits = map[string][]time.Time{}
	}
	f.prune(key, now)
	f.hits[key] = append(f.hits[key], now)
}

func (f *failWindow) prune(key string, now time.Time) {
	h := f.hits[key]
	i := 0
	for i < len(h) && now.Sub(h[i]) > f.window {
		i++
	}
	if i == len(h) {
		delete(f.hits, key)
		return
	}
	f.hits[key] = h[i:]
}

// accountLocked reports whether a user is temporarily locked after failures.
func (a *Auth) accountLocked(ctx context.Context, uid int64) (time.Duration, bool) {
	var until int64
	_ = a.db.QueryRowContext(ctx, `SELECT locked_until FROM users WHERE id=?`, uid).Scan(&until)
	if d := time.Unix(until, 0).Sub(a.now()); d > 0 {
		return d, true
	}
	return 0, false
}

// failLogin records a failed password or 2FA attempt: per IP, and per account
// with a lock that doubles from 30 s after 5 failures, up to an hour.
func (a *Auth) failLogin(ctx context.Context, r *http.Request, uid int64, actor audit.Actor, why string) {
	a.ipFails.add(ClientIP(r), a.now())
	if uid > 0 {
		var n int
		_ = a.db.QueryRowContext(ctx, `UPDATE users SET failed_logins=failed_logins+1 WHERE id=? RETURNING failed_logins`, uid).Scan(&n)
		if n >= 5 {
			lock := 30 * time.Second << min(n-5, 7)
			lock = min(lock, time.Hour)
			_, _ = a.db.ExecContext(ctx, `UPDATE users SET locked_until=? WHERE id=?`, a.now().Add(lock).Unix(), uid)
		}
	}
	_ = a.audit.Write(ctx, actor, "auth.login", "", map[string]any{"ip": ClientIP(r)}, "denied", why)
}
