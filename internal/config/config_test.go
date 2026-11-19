package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func base() map[string]string {
	return map[string]string{
		"DZM_SECRET_KEY":        base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"DZM_PUBLIC_URL":        "https://dzm.example.com",
		"DZM_INGEST_PUBLIC_URL": "https://dzm-ingest.example.com",
	}
}

func withOIDC(m map[string]string) map[string]string {
	m["DZM_OIDC_ISSUER"] = "https://sso.example.com/application/o/dz-manager/"
	m["DZM_OIDC_CLIENT_ID"] = "id"
	m["DZM_OIDC_CLIENT_SECRET"] = "secret"
	return m
}

func load(m map[string]string) (*Config, error) {
	return FromEnv(func(k string) string { return m[k] })
}

func TestDefaults(t *testing.T) {
	c, err := load(base())
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:8080" || c.DataDir != "/var/lib/dzm" || c.Location.String() != "Europe/Helsinki" || !c.SecureCookies() {
		t.Fatalf("bad defaults: %+v", c)
	}
	// Single sign-on is optional; Steam sign-in is on unless turned off.
	if c.OIDCIssuer != "" || !c.SteamLogin {
		t.Fatalf("sign-in defaults: oidc %q steam %v", c.OIDCIssuer, c.SteamLogin)
	}
	m := withOIDC(base())
	m["DZM_STEAM_LOGIN"] = "0"
	if c, err = load(m); err != nil || c.OIDCLabel != "Single sign-on" || c.SteamLogin {
		t.Fatalf("with oidc: %v %+v", err, c)
	}
}

func TestFailClosed(t *testing.T) {
	cases := map[string]func(m map[string]string){
		"missing key":     func(m map[string]string) { delete(m, "DZM_SECRET_KEY") },
		"short key":       func(m map[string]string) { m["DZM_SECRET_KEY"] = base64.StdEncoding.EncodeToString(make([]byte, 16)) },
		"public listen":   func(m map[string]string) { m["DZM_LISTEN"] = "0.0.0.0:8080" },
		"http public url": func(m map[string]string) { m["DZM_PUBLIC_URL"] = "http://dzm.example.com" },
		"half oidc":       func(m map[string]string) { withOIDC(m); delete(m, "DZM_OIDC_CLIENT_SECRET") },
		"http issuer":     func(m map[string]string) { withOIDC(m); m["DZM_OIDC_ISSUER"] = "http://auth.example/" },
		"bad tz":          func(m map[string]string) { m["DZM_TZ"] = "Mars/Olympus" },
		"bad log level":   func(m map[string]string) { m["DZM_LOG_LEVEL"] = "loud" },
		"missing ingest":  func(m map[string]string) { delete(m, "DZM_INGEST_PUBLIC_URL") },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			m := base()
			mut(m)
			if _, err := load(m); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestAllErrorsReported(t *testing.T) {
	_, err := load(map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "DZM_SECRET_KEY") || !strings.Contains(err.Error(), "DZM_PUBLIC_URL") {
		t.Fatalf("errors not aggregated: %v", err)
	}
}
