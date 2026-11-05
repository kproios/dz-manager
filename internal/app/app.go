// Package app wires all components together (used by cmd/dzm and tests).
package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/kproios/dz-manager/internal/alert"
	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/automsg"
	"github.com/kproios/dz-manager/internal/bans"
	"github.com/kproios/dz-manager/internal/bridge"
	"github.com/kproios/dz-manager/internal/caps"
	"github.com/kproios/dz-manager/internal/chat"
	"github.com/kproios/dz-manager/internal/config"
	"github.com/kproios/dz-manager/internal/intel"
	"github.com/kproios/dz-manager/internal/mapevents"
	"github.com/kproios/dz-manager/internal/metricz"
	"github.com/kproios/dz-manager/internal/players"
	"github.com/kproios/dz-manager/internal/profiles"
	"github.com/kproios/dz-manager/internal/rcon"
	"github.com/kproios/dz-manager/internal/restarts"
	"github.com/kproios/dz-manager/internal/secret"
	"github.com/kproios/dz-manager/internal/server"
	"github.com/kproios/dz-manager/internal/store"
)

// App holds the running components.
type App struct {
	Cfg      *config.Config
	DB       *store.DB
	Box      *secret.Box
	Server   *server.Server
	Profiles *profiles.Store
	Caps     *caps.Registry
	RCON     *rcon.Monitor
	MetricZ  *metricz.Receiver
	Restarts *restarts.Service
	Alert    *alert.Sender
	Bridge   *bridge.Bridge
	Players  *players.Service
	Bans     *bans.Service

	MapEvents    *mapevents.Service
	Intel        *intel.Service
	Chat         *chat.Service
	AutoMsg      *automsg.Service
	BridgeExtras []func(context.Context) map[string]any

	starters        []func(context.Context)
	onProfileChange []func()
}

// New opens the database and builds every component. It does not start background work.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger, version string) (*App, error) {
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "tiles"), 0o750); err != nil {
		return nil, err
	}
	db, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return nil, err
	}
	box, err := secret.New(cfg.SecretKey)
	if err != nil {
		db.Close()
		return nil, err
	}
	hub := server.NewHub()
	al := audit.New(db)
	registry := caps.NewRegistry(func(id string) { hub.Publish("capabilities", map[string]string{"server_id": id}) })
	ps := profiles.New(db, box)
	a := &App{Cfg: cfg, DB: db, Box: box, Profiles: ps, Caps: registry}
	a.Server = server.New(&server.Server{
		Cfg: cfg, DB: db, Auth: auth.New(cfg, db, box, al, log), Audit: al, Caps: registry, Hub: hub,
		Profiles: ps, Log: log, Version: version,
	})
	(&profiles.API{Store: ps, Audit: al, IngestURL: cfg.IngestPublicURL.String(), Extras: func(ctx context.Context) map[string]any {
		out := map[string]any{}
		for _, f := range a.BridgeExtras {
			for k, v := range f(ctx) {
				out[k] = v
			}
		}
		return out
	}}).Register(a.Server.API)
	ps.OnChange = func() {
		for _, f := range a.onProfileChange {
			f()
		}
		hub.Publish("profiles", map[string]bool{"changed": true})
	}
	if err := a.registerFeatures(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return a, nil
}

// Start launches background workers.
func (a *App) Start(ctx context.Context) {
	for _, s := range a.starters {
		s(ctx)
	}
}

// Close releases resources.
func (a *App) Close() { a.DB.Close() }
