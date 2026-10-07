package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/secret"
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
	if cfg.Addr != ":8080" || cfg.DataDir != "./data" || cfg.PollInterval != time.Minute || cfg.LogLevel != slog.LevelInfo || !cfg.AutoScan {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{
		"OMINI_ADDR": "127.0.0.1:9000", "OMINI_DATA_DIR": "/data", "OMINI_POLL_INTERVAL": "30",
		"OMINI_LOG_LEVEL": "debug", "OMINI_SECRET_KEY": "k", "OMINI_AUTOSCAN": "false",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:9000" || cfg.DataDir != "/data" || cfg.PollInterval != 30*time.Second ||
		cfg.LogLevel != slog.LevelDebug || cfg.SecretKey != "k" || cfg.AutoScan {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	for _, vars := range []map[string]string{
		{"OMINI_POLL_INTERVAL": "5s"}, // too short
		{"OMINI_POLL_INTERVAL": "soon"},
		{"OMINI_LOG_LEVEL": "loud"},
		{"OMINI_AUTOSCAN": "sometimes"},
	} {
		if _, err := loadConfig(env(vars)); err == nil {
			t.Errorf("loadConfig(%v) should fail", vars)
		}
	}
}

// TestRunServes starts the whole server and checks the API answers, then
// shuts it down cleanly.
func TestRunServes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := config{Addr: "127.0.0.1:0", DataDir: t.TempDir(), PollInterval: time.Minute}
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

	if err := firstRun(ctx, st, testBox(t), config{AutoScan: true}); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListIntegrations(ctx)
	if len(list) != 1 || list[0].Type != "network" || list[0].Config.String("subnets") != "auto" {
		t.Fatalf("integrations = %+v", list)
	}
	// Only on the very first start: existing setups are never touched.
	if err := firstRun(ctx, st, testBox(t), config{AutoScan: true}); err != nil {
		t.Fatal(err)
	}
	if list, _ := st.ListIntegrations(ctx); len(list) != 1 {
		t.Fatalf("first run must not add integrations again: %+v", list)
	}
}

func TestFirstRunRemovesTheFormerDemoNetwork(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.CreateIntegration(ctx, store.Integration{Name: "Demo network", Type: "demo", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := firstRun(ctx, st, testBox(t), config{AutoScan: true}); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListIntegrations(ctx)
	if len(list) != 1 || list[0].Type != "network" {
		t.Fatalf("integrations = %+v (demo removed, network scan created)", list)
	}
}

func testBox(t *testing.T) *secret.Box {
	t.Helper()
	box, err := secret.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func TestFirstRunMovesSNMPIntoTheNetworkScan(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	box := testBox(t)
	for _, c := range []string{"homelab", "public"} {
		sealed, _ := box.Seal(c)
		if _, err := st.CreateIntegration(ctx, store.Integration{
			Name: "switch", Type: "snmp", Enabled: true,
			Config: integration.Config{"host": "192.168.1.2", "community": sealed},
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := firstRun(ctx, st, box, config{}); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListIntegrations(ctx)
	if len(list) != 1 || list[0].Type != "network" {
		t.Fatalf("integrations = %+v (SNMP removed, network scan created)", list)
	}
	communities, err := box.Open(list[0].Config.String("snmp_communities"))
	if err != nil {
		t.Fatal(err)
	}
	if communities != "homelab, public" {
		t.Fatalf("communities = %q", communities)
	}

	// Idempotent: nothing left to move.
	if err := firstRun(ctx, st, box, config{}); err != nil {
		t.Fatal(err)
	}
	if list, _ := st.ListIntegrations(ctx); len(list) != 1 {
		t.Fatalf("integrations after second start = %+v", list)
	}
}

func TestFirstRunKeepsASingleNetworkScan(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	first, _ := st.CreateIntegration(ctx, store.Integration{Name: "Network scan", Type: "network", Enabled: true, Config: integration.Config{"subnets": "192.168.1.0/24"}})
	_, _ = st.CreateIntegration(ctx, store.Integration{Name: "Network scan", Type: "network", Enabled: true, Config: integration.Config{"subnets": "auto"}})
	if err := firstRun(ctx, st, testBox(t), config{AutoScan: true}); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListIntegrations(ctx)
	if len(list) != 1 || list[0].ID != first.ID {
		t.Fatalf("integrations = %+v (the oldest scan is kept)", list)
	}
}
