package automsg

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kproios/dz-manager/internal/bridge"
	"github.com/kproios/dz-manager/internal/rcon"
	"github.com/kproios/dz-manager/internal/store"
	"github.com/woozymasta/dzid"
)

func TestValidateAndFill(t *testing.T) {
	c := Defaults()
	c.Welcome.Enabled = true
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Rotation.IntervalMinutes = 7
	if err := c.Validate(); err == nil {
		t.Fatal("interval outside the fixed choices accepted")
	}
	c = Defaults()
	c.Rotation.Messages = []string{"two\nlines"}
	if err := c.Validate(); err == nil {
		t.Fatal("multi-line message accepted")
	}
	if got := Fill("Welcome %playername%! %players% online", "Alice", 3); got != "Welcome Alice! 3 online" {
		t.Fatalf("fill %q", got)
	}
}

func TestWelcomeAndRotation(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenPath(ctx, filepath.Join(t.TempDir(), "dzm.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const steam = "76561198000000001"
	guid := dzid.BattlEyeString(76561198000000001)
	var sent []string
	var later []func()
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	s := &Service{DB: db}
	s.Now = func() time.Time { return now }
	s.Exec = func(_ context.Context, cmd string) error { sent = append(sent, cmd); return nil }
	s.Roster = func(context.Context) ([]rcon.Player, error) {
		return []rcon.Player{{Number: 4, Name: "Alice", BEGUID: guid}}, nil
	}
	s.After = func(_ time.Duration, f func()) { later = append(later, f) }
	s.Init()
	c := Defaults()
	c.Welcome.Enabled, c.Rotation.Enabled, c.Rotation.IntervalMinutes = true, true, 5
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	b := `{"channel":"rcon","welcome":{"enabled":true,"text":"Welcome %playername%!","delay_seconds":60},"rotation":{"enabled":true,"interval_minutes":5,"messages":["Tip one","Tip two"],"only_with_players":true}}`
	if _, err := db.Exec(`INSERT INTO settings(key,value) VALUES('auto_messages',?)`, b); err != nil {
		t.Fatal(err)
	}
	ev := bridge.Event{Type: "player.connect", Actor: steam, ServerID: "srv", Data: []byte(`{"name":"Alice"}`)}
	s.onConnect(ctx, ev)
	s.onConnect(ctx, ev) // duplicate join within 10 minutes: one welcome only
	if len(later) != 1 {
		t.Fatalf("scheduled %d welcomes", len(later))
	}
	later[0]()
	s.Tick(ctx)
	now = now.Add(2 * time.Minute)
	s.Tick(ctx) // not due yet
	now = now.Add(4 * time.Minute)
	s.Tick(ctx)
	want := "say 4 Welcome Alice!|say -1 Tip one|say -1 Tip two"
	if got := strings.Join(sent, "|"); got != want {
		t.Fatalf("sent %q, want %q", got, want)
	}
}
