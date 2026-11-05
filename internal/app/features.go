package app

import (
	"context"

	"github.com/kproios/dz-manager/internal/actions"
	"github.com/kproios/dz-manager/internal/alert"
	"github.com/kproios/dz-manager/internal/automsg"
	"github.com/kproios/dz-manager/internal/bans"
	"github.com/kproios/dz-manager/internal/bridge"
	"github.com/kproios/dz-manager/internal/chat"
	"github.com/kproios/dz-manager/internal/intel"
	"github.com/kproios/dz-manager/internal/mapevents"
	"github.com/kproios/dz-manager/internal/metricz"
	"github.com/kproios/dz-manager/internal/players"
	"github.com/kproios/dz-manager/internal/priority"
	"github.com/kproios/dz-manager/internal/rcon"
	"github.com/kproios/dz-manager/internal/restarts"
)

// registerFeatures wires feature modules (RCON, MetricZ, restarts, bridge…).
func (a *App) registerFeatures(ctx context.Context) error {
	srv := a.Server

	// #15 RCON + A2S connector.
	a.Alert = &alert.Sender{URL: a.Cfg.AlertWebhook, Log: srv.Log}
	a.Restarts = &restarts.Service{DB: a.DB, Profiles: a.Profiles, Caps: a.Caps, Audit: srv.Audit, Alert: a.Alert, Log: srv.Log, TZ: a.Cfg.Location.String(),
		Publish: func() { srv.Hub.Publish("restarts", map[string]bool{"changed": true}) }}
	a.Restarts.Init()
	a.RCON = &rcon.Monitor{Profiles: a.Profiles, Caps: a.Caps, Log: srv.Log,
		Publish: func(s rcon.Status) {
			srv.Hub.Publish("server_status", s)
			a.Restarts.Observe(context.Background(), s.ServerID, s.Online())
			if a.Intel != nil {
				seen := make([]intel.Seen, 0, len(s.Players))
				for _, p := range s.Players {
					seen = append(seen, intel.Seen{BEGUID: p.BEGUID, IP: p.IP})
				}
				a.Intel.Observe(context.Background(), seen)
			}
		}}
	a.RCON.Init()
	(&rcon.API{Mon: a.RCON, Audit: srv.Audit}).Register(srv.API)
	a.onProfileChange = append(a.onProfileChange, a.RCON.Reload)
	a.starters = append(a.starters, func(ctx context.Context) { srv.Background(ctx, "rcon", a.RCON.Run) })

	// #4 restart observation (provider is the authority).
	a.Restarts.Exec = func(ctx context.Context, cmd string) error { _, _, err := a.RCON.Exec(ctx, cmd); return err }
	a.Restarts.KickAll = a.RCON.KickAll
	a.Restarts.OnLock = a.RCON.SetLocked
	a.Restarts.Register(srv.API)
	a.starters = append(a.starters, func(ctx context.Context) { srv.Background(ctx, "restarts", a.Restarts.Run) })

	// #16 bridge (event ingest + command queue) and #11 player history.
	a.Bridge = &bridge.Bridge{DB: a.DB, Profiles: a.Profiles, Caps: a.Caps, Audit: srv.Audit, Log: srv.Log,
		Publish: func(id string, types []string) {
			srv.Hub.Publish("events", map[string]any{"server_id": id, "types": types})
		}}
	a.Bridge.Init()
	a.Bridge.Register(srv.Ingest)
	a.Bridge.RegisterAPI(srv.API)
	a.starters = append(a.starters, func(ctx context.Context) { srv.Background(ctx, "bridge-watch", a.Bridge.Watch) })
	a.Players = &players.Service{DB: a.DB, Profiles: a.Profiles, Caps: a.Caps, Audit: srv.Audit, Log: srv.Log}
	a.Players.Init()
	a.Players.Attach(a.Bridge)
	a.Players.Register(srv.API)

	// #5 MetricZ ingest + live map.
	a.MetricZ = &metricz.Receiver{DB: a.DB, Profiles: a.Profiles, Caps: a.Caps, Log: srv.Log,
		Publish: func(s metricz.Snapshot) {
			srv.Hub.Publish("map", map[string]any{"server_id": s.ServerID, "received_at": s.ReceivedAt})
		}}
	a.MetricZ.OnPlayers = a.Players.Presence
	a.MetricZ.Register(srv.Ingest)
	a.MetricZ.RegisterAPI(srv.API)
	a.starters = append(a.starters, func(ctx context.Context) { srv.Background(ctx, "metricz-watch", a.MetricZ.Watch) })

	// #7 mod-backed player actions, #6 map events, #10 bans.
	(&actions.API{Bridge: a.Bridge, Players: a.Players, Profiles: a.Profiles, Audit: srv.Audit}).Register(srv.API)
	a.MapEvents = &mapevents.Service{DB: a.DB, Profiles: a.Profiles, Audit: srv.Audit, Log: srv.Log,
		Publish: func() { srv.Hub.Publish("map_events", map[string]bool{"changed": true}) }}
	a.MapEvents.Attach(a.Bridge)
	a.MapEvents.Register(srv.API)
	a.Bridge.Tracking = func(ctx context.Context) any { return a.MapEvents.LoadTracking(ctx) }
	a.BridgeExtras = append(a.BridgeExtras, a.MapEvents.BridgeConfig)
	a.starters = append(a.starters, func(ctx context.Context) { srv.Background(ctx, "map-events", a.MapEvents.Run) })
	a.Bans = &bans.Service{DB: a.DB, Audit: srv.Audit, Players: a.Players, RCON: a.RCON, Roster: rconRoster{a.RCON}}
	a.Bans.Register(srv.API)

	// #13 priority queue, phase 1 (list + export; the provider applies it).
	(&priority.API{DB: a.DB, Audit: srv.Audit}).Register(srv.API)

	// Player intel (Steam profile/bans, IP country/VPN), RCON chat and automatic messages.
	a.Intel = &intel.Service{DB: a.DB, Box: a.Box, Audit: srv.Audit, Log: srv.Log,
		Publish: func() { srv.Hub.Publish("intel", map[string]bool{"changed": true}) }}
	a.Intel.Init()
	a.Intel.Register(srv.API)
	a.starters = append(a.starters, func(ctx context.Context) { srv.Background(ctx, "intel", a.Intel.Run) })
	a.Chat = &chat.Service{DB: a.DB, Profiles: a.Profiles, Log: srv.Log,
		Publish: func() { srv.Hub.Publish("chat", map[string]bool{"changed": true}) }}
	a.Chat.Register(srv.API)
	a.RCON.OnMessage = a.Chat.OnMessage
	a.starters = append(a.starters, func(ctx context.Context) { srv.Background(ctx, "chat", a.Chat.Run) })
	a.starters = append(a.starters, func(ctx context.Context) { srv.Background(ctx, "rcon-pump", a.RCON.Pump) })
	a.AutoMsg = &automsg.Service{DB: a.DB, Profiles: a.Profiles, Bridge: a.Bridge, Audit: srv.Audit, Log: srv.Log, Roster: a.RCON.FreshPlayers}
	a.AutoMsg.Exec = func(ctx context.Context, cmd string) error { _, _, err := a.RCON.Exec(ctx, cmd); return err }
	a.AutoMsg.Init()
	a.AutoMsg.Attach(a.Bridge)
	a.AutoMsg.Register(srv.API)
	a.starters = append(a.starters, func(ctx context.Context) { srv.Background(ctx, "auto-messages", a.AutoMsg.Run) })

	_ = ctx
	return nil
}

type rconRoster struct{ m *rcon.Monitor }

func (r rconRoster) OnlineNumber(ctx context.Context, guid string) (int, bool) {
	ps, err := r.m.FreshPlayers(ctx)
	if err != nil {
		return 0, false
	}
	for _, p := range ps {
		if p.BEGUID == guid {
			return p.Number, true
		}
	}
	return 0, false
}
