package app

import (
	"context"

	"github.com/kproios/dz-manager/internal/rcon"
)

// registerFeatures wires feature modules (RCON, MetricZ, restarts, bridge…).
func (a *App) registerFeatures(ctx context.Context) error {
	srv := a.Server

	// #15 RCON + A2S connector.
	a.RCON = &rcon.Monitor{Profiles: a.Profiles, Caps: a.Caps, Log: srv.Log,
		Publish: func(s rcon.Status) { srv.Hub.Publish("server_status", s) }}
	a.RCON.Init()
	(&rcon.API{Mon: a.RCON, Audit: srv.Audit}).Register(srv.API)
	a.onProfileChange = append(a.onProfileChange, a.RCON.Reload)
	a.starters = append(a.starters, func(ctx context.Context) { srv.Background(ctx, "rcon", a.RCON.Run) })

	_ = ctx
	return nil
}
