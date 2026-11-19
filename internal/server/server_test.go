package server_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/kproios/dz-manager/internal/apptest"
	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/profiles"
)

func TestHealthReadyOpen(t *testing.T) {
	e := apptest.New(t, "")
	var ready map[string]string
	if code := e.JSON("GET", "/ready", nil, &ready); code != 200 || ready["version"] != "v0.0.0-test" || ready["status"] != "ready" {
		t.Fatalf("ready = %d %v", code, ready)
	}
	if code := e.JSON("GET", "/health", nil, nil); code != 200 {
		t.Fatalf("health = %d", code)
	}
}

func TestUnauthenticated(t *testing.T) {
	e := apptest.New(t, "")
	if code := e.JSON("GET", "/api/status", nil, nil); code != 401 {
		t.Fatalf("api without session = %d, want 401", code)
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noFollow.Get(e.TS.URL + "/map")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 302 || !strings.HasPrefix(resp.Header.Get("Location"), "/auth/login?return=") {
		t.Fatalf("page without session = %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp.Header.Get("Content-Security-Policy") == "" || resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatal("security headers missing")
	}
}

func TestLoginFlowAndCSRF(t *testing.T) {
	e := apptest.New(t, "")
	if resp := e.Login(); resp.StatusCode != 200 {
		t.Fatalf("login landed on %d", resp.StatusCode)
	}
	if e.CSRF == "" {
		t.Fatal("no csrf token after login")
	}
	var st map[string]any
	if code := e.JSON("GET", "/api/status", nil, &st); code != 200 || st["version"] != "v0.0.0-test" {
		t.Fatalf("status = %d %v", code, st)
	}
	// State-changing request without CSRF header is refused.
	good := e.CSRF
	e.CSRF = "bogus"
	if code := e.JSON("POST", "/api/profiles", profiles.Input{Name: "x"}, nil); code != 403 {
		t.Fatalf("bad csrf = %d, want 403", code)
	}
	e.CSRF = good
	// Cross-origin request refused even with the right token.
	req, _ := http.NewRequest("POST", e.TS.URL+"/api/profiles", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("X-DZM-CSRF", good)
	resp, _ := e.Client.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("cross-origin = %d, want 403", resp.StatusCode)
	}
	if code := e.JSON("POST", "/api/profiles", profiles.Input{Name: "TestServer", ProviderKind: "selfhosted", GameHost: "192.0.2.10"}, nil); code != 201 {
		t.Fatalf("create profile = %d", code)
	}
	var log []audit.Entry
	e.JSON("GET", "/api/audit", nil, &log)
	if len(log) < 2 || log[0].Action != "profile.create" || log[0].ActorName != "owner" {
		t.Fatalf("audit = %+v", log)
	}
	// Logout kills the session.
	resp = e.Do("POST", "/auth/logout", nil)
	resp.Body.Close()
	if code := e.JSON("GET", "/api/status", nil, nil); code != 401 {
		t.Fatalf("after logout = %d", code)
	}
}

func TestLoginDenied(t *testing.T) {
	cases := map[string]func(e *apptest.Env){
		"not in group":   func(e *apptest.Env) { e.Issuer.User.Groups = []string{"users"} },
		"wrong audience": func(e *apptest.Env) { e.Issuer.WrongAudience = true },
		"wrong nonce":    func(e *apptest.Env) { e.Issuer.WrongNonce = true },
		"expired token":  func(e *apptest.Env) { e.Issuer.Expired = true },
		"wrong issuer":   func(e *apptest.Env) { e.Issuer.WrongIssuer = true },
	}
	for name, tamper := range cases {
		t.Run(name, func(t *testing.T) {
			e := apptest.New(t, "")
			tamper(e)
			if resp := e.Login(); resp.StatusCode != 403 {
				t.Fatalf("login = %d, want 403", resp.StatusCode)
			}
			if code := e.JSON("GET", "/api/status", nil, nil); code != 401 {
				t.Fatalf("session created despite failure: %d", code)
			}
			var n int
			e.App.DB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='auth.login' AND result='denied'`).Scan(&n)
			if n != 1 {
				t.Fatalf("denied audit rows = %d", n)
			}
		})
	}
}

func TestCallbackStateMismatch(t *testing.T) {
	e := apptest.New(t, "")
	noFollow := &http.Client{Jar: e.Client.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, _ := noFollow.Get(e.TS.URL + "/auth/login")
	resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	resp, _ = noFollow.Get(e.TS.URL + "/auth/callback?code=x&state=forged")
	resp.Body.Close()
	if resp.StatusCode != 400 || loc.Query().Get("state") == "forged" {
		t.Fatalf("forged state = %d", resp.StatusCode)
	}
	// Callback without the flow cookie is refused.
	resp, _ = http.Get(e.TS.URL + "/auth/callback?code=x&state=y")
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("no cookie = %d", resp.StatusCode)
	}
}

func TestSessionIdleExpiry(t *testing.T) {
	e := apptest.New(t, "")
	e.Login()
	if _, err := e.App.DB.Exec(`UPDATE sessions SET last_seen_at = last_seen_at - 3*3600`); err != nil {
		t.Fatal(err)
	}
	if code := e.JSON("GET", "/api/me", nil, nil); code != 401 {
		t.Fatalf("idle session still valid: %d", code)
	}
}

func TestAuditAppendOnly(t *testing.T) {
	e := apptest.New(t, "")
	e.Login()
	if _, err := e.App.DB.Exec(`UPDATE audit_log SET result='ok'`); err == nil {
		t.Fatal("audit_log update should fail")
	}
	if _, err := e.App.DB.Exec(`DELETE FROM audit_log`); err == nil {
		t.Fatal("audit_log delete should fail")
	}
}

func TestIngestPing(t *testing.T) {
	e := apptest.New(t, "")
	e.Login()
	var p profiles.Profile
	e.JSON("POST", "/api/profiles", profiles.Input{Name: "s"}, &p)
	if code, _ := e.Post("/ingest/ping", "application/json", nil, nil); code != 401 {
		t.Fatalf("ping without token = %d", code)
	}
	var wz profiles.Wizard
	e.JSON("GET", "/api/profiles/"+p.ID+"/wizard", nil, &wz)
	tok := wz.BridgeConfig["server_token"].(string)
	if code, body := e.Post("/ingest/ping", "application/json", nil, map[string]string{"Authorization": "Bearer " + tok}); code != 200 || !strings.Contains(body, p.ID) {
		t.Fatalf("ping with token = %d %s", code, body)
	}
}
