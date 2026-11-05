// Package feeds turns game events from @DZMBridge into public hook events
// (kill feed, joins, raids, world events, server starts).
package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kproios/dz-manager/internal/bridge"
	"github.com/kproios/dz-manager/internal/hooks"
	"github.com/kproios/dz-manager/internal/store"
)

// Feeds publishes bridge events to the hook bus.
type Feeds struct {
	DB  *store.DB
	Bus *hooks.Bus

	mu   sync.Mutex
	last map[string]time.Time
}

var worldNames = map[string]string{"heli_crash": "Helicrash", "plane_crash": "Aircraft crash", "airdrop": "Airdrop", "military_convoy": "Military convoy", "train": "Train"}

// Attach subscribes to the bridge.
func (f *Feeds) Attach(b *bridge.Bridge) {
	b.Subscribe(f.onDeath, "player.death")
	b.Subscribe(f.onJoin, "player.connect", "player.disconnect")
	b.Subscribe(f.onRaid, "raid.hit", "raid.destroyed", "raid.codelock")
	b.Subscribe(f.onWorld, "event.start")
	b.Subscribe(f.onServer, "server.start")
}

func (f *Feeds) name(ctx context.Context, steam64 string) string {
	var n string
	_ = f.DB.QueryRowContext(ctx, `SELECT name FROM players WHERE steam64=?`, steam64).Scan(&n)
	if n == "" {
		return "a survivor"
	}
	return n
}

// Grid is the 3-digit map grid reference (same as the panel).
func Grid(x, z float64) string {
	p := func(n float64) string {
		if n < 0 {
			n = 0
		}
		return fmt.Sprintf("%03d", int(n/100))
	}
	return p(x) + " " + p(15360-z)
}

func (f *Feeds) onDeath(ctx context.Context, ev bridge.Event) {
	var d struct {
		Weapon   string  `json:"weapon"`
		Cause    string  `json:"cause"`
		Distance float64 `json:"distance_m"`
	}
	_ = json.Unmarshal(ev.Data, &d)
	victim := f.name(ctx, ev.Actor)
	if ev.Target == "" {
		cause := d.Cause
		if cause == "" {
			cause = "unknown causes"
		}
		f.Bus.Publish(hooks.Event{Kind: hooks.Death, Time: ev.Time, Title: victim + " died", Text: "Cause: " + cause})
		return
	}
	killer := f.name(ctx, ev.Target)
	fields := []hooks.Field{{Name: "Weapon", Value: orDash(d.Weapon)}, {Name: "Distance", Value: fmt.Sprintf("%.0f m", d.Distance)}}
	f.Bus.Publish(hooks.Event{Kind: hooks.Kill, Time: ev.Time, Title: killer + " killed " + victim, Fields: fields})
}

func (f *Feeds) onJoin(ctx context.Context, ev bridge.Event) {
	var d struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(ev.Data, &d)
	n := d.Name
	if n == "" {
		n = f.name(ctx, ev.Actor)
	}
	if ev.Type == "player.connect" {
		f.Bus.Publish(hooks.Event{Kind: hooks.Join, Time: ev.Time, Title: n + " joined"})
		return
	}
	f.Bus.Publish(hooks.Event{Kind: hooks.Leave, Time: ev.Time, Title: n + " left"})
}

func (f *Feeds) onRaid(ctx context.Context, ev bridge.Event) {
	var d struct {
		Class string `json:"class"`
	}
	_ = json.Unmarshal(ev.Data, &d)
	what := map[string]string{"raid.hit": "A base is under attack", "raid.destroyed": "A base part was destroyed", "raid.codelock": "A code lock is being raided"}[ev.Type]
	fields := []hooks.Field{{Name: "Structure", Value: orDash(d.Class)}}
	if ev.Pos != nil {
		fields = append(fields, hooks.Field{Name: "Grid", Value: Grid(ev.Pos.X, ev.Pos.Z)})
	}
	f.Bus.Publish(hooks.Event{Kind: hooks.Raid, Time: ev.Time, Title: what, Fields: fields})
}

func (f *Feeds) onWorld(ctx context.Context, ev bridge.Event) {
	var d struct {
		Category string `json:"category"`
	}
	_ = json.Unmarshal(ev.Data, &d)
	label, ok := worldNames[d.Category]
	if !ok {
		return // items, police wrecks, bases and other noise stay private
	}
	// A restart re-detects every existing wreck: announce each kind at most every 15 minutes.
	f.mu.Lock()
	if f.last == nil {
		f.last = map[string]time.Time{}
	}
	if t, seen := f.last[d.Category]; seen && ev.Time.Sub(t) < 15*time.Minute {
		f.mu.Unlock()
		return
	}
	f.last[d.Category] = ev.Time
	f.mu.Unlock()
	f.Bus.Publish(hooks.Event{Kind: hooks.WorldEvent, Time: ev.Time, Title: label + " spotted", Text: "Somewhere in Chernarus. Good luck!"})
}

func (f *Feeds) onServer(ctx context.Context, ev bridge.Event) {
	f.Bus.Publish(hooks.Event{Kind: hooks.Server, Time: ev.Time, Title: "Server is up", Text: "The server has started and is accepting players."})
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
