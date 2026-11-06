package app

import (
	"context"
	"time"

	"github.com/kproios/dz-manager/internal/actions"
	"github.com/kproios/dz-manager/internal/alert"
	"github.com/kproios/dz-manager/internal/automsg"
	"github.com/kproios/dz-manager/internal/bans"
	"github.com/kproios/dz-manager/internal/bridge"
	"github.com/kproios/dz-manager/internal/chat"
	"github.com/kproios/dz-manager/internal/discord"
	"github.com/kproios/dz-manager/internal/feeds"
	"github.com/kproios/dz-manager/internal/hooks"
	"github.com/kproios/dz-manager/internal/intel"
	"github.com/kproios/dz-manager/internal/mapevents"
	"github.com/kproios/dz-manager/internal/metricz"
	"github.com/kproios/dz-manager/internal/players"
	"github.com/kproios/dz-manager/internal/priority"
	"github.com/kproios/dz-manager/internal/publicapi"
	"github.com/kproios/dz-manager/internal/rcon"
	"github.com/kproios/dz-manager/internal/restarts"
	"github.com/kproios/dz-manager/internal/retention"
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
		},
		PublishLive: func(d metricz.LiveDelta) { srv.Hub.Publish("live", d) }}
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
	a.Bans.AttachBridge(a.Bridge)

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

	// Public hooks: one bus for kills, joins, raids, world events, bans and
	// restarts; Discord webhooks and the bot API read from it.
	a.Hooks = &hooks.Bus{}
	a.Bans.Hooks = a.Hooks
	a.Restarts.Hooks = a.Hooks
	(&feeds.Feeds{DB: a.DB, Bus: a.Hooks}).Attach(a.Bridge)
	a.Discord = &discord.Service{DB: a.DB, Box: a.Box, Audit: srv.Audit, Log: srv.Log}
	a.Discord.Init()
	a.Discord.Register(srv.API)
	a.Hooks.Subscribe(a.Discord.OnEvent)
	a.starters = append(a.starters, func(ctx context.Context) { srv.Background(ctx, "discord", a.Discord.Run) })
	bot := &publicapi.API{DB: a.DB, Box: a.Box, Audit: srv.Audit, Bus: a.Hooks, Status: a.publicStatus, Online: a.onlineNames}
	bot.RegisterAdmin(srv.API)
	bot.RegisterPublic(srv.Public)

	// Data retention with compressed archives. (The SSH emergency kill was
	// removed: VYKIX gives no SSH and RCON cannot stop a frozen process.)
	a.Retention = &retention.Service{DB: a.DB, Audit: srv.Audit, Log: srv.Log, DataDir: a.Cfg.DataDir}
	a.Retention.Register(srv.API)
	a.starters = append(a.starters, func(ctx context.Context) { srv.Background(ctx, "retention", a.Retention.Run) })

	_ = ctx
	return nil
}

// publicStatus is the bot API's view of the server: nothing private.
func (a *App) publicStatus(ctx context.Context) map[string]any {
	s := a.RCON.Status()
	out := map[string]any{"online": s.Online(), "players": len(s.Players)}
	if s.Info != nil {
		out["name"], out["map"], out["version"], out["max_players"] = s.Info.Name, s.Info.Map, s.Info.Version, s.Info.MaxPlayers
		if s.RCON.State != rcon.StateOK {
			out["players"] = s.Info.Players
		}
	}
	if m := a.MetricZ.State(ctx); m.Available && m.Perf != nil {
		if m.Perf.UptimeSecs != nil {
			out["uptime_seconds"] = *m.Perf.UptimeSecs
		}
		if m.Perf.FPSAvg != nil {
			out["fps"] = *m.Perf.FPSAvg
		}
	}
	if p, _, err := a.Profiles.Active(ctx); err == nil {
		sc := a.Restarts.Load(ctx, p.ID)
		if next, ok := sc.Next(time.Now()); ok && sc.Enabled {
			out["next_restart"] = next.UTC()
			if t, skipped := a.Restarts.SkippedAt(ctx, p.ID); skipped && t.Equal(next.UTC().Truncate(time.Second)) {
				out["next_restart_skipped"] = true
			}
		}
	}
	return out
}

func (a *App) onlineNames(ctx context.Context) []string {
	names := []string{}
	for _, p := range a.RCON.Status().Players {
		names = append(names, p.Name)
	}
	return names
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
