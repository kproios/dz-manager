package hostctl

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kproios/dz-manager/internal/secret"
	"github.com/kproios/dz-manager/internal/store"
)

func TestKeyAndKillCommand(t *testing.T) {
	priv, pub, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(priv, "-----BEGIN EC PRIVATE KEY-----") || !strings.HasPrefix(pub, "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTY") {
		t.Fatalf("key format: %q", pub)
	}
	if fp := Fingerprint(pub); !strings.HasPrefix(fp, "SHA256:") || len(fp) < 40 {
		t.Fatalf("fingerprint %q", fp)
	}
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.OpenPath(ctx, filepath.Join(dir, "dzm.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	box, err := secret.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	s := &Service{DB: db, Box: box, DataDir: dir}
	s.Run = func(_ context.Context, a []string) ([]byte, error) { args = a; return []byte("killed\n"), nil }
	s.Init()
	if _, err := s.Exec(ctx, "kill"); err == nil {
		t.Fatal("kill ran without configuration")
	}
	c := Config{Host: "192.0.2.10", Port: 22, User: "root", KeyEnc: box.SealString(priv, settingsKey), PublicKey: pub}
	if err := s.save(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Exec(ctx, "rm -rf /"); err == nil {
		t.Fatal("arbitrary command accepted")
	}
	out, err := s.Exec(ctx, "kill")
	if err != nil || out != "killed" {
		t.Fatalf("kill: %q %v", out, err)
	}
	if got := strings.Join(args[len(args)-2:], " "); got != "root@192.0.2.10 kill" {
		t.Fatalf("ssh target %q", got)
	}
	if !strings.Contains(c.AuthorizedKeysLine(), `command="`+WrapperPath+`",restrict ecdsa-sha2-nistp256 `) {
		t.Fatalf("authorized_keys line %q", c.AuthorizedKeysLine())
	}
}
