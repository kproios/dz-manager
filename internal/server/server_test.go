package server_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kproios/dz-manager/internal/apptest"
	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
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
	// The UI shell is public and shows the sign-in page; it carries the headers.
	resp, err := http.Get(e.TS.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Security-Policy") == "" || resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("shell = %d, security headers %q", resp.StatusCode, resp.Header.Get("Content-Security-Policy"))
	}
	var st map[string]any
	if code := e.JSON("GET", "/auth/state", nil, &st); code != 200 || st["signed_in"] != false || st["setup_needed"] != true {
		t.Fatalf("state = %d %v", code, st)
	}
	if code := e.JSON("GET", "/tiles/x/0/0/0.png", nil, nil); code != 401 {
		t.Fatalf("tiles without session = %d", code)
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
	var me auth.User
	if code := e.JSON("GET", "/api/me", nil, &me); code != 200 || me.Role != "owner" || len(me.Permissions) != 10 || me.Method != "password" {
		t.Fatalf("me = %d %+v", code, me)
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

func TestPasswordAndSecondFactorFailures(t *testing.T) {
	e := apptest.New(t, "")
	e.CreateUser("owner", "owner")
	// Wrong password and unknown user look the same.
	for _, body := range []map[string]string{{"login": "owner", "password": "wrong password!!"}, {"login": "nobody", "password": apptest.Password}} {
		var out map[string]string
		if code := e.JSON("POST", "/auth/password", body, &out); code != 401 || out["error"] != "Wrong user name or password." {
			t.Fatalf("bad sign-in = %d %v", code, out)
		}
	}
	// The second factor can't be skipped: no session after the password alone.
	var next map[string]string
	if code := e.JSON("POST", "/auth/password", map[string]string{"login": "OWNER", "password": apptest.Password}, &next); code != 200 || next["next"] != "totp" {
		t.Fatalf("password = %d %v", code, next)
	}
	if code := e.JSON("GET", "/api/me", nil, nil); code != 401 {
		t.Fatalf("session before 2FA: %d", code)
	}
	if code := e.JSON("POST", "/auth/totp", map[string]string{"code": "000000"}, nil); code != 401 {
		t.Fatalf("wrong code = %d", code)
	}
	code := auth.TOTPAt(apptest.TOTPSecret, time.Now())
	if c := e.JSON("POST", "/auth/totp", map[string]string{"code": code}, nil); c != 200 {
		t.Fatalf("right code = %d", c)
	}
	// The same code can't be used twice (another sign-in in the same 30 s).
	jar, _ := cookiejar.New(nil)
	e.Client = &http.Client{Jar: jar}
	e.JSON("POST", "/auth/password", map[string]string{"login": "owner", "password": apptest.Password}, nil)
	if c := e.JSON("POST", "/auth/totp", map[string]string{"code": code}, nil); c != 401 {
		t.Fatalf("replayed code = %d, want 401", c)
	}
	var n int
	e.App.DB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='auth.login' AND result='denied'`).Scan(&n)
	if n != 4 {
		t.Fatalf("denied audit rows = %d, want 4", n)
	}
	// Five failures in a row (the replay above counts; the successful sign-in
	// before it reset the count) lock the account, even with the right password.
	for i := 0; i < 4; i++ {
		e.JSON("POST", "/auth/password", map[string]string{"login": "owner", "password": "wrong password!!"}, nil)
	}
	if c := e.JSON("POST", "/auth/password", map[string]string{"login": "owner", "password": apptest.Password}, nil); c != 429 {
		t.Fatalf("locked account = %d, want 429", c)
	}
}

func TestOIDCSignInNeedsALinkedAccount(t *testing.T) {
	e := apptest.New(t, "")
	noFollow := func() *http.Client {
		jar, _ := cookiejar.New(nil)
		return &http.Client{Jar: jar, CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if strings.HasPrefix(r.URL.String(), e.TS.URL+"/#") || r.URL.Fragment != "" {
				return http.ErrUseLastResponse
			}
			return nil
		}}
	}
	finalHash := func(c *http.Client, path string) string {
		resp, err := c.Get(e.TS.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		u, _ := url.Parse(resp.Header.Get("Location"))
		return u.Fragment
	}
	// Nobody is linked to the provider's subject yet.
	if h := finalHash(noFollow(), "/auth/oidc/login"); h != "/login?e=no_account" {
		t.Fatalf("unlinked = %q", h)
	}
	// Link it: the provider proves who it is, DZ Manager decides the role and asks for 2FA.
	uid := e.CreateUser("owner", "owner")
	e.App.DB.Exec(`UPDATE users SET oidc_sub='user-1' WHERE id=?`, uid)
	c := noFollow()
	if h := finalHash(c, "/auth/oidc/login"); h != "/login" {
		t.Fatalf("linked = %q", h)
	}
	e.Client = c
	var st map[string]any
	e.JSON("GET", "/auth/state", nil, &st)
	if st["pending"] != "totp" || st["signed_in"] != false {
		t.Fatalf("state after provider = %v", st)
	}
	if code := e.JSON("POST", "/auth/totp", map[string]string{"code": auth.TOTPAt(apptest.TOTPSecret, time.Now())}, nil); code != 200 {
		t.Fatalf("totp = %d", code)
	}
	var me auth.User
	if e.JSON("GET", "/api/me", nil, &me); me.Method != "oidc" || me.Username != "owner" {
		t.Fatalf("me = %+v", me)
	}
	e.App.DB.Exec(`UPDATE users SET totp_secret='' WHERE id=?`, uid)
	c = noFollow()
	finalHash(c, "/auth/oidc/login")
	e.Client = c
	if code := e.JSON("GET", "/api/me", nil, &me); code != 200 || me.Method != "oidc" || me.TOTP {
		t.Fatalf("OIDC without 2FA = %d", code)
	}
	// Tampered tokens never get as far as 2FA.
	for name, tamper := range map[string]func(){
		"wrong audience": func() { e.Issuer.WrongAudience = true },
		"wrong nonce":    func() { e.Issuer.WrongNonce = true },
		"expired token":  func() { e.Issuer.Expired = true },
		"wrong issuer":   func() { e.Issuer.WrongIssuer = true },
	} {
		e.Issuer.WrongAudience, e.Issuer.WrongNonce, e.Issuer.Expired, e.Issuer.WrongIssuer = false, false, false, false
		tamper()
		if h := finalHash(noFollow(), "/auth/oidc/login"); h != "/login?e=failed" {
			t.Fatalf("%s = %q", name, h)
		}
	}
	// A forged state or a missing flow cookie is refused.
	if h := finalHash(noFollow(), "/auth/callback?code=x&state=forged"); h != "/login?e=expired" {
		t.Fatalf("forged state = %q", h)
	}
}

func TestSteamSignIn(t *testing.T) {
	e := apptest.New(t, "")
	valid := true
	steam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Method != "POST" || r.Form.Get("openid.mode") != "check_authentication" {
			http.Error(w, "unexpected", 400)
			return
		}
		fmt.Fprintf(w, "ns:http://specs.openid.net/auth/2.0\nis_valid:%v\n", valid)
	}))
	defer steam.Close()
	e.App.Server.Auth.SteamEndpoint = steam.URL
	uid := e.CreateUser("owner", "owner")
	e.App.DB.Exec(`UPDATE users SET steam64='76561198000000001' WHERE id=?`, uid)

	signIn := func(steam64 string) (*http.Client, string) {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, _ := c.Get(e.TS.URL + "/auth/steam/login")
		resp.Body.Close()
		loc, _ := url.Parse(resp.Header.Get("Location"))
		ret := loc.Query().Get("openid.return_to")
		if !strings.HasPrefix(loc.String(), steam.URL) || !strings.HasPrefix(ret, e.TS.URL+"/auth/steam/callback?state=") {
			t.Fatalf("redirect = %s", loc)
		}
		id := "https://steamcommunity.com/openid/id/" + steam64
		q := url.Values{"openid.ns": {"http://specs.openid.net/auth/2.0"}, "openid.mode": {"id_res"}, "openid.op_endpoint": {steam.URL},
			"openid.claimed_id": {id}, "openid.identity": {id}, "openid.return_to": {ret}, "openid.response_nonce": {"n"}, "openid.assoc_handle": {"h"},
			"openid.signed": {"signed,op_endpoint,claimed_id,identity,return_to,response_nonce,assoc_handle"}, "openid.sig": {"x"}}
		resp, _ = c.Get(ret + "&" + q.Encode())
		resp.Body.Close()
		l, _ := url.Parse(resp.Header.Get("Location"))
		return c, l.Fragment
	}
	c, h := signIn("76561198000000001")
	if h != "/login" {
		t.Fatalf("linked steam = %q", h)
	}
	e.Client = c
	if code := e.JSON("POST", "/auth/totp", map[string]string{"code": auth.TOTPAt(apptest.TOTPSecret, time.Now())}, nil); code != 200 {
		t.Fatalf("totp after steam = %d", code)
	}
	if _, h := signIn("76561198000000002"); h != "/login?e=no_account" {
		t.Fatalf("unknown steam account = %q", h)
	}
	e.App.DB.Exec(`UPDATE users SET totp_secret='' WHERE id=?`, uid)
	c, _ = signIn("76561198000000001")
	e.Client = c
	var me auth.User
	if code := e.JSON("GET", "/api/me", nil, &me); code != 200 || me.Method != "steam" || me.TOTP {
		t.Fatalf("Steam without 2FA = %d", code)
	}
	valid = false
	if _, h := signIn("76561198000000001"); h != "/login?e=failed" {
		t.Fatalf("steam said not valid = %q", h)
	}
}

func TestOptionalTOTP(t *testing.T) {
	e := apptest.New(t, "")
	uid := e.CreateUser("owner", "owner")
	e.App.DB.Exec(`UPDATE users SET totp_secret='' WHERE id=?`, uid)
	password := map[string]string{"login": "owner", "password": apptest.Password}
	var next map[string]string
	if code := e.JSON("POST", "/auth/password", password, &next); code != 200 || next["next"] != "signed_in" {
		t.Fatalf("password without 2FA = %d", code)
	}
	var me auth.User
	e.JSON("GET", "/api/me", nil, &me)
	e.CSRF = me.CSRF
	if me.TOTP || me.CSRF == "" {
		t.Fatal("expected authenticated account with 2FA off")
	}
	// Sensitive routes remain usable when the account has no second factor.
	e.App.DB.Exec(`UPDATE sessions SET mfa_at=0`)
	if code := e.JSON("POST", "/api/profiles", profiles.Input{Name: "optional-2fa"}, nil); code != 201 {
		t.Fatalf("sensitive route without 2FA = %d", code)
	}
	if code := e.JSON("POST", "/api/account/recovery-codes", nil, nil); code != 400 {
		t.Fatalf("recovery codes without 2FA = %d", code)
	}
	other := *e
	jar, _ := cookiejar.New(nil)
	other.Client = &http.Client{Jar: jar}
	other.JSON("POST", "/auth/password", password, nil)
	var enr map[string]string
	if code := e.JSON("POST", "/api/account/2fa/start", nil, &enr); code != 200 {
		t.Fatalf("enable start = %d", code)
	}
	var done struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if code := e.JSON("POST", "/api/account/2fa/finish", map[string]string{"ticket": enr["ticket"], "code": auth.TOTPAt(enr["secret"], time.Now())}, &done); code != 200 || len(done.RecoveryCodes) != 10 {
		t.Fatalf("enable finish = %d", code)
	}
	if code := other.JSON("GET", "/api/me", nil, nil); code != 401 {
		t.Fatalf("old session survived enabling 2FA: %d", code)
	}
	e.JSON("GET", "/api/me", nil, &me)
	if !me.TOTP {
		t.Fatal("2FA was not enabled")
	}
	resp := e.Do("POST", "/auth/logout", nil)
	resp.Body.Close()
	if code := e.JSON("POST", "/auth/password", password, &next); code != 200 || next["next"] != "totp" {
		t.Fatalf("enabled account skipped 2FA = %d", code)
	}
	if code := e.JSON("POST", "/auth/enroll/skip", nil, nil); code != 401 {
		t.Fatalf("enabled account bypassed 2FA = %d", code)
	}
	if code := e.JSON("GET", "/api/me", nil, nil); code != 401 {
		t.Fatalf("password alone created a session = %d", code)
	}
	if code := e.JSON("POST", "/auth/totp", map[string]string{"recovery": done.RecoveryCodes[0]}, nil); code != 200 {
		t.Fatalf("recovery sign-in = %d", code)
	}
	e.JSON("GET", "/api/me", nil, &me)
	e.CSRF = me.CSRF
	e.App.DB.Exec(`UPDATE sessions SET mfa_at=0`)
	if code := e.JSON("DELETE", "/api/account/2fa", nil, nil); code != 403 {
		t.Fatalf("disabled without fresh confirmation = %d", code)
	}
	if code := e.JSON("POST", "/api/account/confirm", map[string]string{"recovery": done.RecoveryCodes[1]}, nil); code != 200 {
		t.Fatalf("confirm disabling = %d", code)
	}
	if code := e.JSON("DELETE", "/api/account/2fa", nil, nil); code != 200 {
		t.Fatalf("disable = %d", code)
	}
	e.JSON("GET", "/api/me", nil, &me)
	var remaining int
	e.App.DB.QueryRow(`SELECT COUNT(*) FROM recovery_codes WHERE user_id=?`, uid).Scan(&remaining)
	if me.TOTP || remaining != 0 {
		t.Fatal("2FA or recovery codes survived disabling")
	}
	if code := e.JSON("POST", "/auth/password", password, &next); code != 200 || next["next"] != "signed_in" {
		t.Fatalf("sign-in after disabling = %d", code)
	}
}

func TestLegacyEnrollmentChoice(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			e := apptest.New(t, "")
			uid := e.CreateUser("owner", "owner")
			if !enabled {
				e.App.DB.Exec(`UPDATE users SET totp_secret='' WHERE id=?`, uid)
			}
			p, _ := json.Marshal(map[string]any{"u": uid, "m": "password", "k": "enroll", "s": auth.NewTOTPSecret(), "e": time.Now().Add(time.Minute).Unix()})
			u, _ := url.Parse(e.TS.URL)
			e.Client.Jar.SetCookies(u, []*http.Cookie{{Name: "dzm_pending", Value: e.App.Box.SealString(string(p), "pending"), Path: "/"}})
			want := 200
			if enabled {
				want = 401
			}
			if code := e.JSON("POST", "/auth/enroll/skip", nil, nil); code != want {
				t.Fatalf("legacy skip = %d, want %d", code, want)
			}
		})
	}
}

func TestInviteRolesAndStepUp(t *testing.T) {
	e := apptest.New(t, "")
	e.Login()
	var inv struct{ ID, Link string }
	if code := e.JSON("POST", "/api/users/invites", map[string]any{"role": "moderator", "note": "Mod Steve"}, &inv); code != 201 || !strings.Contains(inv.Link, "/#/invite/") {
		t.Fatalf("invite = %d %+v", code, inv)
	}
	token := inv.Link[strings.LastIndex(inv.Link, "/")+1:]
	owner := e.Client
	ownerCSRF := e.CSRF

	// The invitee signs in immediately and can then choose to enable 2FA.
	jar, _ := cookiejar.New(nil)
	e.Client, e.CSRF = &http.Client{Jar: jar}, ""
	var info map[string]any
	if code := e.JSON("GET", "/auth/invite/"+token, nil, &info); code != 200 || info["role"] != "moderator" {
		t.Fatalf("invite info = %d %v", code, info)
	}
	if code := e.JSON("POST", "/auth/invite/"+token+"/password", map[string]string{"username": "Steve", "password": "short"}, nil); code != 400 {
		t.Fatalf("weak password = %d", code)
	}
	var next map[string]string
	if code := e.JSON("POST", "/auth/invite/"+token+"/password", map[string]string{"username": "Steve", "password": "a long enough password"}, &next); code != 200 || next["next"] != "signed_in" {
		t.Fatalf("accept = %d %v", code, next)
	}
	var me auth.User
	if code := e.JSON("GET", "/api/me", nil, &me); code != 200 || me.TOTP {
		t.Fatalf("sign-in without 2FA = %d", code)
	}
	e.CSRF = me.CSRF
	if code := e.JSON("GET", "/api/users", nil, nil); code != 403 {
		t.Fatalf("moderator without 2FA escaped role permissions: %d", code)
	}
	var enr map[string]string
	if code := e.JSON("POST", "/api/account/2fa/start", nil, &enr); code != 200 || !strings.HasPrefix(enr["qr"], "data:image/png;base64,") || !strings.HasPrefix(enr["uri"], "otpauth://totp/") {
		t.Fatalf("enroll start = %d", code)
	}
	// Each setup response carries its own ticket. Another request must not
	// invalidate the key the user already sees.
	var again map[string]string
	if code := e.JSON("POST", "/api/account/2fa/start", nil, &again); code != 200 || again["ticket"] == "" {
		t.Fatalf("another setup request = %d", code)
	}
	var done struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if code := e.JSON("POST", "/api/account/2fa/finish", map[string]string{"code": auth.TOTPAt(enr["secret"], time.Now()), "ticket": enr["ticket"]}, &done); code != 200 || len(done.RecoveryCodes) != 10 {
		t.Fatalf("enroll finish = %d %+v", code, done)
	}
	e.JSON("GET", "/api/me", nil, &me)
	e.CSRF = me.CSRF
	if me.Role != "moderator" || me.Username != "Steve" || !me.TOTP {
		t.Fatalf("invitee = %+v", me)
	}
	// The invite works once.
	if code := e.JSON("GET", "/auth/invite/"+token, nil, nil); code != 404 {
		t.Fatalf("used invite = %d", code)
	}
	// Moderator: may kick, may not run strong actions, settings or user management.
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{"POST", "/api/players/76561198000000001/actions/kill", 403},
		{"POST", "/api/rcon/kick-all", 403},
		{"GET", "/api/settings/discord", 403},
		{"GET", "/api/users", 403},
		{"GET", "/api/settings/api-keys", 403},
	} {
		if code := e.JSON(c.method, c.path, map[string]string{}, nil); code != c.want {
			t.Errorf("moderator %s %s = %d, want %d", c.method, c.path, code, c.want)
		}
	}
	if code := e.JSON("POST", "/api/rcon/kick", map[string]string{"be_guid": strings.Repeat("a", 32), "reason": "x"}, nil); code == 403 {
		t.Error("moderator kick refused by permissions")
	}
	if code := e.JSON("POST", "/api/players/76561198000000001/actions/heal", map[string]string{}, nil); code == 403 {
		t.Error("moderator heal refused by permissions")
	}
	// A moderator's audit log shows only their own entries.
	var log []audit.Entry
	e.JSON("GET", "/api/audit", nil, &log)
	for _, l := range log {
		if l.ActorName != "Steve" {
			t.Fatalf("moderator sees %s's entry %s", l.ActorName, l.Action)
		}
	}

	// Back to the owner: sensitive routes need a fresh 2FA confirmation.
	e.Client, e.CSRF = owner, ownerCSRF
	e.App.DB.Exec(`UPDATE sessions SET mfa_at = mfa_at - 3600`)
	var refused map[string]any
	if code := e.JSON("POST", "/api/profiles", profiles.Input{Name: "x"}, &refused); code != 403 || refused["step_up"] != true {
		t.Fatalf("stale 2FA = %d %v", code, refused)
	}
	if code := e.Confirm(); code != 200 {
		t.Fatalf("confirm = %d", code)
	}
	if code := e.JSON("POST", "/api/profiles", profiles.Input{Name: "x"}, nil); code != 201 {
		t.Fatalf("after confirm = %d", code)
	}
	// Owners can't lock themselves out.
	var users struct {
		Users []struct {
			ID   int64
			Role string
			Self bool
		}
	}
	e.JSON("GET", "/api/users", nil, &users)
	var self, steve int64
	for _, u := range users.Users {
		if u.Self {
			self = u.ID
		} else {
			steve = u.ID
		}
	}
	if code := e.JSON("POST", fmt.Sprintf("/api/users/%d/disable", self), nil, nil); code != 400 {
		t.Fatalf("disable self = %d", code)
	}
	// Disabling Steve ends his session at once.
	if code := e.JSON("POST", fmt.Sprintf("/api/users/%d/disable", steve), nil, nil); code != 200 {
		t.Fatalf("disable = %d", code)
	}
	var n int
	e.App.DB.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id=?`, steve).Scan(&n)
	if n != 0 {
		t.Fatalf("disabled user still has %d sessions", n)
	}
}

func TestAPIKeyFollowsItsUsersRole(t *testing.T) {
	e := apptest.New(t, "")
	e.Login()
	boss := e.CreateUser("boss", "owner")
	e.LoginAs("boss", "owner")
	var k struct {
		Token string `json:"token"`
	}
	if code := e.JSON("POST", "/api/settings/api-keys", map[string]any{"name": "bot", "scopes": []string{"public", "actions:write"}}, &k); code != 201 {
		t.Fatalf("create key = %d", code)
	}
	call := func() int {
		req, _ := http.NewRequest("POST", e.TS.URL+"/public/v1/players/76561198000000001/actions/kill", strings.NewReader(`{"confirm":"KILL"}`))
		req.Header.Set("Authorization", "Bearer "+k.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := call(); code == 403 || code == 401 {
		t.Fatalf("owner's key refused: %d", code)
	}
	// The other owner makes boss a moderator: no more strong actions.
	e.Login()
	e.JSON("PUT", fmt.Sprintf("/api/users/%d/role", boss), map[string]string{"role": "moderator"}, nil)
	if code := call(); code != 403 {
		t.Fatalf("moderator's key kill = %d, want 403", code)
	}
	// Viewer: the actions:write scope is gone entirely.
	e.JSON("PUT", fmt.Sprintf("/api/users/%d/role", boss), map[string]string{"role": "viewer"}, nil)
	if code := call(); code != 403 {
		t.Fatalf("viewer's key = %d, want 403", code)
	}
	// Disabled: the key stops working.
	e.JSON("POST", fmt.Sprintf("/api/users/%d/disable", boss), nil, nil)
	if code := call(); code != 401 {
		t.Fatalf("disabled user's key = %d, want 401", code)
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
