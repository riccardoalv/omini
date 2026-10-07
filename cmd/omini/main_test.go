package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/store"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8080" || cfg.DataDir != "./data" || cfg.PollInterval != time.Minute || cfg.LogLevel != slog.LevelInfo || cfg.Demo || !cfg.AutoScan {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{
		"OMINI_ADDR": "127.0.0.1:9000", "OMINI_DATA_DIR": "/data", "OMINI_POLL_INTERVAL": "30",
		"OMINI_LOG_LEVEL": "debug", "OMINI_DEMO": "true", "OMINI_SECRET_KEY": "k", "OMINI_AUTOSCAN": "false",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:9000" || cfg.DataDir != "/data" || cfg.PollInterval != 30*time.Second ||
		cfg.LogLevel != slog.LevelDebug || !cfg.Demo || cfg.SecretKey != "k" || cfg.AutoScan {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	for _, vars := range []map[string]string{
		{"OMINI_POLL_INTERVAL": "5s"}, // too short
		{"OMINI_POLL_INTERVAL": "soon"},
		{"OMINI_LOG_LEVEL": "loud"},
		{"OMINI_DEMO": "maybe"},
		{"OMINI_AUTOSCAN": "sometimes"},
	} {
		if _, err := loadConfig(env(vars)); err == nil {
			t.Errorf("loadConfig(%v) should fail", vars)
		}
	}
}

// TestRunServesDemo starts the whole server with the demo network and checks
// the API answers, then shuts it down cleanly.
func TestRunServesDemo(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := config{Addr: "127.0.0.1:0", DataDir: t.TempDir(), PollInterval: time.Minute, Demo: true}
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, ready) }()

	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		t.Fatalf("server exited early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("server did not start")
	}

	resp, err := http.Get("http://" + addr + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	var health map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&health)
	resp.Body.Close()
	if health["status"] != "ok" || health["version"] != "dev" {
		t.Fatalf("health: %v", health)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not shut down")
	}
}

func TestFirstRunCreatesTheNetworkScan(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := firstRun(ctx, st, config{AutoScan: true}); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListIntegrations(ctx)
	if len(list) != 1 || list[0].Type != "network" || list[0].Config.String("subnets") != "auto" {
		t.Fatalf("integrations = %+v", list)
	}
	// Only on the very first start: existing setups are never touched.
	if err := firstRun(ctx, st, config{AutoScan: true, Demo: true}); err != nil {
		t.Fatal(err)
	}
	if list, _ := st.ListIntegrations(ctx); len(list) != 1 {
		t.Fatalf("first run must not add integrations again: %+v", list)
	}
}
