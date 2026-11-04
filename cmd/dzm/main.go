// Command dzm is the DZ Manager control plane: one static binary with the UI embedded.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kproios/dz-manager/internal/app"
	"github.com/kproios/dz-manager/internal/config"
	"github.com/kproios/dz-manager/internal/store"
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
  check-config  validate environment, data directory and SSO discovery`)
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, cfg.OIDCIssuer+"/.well-known/openid-configuration", nil)
	if resp, err := http.DefaultClient.Do(req); err != nil {
		errs = append(errs, fmt.Errorf("sso discovery: %w", err))
	} else {
		resp.Body.Close()
		if resp.StatusCode != 200 {
			errs = append(errs, fmt.Errorf("sso discovery returned HTTP %d", resp.StatusCode))
		}
	}
	return errors.Join(errs...)
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
