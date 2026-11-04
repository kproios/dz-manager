// Package apptest starts a full DZ Manager instance against a fake Authentik
// for integration tests.
package apptest

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/kproios/dz-manager/internal/app"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/auth/oidctest"
	"github.com/kproios/dz-manager/internal/config"
)

// Env is a running test instance.
type Env struct {
	T      *testing.T
	App    *app.App
	TS     *httptest.Server
	Issuer *oidctest.Issuer
	Client *http.Client
	CSRF   string
	Ctx    context.Context
}

// Key is the fixed test secret key.
var Key = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))

// EnvVars returns the environment for a test instance.
func EnvVars(dataDir, publicURL, issuer, clientID, clientSecret string) map[string]string {
	return map[string]string{
		"DZM_DATA_DIR": dataDir, "DZM_SECRET_KEY": Key, "DZM_PUBLIC_URL": publicURL,
		"DZM_INGEST_PUBLIC_URL": publicURL, "DZM_OIDC_ISSUER": issuer,
		"DZM_OIDC_CLIENT_ID": clientID, "DZM_OIDC_CLIENT_SECRET": clientSecret, "DZM_LOG_LEVEL": "error",
	}
}

// New starts an instance. dataDir may be "" for a fresh temp dir.
func New(t *testing.T, dataDir string) *Env {
	t.Helper()
	if dataDir == "" {
		dataDir = t.TempDir()
	}
	is := oidctest.New()
	t.Cleanup(is.Close)
	ts := httptest.NewUnstartedServer(nil)
	url := "http://" + ts.Listener.Addr().String()
	vars := EnvVars(dataDir, url, is.URL(), is.ClientID, is.Secret)
	cfg, err := config.FromEnv(func(k string) string { return vars[k] })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a, err := app.New(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), "v0.0.0-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	a.Server.SetReady(true)
	ts.Config.Handler = a.Server.Handler()
	ts.Start()
	t.Cleanup(ts.Close)
	// Background workers are not started: tests drive pollers explicitly.
	jar, _ := cookiejar.New(nil)
	return &Env{T: t, App: a, TS: ts, Issuer: is, Client: &http.Client{Jar: jar}, Ctx: ctx}
}

// Login performs the full OIDC round-trip and stores the CSRF token.
func (e *Env) Login() *http.Response {
	e.T.Helper()
	resp, err := e.Client.Get(e.TS.URL + "/auth/login")
	if err != nil {
		e.T.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		var me auth.User
		if code := e.JSON("GET", "/api/me", nil, &me); code == 200 {
			e.CSRF = me.CSRF
		}
	}
	return resp
}

// Do sends an API request with Origin + CSRF headers.
func (e *Env) Do(method, path string, body any) *http.Response {
	e.T.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.TS.URL+path, rd)
	req.Header.Set("Origin", e.TS.URL)
	req.Header.Set(auth.CSRFHeader, e.CSRF)
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.Client.Do(req)
	if err != nil {
		e.T.Fatal(err)
	}
	return resp
}

// JSON sends a request and decodes the JSON response into out (may be nil). Returns the status code.
func (e *Env) JSON(method, path string, body, out any) int {
	e.T.Helper()
	resp := e.Do(method, path, body)
	defer resp.Body.Close()
	if out != nil {
		b, _ := io.ReadAll(resp.Body)
		if len(b) > 0 {
			if err := json.Unmarshal(b, out); err != nil {
				e.T.Fatalf("%s %s: decode %q: %v", method, path, b, err)
			}
		}
	}
	return resp.StatusCode
}

// Post sends a raw body to an unauthenticated path (ingest), returning status and body.
func (e *Env) Post(path, contentType string, body []byte, hdr map[string]string) (int, string) {
	e.T.Helper()
	req, _ := http.NewRequest("POST", e.TS.URL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.T.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}
