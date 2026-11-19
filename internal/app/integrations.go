package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/kproios/dz-manager/internal/audit"
)

// liveMap is the map:read view: world, live players and vehicles, active events.
func (a *App) liveMap(ctx context.Context) any {
	st := a.MetricZ.State(ctx)
	w := a.World.Active(ctx)
	type event struct {
		Key       string  `json:"key"`
		Category  string  `json:"category"`
		Class     string  `json:"class"`
		X         float64 `json:"x"`
		Z         float64 `json:"z"`
		StartedAt string  `json:"started_at"`
	}
	events := []event{}
	if p, _, err := a.Profiles.Active(ctx); err == nil {
		rows, err := a.DB.QueryContext(ctx, `SELECT key, category, class, COALESCE(x,0), COALESCE(z,0), started_at FROM map_events WHERE server_id=? AND status='active' ORDER BY started_at DESC LIMIT 500`, p.ID)
		if err == nil {
			for rows.Next() {
				var e event
				if rows.Scan(&e.Key, &e.Category, &e.Class, &e.X, &e.Z, &e.StartedAt) == nil {
					events = append(events, e)
				}
			}
			rows.Close()
		}
	}
	return map[string]any{
		"world":       map[string]any{"name": w.World, "label": w.Label, "size": w.Size},
		"available":   st.Available,
		"fresh":       st.Fresh,
		"source":      st.Source,
		"received_at": st.ReceivedAt,
		"players":     st.Players,
		"vehicles":    st.Vehicles,
		"events":      events,
	}
}

// notify shows an on-screen message through @DZMBridge, or broadcasts it over
// RCON when the bridge can't (RCON can only reach everyone, not one player).
func (a *App) notify(ctx context.Context, actor audit.Actor, steam64, text string) error {
	p, _, err := a.Profiles.Active(ctx)
	if err != nil {
		return errors.New("no active server profile")
	}
	target := steam64
	if target == "" {
		target = "all players"
	}
	if a.Bridge.Supports(p.ID, "notify") {
		id, err := a.Bridge.Enqueue(ctx, p.ID, "notify", steam64, map[string]any{"title": "Server", "text": text}, actor, 20*time.Second)
		if err != nil {
			_ = a.Server.Audit.Write(ctx, actor, "message.send", target, map[string]any{"text": text}, "error", err.Error())
			return err
		}
		wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 22*time.Second)
		defer cancel()
		c, err := a.Bridge.Await(wctx, id)
		if err != nil || c == nil {
			_ = a.Server.Audit.Write(ctx, actor, "message.send", target, map[string]any{"text": text}, "error", "no result from the game server")
			return errors.New("no result from the game server")
		}
		if c.Status != "ok" {
			_ = a.Server.Audit.Write(ctx, actor, "message.send", target, map[string]any{"text": text}, "error", c.Error)
			return errors.New(c.Error)
		}
		_ = a.Server.Audit.Write(ctx, actor, "message.send", target, map[string]any{"text": text, "via": "bridge"}, "ok", "")
		return nil
	}
	if steam64 != "" {
		return errors.New("messages to one player need @DZMBridge; without it only broadcasts work")
	}
	clean := strings.Map(func(r rune) rune {
		if r < 32 {
			return ' '
		}
		return r
	}, text)
	if _, _, err := a.RCON.Exec(ctx, "say -1 "+clean); err != nil {
		_ = a.Server.Audit.Write(ctx, actor, "message.send", target, map[string]any{"text": text}, "error", err.Error())
		return err
	}
	_ = a.Server.Audit.Write(ctx, actor, "message.send", target, map[string]any{"text": text, "via": "rcon"}, "ok", "")
	return nil
}
