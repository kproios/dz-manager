// Package hooks is the in-process bus for everything worth telling the
// outside world: kills, joins, raids, world events, bans, restarts. Discord
// webhooks and the public bot API (/public/v1/feed) both read from it, so a
// new producer automatically reaches every integration.
package hooks

import (
	"sync"
	"time"
)

// Kinds.
const (
	Kill       = "kill"
	Death      = "death"
	Join       = "join"
	Leave      = "leave"
	Raid       = "raid"
	WorldEvent = "world_event"
	Ban        = "ban"
	Unban      = "unban"
	Restart    = "restart"
	Server     = "server"
)

// Field is one labelled value.
type Field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Event is one public happening.
type Event struct {
	ID     int64     `json:"id"`
	Kind   string    `json:"kind"`
	Time   time.Time `json:"time"`
	Title  string    `json:"title"`
	Text   string    `json:"text,omitempty"`
	Fields []Field   `json:"fields,omitempty"`
}

// Bus fans events out to subscribers and keeps the last 500 for polling.
type Bus struct {
	mu   sync.RWMutex
	subs []func(Event)
	ring []Event
	seq  int64
}

// Subscribe registers f; it must not block (queue work instead).
func (b *Bus) Subscribe(f func(Event)) {
	b.mu.Lock()
	b.subs = append(b.subs, f)
	b.mu.Unlock()
}

// Publish stamps and delivers an event.
func (b *Bus) Publish(e Event) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.seq++
	e.ID = b.seq
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	b.ring = append(b.ring, e)
	if len(b.ring) > 500 {
		b.ring = b.ring[len(b.ring)-500:]
	}
	subs := append([]func(Event)(nil), b.subs...)
	b.mu.Unlock()
	for _, f := range subs {
		f(e)
	}
}

// After returns buffered events with ID > after (oldest first, at most limit).
func (b *Bus) After(after int64, limit int) []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := []Event{}
	for _, e := range b.ring {
		if e.ID > after {
			out = append(out, e)
			if len(out) == limit {
				break
			}
		}
	}
	return out
}
