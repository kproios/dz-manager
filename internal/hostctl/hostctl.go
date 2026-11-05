// Package hostctl is the emergency "kill the game server process" control for
// a frozen server that no longer answers RCON. It runs the system ssh client
// with a key DZ Manager generates (ECDSA P-256, stored encrypted). The game
// host pins that key to one wrapper script (authorized_keys command=...), so
// the key can only ask for "status" or "kill" and nothing else. The provider
// (Architect) then relaunches the process as it does after a crash.
package hostctl

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/secret"
	"github.com/kproios/dz-manager/internal/store"
)

// WrapperPath is where the game host keeps the command wrapper.
const WrapperPath = "/usr/local/bin/dzm-host-ctl"

const settingsKey = "host_control"

var reHost = regexp.MustCompile(`^[A-Za-z0-9.-]{1,253}$`)
var reUser = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// Config is stored in settings (private key encrypted).
type Config struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	User      string `json:"user"`
	KeyEnc    string `json:"key_enc,omitempty"`
	PublicKey string `json:"public_key,omitempty"`
}

// Service runs ssh.
type Service struct {
	DB      *store.DB
	Box     *secret.Box
	Audit   *audit.Log
	DataDir string
	// Run executes ssh (replaced in tests).
	Run func(ctx context.Context, args []string) ([]byte, error)

	mu sync.Mutex
}

// Init sets defaults.
func (s *Service) Init() {
	if s.Run == nil {
		s.Run = func(ctx context.Context, args []string) ([]byte, error) {
			return exec.CommandContext(ctx, "ssh", args...).CombinedOutput()
		}
	}
}

// Load returns the configuration.
func (s *Service) Load(ctx context.Context) Config {
	c := Config{Port: 22, User: "root"}
	var raw string
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, settingsKey).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &c)
	}
	return c
}

func (s *Service) save(ctx context.Context, c Config) error {
	b, _ := json.Marshal(c)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, settingsKey, string(b))
	return err
}

// Ready reports whether the kill control is configured.
func (c Config) Ready() bool { return c.Host != "" && c.KeyEnc != "" }

// AuthorizedKeysLine is what the owner adds on the game host.
func (c Config) AuthorizedKeysLine() string {
	if c.PublicKey == "" {
		return ""
	}
	return `command="` + WrapperPath + `",restrict ` + c.PublicKey + " dz-manager-host-control"
}

// WrapperScript is installed at WrapperPath on the game host.
func WrapperScript(processMatch string) string {
	return `#!/bin/sh
# DZ Manager host control: the only commands its SSH key may run.
case "$SSH_ORIGINAL_COMMAND" in
  status) pgrep -af '` + processMatch + `' >/dev/null && echo running || echo stopped ;;
  kill) if pkill -KILL -f '` + processMatch + `'; then echo killed; else echo "not running"; fi ;;
  *) echo "unsupported command" >&2; exit 2 ;;
esac
`
}

// GenerateKey creates a new ECDSA P-256 key pair (PEM private key, OpenSSH public key).
func GenerateKey() (privatePEM, publicLine string, err error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	der, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		return "", "", err
	}
	privatePEM = string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
	pub, err := k.PublicKey.ECDH()
	if err != nil {
		return "", "", err
	}
	return privatePEM, "ecdsa-sha2-nistp256 " + base64.StdEncoding.EncodeToString(sshECDSABlob(pub)), nil
}

func sshECDSABlob(pub *ecdh.PublicKey) []byte {
	var b bytes.Buffer
	for _, part := range [][]byte{[]byte("ecdsa-sha2-nistp256"), []byte("nistp256"), pub.Bytes()} {
		_ = binary.Write(&b, binary.BigEndian, uint32(len(part)))
		b.Write(part)
	}
	return b.Bytes()
}

// Fingerprint is the OpenSSH SHA256 fingerprint of a public key line.
func Fingerprint(publicLine string) string {
	f := strings.Fields(publicLine)
	if len(f) < 2 {
		return ""
	}
	blob, err := base64.StdEncoding.DecodeString(f[1])
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(blob)
	return "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:]), "=")
}

// Exec runs one allowed command ("status" or "kill") on the game host.
func (s *Service) Exec(ctx context.Context, command string) (string, error) {
	if command != "status" && command != "kill" {
		return "", errors.New("unsupported command")
	}
	c := s.Load(ctx)
	if !c.Ready() {
		return "", errors.New("host control is not set up (Settings → Server)")
	}
	key, err := s.Box.OpenString(c.KeyEnc, settingsKey)
	if err != nil {
		return "", errors.New("cannot decrypt the host key")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.DataDir, "hostctl")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	keyFile := filepath.Join(dir, "id_ecdsa")
	if err := os.WriteFile(keyFile, []byte(key), 0o600); err != nil {
		return "", err
	}
	defer os.Remove(keyFile)
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := s.Run(cctx, []string{"-i", keyFile, "-p", fmt.Sprint(c.Port),
		"-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes", "-o", "ConnectTimeout=8",
		"-o", "StrictHostKeyChecking=accept-new", "-o", "UserKnownHostsFile=" + filepath.Join(dir, "known_hosts"),
		c.User + "@" + c.Host, command})
	text := strings.TrimSpace(string(out))
	if len(text) > 300 {
		text = text[:300]
	}
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, errors.New("ssh failed: " + text)
	}
	return text, nil
}

func actor(r *http.Request) audit.Actor {
	if u := auth.FromContext(r.Context()); u != nil {
		return u.Actor()
	}
	return audit.System
}

// Register adds the API.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/host", func(w http.ResponseWriter, r *http.Request) {
		c := s.Load(r.Context())
		httpx.JSON(w, 200, s.view(c))
	})
	mux.HandleFunc("PUT /api/settings/host", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Host string `json:"host"`
			Port int    `json:"port"`
			User string `json:"user"`
		}
		if err := httpx.Decode(r, 2048, &in); err != nil {
			httpx.Error(w, 400, err.Error())
			return
		}
		in.Host, in.User = strings.TrimSpace(in.Host), strings.TrimSpace(in.User)
		if !reHost.MatchString(in.Host) || in.Port < 1 || in.Port > 65535 || !reUser.MatchString(in.User) {
			httpx.Error(w, 400, "enter a host name or IP, a port 1-65535 and a Linux user name")
			return
		}
		c := s.Load(r.Context())
		c.Host, c.Port, c.User = in.Host, in.Port, in.User
		if err := s.save(r.Context(), c); err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		_ = s.Audit.Write(r.Context(), actor(r), "settings.host_update", c.Host, map[string]any{"port": c.Port, "user": c.User}, "ok", "")
		httpx.JSON(w, 200, s.view(c))
	})
	mux.HandleFunc("POST /api/settings/host/key", func(w http.ResponseWriter, r *http.Request) {
		priv, pub, err := GenerateKey()
		if err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		c := s.Load(r.Context())
		c.KeyEnc, c.PublicKey = s.Box.SealString(priv, settingsKey), pub
		if err := s.save(r.Context(), c); err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		_ = s.Audit.Write(r.Context(), actor(r), "settings.host_key", c.Host, map[string]any{"fingerprint": Fingerprint(pub)}, "ok", "new key; the old one stops working")
		httpx.JSON(w, 200, s.view(c))
	})
	mux.HandleFunc("POST /api/server/host-status", func(w http.ResponseWriter, r *http.Request) {
		out, err := s.Exec(r.Context(), "status")
		if err != nil {
			httpx.Error(w, 502, err.Error())
			return
		}
		httpx.JSON(w, 200, map[string]string{"status": out})
	})
	mux.HandleFunc("POST /api/server/kill", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Confirm string `json:"confirm"`
		}
		if err := httpx.Decode(r, 1024, &in); err != nil {
			httpx.Error(w, 400, err.Error())
			return
		}
		if in.Confirm != "KILL" {
			httpx.Error(w, 400, "type KILL to confirm")
			return
		}
		out, err := s.Exec(r.Context(), "kill")
		res := "ok"
		if err != nil {
			res = "error"
		}
		_ = s.Audit.Write(r.Context(), actor(r), "server.kill", s.Load(r.Context()).Host, nil, res, out)
		if err != nil {
			httpx.Error(w, 502, err.Error())
			return
		}
		httpx.JSON(w, 200, map[string]string{"result": out})
	})
}

func (s *Service) view(c Config) map[string]any {
	return map[string]any{"host": c.Host, "port": c.Port, "user": c.User, "ready": c.Ready(),
		"authorized_keys_line": c.AuthorizedKeysLine(), "fingerprint": Fingerprint(c.PublicKey), "wrapper_path": WrapperPath,
		"wrapper_script": WrapperScript("DayZServer -config")}
}
