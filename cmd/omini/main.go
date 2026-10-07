// Command omini runs the Omini server: collector, API and web UI.
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
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/riccardoalv/omini/internal/api"
	"github.com/riccardoalv/omini/internal/appicons"
	"github.com/riccardoalv/omini/internal/auth"
	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/netscan"
	"github.com/riccardoalv/omini/internal/secret"
	"github.com/riccardoalv/omini/internal/snmp"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/webui"
	"github.com/riccardoalv/omini/web"
)

// version is set at build time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

type config struct {
	Addr         string
	DataDir      string
	PollInterval time.Duration
	SecretKey    string
	LogLevel     slog.Level
	AutoScan     bool // create the network scan integration on first start
}

func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{Addr: ":8080", DataDir: "./data", PollInterval: time.Minute, LogLevel: slog.LevelInfo, AutoScan: true}
	if v := getenv("OMINI_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := getenv("OMINI_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := getenv("OMINI_POLL_INTERVAL"); v != "" {
		d, err := parseInterval(v)
		if err != nil {
			return cfg, fmt.Errorf("OMINI_POLL_INTERVAL: %w", err)
		}
		cfg.PollInterval = d
	}
	cfg.SecretKey = getenv("OMINI_SECRET_KEY")
	if v := getenv("OMINI_LOG_LEVEL"); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(v)); err != nil {
			return cfg, fmt.Errorf("OMINI_LOG_LEVEL: %w", err)
		}
	}
	if v := getenv("OMINI_AUTOSCAN"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return cfg, fmt.Errorf("OMINI_AUTOSCAN: %w", err)
		}
		cfg.AutoScan = b
	}
	return cfg, nil
}

// parseInterval accepts seconds ("60") or a Go duration ("1m30s"), minimum 10s.
func parseInterval(v string) (time.Duration, error) {
	d, err := time.ParseDuration(v)
	if err != nil {
		secs, err2 := strconv.Atoi(v)
		if err2 != nil {
			return 0, fmt.Errorf("invalid interval %q (use seconds or a duration like 1m)", v)
		}
		d = time.Duration(secs) * time.Second
	}
	if d < 10*time.Second {
		return 0, fmt.Errorf("interval %s is too short (minimum 10s)", d)
	}
	return d, nil
}

func main() {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "omini:", err)
		os.Exit(2)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel})))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err = run(ctx, cfg, nil)
	stop()
	if err != nil {
		slog.Error("omini stopped", "err", err)
		os.Exit(1)
	}
}

// run starts Omini and blocks until ctx is done. If ready is not nil, it
// receives the listening address once the server accepts connections.
func run(ctx context.Context, cfg config, ready chan<- string) error {
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return err
	}
	st, err := store.Open(ctx, filepath.Join(cfg.DataDir, "omini.db"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()
	box, err := secret.LoadOrCreate(cfg.SecretKey, filepath.Join(cfg.DataDir, "secret.key"))
	if err != nil {
		return fmt.Errorf("load secret key: %w", err)
	}

	reg := integration.NewRegistry()
	reg.Register(snmp.New())
	reg.Register(netscan.New())

	if err := firstRun(ctx, st, cfg); err != nil {
		return err
	}

	coll := collector.New(st, reg, box, collector.Options{Interval: cfg.PollInterval})
	server := &api.Server{
		Store: st, Registry: reg, Box: box, Collector: coll, Auth: auth.New(st, 0),
		Discover: snmp.Discover, WebUI: webui.New(), UI: web.FS(), Version: version,
		Icons: appicons.NewServer(filepath.Join(cfg.DataDir, "icons")),
	}
	httpServer := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}

	errs := make(chan error, 2)
	go func() { errs <- coll.Run(ctx) }()
	go func() {
		if err := httpServer.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()
	slog.Info("omini started", "version", version, "addr", ln.Addr().String(), "data_dir", cfg.DataDir, "poll_interval", cfg.PollInterval)
	if ready != nil {
		ready <- ln.Addr().String()
	}

	select {
	case <-ctx.Done():
	case err := <-errs:
		if err != nil {
			return err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	slog.Info("shutting down")
	return httpServer.Shutdown(shutdownCtx)
}

// firstRun sets up the network scan when there are no integrations yet, so
// Omini shows the network with zero configuration. It also removes
// integrations of types that no longer exist (the former demo network).
func firstRun(ctx context.Context, st *store.Store, cfg config) error {
	existing, err := st.ListIntegrations(ctx)
	if err != nil {
		return err
	}
	kept := 0
	for _, in := range existing {
		if in.Type == "demo" {
			if err := st.DeleteIntegration(ctx, in.ID); err != nil {
				return err
			}
			continue
		}
		kept++
	}
	if kept > 0 {
		return nil
	}
	if cfg.AutoScan {
		if _, err := st.CreateIntegration(ctx, store.Integration{
			Name: "Network scan", Type: "network", Enabled: true,
			Config: integration.Config{"subnets": "auto", "port_scan": true},
		}); err != nil {
			return err
		}
	}
	return nil
}
