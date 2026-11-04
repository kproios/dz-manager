// Package auth implements Authentik-only login (OIDC authorization code +
// PKCE), server-side sessions, and CSRF/Origin protection. There are no local
// passwords and no bypass.
package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/config"
	"github.com/kproios/dz-manager/internal/secret"
	"github.com/kproios/dz-manager/internal/store"
)

const (
	sessionCookie = "dzm_session"
	flowCookie    = "dzm_oidc"
	// CSRFHeader must echo the session's CSRF token on state-changing requests.
	CSRFHeader = "X-DZM-CSRF"

	absoluteTTL = 12 * time.Hour
	idleTTL     = 2 * time.Hour
	flowTTL     = 10 * time.Minute
)

// User is the authenticated principal attached to a request.
type User struct {
	ID       int64  `json:"id"`
	Sub      string `json:"sub"`
	Username string `json:"username"`
	Email    string `json:"email"`
	CSRF     string `json:"csrf"`
}

// Actor converts a user to an audit actor.
func (u *User) Actor() audit.Actor { return audit.Actor{Sub: u.Sub, Name: u.Username} }

type ctxKey struct{}

// FromContext returns the session user (nil if none).
func FromContext(ctx context.Context) *User {
	u, _ := ctx.Value(ctxKey{}).(*User)
	return u
}

// WithUser attaches a user to a context (used by tests and internal callers).
func WithUser(ctx context.Context, u *User) context.Context { return context.WithValue(ctx, ctxKey{}, u) }

// Auth wires OIDC and sessions.
type Auth struct {
	cfg   *config.Config
	db    *store.DB
	box   *secret.Box
	audit *audit.Log
	log   *slog.Logger
	now   func() time.Time

	mu       sync.Mutex
	provider *oidc.Provider
	endSess  string
	// HTTPClient is used for OIDC discovery and token calls (tests inject one).
	HTTPClient *http.Client
}

// New creates an Auth. OIDC discovery is lazy so Authentik being down never
// prevents the service from starting or reporting ready.
func New(cfg *config.Config, db *store.DB, box *secret.Box, al *audit.Log, log *slog.Logger) *Auth {
	return &Auth{cfg: cfg, db: db, box: box, audit: al, log: log, now: time.Now}
}

func (a *Auth) ctx(ctx context.Context) context.Context {
	if a.HTTPClient != nil {
		return oidc.ClientContext(ctx, a.HTTPClient)
	}
	return ctx
}

// Provider returns the discovered OIDC provider, discovering on first use.
func (a *Auth) Provider(ctx context.Context) (*oidc.Provider, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.provider != nil {
		return a.provider, nil
	}
	dctx, cancel := context.WithTimeout(a.ctx(ctx), 10*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(dctx, a.cfg.OIDCIssuer)
	if err != nil {
		return nil, err
	}
	var extra struct {
		EndSession string `json:"end_session_endpoint"`
	}
	_ = p.Claims(&extra)
	a.provider, a.endSess = p, extra.EndSession
	return p, nil
}

func (a *Auth) oauth(p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     a.cfg.OIDCClientID,
		ClientSecret: a.cfg.OIDCClientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  a.cfg.PublicURL.String() + "/auth/callback",
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
}

type flowState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Return   string `json:"r"`
	Exp      int64  `json:"e"`
}

// Login starts the OIDC flow (GET /auth/login?return=/path).
func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	p, err := a.Provider(r.Context())
	if err != nil {
		a.log.Error("oidc discovery failed", "err", err)
		http.Error(w, "Authentik is unreachable right now; try again shortly.", http.StatusBadGateway)
		return
	}
	fs := flowState{
		State:    secret.Token(24),
		Nonce:    secret.Token(24),
		Verifier: oauth2.GenerateVerifier(),
		Return:   safeReturn(r.URL.Query().Get("return")),
		Exp:      a.now().Add(flowTTL).Unix(),
	}
	b, _ := json.Marshal(fs)
	http.SetCookie(w, &http.Cookie{
		Name: flowCookie, Value: a.box.SealString(string(b), flowCookie), Path: "/auth",
		HttpOnly: true, Secure: a.cfg.SecureCookies(), SameSite: http.SameSiteLaxMode,
		MaxAge: int(flowTTL.Seconds()),
	})
	u := a.oauth(p).AuthCodeURL(fs.State, oidc.Nonce(fs.Nonce), oauth2.S256ChallengeOption(fs.Verifier))
	http.Redirect(w, r, u, http.StatusFound)
}

type idClaims struct {
	Sub      string   `json:"sub"`
	Username string   `json:"preferred_username"`
	Name     string   `json:"name"`
	Email    string   `json:"email"`
	Groups   []string `json:"groups"`
	Nonce    string   `json:"nonce"`
}

// Callback completes the OIDC flow (GET /auth/callback).
func (a *Auth) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	fail := func(status int, msg string, err error) {
		a.log.Warn("login failed", "reason", msg, "err", err)
		_ = a.audit.Write(ctx, audit.Actor{Sub: "anonymous", Name: "anonymous"}, "auth.login", "", nil, "denied", msg)
		http.Error(w, msg, status)
	}
	c, err := r.Cookie(flowCookie)
	if err != nil {
		fail(http.StatusBadRequest, "login session missing or expired; start again", err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Value: "", Path: "/auth", MaxAge: -1, HttpOnly: true, Secure: a.cfg.SecureCookies(), SameSite: http.SameSiteLaxMode})
	raw, err := a.box.OpenString(c.Value, flowCookie)
	var fs flowState
	if err != nil || json.Unmarshal([]byte(raw), &fs) != nil || a.now().Unix() > fs.Exp {
		fail(http.StatusBadRequest, "login session invalid or expired; start again", err)
		return
	}
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		fail(http.StatusForbidden, "Authentik refused the login: "+e, nil)
		return
	}
	if !secret.Equal(q.Get("state"), fs.State) {
		fail(http.StatusBadRequest, "state mismatch", nil)
		return
	}
	p, err := a.Provider(ctx)
	if err != nil {
		fail(http.StatusBadGateway, "Authentik unreachable", err)
		return
	}
	tok, err := a.oauth(p).Exchange(a.ctx(ctx), q.Get("code"), oauth2.VerifierOption(fs.Verifier))
	if err != nil {
		fail(http.StatusForbidden, "code exchange failed", err)
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	if rawID == "" {
		fail(http.StatusForbidden, "no id_token returned", nil)
		return
	}
	idt, err := p.Verifier(&oidc.Config{ClientID: a.cfg.OIDCClientID, Now: a.now}).Verify(a.ctx(ctx), rawID)
	if err != nil {
		fail(http.StatusForbidden, "id_token verification failed", err)
		return
	}
	var cl idClaims
	if err := idt.Claims(&cl); err != nil || cl.Sub == "" {
		fail(http.StatusForbidden, "id_token claims invalid", err)
		return
	}
	if !secret.Equal(cl.Nonce, fs.Nonce) {
		fail(http.StatusForbidden, "nonce mismatch", nil)
		return
	}
	name := firstNonEmpty(cl.Username, cl.Name, cl.Email, cl.Sub)
	if !slices.Contains(cl.Groups, a.cfg.OIDCAdminGroup) {
		a.log.Warn("login denied: not in admin group", "user", name)
		_ = a.audit.Write(ctx, audit.Actor{Sub: cl.Sub, Name: name}, "auth.login", "", nil, "denied", "not a member of "+a.cfg.OIDCAdminGroup)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>Access denied</title><body style="font-family:sans-serif;padding:2rem"><h1>Access denied</h1><p>Your Authentik account is not a member of the DZ Manager admin group.</p><form method="post" action="/auth/logout"><button>Sign out of Authentik</button></form></body>`))
		return
	}
	uid, err := a.upsertUser(ctx, cl.Sub, name, cl.Email)
	if err != nil {
		fail(http.StatusInternalServerError, "could not record user", err)
		return
	}
	token := secret.Token(32)
	now := a.now().Unix()
	if _, err := a.db.ExecContext(ctx, `INSERT INTO sessions(id_hash,user_id,csrf,id_token,created_at,last_seen_at,expires_at) VALUES(?,?,?,?,?,?,?)`,
		a.box.Hash(token), uid, secret.Token(24), rawID, now, now, now+int64(absoluteTTL.Seconds())); err != nil {
		fail(http.StatusInternalServerError, "could not create session", err)
		return
	}
	_, _ = a.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ? OR last_seen_at < ?`, now, now-int64(idleTTL.Seconds()))
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: a.cfg.SecureCookies(), SameSite: http.SameSiteLaxMode, MaxAge: int(absoluteTTL.Seconds())})
	_ = a.audit.Write(ctx, audit.Actor{Sub: cl.Sub, Name: name}, "auth.login", "", nil, "ok", "")
	http.Redirect(w, r, fs.Return, http.StatusFound)
}

func (a *Auth) upsertUser(ctx context.Context, sub, name, email string) (int64, error) {
	ts := store.Now()
	_, err := a.db.ExecContext(ctx, `INSERT INTO users(sub,username,email,created_at,last_login_at) VALUES(?,?,?,?,?)
		ON CONFLICT(sub) DO UPDATE SET username=excluded.username, email=excluded.email, last_login_at=excluded.last_login_at`, sub, name, email, ts, ts)
	if err != nil {
		return 0, err
	}
	var id int64
	err = a.db.QueryRowContext(ctx, `SELECT id FROM users WHERE sub=?`, sub).Scan(&id)
	return id, err
}

// Logout ends the session (POST /auth/logout) and redirects to Authentik's end-session endpoint.
func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	if !a.sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	idToken := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		h := a.box.Hash(c.Value)
		_ = a.db.QueryRowContext(r.Context(), `SELECT id_token FROM sessions WHERE id_hash=?`, h).Scan(&idToken)
		_, _ = a.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE id_hash=?`, h)
	}
	if u := FromContext(r.Context()); u != nil {
		_ = a.audit.Write(r.Context(), u.Actor(), "auth.logout", "", nil, "ok", "")
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.cfg.SecureCookies(), SameSite: http.SameSiteLaxMode})
	target := "/"
	if _, err := a.Provider(r.Context()); err == nil && a.endSess != "" {
		v := url.Values{"post_logout_redirect_uri": {a.cfg.PublicURL.String() + "/"}, "client_id": {a.cfg.OIDCClientID}}
		if idToken != "" {
			v.Set("id_token_hint", idToken)
		}
		target = a.endSess + "?" + v.Encode()
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// errNoSession is returned when no valid session exists.
var errNoSession = errors.New("no session")

// Session resolves the request's session, extending idle expiry.
func (a *Auth) Session(r *http.Request) (*User, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil, errNoSession
	}
	h := a.box.Hash(c.Value)
	now := a.now().Unix()
	var u User
	var lastSeen, expires int64
	err = a.db.QueryRowContext(r.Context(), `SELECT u.id,u.sub,u.username,u.email,s.csrf,s.last_seen_at,s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.id_hash=?`, h).
		Scan(&u.ID, &u.Sub, &u.Username, &u.Email, &u.CSRF, &lastSeen, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoSession
	}
	if err != nil {
		return nil, err
	}
	if now > expires || now-lastSeen > int64(idleTTL.Seconds()) {
		_, _ = a.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE id_hash=?`, h)
		return nil, errNoSession
	}
	if now-lastSeen > 60 {
		_, _ = a.db.ExecContext(r.Context(), `UPDATE sessions SET last_seen_at=? WHERE id_hash=?`, now, h)
	}
	return &u, nil
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
			writeJSONError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if !safeMethod(r.Method) {
			if !a.sameOrigin(r) || !secret.Equal(r.Header.Get(CSRFHeader), u.CSRF) {
				writeJSONError(w, http.StatusForbidden, "CSRF check failed")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), u)))
	})
}

// RequirePage protects HTML pages: redirect to login without a session.
func (a *Auth) RequirePage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.Session(r)
		if err != nil {
			http.Redirect(w, r, "/auth/login?return="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), u)))
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

func safeMethod(m string) bool { return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions }

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

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
