// Package automsg sends owner-configured automatic messages: a personal welcome
// when a player joins (%playername%) and rotating tips/announcements to
// everyone. Messages go out as RCON chat, or as an on-screen notification
// through @DZMBridge's "notify" handler when the connected bridge offers it.
package automsg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/bridge"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/profiles"
	"github.com/kproios/dz-manager/internal/rcon"
	"github.com/kproios/dz-manager/internal/store"
	"github.com/woozymasta/dzid"
)

// Channels.
const (
	ChannelRCON   = "rcon"   // BattlEye chat line
	ChannelNotify = "notify" // on-screen notification via @DZMBridge
)

// Allowed fixed choices (no free-form timing).
var (
	WelcomeDelays    = []int{15, 30, 60, 120}
	RotationMinutes  = []int{5, 10, 15, 30, 60, 120}
	settingsKey      = "auto_messages"
	errNotifyMissing = errors.New("the connected @DZMBridge does not offer on-screen notifications yet")
)

// Welcome greets one player after they join.
type Welcome struct {
	Enabled      bool   `json:"enabled"`
	Text         string `json:"text"`
	DelaySeconds int    `json:"delay_seconds"`
}

// Rotation broadcasts tips in turn.
type Rotation struct {
	Enabled         bool     `json:"enabled"`
	IntervalMinutes int      `json:"interval_minutes"`
	Messages        []string `json:"messages"`
	OnlyWithPlayers bool     `json:"only_with_players"`
}

// Config is stored as JSON in settings.
type Config struct {
	Channel  string   `json:"channel"`
	Welcome  Welcome  `json:"welcome"`
	Rotation Rotation `json:"rotation"`
}

// Defaults returns a disabled starter configuration.
func Defaults() Config {
	var c Config
	c.Channel = ChannelRCON
	c.Welcome.Text = "Welcome to the server, %playername%! Have fun and play fair."
	c.Welcome.DelaySeconds = 60
	c.Rotation.IntervalMinutes = 30
	c.Rotation.OnlyWithPlayers = true
	c.Rotation.Messages = []string{"Join our Discord for news and support.", "Respect the rules: no combat logging, no exploits."}
	return c
}

func oneOf(v int, set []int) bool {
	for _, x := range set {
		if x == v {
			return true
		}
	}
	return false
}

func cleanLine(s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("messages cannot be empty")
	}
	if strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return "", errors.New("messages must be a single line")
	}
	if len([]rune(s)) > max {
		return "", fmt.Errorf("messages are limited to %d characters", max)
	}
	return s, nil
}

// Validate normalises a configuration.
func (c *Config) Validate() error {
	if c.Channel == "" {
		c.Channel = ChannelRCON
	}
	if c.Channel != ChannelRCON && c.Channel != ChannelNotify {
		return errors.New("channel must be rcon or notify")
	}
	if !oneOf(c.Welcome.DelaySeconds, WelcomeDelays) {
		return fmt.Errorf("welcome delay must be one of %v seconds", WelcomeDelays)
	}
	if !oneOf(c.Rotation.IntervalMinutes, RotationMinutes) {
		return fmt.Errorf("rotation interval must be one of %v minutes", RotationMinutes)
	}
	var err error
	if c.Welcome.Text, err = cleanLine(c.Welcome.Text, 200); err != nil && c.Welcome.Enabled {
		return fmt.Errorf("welcome: %w", err)
	}
	if len(c.Rotation.Messages) > 20 {
		return errors.New("at most 20 rotating messages")
	}
	msgs := make([]string, 0, len(c.Rotation.Messages))
	for _, m := range c.Rotation.Messages {
		m, err := cleanLine(m, 200)
		if err != nil {
			return fmt.Errorf("rotation: %w", err)
		}
		msgs = append(msgs, m)
	}
	c.Rotation.Messages = msgs
	if c.Rotation.Enabled && len(msgs) == 0 {
		return errors.New("add at least one rotating message or turn rotation off")
	}
	return nil
}

// Fill replaces the supported placeholders.
func Fill(text, player string, online int) string {
	return strings.NewReplacer("%playername%", player, "%players%", strconv.Itoa(online), "%online%", strconv.Itoa(online)).Replace(text)
}

// Service runs the welcome and rotation.
type Service struct {
	DB       *store.DB
	Profiles *profiles.Store
	Bridge   *bridge.Bridge
	Audit    *audit.Log
	Log      *slog.Logger
	Exec     func(ctx context.Context, cmd string) error
	Roster   func(ctx context.Context) ([]rcon.Player, error)
	Now      func() time.Time
	// After schedules f after d (time.AfterFunc; replaced in tests).
	After func(d time.Duration, f func())

	mu       sync.Mutex
	welcomed map[string]time.Time
	lastRot  time.Time
	rotIdx   int
}

// Init sets defaults.
func (s *Service) Init() {
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.After == nil {
		s.After = func(d time.Duration, f func()) { time.AfterFunc(d, f) }
	}
	s.welcomed = map[string]time.Time{}
}

// Load returns the stored configuration (defaults when none).
func (s *Service) Load(ctx context.Context) Config {
	var raw string
	c := Defaults()
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, settingsKey).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &c)
	}
	return c
}

// Attach subscribes to joins.
func (s *Service) Attach(b *bridge.Bridge) { b.Subscribe(s.onConnect, "player.connect") }

func (s *Service) onConnect(ctx context.Context, ev bridge.Event) {
	c := s.Load(ctx)
	if !c.Welcome.Enabled || ev.Actor == "" {
		return
	}
	s.mu.Lock()
	if t, ok := s.welcomed[ev.Actor]; ok && s.Now().Sub(t) < 10*time.Minute {
		s.mu.Unlock()
		return // reconnect loop or duplicate event
	}
	s.welcomed[ev.Actor] = s.Now()
	if len(s.welcomed) > 1000 {
		s.welcomed = map[string]time.Time{ev.Actor: s.Now()}
	}
	s.mu.Unlock()
	var d struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(ev.Data, &d)
	steam, server := ev.Actor, ev.ServerID
	// Players are still loading right after connect: wait the configured delay.
	s.After(time.Duration(c.Welcome.DelaySeconds)*time.Second, func() {
		if err := s.welcome(context.Background(), c, server, steam, d.Name); err != nil && s.Log != nil {
			s.Log.Warn("welcome message", "err", err)
		}
	})
}

func (s *Service) welcome(ctx context.Context, c Config, serverID, steam64, name string) error {
	if c.Channel == ChannelNotify {
		if s.Bridge == nil || !s.Bridge.Supports(serverID, "notify") {
			return errNotifyMissing
		}
		text := Fill(c.Welcome.Text, name, 0)
		_, err := s.Bridge.Enqueue(ctx, serverID, "notify", steam64, map[string]any{"title": "Welcome", "text": text}, audit.System, time.Minute)
		return err
	}
	id, err := strconv.ParseUint(steam64, 10, 64)
	if err != nil {
		return err
	}
	guid := dzid.BattlEyeString(id)
	ps, err := s.Roster(ctx)
	if err != nil {
		return err
	}
	for _, p := range ps {
		if strings.EqualFold(p.BEGUID, guid) {
			if name == "" {
				name = p.Name
			}
			return s.Exec(ctx, fmt.Sprintf("say %d %s", p.Number, Fill(c.Welcome.Text, name, len(ps))))
		}
	}
	return errors.New("player left before the welcome was sent")
}

// Run sends rotating messages on the configured interval.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.Tick(ctx)
	}
}

// Tick sends the next rotating message when it is due.
func (s *Service) Tick(ctx context.Context) {
	c := s.Load(ctx)
	if !c.Rotation.Enabled || len(c.Rotation.Messages) == 0 {
		return
	}
	s.mu.Lock()
	due := s.lastRot.IsZero() || s.Now().Sub(s.lastRot) >= time.Duration(c.Rotation.IntervalMinutes)*time.Minute
	s.mu.Unlock()
	if !due {
		return
	}
	online := -1
	if ps, err := s.Roster(ctx); err == nil {
		online = len(ps)
	}
	if c.Rotation.OnlyWithPlayers && online <= 0 {
		return
	}
	s.mu.Lock()
	s.lastRot = s.Now()
	msg := c.Rotation.Messages[s.rotIdx%len(c.Rotation.Messages)]
	s.rotIdx++
	s.mu.Unlock()
	if err := s.broadcast(ctx, c, Fill(msg, "survivors", max(online, 0))); err != nil && s.Log != nil {
		s.Log.Warn("rotating message", "err", err)
	}
}

func (s *Service) broadcast(ctx context.Context, c Config, text string) error {
	if c.Channel == ChannelNotify {
		p, _, err := s.Profiles.Active(ctx)
		if err != nil {
			return err
		}
		if s.Bridge == nil || !s.Bridge.Supports(p.ID, "notify") {
			return errNotifyMissing
		}
		_, err = s.Bridge.Enqueue(ctx, p.ID, "notify", "", map[string]any{"title": "Server", "text": text}, audit.System, time.Minute)
		return err
	}
	return s.Exec(ctx, "say -1 "+text)
}

// Register adds the settings API.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/messages", func(w http.ResponseWriter, r *http.Request) {
		notify := false
		if p, _, err := s.Profiles.Active(r.Context()); err == nil && s.Bridge != nil {
			notify = s.Bridge.Supports(p.ID, "notify")
		}
		httpx.JSON(w, 200, map[string]any{"config": s.Load(r.Context()), "notify_available": notify,
			"welcome_delays": WelcomeDelays, "rotation_minutes": RotationMinutes})
	})
	mux.HandleFunc("PUT /api/settings/messages", func(w http.ResponseWriter, r *http.Request) {
		var c Config
		if err := httpx.Decode(r, 16<<10, &c); err != nil {
			httpx.Error(w, 400, err.Error())
			return
		}
		if err := c.Validate(); err != nil {
			httpx.Error(w, 400, err.Error())
			return
		}
		b, _ := json.Marshal(c)
		if _, err := s.DB.ExecContext(r.Context(), `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, settingsKey, string(b)); err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		a := audit.System
		if u := auth.FromContext(r.Context()); u != nil {
			a = u.Actor()
		}
		_ = s.Audit.Write(r.Context(), a, "settings.messages_update", "", map[string]any{"welcome": c.Welcome.Enabled, "rotation": c.Rotation.Enabled, "interval_minutes": c.Rotation.IntervalMinutes, "channel": c.Channel}, "ok", "")
		httpx.JSON(w, 200, c)
	})
	mux.HandleFunc("POST /api/settings/messages/send", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text string `json:"text"`
		}
		if err := httpx.Decode(r, 4<<10, &in); err != nil {
			httpx.Error(w, 400, err.Error())
			return
		}
		text, err := cleanLine(in.Text, 200)
		if err != nil {
			httpx.Error(w, 400, err.Error())
			return
		}
		online := 0
		if ps, err := s.Roster(r.Context()); err == nil {
			online = len(ps)
		}
		if err := s.broadcast(r.Context(), s.Load(r.Context()), Fill(text, "survivors", online)); err != nil {
			httpx.Error(w, 502, err.Error())
			return
		}
		a := audit.System
		if u := auth.FromContext(r.Context()); u != nil {
			a = u.Actor()
		}
		_ = s.Audit.Write(r.Context(), a, "rcon.broadcast", "all", map[string]any{"message": text}, "ok", "sent from automatic messages")
		httpx.JSON(w, 200, map[string]bool{"ok": true})
	})
}
