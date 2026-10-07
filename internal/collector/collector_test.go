package collector_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/demo"
	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/secret"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

// fake is an integration whose result the test controls.
type fake struct {
	mu      sync.Mutex
	devices []model.Device
	err     error
	panic   bool
	gotCfg  integration.Config
}

func (*fake) Info() integration.Info {
	return integration.Info{Type: "fake", Name: "Fake", Kind: integration.KindCore, Fields: []model.FormField{
		{Key: "host", Type: model.FormFieldTypeHost},
		{Key: "password", Type: model.FormFieldTypeSecret},
	}}
}

func (f *fake) Collect(_ context.Context, cfg integration.Config) ([]model.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotCfg = cfg
	if f.panic {
		panic("boom")
	}
	return f.devices, f.err
}

func (*fake) Test(context.Context, integration.Config) (string, error) { return "ok", nil }

func (f *fake) set(devices []model.Device, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.devices, f.err = devices, err
}

type env struct {
	st    *store.Store
	coll  *collector.Collector
	fake  *fake
	clock time.Time
	box   *secret.Box
}

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := secret.New(make([]byte, 32))
	reg := integration.NewRegistry()
	f := &fake{}
	reg.Register(f)
	reg.Register(demo.New())
	e := &env{st: st, fake: f, clock: time.Unix(1_790_000_000, 0).UTC(), box: box}
	e.coll = collector.New(st, reg, box, collector.Options{Now: func() time.Time { return e.clock }})
	return e
}

func (e *env) addIntegration(t *testing.T, typ string, cfg integration.Config) store.Integration {
	t.Helper()
	sealed, err := integration.SealSecrets(e.box, (&fake{}).Info().Fields, cfg)
	if err != nil {
		t.Fatal(err)
	}
	in, err := e.st.CreateIntegration(context.Background(), store.Integration{Name: typ, Type: typ, Enabled: true, Config: sealed})
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func (e *env) collect(t *testing.T) collector.State {
	t.Helper()
	if err := e.coll.CollectNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	return e.coll.State()
}

func node(s collector.State, id string) (topology.Node, bool) {
	for _, n := range s.Topology.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return topology.Node{}, false
}

func TestDemoEndToEnd(t *testing.T) {
	e := setup(t)
	e.addIntegration(t, "demo", integration.Config{})
	s := e.collect(t)

	if len(s.Statuses) != 1 || !s.Statuses[0].OK || s.Statuses[0].Devices != 3 {
		t.Fatalf("unexpected statuses: %+v", s.Statuses)
	}
	// 3 devices + 10 Wi-Fi clients + NAS, TV, desktop + 2 segments + 4 and 5 MACs inside them.
	if len(s.Topology.Nodes) != 27 {
		t.Fatalf("demo map has %d nodes, want 27", len(s.Topology.Nodes))
	}
	inv, _ := e.st.ListInventory(context.Background())
	if len(inv) != len(s.Topology.Nodes) {
		t.Fatalf("inventory has %d entries, map has %d nodes", len(inv), len(s.Topology.Nodes))
	}
}

func TestSecretsAreDecryptedForIntegrations(t *testing.T) {
	e := setup(t)
	e.addIntegration(t, "fake", integration.Config{"host": "10.0.0.1", "password": "s3cret"})
	e.collect(t)
	if e.fake.gotCfg["password"] != "s3cret" || e.fake.gotCfg["host"] != "10.0.0.1" {
		t.Fatalf("integration received %v", e.fake.gotCfg)
	}
}

func TestFailedCollectionKeepsLastDevicesAsOffline(t *testing.T) {
	e := setup(t)
	e.addIntegration(t, "fake", integration.Config{})
	sw := model.Device{Key: "aa:00:00:00:00:01", Name: "sw"}

	e.fake.set([]model.Device{sw}, nil)
	e.collect(t)
	e.fake.set(nil, errors.New("timeout"))
	s := e.collect(t)

	if s.Statuses[0].OK || s.Statuses[0].Error != "timeout" {
		t.Fatalf("status should report the failure: %+v", s.Statuses[0])
	}
	n, ok := node(s, "dev:aa:00:00:00:00:01")
	if !ok || n.Online {
		t.Fatalf("device should stay on the map as offline: %+v (found=%v)", n, ok)
	}
}

func TestPanickingIntegrationDoesNotCrash(t *testing.T) {
	e := setup(t)
	e.addIntegration(t, "fake", integration.Config{})
	e.addIntegration(t, "demo", integration.Config{})
	e.fake.panic = true
	s := e.collect(t)

	if s.Statuses[0].OK || s.Statuses[0].Error == "" {
		t.Fatalf("panicking integration should fail: %+v", s.Statuses[0])
	}
	if !s.Statuses[1].OK {
		t.Fatal("other integrations must still be collected")
	}
}

func TestClientThatLeftIsShownOfflineForAnHour(t *testing.T) {
	e := setup(t)
	e.addIntegration(t, "fake", integration.Config{})
	phone := model.MACAddress("f0:18:98:00:00:02")
	sw := model.Device{Key: "aa:00:00:00:00:01", Name: "sw", Fdb: []model.FdbEntry{{MAC: phone, Port: "ge3"}}}

	e.fake.set([]model.Device{sw}, nil)
	e.collect(t)

	sw.Fdb = nil // the phone left
	e.fake.set([]model.Device{sw}, nil)
	e.clock = e.clock.Add(30 * time.Minute)
	s := e.collect(t)
	n, ok := node(s, "mac:"+string(phone))
	if !ok || n.Online || n.LastSeen == nil || n.ParentID != "dev:aa:00:00:00:00:01" || n.Port != "ge3" {
		t.Fatalf("phone should be shown offline at its last place: %+v (found=%v)", n, ok)
	}

	e.clock = e.clock.Add(31 * time.Minute)
	s = e.collect(t)
	if _, ok := node(s, "mac:"+string(phone)); ok {
		t.Fatal("phone should leave the map after an hour offline")
	}
	inv, _ := e.st.ListInventory(context.Background())
	found := false
	for _, entry := range inv {
		found = found || entry.ID == "mac:"+string(phone)
	}
	if !found {
		t.Fatal("phone must stay in the inventory forever")
	}
}

func TestAliasAndPinAreApplied(t *testing.T) {
	e := setup(t)
	e.addIntegration(t, "demo", integration.Config{})
	e.collect(t)

	alias, pinned := "NAS (Synology)", true
	if _, err := e.st.UpdateInventory(context.Background(), "mac:00:11:32:aa:00:01", store.InventoryUpdate{Alias: &alias, Pinned: &pinned}); err != nil {
		t.Fatal(err)
	}
	s := e.collect(t)
	n, _ := node(s, "mac:00:11:32:aa:00:01")
	if n.Label != alias || !n.Pinned {
		t.Fatalf("alias/pin not applied: %+v", n)
	}
	inv, _ := e.st.ListInventory(context.Background())
	for _, entry := range inv {
		if entry.ID == n.ID && entry.Label != "nas" {
			t.Fatalf("inventory label must keep the observed name, got %q", entry.Label)
		}
	}
}

func TestDisabledAndDeletedIntegrationsDropOut(t *testing.T) {
	e := setup(t)
	in := e.addIntegration(t, "demo", integration.Config{})
	e.collect(t)

	in.Enabled = false
	if _, err := e.st.UpdateIntegration(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	s := e.collect(t)
	if len(s.Statuses) != 0 {
		t.Fatalf("disabled integration still reported: %+v", s.Statuses)
	}
	for _, n := range s.Topology.Nodes {
		if n.Online {
			t.Fatalf("no node should be online without integrations: %+v", n)
		}
	}
}

func TestLoadRestoresStateAfterRestart(t *testing.T) {
	e := setup(t)
	e.addIntegration(t, "demo", integration.Config{})
	before := e.collect(t)

	// A new collector on the same database (simulates a restart).
	reg := integration.NewRegistry()
	reg.Register(demo.New())
	restarted := collector.New(e.st, reg, e.box, collector.Options{Now: func() time.Time { return e.clock }})
	if err := restarted.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(restarted.State().Topology.Nodes); got != len(before.Topology.Nodes) {
		t.Fatalf("restored %d nodes, want %d", got, len(before.Topology.Nodes))
	}
}

func TestNodesAreClassifiedAndUserCorrectionsWin(t *testing.T) {
	e := setup(t)
	e.addIntegration(t, "demo", integration.Config{})
	s := e.collect(t)

	fw, _ := node(s, "dev:00:e0:4c:68:00:02")
	if fw.Type != "firewall" {
		t.Fatalf("firewall classified as %q", fw.Type)
	}
	phone, _ := node(s, "mac:da:a1:19:00:00:01") // iphone-ana, private MAC, Wi-Fi
	if phone.Type != "phone" || phone.OS != "ios" {
		t.Fatalf("iphone classified as %q/%q (%v)", phone.Type, phone.OS, phone.Reasons)
	}

	tv, icon := "tv", "samsung"
	if _, err := e.st.UpdateInventory(context.Background(), "mac:00:11:32:aa:00:01",
		store.InventoryUpdate{DeviceType: &tv, Icon: &icon}); err != nil {
		t.Fatal(err)
	}
	s = e.collect(t)
	nas, _ := node(s, "mac:00:11:32:aa:00:01")
	if nas.Type != "tv" || nas.Icon != "samsung" || nas.Reasons[0] != "user" {
		t.Fatalf("user correction not applied: %+v", nas)
	}
}
