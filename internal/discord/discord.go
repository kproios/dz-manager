// Package discord posts hook events to Discord channels through webhooks
// (one webhook URL per channel kind, stored encrypted). Delivery is queued and
// rate limited; Discord's 429 retry_after is honoured.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/hooks"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/secret"
	"github.com/kproios/dz-manager/internal/store"
)

// Channel kinds and which hook events they carry.
var Channels = []struct {
	Key, Label, Help string
	Kinds            []string
}{
	{"killfeed", "Kill feed", "Player kills and deaths", []string{hooks.Kill, hooks.Death}},
	{"joins", "Joins & leaves", "Who joined or left", []string{hooks.Join, hooks.Leave}},
	{"raids", "Raids", "Bases under attack, destroyed walls, code lock raids (with grid)", []string{hooks.Raid}},
	{"world", "World events", "Helicrashes, airdrops, convoys spotted (no exact location)", []string{hooks.WorldEvent}},
	{"bans", "Ban list", "Bans and unbans issued by admins", []string{hooks.Ban, hooks.Unban}},
	{"server", "Server status", "Restart warnings, restarts and the server coming back", []string{hooks.Restart, hooks.Server}},
}

func color(kind string) int {
	switch kind {
	case hooks.Kill, hooks.Ban:
		return 0xe34948
	case hooks.Join, hooks.Server:
		return 0x3ccf7e
	case hooks.Raid, hooks.Restart:
		return 0xff5a3c
	case hooks.WorldEvent:
		return 0xf1c40f
	case hooks.Unban:
		return 0x3987e5
	}
	return 0x8a96a3
}

var reWebhook = regexp.MustCompile(`^https://(discord\.com|discordapp\.com|ptb\.discord\.com|canary\.discord\.com)/api/webhooks/\d{5,25}/[A-Za-z0-9_-]{20,100}$`)

const settingsKey = "discord"

// Channel is one configured webhook.
type Channel struct {
	Enabled bool   `json:"enabled"`
	URLEnc  string `json:"url_enc,omitempty"`
}

// Settings are stored as JSON.
type Settings struct {
	Channels map[string]Channel `json:"channels"`
}

type job struct {
	url  string
	body []byte
}

// Service delivers events.
type Service struct {
	DB    *store.DB
	Box   *secret.Box
	Audit *audit.Log
	Log   *slog.Logger
	HTTP  *http.Client

	queue chan job
	mu    sync.Mutex
	cache *Settings
}

// Init starts nothing; call Run in the background.
func (s *Service) Init() {
	if s.HTTP == nil {
		s.HTTP = &http.Client{Timeout: 10 * time.Second}
	}
	s.queue = make(chan job, 300)
}

// Load returns the settings.
func (s *Service) Load(ctx context.Context) Settings {
	s.mu.Lock()
	if s.cache != nil {
		c := *s.cache
		s.mu.Unlock()
		return c
	}
	s.mu.Unlock()
	st := Settings{Channels: map[string]Channel{}}
	var raw string
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, settingsKey).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &st)
	}
	if st.Channels == nil {
		st.Channels = map[string]Channel{}
	}
	s.mu.Lock()
	s.cache = &st
	s.mu.Unlock()
	return st
}

func (s *Service) save(ctx context.Context, st Settings) error {
	b, _ := json.Marshal(st)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, settingsKey, string(b))
	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()
	return err
}

// Embed renders an event as a Discord webhook payload.
func Embed(e hooks.Event) []byte {
	fields := []map[string]any{}
	for _, f := range e.Fields {
		fields = append(fields, map[string]any{"name": f.Name, "value": f.Value, "inline": true})
	}
	embed := map[string]any{"title": trunc(e.Title, 250), "color": color(e.Kind), "timestamp": e.Time.UTC().Format(time.RFC3339)}
	if e.Text != "" {
		embed["description"] = trunc(e.Text, 2000)
	}
	if len(fields) > 0 {
		embed["fields"] = fields
	}
	b, _ := json.Marshal(map[string]any{"username": "DZ Manager", "allowed_mentions": map[string]any{"parse": []string{}}, "embeds": []any{embed}})
	return b
}

func trunc(v string, n int) string {
	if len(v) > n {
		return v[:n]
	}
	return v
}

// OnEvent queues an event for every enabled channel that carries its kind.
func (s *Service) OnEvent(e hooks.Event) {
	st := s.Load(context.Background())
	for _, c := range Channels {
		ch, ok := st.Channels[c.Key]
		if !ok || !ch.Enabled || ch.URLEnc == "" || !contains(c.Kinds, e.Kind) {
			continue
		}
		url, err := s.Box.OpenString(ch.URLEnc, settingsKey+":"+c.Key)
		if err != nil {
			continue
		}
		select {
		case s.queue <- job{url: url, body: Embed(e)}:
		default: // queue full: drop rather than block the game event path
		}
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Run delivers queued messages (about one per second, honouring 429s).
func (s *Service) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-s.queue:
			wait := s.post(ctx, j)
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}
}

func (s *Service) post(ctx context.Context, j job) time.Duration {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.url, bytes.NewReader(j.body))
	if err != nil {
		return time.Second
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.HTTP.Do(req)
	if err != nil {
		if s.Log != nil {
			s.Log.Warn("discord webhook", "err", "request failed")
		}
		return 2 * time.Second
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusTooManyRequests {
		var r struct {
			RetryAfter float64 `json:"retry_after"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&r)
		select {
		case s.queue <- j: // try again after the pause
		default:
		}
		return time.Duration(r.RetryAfter*1000)*time.Millisecond + time.Second
	}
	if res.StatusCode >= 300 && s.Log != nil {
		s.Log.Warn("discord webhook", "status", res.StatusCode)
	}
	return 1100 * time.Millisecond
}

func actor(r *http.Request) audit.Actor {
	if u := auth.FromContext(r.Context()); u != nil {
		return u.Actor()
	}
	return audit.System
}

// Register adds the settings API.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/discord", func(w http.ResponseWriter, r *http.Request) {
		st := s.Load(r.Context())
		out := []map[string]any{}
		for _, c := range Channels {
			ch := st.Channels[c.Key]
			out = append(out, map[string]any{"key": c.Key, "label": c.Label, "help": c.Help, "enabled": ch.Enabled, "has_url": ch.URLEnc != ""})
		}
		httpx.JSON(w, 200, out)
	})
	mux.HandleFunc("PUT /api/settings/discord/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		known := false
		for _, c := range Channels {
			known = known || c.Key == key
		}
		if !known {
			httpx.Error(w, 404, "unknown channel")
			return
		}
		var in struct {
			Enabled bool    `json:"enabled"`
			URL     *string `json:"url"`
		}
		if err := httpx.Decode(r, 4096, &in); err != nil {
			httpx.Error(w, 400, err.Error())
			return
		}
		st := s.Load(r.Context())
		ch := st.Channels[key]
		ch.Enabled = in.Enabled
		if in.URL != nil {
			u := strings.TrimSpace(*in.URL)
			switch {
			case u == "":
				ch.URLEnc, ch.Enabled = "", false
			case !reWebhook.MatchString(u):
				httpx.Error(w, 400, "paste a Discord webhook URL (Channel settings → Integrations → Webhooks → Copy URL)")
				return
			default:
				ch.URLEnc = s.Box.SealString(u, settingsKey+":"+key)
			}
		}
		if ch.Enabled && ch.URLEnc == "" {
			httpx.Error(w, 400, "add the channel's webhook URL first")
			return
		}
		chans := map[string]Channel{}
		for k, v := range st.Channels {
			chans[k] = v
		}
		chans[key] = ch
		if err := s.save(r.Context(), Settings{Channels: chans}); err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		_ = s.Audit.Write(r.Context(), actor(r), "settings.discord_update", key, map[string]any{"enabled": ch.Enabled, "webhook": ch.URLEnc != ""}, "ok", "")
		httpx.JSON(w, 200, map[string]any{"key": key, "enabled": ch.Enabled, "has_url": ch.URLEnc != ""})
	})
	mux.HandleFunc("POST /api/settings/discord/{key}/test", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		ch := s.Load(r.Context()).Channels[key]
		if ch.URLEnc == "" {
			httpx.Error(w, 400, "no webhook URL saved for this channel")
			return
		}
		url, err := s.Box.OpenString(ch.URLEnc, settingsKey+":"+key)
		if err != nil {
			httpx.Error(w, 500, "cannot decrypt the webhook URL")
			return
		}
		body := Embed(hooks.Event{Kind: hooks.Server, Time: time.Now(), Title: "DZ Manager test message", Text: fmt.Sprintf("This channel will receive: %s.", key)})
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := s.HTTP.Do(req)
		if err != nil {
			httpx.Error(w, 502, "Discord did not answer")
			return
		}
		res.Body.Close()
		if res.StatusCode >= 300 {
			httpx.Error(w, 502, fmt.Sprintf("Discord answered HTTP %d (wrong or deleted webhook?)", res.StatusCode))
			return
		}
		httpx.JSON(w, 200, map[string]bool{"ok": true})
	})
}
