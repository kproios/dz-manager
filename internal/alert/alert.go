// Package alert sends operational alerts to an optional webhook
// (Discord-compatible JSON, or a plain-text POST for ntfy and similar).
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Sender posts alerts. A nil or empty-URL sender only logs.
type Sender struct {
	URL    string
	Log    *slog.Logger
	Client *http.Client
}

// Send delivers msg asynchronously; failures are logged, never fatal.
func (s *Sender) Send(msg string) {
	if s == nil {
		return
	}
	if s.Log != nil {
		s.Log.Warn("alert", "message", msg)
	}
	if s.URL == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var body []byte
		ct := "text/plain; charset=utf-8"
		if strings.Contains(s.URL, "discord") {
			body, _ = json.Marshal(map[string]string{"content": "DZ Manager: " + msg})
			ct = "application/json"
		} else {
			body = []byte("DZ Manager: " + msg)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", ct)
		c := s.Client
		if c == nil {
			c = http.DefaultClient
		}
		resp, err := c.Do(req)
		if err != nil {
			if s.Log != nil {
				s.Log.Error("alert delivery failed", "err", err)
			}
			return
		}
		resp.Body.Close()
	}()
}
