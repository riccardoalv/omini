package collector_test

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/secret"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

// switchy is a switch integration that applies port.enabled.
type switchy struct {
	mu sync.Mutex
	// hold, when set, is called during a collection with what it read.
	hold    func()
	devices []model.Device
	applied []model.Change
	err     error
	// config is what the firewall's backup reads; backups counts them.
	config  string
	backups int
}

func (*switchy) Info() integration.Info {
	return integration.Info{
		Type: "switchy", Name: "Switchy", Kind: integration.KindCore,
		Changes: []model.ChangeKind{
			model.ChangeKindPortEnabled, model.ChangeKindPortVlans, model.ChangeKindVlanSet, model.ChangeKindVlanDelete,
			model.ChangeKindNetworkSet, model.ChangeKindNetworkDelete, model.ChangeKindReservationSet, model.ChangeKindReservationDelete,
			model.ChangeKindDnsSet, model.ChangeKindDnsDelete, model.ChangeKindWolSend, model.ChangeKindPauseSet, model.ChangeKindPauseDelete,
		},
		Backup: true,
	}
}

func (s *switchy) Collect(context.Context, integration.Config) ([]model.Device, error) {
	s.mu.Lock()
	read, hold := slices.Clone(s.devices), s.hold
	s.mu.Unlock()
	if hold != nil {
		hold() // the switch answers slowly: what it read is old by now
	}
	return read, nil
}

func (*switchy) Test(context.Context, integration.Config) (string, error) { return "ok", nil }

func (s *switchy) Backup(context.Context, integration.Config) ([]model.ConfigBackup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backups++
	return []model.ConfigBackup{{Device: string(fwMAC), Format: model.ConfigBackupFormatText, Content: s.config}}, nil
}

func (s *switchy) Apply(_ context.Context, _ integration.Config, c model.Change) ([]model.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applied = append(s.applied, c)
	if s.err != nil {
		return nil, s.err
	}
	switch v := c.Value.(type) {
	case model.DnsRecord:
		fw := s.devices[0]
		fw.DnsRecords = append(slices.DeleteFunc(slices.Clone(fw.DnsRecords), func(r model.DnsRecord) bool { return r.Name == v.Name }), v)
		s.devices[0] = fw
		return []model.Device{fw}, nil
	case collector.DnsDelete:
		fw := s.devices[0]
		fw.DnsRecords = slices.DeleteFunc(slices.Clone(fw.DnsRecords), func(r model.DnsRecord) bool { return r.Name == v.Name })
		s.devices[0] = fw
		return []model.Device{fw}, nil
	case model.PausedDevice:
		fw := s.devices[0]
		fw.Paused = append(slices.DeleteFunc(slices.Clone(fw.Paused), func(p model.PausedDevice) bool { return p.MAC == v.MAC }), v)
		s.devices[0] = fw
		return []model.Device{fw}, nil
	case collector.MACValue:
		fw := s.devices[0]
		fw.Paused = slices.DeleteFunc(slices.Clone(fw.Paused), func(p model.PausedDevice) bool { return p.MAC == v.MAC })
		s.devices[0] = fw
		return []model.Device{fw}, nil
	case collector.WolValue:
		return []model.Device{s.devices[0]}, nil
	}
	if c.Kind == model.ChangeKindReservationSet || c.Kind == model.ChangeKindReservationDelete {
		fw := s.devices[0]
		var mac model.MACAddress
		if r, ok := c.Value.(model.DhcpReservation); ok {
			mac = r.MAC
		} else {
			mac = c.Value.(collector.ReservationDelete).MAC
		}
		fw.DhcpReservations = slices.DeleteFunc(slices.Clone(fw.DhcpReservations), func(x model.DhcpReservation) bool { return x.MAC == mac })
		if r, ok := c.Value.(model.DhcpReservation); ok {
			fw.DhcpReservations = append(fw.DhcpReservations, r)
		}
		s.devices[0] = fw
		return []model.Device{fw}, nil
	}
	if c.Kind == model.ChangeKindNetworkSet || c.Kind == model.ChangeKindNetworkDelete {
		fw := s.devices[0]
		fw.Networks = slices.Clone(fw.Networks)
		var id uint16
		if nw, ok := c.Value.(model.Network); ok {
			id = nw.Vlan
		} else {
			id = c.Value.(collector.NetworkDelete).Vlan
		}
		fw.Networks = slices.DeleteFunc(fw.Networks, func(x model.Network) bool { return x.Vlan == id })
		if nw, ok := c.Value.(model.Network); ok {
			fw.Networks = append(fw.Networks, nw)
		}
		s.devices[0] = fw
		return []model.Device{fw}, nil
	}
	sw := s.devices[1]
	sw.Interfaces, sw.Vlans = slices.Clone(sw.Interfaces), slices.Clone(sw.Vlans)
	if v, ok := c.Value.(collector.VlanValue); ok {
		sw.Vlans = slices.DeleteFunc(sw.Vlans, func(x model.Vlan) bool { return x.ID == v.ID })
		if c.Kind == model.ChangeKindVlanSet {
			sw.Vlans = append(sw.Vlans, model.Vlan{ID: v.ID, Name: model.Ptr(v.Name)})
		}
		s.devices[1] = sw
		return []model.Device{sw}, nil
	}
	for i := range sw.Interfaces {
		if sw.Interfaces[i].Name == *c.Port {
			if v, ok := c.Value.(model.PortVlans); ok {
				sw.Interfaces[i].Vlans = &v
			} else {
				sw.Interfaces[i].Enabled = model.Ptr(c.Value.(bool))
			}
		}
	}
	s.devices[1] = sw
	return []model.Device{sw}, nil
}

const (
	fwMAC    = model.MACAddress("58:9c:fc:00:00:01")
	swMAC    = model.MACAddress("1c:2a:a3:00:00:01")
	ominiMAC = model.MACAddress("bc:24:11:00:00:99")
	phoneMAC = model.MACAddress("92:34:c8:00:00:01")
	swID     = "dev:1c:2a:a3:00:00:01"
)

// A firewall and a switch: the firewall on Port 9, a phone alone on Port 2,
// Omini on Port 3 and a dumb switch with three PCs on Port 4.
func switchNetwork(t *testing.T) (*collector.Collector, *switchy, *store.Store) {
	t.Helper()
	return switchNetworkWith(t, nil)
}

// switchNetworkWith is switchNetwork with options of the test's.
func switchNetworkWith(t *testing.T, opt func(*collector.Options)) (*collector.Collector, *switchy, *store.Store) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := secret.New(make([]byte, 32))
	reg := integration.NewRegistry()
	s := &switchy{}
	reg.Register(s)
	var clock sync.Mutex
	now := time.Unix(1_790_000_000, 0).UTC()
	opts := collector.Options{
		Now: func() time.Time { // a second per call: what happened first is older
			clock.Lock()
			defer clock.Unlock()
			now = now.Add(time.Second)
			return now
		},
		Self:      collector.Self{MACs: []model.MACAddress{ominiMAC}, IPs: []string{"192.168.1.3"}},
		LocalNets: func() []netip.Prefix { return nil },
	}
	if opt != nil {
		opt(&opts)
	}
	coll := collector.New(st, reg, box, opts)
	if _, err := st.CreateIntegration(ctx, store.Integration{Name: "sw", Type: "switchy", Enabled: true, Config: integration.Config{}}); err != nil {
		t.Fatal(err)
	}
	var ports []model.Interface
	for _, p := range []string{"Port 2", "Port 3", "Port 4", "Port 9"} {
		ports = append(ports, model.Interface{
			Name: p, Up: model.Ptr(true), Enabled: model.Ptr(true), Vlans: &model.PortVlans{Untagged: model.Ptr(uint16(1))},
		})
	}
	s.devices = []model.Device{
		{
			Key: string(fwMAC), Name: "fw", Role: model.Ptr(model.DeviceRoleFirewall), MACs: []model.MACAddress{fwMAC},
			Arp:       []model.ArpEntry{{IP: "192.168.1.2", MAC: swMAC}, {IP: "192.168.1.41", MAC: phoneMAC}},
			DnsDomain: model.Ptr("lan"), DnsRecords: []model.DnsRecord{},
			Networks: []model.Network{{
				Vlan: 1, Name: model.Ptr("LAN"), Gateway: model.Ptr("192.168.1.1/24"),
				Dhcp:   &model.NetworkDhcp{Enabled: true, From: model.Ptr("192.168.1.100"), To: model.Ptr("192.168.1.250")},
				Access: model.Ptr(model.NetworkAccessAll),
			}},
		},
		{
			Key: string(swMAC), Name: "sw", Role: model.Ptr(model.DeviceRoleSwitch), MACs: []model.MACAddress{swMAC},
			Interfaces: ports,
			Vlans:      []model.Vlan{{ID: 1}, {ID: 10, Name: model.Ptr("Office")}, {ID: 20, Name: model.Ptr("Guests")}},
			Fdb: []model.FdbEntry{
				{MAC: fwMAC, Port: "Port 9"},
				{MAC: phoneMAC, Port: "Port 2"},
				{MAC: ominiMAC, Port: "Port 3"},
				{MAC: "00:11:22:00:00:01", Port: "Port 4"},
				{MAC: "00:11:22:00:00:02", Port: "Port 4"},
				{MAC: "00:11:22:00:00:03", Port: "Port 4"},
			},
		},
	}
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	return coll, s, st
}

func switchNode(t *testing.T, coll *collector.Collector) topology.Node {
	t.Helper()
	n, ok := node(coll.State(), swID)
	if !ok {
		t.Fatal("no switch on the map")
	}
	return n
}

func portEnabled(n topology.Node, port string) *bool {
	for _, i := range n.Device.Interfaces {
		if i.Name == port {
			return i.Enabled
		}
	}
	return nil
}

func TestProtectedPorts(t *testing.T) {
	coll, _, _ := switchNetwork(t)
	n := switchNode(t, coll)
	if !slices.Contains(n.Changes, model.ChangeKindPortEnabled) || !slices.Contains(n.Changes, model.ChangeKindVlanSet) {
		t.Fatalf("changes: %v", n.Changes)
	}
	if p := n.Protected["Port 3"]; p.Reason != "omini" {
		t.Errorf("Port 3 is Omini's way to the switch: %+v", p)
	}
	if p := n.Protected["Port 9"]; p.Reason != "internet" || p.Node != "dev:"+string(fwMAC) {
		t.Errorf("Port 9 leads to the firewall: %+v", p)
	}
	// Taking a device off the network is an ordinary change: a phone, a dumb
	// switch with three PCs.
	for _, port := range []string{"Port 2", "Port 4"} {
		if p, ok := n.Protected[port]; ok {
			t.Errorf("%s: %+v", port, p)
		}
	}
}

func TestDisableAPort(t *testing.T) {
	coll, s, st := switchNetwork(t)
	ctx := context.Background()
	rec, err := coll.Apply(ctx, collector.ChangeRequest{NodeID: swID, Kind: model.ChangeKindPortEnabled, Port: "Port 2", Value: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.applied) != 1 || s.applied[0].Device != string(swMAC) || *s.applied[0].Port != "Port 2" {
		t.Fatalf("applied: %+v", s.applied)
	}
	if e := portEnabled(switchNode(t, coll), "Port 2"); e == nil || *e {
		t.Fatal("the map shows the port disabled at once")
	}
	list, _ := st.ConfigChanges(ctx, swID, 10)
	if len(list) != 1 || list[0].ID != rec.ID || string(list[0].Before) != "true" || string(list[0].After) != "false" || list[0].Error != "" {
		t.Fatalf("history: %+v", list)
	}
}

func TestADestructiveChangeIsBlockedThenConfirmedTwice(t *testing.T) {
	coll, s, _ := switchNetwork(t)
	ctx := context.Background()
	req := collector.ChangeRequest{NodeID: swID, Kind: model.ChangeKindPortEnabled, Port: "Port 9", Value: false}
	var de *collector.DestructiveError
	if _, err := coll.Apply(ctx, req); !errors.As(err, &de) || de.Allowed || de.Reason != "internet" {
		t.Fatalf("blocked by default: %v", err)
	}
	if err := coll.SetAllowDestructive(ctx, true); err != nil {
		t.Fatal(err)
	}
	if _, err := coll.Apply(ctx, req); !errors.As(err, &de) || !de.Allowed {
		t.Fatalf("allowed, but asked again: %v", err)
	}
	if len(s.applied) != 0 {
		t.Fatal("nothing reaches the device before the second confirmation")
	}
	req.Confirmed = true
	if _, err := coll.Apply(ctx, req); err != nil || len(s.applied) != 1 {
		t.Fatalf("confirmed twice: %v", err)
	}
	// Enabling a port is never destructive.
	if _, err := coll.Apply(ctx, collector.ChangeRequest{NodeID: swID, Kind: model.ChangeKindPortEnabled, Port: "Port 9", Value: true}); err != nil {
		t.Fatal(err)
	}
}

func TestARefusedChangeIsKeptAndTheMapStays(t *testing.T) {
	coll, s, st := switchNetwork(t)
	ctx := context.Background()
	s.err = errors.New("the switch did not answer")
	_, err := coll.Apply(ctx, collector.ChangeRequest{NodeID: swID, Kind: model.ChangeKindPortEnabled, Port: "Port 2", Value: false})
	if err == nil {
		t.Fatal("the error reaches the user")
	}
	if e := portEnabled(switchNode(t, coll), "Port 2"); e == nil || !*e {
		t.Fatal("the port is still enabled on the map")
	}
	if list, _ := st.ConfigChanges(ctx, swID, 10); len(list) != 1 || list[0].Error != "the switch did not answer" {
		t.Fatalf("history: %+v", list)
	}
}

func TestInvalidChanges(t *testing.T) {
	coll, s, _ := switchNetwork(t)
	ctx := context.Background()
	for _, c := range []struct {
		req  collector.ChangeRequest
		want error
	}{
		{collector.ChangeRequest{NodeID: "mac:" + string(phoneMAC), Kind: model.ChangeKindPortEnabled, Port: "Port 2", Value: false}, collector.ErrNotChangeable},
		{collector.ChangeRequest{NodeID: swID, Kind: "port.vlan", Port: "Port 2", Value: 10}, collector.ErrNotChangeable},
		{collector.ChangeRequest{NodeID: swID, Kind: model.ChangeKindPortEnabled, Port: "Port 7", Value: false}, collector.ErrInvalidChange},
		{collector.ChangeRequest{NodeID: swID, Kind: model.ChangeKindPortEnabled, Port: "Port 2", Value: "off"}, collector.ErrInvalidChange},
	} {
		if _, err := coll.Apply(ctx, c.req); !errors.Is(err, c.want) {
			t.Errorf("%+v: %v, want %v", c.req, err, c.want)
		}
	}
	if len(s.applied) != 0 {
		t.Fatalf("nothing reaches the device: %+v", s.applied)
	}
}

// A change during a round waits only for the switch's own collection, and that
// collection (read before the change) does not put the old state back.
func TestAChangeDuringARoundIsNotUndoneByIt(t *testing.T) {
	coll, s, _ := switchNetwork(t)
	ctx := context.Background()
	reading, release := make(chan struct{}), make(chan struct{})
	s.mu.Lock()
	s.hold = func() {
		close(reading)
		<-release
	}
	s.mu.Unlock()
	round := make(chan error)
	go func() { round <- coll.CollectNow(ctx) }()
	<-reading // the round read the switch: Port 2 enabled

	applied := make(chan error)
	go func() {
		_, err := coll.Apply(ctx, collector.ChangeRequest{NodeID: swID, Kind: model.ChangeKindPortEnabled, Port: "Port 2", Value: false})
		applied <- err
	}()
	select {
	case err := <-applied:
		t.Fatalf("the change ran while the switch was being read: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	s.mu.Lock()
	s.hold = nil
	s.mu.Unlock()
	close(release)
	if err := <-applied; err != nil {
		t.Fatal(err)
	}
	if err := <-round; err != nil {
		t.Fatal(err)
	}
	if e := portEnabled(switchNode(t, coll), "Port 2"); e == nil || *e {
		t.Fatal("the round's older read put Port 2 back as enabled")
	}
}

func TestProtectionFollowsOminiAndTheInternet(t *testing.T) {
	dev := func(id, role string) topology.Node {
		return topology.Node{ID: id, Kind: topology.KindDevice, Role: role, Device: &model.Device{Key: id}}
	}
	edge := func(src, sport, dst, dport string) topology.Edge {
		return topology.Edge{ID: src + dst, Source: src, SourcePort: sport, Target: dst, TargetPort: dport, Kind: topology.EdgeFDB}
	}
	topo := topology.Topology{
		Nodes: []topology.Node{
			dev("fw", "firewall"), dev("sw1", "switch"), dev("sw2", "switch"), dev("ap", "ap"), dev("fw2", "router"),
			{ID: "mac:42", Kind: topology.KindClient, IP: "192.168.1.42", ParentID: "sw2", Port: "Port 1"},
			{ID: "mac:phone", Kind: topology.KindClient, ParentID: "ap"},
		},
		Edges: []topology.Edge{
			edge("fw", "lan", "sw1", "Port 9"),
			edge("sw1", "Port 3", "sw2", "Port 8"),
			edge("sw2", "Port 1", "mac:42", ""),
			edge("sw1", "Port 5", "ap", "eth0"),
			edge("ap", "wlan0", "mac:phone", ""),
			edge("sw1", "Port 7", "fw2", "wan"), // a router behind the switch (double NAT)
		},
	}
	got := collector.ProtectedPorts(topo, collector.Self{IPs: []string{"192.168.1.42"}}) // found by its address
	want := map[string]map[string]string{
		"sw1": {"Port 9": "internet", "Port 3": "omini", "Port 7": "internet"},
		"sw2": {"Port 1": "omini", "Port 8": "internet"},
		"ap":  {"eth0": "omini"}, // Omini is upstream of the access point
		"fw2": {"wan": "omini"},
	}
	for node, ports := range want {
		for port, reason := range ports {
			if p := got[node][port]; p.Reason != reason {
				t.Errorf("%s %s: %+v, want %s", node, port, p, reason)
			}
		}
		if len(got[node]) != len(ports) {
			t.Errorf("%s: %+v", node, got[node])
		}
	}
	if p := got["sw1"]["Port 5"]; p.Reason != "" {
		t.Errorf("the access point's port is an ordinary change: %+v", p)
	}

	// Omini not on the map: only the way to the internet is known.
	got = collector.ProtectedPorts(topo, collector.Self{})
	if _, ok := got["sw1"]["Port 3"]; ok || got["sw1"]["Port 9"].Reason != "internet" {
		t.Errorf("unknown Omini: %+v", got["sw1"])
	}
}

// A device whose ports report no state (the firewall here) offers no port
// change, only what it reports (its networks); on the switch, a port that
// reports none cannot be changed either.
func TestPortChangesNeedThePortState(t *testing.T) {
	coll, s, _ := switchNetwork(t)
	if fw, _ := node(coll.State(), "dev:"+string(fwMAC)); !slices.Equal(fw.Changes, []model.ChangeKind{
		model.ChangeKindNetworkSet, model.ChangeKindNetworkDelete, model.ChangeKindReservationSet, model.ChangeKindReservationDelete,
		model.ChangeKindDnsSet, model.ChangeKindDnsDelete, model.ChangeKindWolSend, model.ChangeKindPauseSet, model.ChangeKindPauseDelete,
	}) {
		t.Fatalf("firewall: %v", fw.Changes)
	}
	s.mu.Lock()
	sw := s.devices[1]
	sw.Interfaces = append(slices.Clone(sw.Interfaces), model.Interface{Name: "Port 6"})
	s.devices[1] = sw
	s.mu.Unlock()
	if err := coll.CollectNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err := coll.Apply(context.Background(), collector.ChangeRequest{NodeID: swID, Kind: model.ChangeKindPortEnabled, Port: "Port 6", Value: false})
	if !errors.Is(err, collector.ErrInvalidChange) {
		t.Fatalf("a port without a state: %v", err)
	}
}

func TestChangeAPortsVLANs(t *testing.T) {
	coll, s, st := switchNetwork(t)
	ctx := context.Background()
	if n := switchNode(t, coll); !slices.Contains(n.Changes, model.ChangeKindPortVlans) {
		t.Fatalf("changes: %v", n.Changes)
	}
	vlans := func(port string) *model.PortVlans {
		for _, i := range switchNode(t, coll).Device.Interfaces {
			if i.Name == port {
				return i.Vlans
			}
		}
		return nil
	}
	req := func(port string, value any) collector.ChangeRequest {
		return collector.ChangeRequest{NodeID: swID, Kind: model.ChangeKindPortVlans, Port: port, Value: value}
	}
	// Access: one VLAN untagged (as the UI sends it, decoded from JSON).
	if _, err := coll.Apply(ctx, req("Port 2", map[string]any{"untagged": float64(10)})); err != nil {
		t.Fatal(err)
	}
	if v := vlans("Port 2"); v == nil || *v.Untagged != 10 || len(v.Tagged) != 0 {
		t.Fatalf("access: %+v", v)
	}
	// Trunk: tagged VLANs (sorted, once each) and no native VLAN.
	if _, err := coll.Apply(ctx, req("Port 2", map[string]any{"tagged": []any{float64(20), float64(10), float64(20)}})); err != nil {
		t.Fatal(err)
	}
	if v := vlans("Port 2"); v.Untagged != nil || !slices.Equal(v.Tagged, []uint16{10, 20}) {
		t.Fatalf("trunk: %+v", v)
	}
	list, _ := st.ConfigChanges(ctx, swID, 10)
	if len(list) != 2 || string(list[1].Before) != `{"untagged":1}` || string(list[0].After) != `{"tagged":[10,20]}` {
		t.Fatalf("history: %+v", list)
	}

	for _, bad := range []any{
		map[string]any{"untagged": float64(99)},                        // the device has no VLAN 99
		map[string]any{"untagged": float64(10), "tagged": []any{10.0}}, // both untagged and tagged
		map[string]any{},                    // no VLAN
		map[string]any{"pvid": float64(10)}, // not a port.vlans value
		true,
	} {
		if _, err := coll.Apply(ctx, req("Port 4", bad)); !errors.Is(err, collector.ErrInvalidChange) {
			t.Errorf("%v: %v", bad, err)
		}
	}
	// The uplink's VLANs are protected like its state.
	var de *collector.DestructiveError
	if _, err := coll.Apply(ctx, req("Port 9", map[string]any{"untagged": float64(10)})); !errors.As(err, &de) {
		t.Fatalf("uplink: %v", err)
	}
	if len(s.applied) != 2 {
		t.Fatalf("applied: %+v", s.applied)
	}
}

// A port Omini disables and enables goes down and up: that is no flapping
// link. Another port doing the same on its own is.
func TestOminisOwnPortTogglesAreNoFlapping(t *testing.T) {
	coll, s, _ := switchNetwork(t)
	ctx := context.Background()
	sw := "dev:" + string(swMAC)
	for _, on := range []bool{false, true, false, true} {
		if _, err := coll.Apply(ctx, collector.ChangeRequest{NodeID: sw, Kind: model.ChangeKindPortEnabled, Port: "Port 2", Value: on}); err != nil {
			t.Fatal(err)
		}
		s.mu.Lock()
		for i, p := range s.devices[1].Interfaces {
			if p.Name == "Port 2" || p.Name == "Port 4" {
				s.devices[1].Interfaces[i].Up = model.Ptr(on)
			}
		}
		s.mu.Unlock()
		if err := coll.CollectNow(ctx); err != nil {
			t.Fatal(err)
		}
	}
	flapping := map[string]bool{}
	for _, a := range coll.State().Alerts {
		if a.Rule == "link_flapping" {
			flapping[fmt.Sprint(a.Params["iface"])] = true
		}
	}
	if flapping["Port 2"] || !flapping["Port 4"] {
		t.Fatalf("flapping: %v", flapping)
	}
}
