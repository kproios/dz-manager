package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/secret"
)

// flow is a Steam or OIDC round trip in progress (sealed cookie on /auth).
type flow struct {
	Kind     string `json:"k"` // steam | oidc
	State    string `json:"s"`
	Nonce    string `json:"n,omitempty"`
	Verifier string `json:"v,omitempty"`
	Return   string `json:"r,omitempty"`
	Invite   string `json:"i,omitempty"`
	Name     string `json:"nm,omitempty"`
	LinkUID  int64  `json:"l,omitempty"`
	Exp      int64  `json:"e"`
}

// startFlow reads what the round trip is for: an invite (with the chosen
// name), linking to the signed-in account (needs fresh 2FA when enabled),
// or a plain sign-in.
func (a *Auth) startFlow(w http.ResponseWriter, r *http.Request, kind string) (flow, bool) {
	q := r.URL.Query()
	f := flow{Kind: kind, State: secret.Token(24), Return: safeReturn(q.Get("return")), Exp: a.now().Add(flowTTL).Unix()}
	switch {
	case q.Get("invite") != "":
		f.Invite, f.Name = q.Get("invite"), strings.TrimSpace(q.Get("name"))
		if err := a.checkUsername(r.Context(), f.Name); err != nil {
			loginError(w, r, "name")
			return f, false
		}
		if _, err := a.findInvite(r.Context(), f.Invite); err != nil {
			loginError(w, r, "invite")
			return f, false
		}
	case q.Get("link") == "1":
		u, err := a.Session(r)
		if err != nil || (u.TOTP && a.now().Unix()-u.mfaAt > int64(StepUpTTL.Seconds())) {
			http.Redirect(w, r, "/#/account?e=confirm", http.StatusFound)
			return f, false
		}
		f.LinkUID = u.ID
	}
	return f, true
}

func (a *Auth) saveFlow(w http.ResponseWriter, f flow) {
	b, _ := json.Marshal(f)
	a.setCookie(w, flowCookie, a.box.SealString(string(b), flowCookie), "/auth", flowTTL)
}

func (a *Auth) takeFlow(w http.ResponseWriter, r *http.Request, kind string) (flow, bool) {
	var f flow
	c, err := r.Cookie(flowCookie)
	a.setCookie(w, flowCookie, "", "/auth", 0)
	if err != nil {
		return f, false
	}
	raw, err := a.box.OpenString(c.Value, flowCookie)
	if err != nil || json.Unmarshal([]byte(raw), &f) != nil || a.now().Unix() > f.Exp || f.Kind != kind {
		return flow{}, false
	}
	return f, true
}

// loginError sends the browser back to the login page with a short code the
// page turns into a message.
func loginError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/#/login?e="+url.QueryEscape(code), http.StatusFound)
}

// finishExternal completes a Steam or OIDC round trip whose identity (subject
// in column col) has been verified.
func (a *Auth) finishExternal(w http.ResponseWriter, r *http.Request, f flow, method, col, subject string) {
	ctx := r.Context()
	switch {
	case f.LinkUID > 0:
		_, err := a.db.ExecContext(ctx, `UPDATE users SET `+col+`=? WHERE id=?`, subject, f.LinkUID)
		if err != nil {
			http.Redirect(w, r, "/#/account?e=taken", http.StatusFound)
			return
		}
		_ = a.audit.Write(ctx, a.userActor(ctx, f.LinkUID), "account.link", method, nil, "ok", "")
		http.Redirect(w, r, "/#/account?linked="+method, http.StatusFound)
	case f.Invite != "":
		uid, err := a.acceptInvite(ctx, r, f.Invite, f.Name, map[string]string{col: subject}, method)
		if err != nil {
			code := "invite"
			if strings.Contains(err.Error(), "already linked") {
				code = "taken"
			} else if !errors.Is(err, errInvite) {
				code = "name"
			}
			loginError(w, r, code)
			return
		}
		if next := a.afterFirstFactor(ctx, w, r, uid, method, "/"); next != "" {
			http.Redirect(w, r, "/#/login", http.StatusFound)
		}
	default:
		var uid int64
		var disabled sql.NullString
		err := a.db.QueryRowContext(ctx, `SELECT id, disabled_at FROM users WHERE `+col+`=?`, subject).Scan(&uid, &disabled)
		if err != nil || disabled.Valid {
			why, code := "no account linked", "no_account"
			if err == nil {
				why, code = "account disabled", "disabled"
			}
			_ = a.audit.Write(ctx, audit.Actor{Sub: "anonymous", Name: method + " " + subject}, "auth.login", "", map[string]any{"method": method, "ip": ClientIP(r)}, "denied", why)
			loginError(w, r, code)
			return
		}
		if d, locked := a.accountLocked(ctx, uid); locked {
			_ = d
			loginError(w, r, "locked")
			return
		}
		if next := a.afterFirstFactor(ctx, w, r, uid, method, f.Return); next != "" {
			target := "/#/login"
			if next == "signed_in" {
				target = f.Return
			}
			http.Redirect(w, r, target, http.StatusFound)
		}
	}
}

// ---- Steam (OpenID 2.0) ----

const steamOP = "https://steamcommunity.com/openid/login"

var reSteamID = regexp.MustCompile(`^https://steamcommunity\.com/openid/id/([0-9]{17})$`)

type steamLogin struct{ a *Auth }

func (s *steamLogin) endpoint() string {
	if s.a.SteamEndpoint != "" {
		return s.a.SteamEndpoint
	}
	return steamOP
}

func (s *steamLogin) returnTo(state string) string {
	return s.a.cfg.PublicURL.String() + "/auth/steam/callback?state=" + url.QueryEscape(state)
}

func (s *steamLogin) login(w http.ResponseWriter, r *http.Request) {
	f, ok := s.a.startFlow(w, r, "steam")
	if !ok {
		return
	}
	s.a.saveFlow(w, f)
	v := url.Values{
		"openid.ns":         {"http://specs.openid.net/auth/2.0"},
		"openid.mode":       {"checkid_setup"},
		"openid.return_to":  {s.returnTo(f.State)},
		"openid.realm":      {s.a.cfg.PublicURL.String() + "/"},
		"openid.identity":   {"http://specs.openid.net/auth/2.0/identifier_select"},
		"openid.claimed_id": {"http://specs.openid.net/auth/2.0/identifier_select"},
	}
	http.Redirect(w, r, s.endpoint()+"?"+v.Encode(), http.StatusFound)
}

func (s *steamLogin) callback(w http.ResponseWriter, r *http.Request) {
	a := s.a
	f, ok := a.takeFlow(w, r, "steam")
	q := r.URL.Query()
	if !ok || !secret.Equal(q.Get("state"), f.State) {
		loginError(w, r, "expired")
		return
	}
	if q.Get("openid.mode") != "id_res" {
		loginError(w, r, "cancelled")
		return
	}
	signed := "," + q.Get("openid.signed") + ","
	m := reSteamID.FindStringSubmatch(q.Get("openid.claimed_id"))
	if m == nil || q.Get("openid.identity") != q.Get("openid.claimed_id") || q.Get("openid.return_to") != s.returnTo(f.State) ||
		q.Get("openid.op_endpoint") != s.endpoint() || !strings.Contains(signed, ",claimed_id,") || !strings.Contains(signed, ",return_to,") {
		loginError(w, r, "failed")
		return
	}
	if err := s.verify(r.Context(), q); err != nil {
		a.log.Warn("steam sign-in check failed", "err", err)
		loginError(w, r, "failed")
		return
	}
	a.finishExternal(w, r, f, "steam", "steam64", m[1])
}

// verify asks Steam whether it really signed this response (check_authentication).
func (s *steamLogin) verify(ctx context.Context, q url.Values) error {
	v := url.Values{}
	for k, vals := range q {
		if strings.HasPrefix(k, "openid.") {
			v[k] = vals
		}
	}
	v.Set("openid.mode", "check_authentication")
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint(), strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.a.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "is_valid:true" {
			return nil
		}
	}
	return errors.New("steam says the response is not valid")
}

// ---- OpenID Connect (optional) ----

type oidcProvider struct {
	a        *Auth
	provider *oidc.Provider
	endSess  string
}

func (o *oidcProvider) ctx(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, o.a.client())
}

func (o *oidcProvider) discover(ctx context.Context) (*oidc.Provider, error) {
	o.a.oidcMu.Lock()
	defer o.a.oidcMu.Unlock()
	if o.provider != nil {
		return o.provider, nil
	}
	dctx, cancel := context.WithTimeout(o.ctx(ctx), 10*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(dctx, o.a.cfg.OIDCIssuer)
	if err != nil {
		return nil, err
	}
	var extra struct {
		EndSession string `json:"end_session_endpoint"`
	}
	_ = p.Claims(&extra)
	o.provider, o.endSess = p, extra.EndSession
	return p, nil
}

func (o *oidcProvider) oauth(p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     o.a.cfg.OIDCClientID,
		ClientSecret: o.a.cfg.OIDCClientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  o.a.cfg.PublicURL.String() + "/auth/callback",
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
}

func (o *oidcProvider) login(w http.ResponseWriter, r *http.Request) {
	p, err := o.discover(r.Context())
	if err != nil {
		o.a.log.Error("oidc discovery failed", "err", err)
		loginError(w, r, "sso_down")
		return
	}
	f, ok := o.a.startFlow(w, r, "oidc")
	if !ok {
		return
	}
	f.Nonce, f.Verifier = secret.Token(24), oauth2.GenerateVerifier()
	o.a.saveFlow(w, f)
	http.Redirect(w, r, o.oauth(p).AuthCodeURL(f.State, oidc.Nonce(f.Nonce), oauth2.S256ChallengeOption(f.Verifier)), http.StatusFound)
}

func (o *oidcProvider) callback(w http.ResponseWriter, r *http.Request) {
	a := o.a
	ctx := r.Context()
	f, ok := a.takeFlow(w, r, "oidc")
	q := r.URL.Query()
	if !ok || !secret.Equal(q.Get("state"), f.State) {
		loginError(w, r, "expired")
		return
	}
	if q.Get("error") != "" {
		loginError(w, r, "cancelled")
		return
	}
	p, err := o.discover(ctx)
	if err != nil {
		loginError(w, r, "sso_down")
		return
	}
	tok, err := o.oauth(p).Exchange(o.ctx(ctx), q.Get("code"), oauth2.VerifierOption(f.Verifier))
	if err != nil {
		a.log.Warn("oidc code exchange failed", "err", err)
		loginError(w, r, "failed")
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	idt, err := p.Verifier(&oidc.Config{ClientID: a.cfg.OIDCClientID, Now: a.now}).Verify(o.ctx(ctx), rawID)
	if err != nil {
		a.log.Warn("oidc id_token verification failed", "err", err)
		loginError(w, r, "failed")
		return
	}
	var cl struct {
		Sub   string `json:"sub"`
		Nonce string `json:"nonce"`
	}
	if idt.Claims(&cl) != nil || cl.Sub == "" || !secret.Equal(cl.Nonce, f.Nonce) {
		loginError(w, r, "failed")
		return
	}
	a.finishExternal(w, r, f, "oidc", "oidc_sub", cl.Sub)
}

func (o *oidcProvider) endSessionURL(ctx context.Context) string {
	if _, err := o.discover(ctx); err != nil || o.endSess == "" {
		return "/"
	}
	v := url.Values{"post_logout_redirect_uri": {o.a.cfg.PublicURL.String() + "/"}, "client_id": {o.a.cfg.OIDCClientID}}
	return o.endSess + "?" + v.Encode()
}
