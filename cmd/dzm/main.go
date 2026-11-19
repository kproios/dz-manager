// Command dzm is the DZ Manager control plane: one static binary with the UI embedded.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // DZM_TZ works on hosts and images without a zoneinfo database

	"github.com/kproios/dz-manager/internal/app"
	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/config"
	"github.com/kproios/dz-manager/internal/secret"
	"github.com/kproios/dz-manager/internal/store"
	"github.com/kproios/dz-manager/internal/worldmap"
)

// version is set at build time: -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version", "--version", "-v":
		fmt.Println(version)
	case "serve":
		if err := serve(); err != nil {
			fmt.Fprintln(os.Stderr, "dzm:", err)
			os.Exit(1)
		}
	case "health":
		// For container health checks: is the local server ready?
		addr := os.Getenv("DZM_LISTEN")
		if addr == "" {
			addr = "127.0.0.1:8080"
		}
		if strings.HasPrefix(addr, "0.0.0.0:") || strings.HasPrefix(addr, ":") {
			addr = "127.0.0.1:" + addr[strings.LastIndex(addr, ":")+1:]
		}
		c := &http.Client{Timeout: 4 * time.Second}
		resp, err := c.Get("http://" + addr + "/ready")
		if err != nil {
			fmt.Fprintln(os.Stderr, "dzm: not ready:", err)
			os.Exit(1)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			fmt.Fprintln(os.Stderr, "dzm: not ready: HTTP", resp.StatusCode)
			os.Exit(1)
		}
	case "map":
		if err := mapCmd(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "dzm map:", err)
			os.Exit(1)
		}
	case "admin":
		if err := adminCmd(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "dzm admin:", err)
			os.Exit(1)
		}
	case "check-config":
		if err := checkConfig(); err != nil {
			fmt.Fprintln(os.Stderr, "dzm: config check FAILED:\n"+err.Error())
			os.Exit(1)
		}
		fmt.Println("config OK")
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: dzm <command>

commands:
  serve         run the HTTP server (applies DB migrations first)
  version       print the version
  check-config  validate environment, data directory and OIDC discovery (when set)
  health        exit 0 if the local server answers /ready (container health checks)
  admin invite-owner [--days N] [--note TEXT]
                print a one-time link that creates an owner account (first
                setup, or a way back in when every owner lost their 2FA)
  map list      list the terrain maps on dzhosts.com
  map install   install map tiles for a world:
                  dzm map install --world chernarusplus            (best size from dzhosts.com)
                  dzm map install --world enoch --slug livonia --px 7616
                  dzm map install --world namalsk --file namalsk.png   (made with dayz-terrain-map)
                uses DZM_DATA_DIR (default /var/lib/dzm) and DZM_MAP_MAX_PX (default 10000)`)
}

func mapCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("use 'dzm map list' or 'dzm map install'")
	}
	dataDir := os.Getenv("DZM_DATA_DIR")
	if dataDir == "" {
		dataDir = "/var/lib/dzm"
	}
	maxPx := 10000
	if v := os.Getenv("DZM_MAP_MAX_PX"); v != "" {
		if _, err := fmt.Sscan(v, &maxPx); err != nil {
			return errors.New("DZM_MAP_MAX_PX must be a number")
		}
	}
	svc := &worldmap.Service{DataDir: dataDir, MaxPx: maxPx}
	svc.Init()
	ctx := context.Background()
	fs := flag.NewFlagSet("map "+args[0], flag.ContinueOnError)
	world := fs.String("world", worldmap.DefaultWorld, "CfgWorlds name of the map (chernarusplus, enoch, sakhal, namalsk…)")
	slug := fs.String("slug", "", "dzhosts.com map name (default: matched from --world)")
	px := fs.Int("px", 0, "image size in pixels (default: largest within DZM_MAP_MAX_PX)")
	file := fs.String("file", "", "install a local PNG/JPEG instead of downloading")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	switch args[0] {
	case "list":
		list, err := svc.Sources(ctx, true)
		if err != nil {
			return err
		}
		for _, s := range list {
			fmt.Printf("%-16s %-9s %s\n", s.Slug, s.Group, s.Label)
			for _, f := range s.Files {
				note := ""
				if !f.Fits {
					note = "  (over DZM_MAP_MAX_PX)"
				}
				fmt.Printf("    %6d px  %6s  %s%s\n", f.Px, f.Size, f.URL, note)
			}
		}
		return nil
	case "install":
	default:
		return fmt.Errorf("unknown map command %q", args[0])
	}
	w, ok := worldmap.NormalizeWorld(*world)
	if !ok {
		return errors.New("--world must be a CfgWorlds name such as chernarusplus")
	}
	progress := func(state string, done, total int) {
		if total > 0 && (done%100 == 0 || done == total) {
			fmt.Printf("\r%s %d/%d tiles", state, done, total)
		} else if total == 0 {
			fmt.Printf("%s…\n", state)
		}
	}
	src := *file
	source := "file:" + filepath.Base(*file)
	if src == "" {
		list, err := svc.Sources(ctx, true)
		if err != nil {
			return err
		}
		slugs := []string{}
		for _, s := range list {
			slugs = append(slugs, s.Slug)
		}
		want := *slug
		if want == "" {
			want = worldmap.SuggestSlug(w, slugs)
			if want == "" {
				return fmt.Errorf("no dzhosts.com map matches %q; pass --slug (see 'dzm map list') or --file", w)
			}
		}
		var pick *worldmap.SourceFile
		for _, s := range list {
			if s.Slug != want {
				continue
			}
			if *px == 0 {
				if f, ok := s.BestFile(); ok {
					pick = &f
				}
			}
			for i := range s.Files {
				if s.Files[i].Px == *px {
					pick = &s.Files[i]
				}
			}
		}
		if pick == nil {
			return fmt.Errorf("no %q image of that size on dzhosts.com (see 'dzm map list')", want)
		}
		if pick.Px > maxPx {
			return fmt.Errorf("%d px is over DZM_MAP_MAX_PX (%d)", pick.Px, maxPx)
		}
		tmp, err := os.CreateTemp("", "dzm-map-*.png")
		if err != nil {
			return err
		}
		tmp.Close()
		defer os.Remove(tmp.Name())
		fmt.Println("downloading", pick.URL)
		if err := worldmap.Download(ctx, svc.HTTP, pick.URL, tmp.Name(), 400<<20, func(done, total int64) {
			if total > 0 {
				fmt.Printf("\r%d/%d MB", done>>20, total>>20)
			}
		}); err != nil {
			return err
		}
		fmt.Println()
		src, source = tmp.Name(), "dzhosts:"+strings.TrimSuffix(pick.Name, ".png")
	}
	m, err := worldmap.InstallFile(dataDir, w, src, source, maxPx, progress)
	if err != nil {
		return err
	}
	fmt.Printf("\ninstalled %s: zoom 0–%d from %s into %s\n", w, m.MaxZoom, m.Source, worldmap.TerrainDir(dataDir, w))
	return nil
}

func logger(level string) *slog.Logger {
	var l slog.Level
	_ = l.UnmarshalText([]byte(level))
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}

func checkConfig() error {
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		return err
	}
	var errs []error
	if st, err := os.Stat(cfg.DataDir); err != nil || !st.IsDir() {
		errs = append(errs, fmt.Errorf("DZM_DATA_DIR %s is not a directory", cfg.DataDir))
	} else if f, err := os.CreateTemp(cfg.DataDir, ".check-*"); err != nil {
		errs = append(errs, fmt.Errorf("DZM_DATA_DIR %s is not writable: %w", cfg.DataDir, err))
	} else {
		f.Close()
		os.Remove(f.Name())
	}
	if cfg.OIDCIssuer != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.OIDCIssuer, "/")+"/.well-known/openid-configuration", nil)
		if resp, err := http.DefaultClient.Do(req); err != nil {
			errs = append(errs, fmt.Errorf("OIDC discovery: %w", err))
		} else {
			resp.Body.Close()
			if resp.StatusCode != 200 {
				errs = append(errs, fmt.Errorf("OIDC discovery returned HTTP %d", resp.StatusCode))
			}
		}
	}
	return errors.Join(errs...)
}

func adminCmd(args []string) error {
	if len(args) == 0 || args[0] != "invite-owner" {
		return errors.New("use 'dzm admin invite-owner [--days N] [--note TEXT]'")
	}
	fs := flag.NewFlagSet("admin invite-owner", flag.ContinueOnError)
	days := fs.Int("days", 1, "how many days the link stays valid (1-7)")
	note := fs.String("note", "created on the command line", "note shown in Settings → Users")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *days < 1 || *days > 7 {
		return errors.New("--days must be 1 to 7")
	}
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		return fmt.Errorf("invalid configuration (run it with the same environment as the service):\n%w", err)
	}
	ctx := context.Background()
	db, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	box, err := secret.New(cfg.SecretKey)
	if err != nil {
		return err
	}
	al := audit.New(db)
	a := auth.New(cfg, db, box, al, logger("error"))
	id, link, err := a.CreateInvite(ctx, "owner", *note, "command line", time.Duration(*days)*24*time.Hour)
	if err != nil {
		return err
	}
	_ = al.Write(ctx, audit.Actor{Sub: "cli", Name: "command line"}, "users.invite", id, map[string]any{"role": "owner", "days": *days}, "ok", "")
	fmt.Printf("Open this link within %d day(s) to create an owner account (it works once):\n\n  %s\n\n", *days, link)
	return nil
}

func serve() error {
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		return fmt.Errorf("invalid configuration:\n%w", err)
	}
	log := logger(cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a, err := app.New(ctx, cfg, log, version)
	if err != nil {
		if errors.Is(err, store.ErrSchemaNewer) {
			log.Error("refusing to start", "err", err)
		}
		return err
	}
	defer a.Close()

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: a.Server.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	a.Start(ctx)
	a.Server.SetReady(true)
	log.Info("dzm started", "version", version, "listen", cfg.Listen, "data_dir", cfg.DataDir)

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case <-ctx.Done():
	case err := <-errc:
		return err
	}
	a.Server.SetReady(false)
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	log.Info("shutting down")
	return srv.Shutdown(shCtx)
}
