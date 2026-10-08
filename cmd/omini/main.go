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
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/riccardoalv/omini/internal/api"
	"github.com/riccardoalv/omini/internal/appicons"
	"github.com/riccardoalv/omini/internal/auth"
	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/netscan"
	"github.com/riccardoalv/omini/internal/nmapscan"
	"github.com/riccardoalv/omini/internal/notify"
	"github.com/riccardoalv/omini/internal/plugins"
	"github.com/riccardoalv/omini/internal/secret"
	"github.com/riccardoalv/omini/internal/snmp"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/webui"
	"github.com/riccardoalv/omini/sdk"
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
	AutoScan     bool     // create the network scan integration on first start
	PluginDirs   []string // plugins loaded in place (development)
	UV           string   // uv binary used to build plugin environments
	PluginIndex  string   // the store's remote index; "" = only the shipped list
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
	for _, d := range strings.Split(getenv("OMINI_PLUGIN_DIRS"), ",") {
		if d = strings.TrimSpace(d); d != "" {
			cfg.PluginDirs = append(cfg.PluginDirs, d)
		}
	}
	cfg.UV = getenv("OMINI_UV")
	// The store's index: OMINI_PLUGIN_INDEX=<url>, or "off" for the shipped list only.
	switch v := strings.TrimSpace(getenv("OMINI_PLUGIN_INDEX")); v {
	case "":
		cfg.PluginIndex = plugins.DefaultIndex
	case "off":
	default:
		cfg.PluginIndex = v
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
	reg.Register(netscan.New())
	reg.Register(nmapscan.New())

	// SNMP profiles: the shipped ones plus the user's (<data>/profiles/*.yaml).
	if n, err := snmp.LoadProfiles(filepath.Join(cfg.DataDir, "profiles")); err != nil {
		slog.Warn("some SNMP profiles could not be read", "err", err)
	} else {
		slog.Debug("snmp profiles loaded", "count", n)
	}
	plugs := &plugins.Manager{Dir: filepath.Join(cfg.DataDir, "plugins"), DevDirs: cfg.PluginDirs, UV: cfg.UV, SDK: sdk.Python}
	loaded, loadErrs := plugs.Load()
	for _, err := range loadErrs {
		slog.Warn("plugin not loaded", "err", err)
	}
	for _, p := range loaded {
		reg.Register(plugs.Integration(p))
		slog.Info("plugin loaded", "plugin", p.Manifest.ID, "version", p.Manifest.Version, "dev", p.Dev)
	}

	index := &plugins.Index{URL: cfg.PluginIndex, Cache: filepath.Join(cfg.DataDir, "plugins", "index.json")}
	index.Load()
	go index.Run(ctx)

	if err := firstRun(ctx, st, box, cfg); err != nil {
		return err
	}

	notifier := &notify.Dispatcher{Store: st, Box: box}
	coll := collector.New(st, reg, box, collector.Options{Interval: cfg.PollInterval, OnAlerts: notifier.Handle})
	server := &api.Server{
		Store: st, Registry: reg, Box: box, Collector: coll, Auth: auth.New(st, 0),
		WebUI: webui.New(), Plugins: plugs, PluginIndex: index, UI: web.FS(), Version: version,
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
// Omini shows the network with zero configuration. It also upgrades older
// setups: the former demo network is removed, and standalone SNMP
// integrations become communities of the network scan (which now reads SNMP
// from every device it finds).
func firstRun(ctx context.Context, st *store.Store, box *secret.Box, cfg config) error {
	existing, err := st.ListIntegrations(ctx)
	if err != nil {
		return err
	}
	var (
		scan        *store.Integration
		communities []string
		kept        int
	)
	for _, in := range existing {
		if in.Type == "network" && scan != nil {
			// The network scan is single: keep the oldest (the user's settings).
			slog.Info("removing a duplicate network scan", "integration", in.ID)
			if err := st.DeleteIntegration(ctx, in.ID); err != nil {
				return err
			}
			continue
		}
		switch in.Type {
		case "demo":
		case "snmp":
			if c, err := openSecret(box, in.Config.String("community")); err == nil && c != "" {
				communities = append(communities, c)
			}
		default:
			if in.Type == "network" && scan == nil {
				scan = &in
			}
			kept++
			continue
		}
		if err := st.DeleteIntegration(ctx, in.ID); err != nil {
			return err
		}
	}
	if kept == 0 && (cfg.AutoScan || len(communities) > 0) {
		created, err := st.CreateIntegration(ctx, store.Integration{
			Name: "Network scan", Type: "network", Enabled: true,
			Config: integration.Config{"subnets": "auto", "port_scan": true},
		})
		if err != nil {
			return err
		}
		scan = &created
	}
	if scan == nil || len(communities) == 0 {
		return nil
	}
	return addCommunities(ctx, st, box, *scan, communities)
}

// addCommunities adds SNMP communities to the network scan's list.
func addCommunities(ctx context.Context, st *store.Store, box *secret.Box, scan store.Integration, add []string) error {
	current, err := openSecret(box, scan.Config.String("snmp_communities"))
	if err != nil {
		return err
	}
	var list []string
	for _, c := range append(strings.Split(current, ","), add...) {
		if c = strings.TrimSpace(c); c != "" && !slices.Contains(list, c) {
			list = append(list, c)
		}
	}
	if current == "" && !slices.Contains(list, "public") {
		list = append([]string{"public"}, list...) // keep trying the default community too
	}
	sealed, err := box.Seal(strings.Join(list, ", "))
	if err != nil {
		return err
	}
	scan.Config["snmp_communities"] = sealed
	_, err = st.UpdateIntegration(ctx, scan)
	return err
}

func openSecret(box *secret.Box, v string) (string, error) {
	if v == "" || !secret.IsSealed(v) {
		return v, nil
	}
	return box.Open(v)
}
