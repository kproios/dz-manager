package bans

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/bridge"
)

// Ban lists exported by a server-side adapter through @DZMBridge (for example
// LB Admin Tools' global banlist) arrive in chunks and are stored as an
// external list: evidence for review, never enforced automatically.

type listEntry struct {
	Steam64  string `json:"steam64"`
	Name     string `json:"name"`
	Reason   string `json:"reason"`
	By       string `json:"by"`
	BannedAt int64  `json:"banned_at"`
	Duration int64  `json:"duration"` // seconds, -1 = permanent
}

type listChunk struct {
	List     string      `json:"list"`
	Snapshot string      `json:"snapshot"`
	Part     int         `json:"part"`
	Parts    int         `json:"parts"`
	Entries  []listEntry `json:"entries"`
}

type pendingList struct {
	parts   map[int][]listEntry
	total   int
	started time.Time
}

var chunkMu sync.Mutex

var pending = map[string]*pendingList{}

// AttachBridge subscribes to exported ban list chunks.
func (s *Service) AttachBridge(b *bridge.Bridge) { b.Subscribe(s.onListChunk, "banlist.chunk") }

func (s *Service) onListChunk(ctx context.Context, ev bridge.Event) {
	var c listChunk
	if json.Unmarshal(ev.Data, &c) != nil || c.Snapshot == "" || c.Parts < 1 || c.Parts > 500 || c.Part < 0 || c.Part >= c.Parts {
		return
	}
	name := clean(c.List, 80)
	if name == "" {
		return
	}
	key := ev.ServerID + "|" + name + "|" + c.Snapshot
	chunkMu.Lock()
	for k, p := range pending { // drop abandoned snapshots
		if time.Since(p.started) > 30*time.Minute {
			delete(pending, k)
		}
	}
	p := pending[key]
	if p == nil {
		p = &pendingList{parts: map[int][]listEntry{}, total: c.Parts, started: time.Now()}
		pending[key] = p
	}
	p.parts[c.Part] = c.Entries
	done := len(p.parts) == p.total
	if done {
		delete(pending, key)
	}
	chunkMu.Unlock()
	if !done {
		return
	}
	now := s.now().UTC()
	entries := []Entry{}
	for i := 0; i < p.total; i++ {
		for _, e := range p.parts[i] {
			if !reSteam.MatchString(e.Steam64) {
				continue
			}
			banned := time.Unix(e.BannedAt, 0).UTC()
			if e.Duration > 0 && banned.Add(time.Duration(e.Duration)*time.Second).Before(now) {
				continue // expired
			}
			length := "permanent"
			if e.Duration > 0 {
				length = fmt.Sprintf("%dd", (e.Duration+86399)/86400)
			}
			reason := clean(e.Reason, 200)
			if reason == "" {
				reason = "no reason given"
			}
			if e.By != "" {
				reason += " · by " + clean(e.By, 40)
			}
			reason += " · " + length
			listed := ""
			if e.BannedAt > 0 {
				listed = banned.Format(time.RFC3339)
			}
			entries = append(entries, Entry{Identifier: e.Steam64, Type: "steam64", Reason: reason, ListedAt: listed})
		}
	}
	if _, err := s.ReplaceList(ctx, name, "exported by the game server through @DZMBridge", entries); err != nil {
		return
	}
	_ = s.Audit.Write(ctx, audit.System, "banlist.sync", name, map[string]any{"entries": len(entries)}, "ok", "")
}
