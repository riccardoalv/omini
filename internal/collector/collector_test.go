package collector_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
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

// leveled is a plugin-like integration reporting one device; it records the
// order integrations are collected in.
type leveled struct {
	typ    string
	device model.Device
	calls  *[]string
	mu     *sync.Mutex
}

func (l *leveled) Info() integration.Info {
	return integration.Info{Type: l.typ, Name: l.typ, Kind: integration.KindPlugin}
}

func (l *leveled) Collect(context.Context, integration.Config) ([]model.Device, error) {
	l.mu.Lock()
	*l.calls = append(*l.calls, l.typ)
	l.mu.Unlock()
	return []model.Device{l.device}, nil
}

func (*leveled) Test(context.Context, integration.Config) (string, error) { return "ok", nil }

// A round collects one integration at a time, from the edge of the network to
// its center: the access point, then the switch, then the firewall, and the
// built-in discovery last. The map is built once, from that one picture.
func TestRoundsGoFromTheEdgeToTheCenter(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := secret.New(make([]byte, 32))
	reg := integration.NewRegistry()
	var (
		calls []string
		mu    sync.Mutex
	)
	lldp := func(remote string) []model.Neighbor {
		return []model.Neighbor{{LocalPort: "1", Protocol: model.Ptr(model.NeighborProtocolLldp), RemoteName: model.Ptr(remote)}}
	}
	dev := func(name string, role model.DeviceRole, mac string, up string) model.Device {
		d := model.Device{Key: mac, Name: name, Role: model.Ptr(role), MACs: []model.MACAddress{model.MACAddress(mac)}}
		if up != "" {
			d.Neighbors = lldp(up)
		}
		return d
	}
	// Registered and created root first, so ID order would be wrong.
	for _, l := range []*leveled{
		{typ: "fwplugin", device: dev("fw", model.DeviceRoleFirewall, "aa:00:00:00:00:01", "")},
		{typ: "swplugin", device: dev("sw", model.DeviceRoleSwitch, "aa:00:00:00:00:02", "fw")},
		{typ: "applugin", device: dev("ap", model.DeviceRoleAp, "aa:00:00:00:00:03", "sw")},
	} {
		l.calls, l.mu = &calls, &mu
		reg.Register(l)
	}
	core := &fake{onCollect: func(context.Context) { calls = append(calls, "scan") }}
	reg.Register(core)
	for _, typ := range []string{"fake", "fwplugin", "swplugin", "applugin"} {
		if _, err := st.CreateIntegration(ctx, store.Integration{Name: typ, Type: typ, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	coll := collector.New(st, reg, box, collector.Options{})

	// The very first round knows nothing yet: plugins in the order they were
	// added, built-in discovery last.
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"fwplugin", "swplugin", "applugin", "scan"}; !slices.Equal(calls, want) {
		t.Fatalf("first round: %v, want %v", calls, want)
	}
	// From then on the map says how deep each one is.
	want := []string{"applugin", "swplugin", "fwplugin", "scan"}
	for round := 2; round <= 3; round++ {
		calls = nil
		if err := coll.CollectNow(ctx); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(calls, want) {
			t.Fatalf("round %d: %v, want %v", round, calls, want)
		}
	}
	// After a restart, the saved snapshots rebuild the map first: same order.
	again := collector.New(st, reg, box, collector.Options{})
	if err := again.Load(ctx); err != nil {
		t.Fatal(err)
	}
	calls = nil
	if err := again.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(calls, want) {
		t.Fatalf("after a restart: %v, want %v", calls, want)
	}
	r := coll.State().Round
	if r == nil || len(r.Order) != 4 || r.IntervalS != 60 {
		t.Fatalf("round info: %+v", r)
	}
}

func TestRoundInterval(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	if got := e.coll.RoundInterval(ctx); got != time.Minute {
		t.Fatalf("default: %v", got)
	}
	if err := e.coll.SetRoundInterval(ctx, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := e.coll.RoundInterval(ctx); got != 5*time.Minute {
		t.Fatalf("set: %v", got)
	}
	_ = e.coll.SetRoundInterval(ctx, 0)
	if got := e.coll.RoundInterval(ctx); got != time.Minute {
		t.Fatalf("back to the default: %v", got)
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

func TestPresenceTimelineAlertsAndHistory(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.addIntegration(t, "fake", integration.Config{})
	phone := model.MACAddress("f0:18:98:00:00:02")
	tv := model.MACAddress("f0:18:98:00:00:03")
	sw := model.Device{
		Key: "aa:00:00:00:00:01", Name: "sw", Role: model.Ptr(model.DeviceRoleSwitch), CPUPct: model.Ptr(97.0),
		Fdb:        []model.FdbEntry{{MAC: phone, Port: "ge3"}},
		Interfaces: []model.Interface{{Name: "ge1", RxBytes: model.Ptr(uint64(0)), TxBytes: model.Ptr(uint64(0))}},
	}
	e.fake.set([]model.Device{sw}, nil)
	s := e.collect(t)
	if len(s.Alerts) != 1 || s.Alerts[0].Rule != "high_cpu" {
		t.Fatalf("alerts: %+v", s.Alerts)
	}

	// The CPU calms down; 60 MB went through ge1 in a minute (8 Mbit/s).
	sw.CPUPct = model.Ptr(20.0)
	sw.Interfaces[0].RxBytes = model.Ptr(uint64(60_000_000))
	sw.Interfaces[0].TxBytes = model.Ptr(uint64(6_000_000))
	e.fake.set([]model.Device{sw}, nil)
	e.clock = e.clock.Add(time.Minute)
	s = e.collect(t)
	if len(s.Alerts) != 0 {
		t.Fatalf("resolved alerts still open: %+v", s.Alerts)
	}
	hist, err := e.st.TrafficHistory(ctx, "dev:aa:00:00:00:00:01", "ge1", e.clock.Add(-time.Hour), e.clock)
	if err != nil || len(hist) != 1 || hist[0].RxBps != 8_000_000 {
		t.Fatalf("history: %+v %v", hist, err)
	}

	// After the grace period a TV appears: a new device. The phone leaves:
	// marked gone only once it has been missing long enough.
	e.clock = e.clock.Add(collector.NewDeviceGrace + time.Minute)
	sw.Fdb = []model.FdbEntry{{MAC: phone, Port: "ge3"}, {MAC: tv, Port: "ge4"}}
	e.fake.set([]model.Device{sw}, nil)
	s = e.collect(t)
	if len(s.Alerts) != 1 || s.Alerts[0].Rule != "new_device" || s.Alerts[0].NodeID != "mac:"+string(tv) {
		t.Fatalf("new device: %+v", s.Alerts)
	}
	lastSeen := e.clock
	sw.Fdb = sw.Fdb[1:]
	e.fake.set([]model.Device{sw}, nil)
	e.clock = e.clock.Add(time.Minute)
	e.collect(t)
	events, _ := e.st.ListPresence(ctx, store.PresenceQuery{NodeID: "mac:" + string(phone)})
	if len(events) != 1 || events[0].Kind != "join" || !events[0].First {
		t.Fatalf("phone still present (debounced): %+v", events)
	}
	e.clock = e.clock.Add(6 * time.Minute)
	e.collect(t)
	events, _ = e.st.ListPresence(ctx, store.PresenceQuery{NodeID: "mac:" + string(phone)})
	if len(events) != 2 || events[0].Kind != "leave" || !events[0].At.Equal(lastSeen) {
		t.Fatalf("phone left when it was last seen: %+v", events)
	}
	// It comes back.
	sw.Fdb = append(sw.Fdb, model.FdbEntry{MAC: phone, Port: "ge3"})
	e.fake.set([]model.Device{sw}, nil)
	e.clock = e.clock.Add(time.Minute)
	e.collect(t)
	events, _ = e.st.ListPresence(ctx, store.PresenceQuery{NodeID: "mac:" + string(phone)})
	if len(events) != 3 || events[0].Kind != "join" || events[0].First {
		t.Fatalf("phone came back: %+v", events)
	}
}
