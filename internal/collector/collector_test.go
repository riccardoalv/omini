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
	mu        sync.Mutex
	devices   []model.Device
	err       error
	panic     bool
	gotCfg    integration.Config
	onCollect func(context.Context)
}

func (*fake) Info() integration.Info {
	return integration.Info{Type: "fake", Name: "Fake", Kind: integration.KindCore, Fields: []model.FormField{
		{Key: "host", Type: model.FormFieldTypeHost},
		{Key: "password", Type: model.FormFieldTypeSecret},
	}}
}

func (f *fake) Collect(ctx context.Context, cfg integration.Config) ([]model.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotCfg = cfg
	if f.onCollect != nil {
		f.onCollect(ctx)
	}
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

func TestStatusCountsScannedHosts(t *testing.T) {
	e := setup(t)
	e.addIntegration(t, "fake", integration.Config{})
	gw := model.MACAddress("aa:00:00:00:00:01")
	e.fake.set([]model.Device{{
		Key: string(gw), Name: "gw", MACs: []model.MACAddress{gw}, IPs: []string{"10.0.0.1"},
		Hosts: []model.Host{{IP: "10.0.0.1", MAC: &gw}, {IP: "10.0.0.2"}, {IP: "10.0.0.3"}},
	}}, nil)
	if got := e.collect(t).Statuses[0].Devices; got != 3 {
		t.Fatalf("devices = %d, want 3 (gateway + 2 hosts)", got)
	}
}

func TestCollectIntegrationRunsOneNowAndForces(t *testing.T) {
	e := setup(t)
	in := e.addIntegration(t, "fake", integration.Config{})
	e.fake.set([]model.Device{{Key: "aa:00:00:00:00:01", Name: "sw"}}, nil)

	var forced bool
	e.fake.onCollect = func(ctx context.Context) { forced = integration.Forced(ctx) }
	st, err := e.coll.CollectIntegration(context.Background(), in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !st.OK || st.Devices != 1 || !forced {
		t.Fatalf("status = %+v, forced = %v", st, forced)
	}
	if _, ok := node(e.coll.State(), "dev:aa:00:00:00:00:01"); !ok {
		t.Fatal("the map must be rebuilt right away")
	}

	in.Enabled = false
	_, _ = e.st.UpdateIntegration(context.Background(), in)
	if _, err := e.coll.CollectIntegration(context.Background(), in.ID); !errors.Is(err, collector.ErrDisabled) {
		t.Fatalf("disabled integration: err = %v", err)
	}
}

func TestEachIntegrationHasItsOwnInterval(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	fast, _ := e.st.CreateIntegration(ctx, store.Integration{Name: "fast", Type: "demo", Enabled: true, IntervalS: 15})
	slow, _ := e.st.CreateIntegration(ctx, store.Integration{Name: "slow", Type: "demo", Enabled: true, IntervalS: 3600})
	start := e.clock
	e.collect(t)

	e.clock = e.clock.Add(20 * time.Second)
	if err := e.coll.CollectDue(ctx); err != nil {
		t.Fatal(err)
	}
	at := map[int64]time.Time{}
	for _, s := range e.coll.State().Statuses {
		at[s.IntegrationID] = s.CollectedAt
	}
	if !at[fast.ID].Equal(e.clock) {
		t.Fatalf("the 15 s integration must be collected again after 20 s: %v", at[fast.ID])
	}
	if !at[slow.ID].Equal(start) {
		t.Fatalf("the hourly integration must wait: %v", at[slow.ID])
	}
}

func TestClientKeepsItsSwitchPortWhileTheSwitchForgetsIt(t *testing.T) {
	e := setup(t)
	e.addIntegration(t, "fake", integration.Config{})
	phone := model.MACAddress("92:34:c8:00:00:01")
	fw := model.Device{
		Key: "58:9c:fc:00:00:01", Name: "fw", Role: model.Ptr(model.DeviceRoleFirewall),
		MACs: []model.MACAddress{"58:9c:fc:00:00:01"},
		Arp: []model.ArpEntry{
			{IP: "192.168.1.2", MAC: "1c:2a:a3:00:00:01", Interface: model.Ptr("bridge0")},
			{IP: "192.168.1.41", MAC: phone, Interface: model.Ptr("bridge0")},
		},
	}
	sw := model.Device{
		Key: "1c:2a:a3:00:00:01", Name: "sw", Role: model.Ptr(model.DeviceRoleSwitch),
		MACs: []model.MACAddress{"1c:2a:a3:00:00:01"},
		Fdb:  []model.FdbEntry{{MAC: "58:9c:fc:00:00:01", Port: "Port 9"}, {MAC: phone, Port: "Port 2"}},
	}
	e.fake.set([]model.Device{fw, sw}, nil)
	e.collect(t)

	asleep := sw
	asleep.Fdb = sw.Fdb[:1] // the switch aged the phone out; the firewall still has it in ARP
	e.fake.set([]model.Device{fw, asleep}, nil)
	e.clock = e.clock.Add(10 * time.Minute)
	n, _ := node(e.collect(t), "mac:"+string(phone))
	if n.ParentID != "dev:1c:2a:a3:00:00:01" || n.Port != "Port 2" || !n.Online {
		t.Fatalf("the phone stays on Port 2: %+v", n)
	}

	e.clock = e.clock.Add(collector.LastSeenTTL)
	n, _ = node(e.collect(t), "mac:"+string(phone))
	if n.ParentID != "dev:58:9c:fc:00:00:01" {
		t.Fatalf("after %v the memory is forgotten: %+v", collector.LastSeenTTL, n)
	}
}

func TestRememberedPortsSurviveARestart(t *testing.T) {
	e := setup(t)
	e.addIntegration(t, "fake", integration.Config{})
	phone := model.MACAddress("92:34:c8:00:00:02")
	fw := model.Device{
		Key: "58:9c:fc:00:00:01", Name: "fw", Role: model.Ptr(model.DeviceRoleFirewall),
		MACs: []model.MACAddress{"58:9c:fc:00:00:01"},
		Arp: []model.ArpEntry{
			{IP: "192.168.1.2", MAC: "1c:2a:a3:00:00:01", Interface: model.Ptr("bridge0")},
			{IP: "192.168.1.41", MAC: phone, Interface: model.Ptr("bridge0")},
		},
	}
	sw := model.Device{
		Key: "1c:2a:a3:00:00:01", Name: "sw", Role: model.Ptr(model.DeviceRoleSwitch),
		MACs: []model.MACAddress{"1c:2a:a3:00:00:01"},
		Fdb:  []model.FdbEntry{{MAC: "58:9c:fc:00:00:01", Port: "Port 9"}, {MAC: phone, Port: "Port 2"}},
	}
	e.fake.set([]model.Device{fw, sw}, nil)
	e.collect(t)

	// Omini restarts while the switch forgets the phone.
	reg := integration.NewRegistry()
	reg.Register(e.fake)
	e.coll = collector.New(e.st, reg, e.box, collector.Options{Now: func() time.Time { return e.clock }})
	asleep := sw
	asleep.Fdb = sw.Fdb[:1]
	e.fake.set([]model.Device{fw, asleep}, nil)
	e.clock = e.clock.Add(5 * time.Minute)
	n, _ := node(e.collect(t), "mac:"+string(phone))
	if n.ParentID != "dev:1c:2a:a3:00:00:01" || n.Port != "Port 2" {
		t.Fatalf("after a restart the phone keeps its port: %+v", n)
	}
}
