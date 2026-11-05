// Package chat records in-game chat and admin messages that BattlEye pushes
// over RCON (for example "(Global) Name: text" or "RCon admin #0: (Global) text")
// and serves them to the live page. Join/leave/kill lines come from the bridge
// event store instead, so they are not duplicated here.
package chat

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/profiles"
	"github.com/kproios/dz-manager/internal/store"
)

// Message is one chat line.
type Message struct {
	ID      int64  `json:"id"`
	TS      string `json:"ts"`
	Channel string `json:"channel"` // global, side, direct, vehicle, group, command, admin
	Name    string `json:"name"`
	Text    string `json:"text"`
}

var (
	reAdmin  = regexp.MustCompile(`^RCon admin #\d+: \((\w+)\) (.*)$`)
	rePlayer = regexp.MustCompile(`^\((Global|Side|Direct|Vehicle|Group|Command|Unknown)\) ([^:]{1,64}): (.*)$`)
)

// Parse turns one RCON server message into a chat message (ok=false if it is not chat).
func Parse(text string) (Message, bool) {
	text = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 {
			return ' '
		}
		return r
	}, text))
	if m := reAdmin.FindStringSubmatch(text); m != nil {
		return Message{Channel: "admin", Name: "Admin", Text: trunc(m[2], 500)}, true
	}
	if m := rePlayer.FindStringSubmatch(text); m != nil {
		return Message{Channel: strings.ToLower(m[1]), Name: trunc(strings.TrimSpace(m[2]), 64), Text: trunc(m[3], 500)}, true
	}
	return Message{}, false
}

func trunc(v string, n int) string {
	if len(v) > n {
		return v[:n]
	}
	return v
}

// Service stores and serves chat.
type Service struct {
	DB       *store.DB
	Profiles *profiles.Store
	Log      *slog.Logger
	Now      func() time.Time
	Publish  func()
}

// OnMessage is wired to the RCON monitor.
func (s *Service) OnMessage(serverID, text string) {
	m, ok := Parse(text)
	if !ok || serverID == "" {
		return
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	ctx := context.Background()
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO chat_messages(server_id,ts,channel,name,text) VALUES(?,?,?,?,?)`, serverID, now().UTC().Format(time.RFC3339), m.Channel, m.Name, m.Text); err != nil {
		if s.Log != nil {
			s.Log.Error("chat insert", "err", err)
		}
		return
	}
	if s.Publish != nil {
		s.Publish()
	}
}

// Run prunes chat older than 30 days once an hour.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		_, _ = s.DB.ExecContext(ctx, `DELETE FROM chat_messages WHERE ts < ?`, time.Now().UTC().Add(-30*24*time.Hour).Format(time.RFC3339))
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Register adds GET /api/chat (newest first, cursor paging with before=id).
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/chat", func(w http.ResponseWriter, r *http.Request) {
		out := []Message{}
		p, _, err := s.Profiles.Active(r.Context())
		if err != nil {
			httpx.JSON(w, 200, out)
			return
		}
		limit := httpx.QueryInt(r, "limit", 50)
		if limit < 1 || limit > 200 {
			limit = 50
		}
		before := httpx.QueryInt(r, "before", 0)
		rows, err := s.DB.QueryContext(r.Context(), `SELECT id,ts,channel,name,text FROM chat_messages WHERE server_id=? AND (?=0 OR id<?) ORDER BY id DESC LIMIT ?`, p.ID, before, before, limit)
		if err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		defer rows.Close()
		for rows.Next() {
			var m Message
			if err := rows.Scan(&m.ID, &m.TS, &m.Channel, &m.Name, &m.Text); err != nil {
				httpx.Error(w, 500, err.Error())
				return
			}
			out = append(out, m)
		}
		httpx.JSON(w, 200, out)
	})
}
