// Package smoke builds the real dzm binary and exercises it end to end:
// start → /ready → SSO login → API → audit row → restart → persistence.
package smoke

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kproios/dz-manager/internal/apptest"
	"github.com/kproios/dz-manager/internal/auth/oidctest"
)

func build(t *testing.T) string {
	bin := filepath.Join(t.TempDir(), "dzm")
	cmd := exec.Command("go", "build", "-ldflags", "-X main.version=v9.9.9-smoke", "-o", bin, "../../cmd/dzm")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func freeAddr(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func start(t *testing.T, bin string, env map[string]string) (*exec.Cmd, *strings.Builder) {
	cmd := exec.Command(bin, "serve")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	return cmd, &out
}

func waitReady(t *testing.T, url string) map[string]string {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url + "/ready")
		if err == nil {
			var m map[string]string
			json.NewDecoder(resp.Body).Decode(&m)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return m
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("not ready in time")
	return nil
}

func TestSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := build(t)
	if out, err := exec.Command(bin, "version").Output(); err != nil || strings.TrimSpace(string(out)) != "v9.9.9-smoke" {
		t.Fatalf("version: %q %v", out, err)
	}
	// Bad config fails closed.
	bad := exec.Command(bin, "serve")
	bad.Env = []string{"DZM_PUBLIC_URL=https://x"}
	if out, err := bad.CombinedOutput(); err == nil || !strings.Contains(string(out), "DZM_SECRET_KEY") {
		t.Fatalf("missing config must fail: %v %s", err, out)
	}

	is := oidctest.New()
	defer is.Close()
	addr := freeAddr(t)
	url := "http://" + addr
	data := t.TempDir()
	env := apptest.EnvVars(data, url, is.URL(), is.ClientID, is.Secret)
	env["DZM_LISTEN"] = addr
	cmd, out := start(t, bin, env)
	if r := waitReady(t, url); r["version"] != "v9.9.9-smoke" {
		t.Fatalf("ready: %v", r)
	}
	t.Logf("ready after start")

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	if resp, _ := http.Get(url + "/api/status"); resp.StatusCode != 401 {
		t.Fatalf("unauthenticated api = %d", resp.StatusCode)
	}
	resp, err := c.Get(url + "/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(page), "DZ Manager") {
		t.Fatalf("landing page after login = %d %s", resp.StatusCode, page)
	}
	var me struct{ CSRF string }
	resp, _ = c.Get(url + "/api/me")
	json.NewDecoder(resp.Body).Decode(&me)
	resp.Body.Close()
	req, _ := http.NewRequest("POST", url+"/api/profiles", strings.NewReader(`{"name":"TestServer","provider_kind":"selfhosted","game_host":"192.0.2.10"}`))
	req.Header.Set("Origin", url)
	req.Header.Set("X-DZM-CSRF", me.CSRF)
	resp, _ = c.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("create profile = %d", resp.StatusCode)
	}

	// Graceful stop (SIGTERM) and restart: data persists, schema guard passes.
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { _, err := cmd.Process.Wait(); done <- err }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatalf("no graceful shutdown; log:\n%s", out.String())
	}
	start(t, bin, env)
	waitReady(t, url)
	resp, _ = c.Get(url + "/api/profiles")
	var ps []map[string]any
	json.NewDecoder(resp.Body).Decode(&ps)
	resp.Body.Close()
	if len(ps) != 1 || ps[0]["name"] != "TestServer" {
		t.Fatalf("profile lost across restart (session must survive too): %d %v", resp.StatusCode, ps)
	}
	resp, _ = c.Get(url + "/api/audit")
	var log []map[string]any
	json.NewDecoder(resp.Body).Decode(&log)
	resp.Body.Close()
	if len(log) < 2 {
		t.Fatalf("audit rows lost: %v", log)
	}
	_ = context.Background()
}
