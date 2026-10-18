package collector_test

import (
	"context"
	"errors"
	"testing"

	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
)

func network(gw, from, to string, access model.NetworkAccess) *model.Network {
	return &model.Network{
		Gateway: &gw,
		Dhcp:    &model.NetworkDhcp{Enabled: true, From: &from, To: &to},
		Access:  &access,
	}
}

// The router's networks come into the catalog: the LAN on VLAN 1.
func TestTheRoutersNetworksAreImported(t *testing.T) {
	coll, _, _ := switchNetwork(t)
	list, err := coll.Vlans(context.Background())
	if err != nil || list[0].ID != 1 || list[0].Network == nil || model.Deref(list[0].Network.Gateway) != "192.168.1.1/24" {
		t.Fatalf("imported: %+v %v", list, err)
	}
}

// Creating a VLAN with a network makes the router serve it, before the
// catalog keeps it; the same network again changes nothing.
func TestAVLANsNetworkGoesToTheRouter(t *testing.T) {
	coll, s, _ := switchNetwork(t)
	ctx := context.Background()
	if _, err := coll.Vlans(ctx); err != nil {
		t.Fatal(err)
	}
	v := store.Vlan{ID: 30, Name: "IOT", Network: network("192.168.30.1/24", "192.168.30.100", "192.168.30.250", model.NetworkAccessInternet)}
	saved, done, err := coll.SaveVlan(ctx, v, false)
	if err != nil || len(done) != 1 || len(s.applied) != 1 || saved.Subnet != "192.168.30.0/24" {
		t.Fatalf("saved %+v %+v %v", saved, done, err)
	}
	nw := s.applied[0].Value.(model.Network)
	if s.applied[0].Kind != model.ChangeKindNetworkSet || nw.Vlan != 30 || model.Deref(nw.Name) != "IOT" {
		t.Fatalf("router change: %+v", s.applied[0])
	}
	if _, done, err := coll.SaveVlan(ctx, v, false); err != nil || len(done) != 0 {
		t.Fatalf("again: %+v %v", done, err)
	}
	// A network that does not hold together never reaches the router.
	bad := store.Vlan{ID: 31, Network: network("192.168.31.1/24", "192.168.32.1", "192.168.32.9", model.NetworkAccessInternet)}
	if _, _, err := coll.SaveVlan(ctx, bad, false); !errors.Is(err, store.ErrInvalidVlan) || len(s.applied) != 1 {
		t.Fatalf("bad: %v", err)
	}
}

// Omini is on the LAN: moving its gateway is destructive, its DHCP range is not.
func TestTheNetworkOminiIsInIsProtected(t *testing.T) {
	coll, s, _ := switchNetwork(t)
	ctx := context.Background()
	list, _ := coll.Vlans(ctx)
	lan := list[0]
	lan.Network = network("192.168.1.1/24", "192.168.1.50", "192.168.1.250", model.NetworkAccessAll)
	if _, _, err := coll.SaveVlan(ctx, lan, false); err != nil || len(s.applied) != 1 {
		t.Fatalf("range: %v", err)
	}
	lan.Network = network("192.168.2.1/24", "192.168.2.50", "192.168.2.250", model.NetworkAccessAll)
	var de *collector.DestructiveError
	if _, _, err := coll.SaveVlan(ctx, lan, true); !errors.As(err, &de) || de.Reason != "omini" || de.Allowed {
		t.Fatalf("gateway: %v", err)
	}
	if list, _ := coll.Vlans(ctx); model.Deref(list[0].Network.Gateway) != "192.168.1.1/24" {
		t.Fatalf("the catalog kept what the router refused: %+v", list[0].Network)
	}
}

// Deleting a VLAN takes its network off the router first.
func TestDeletingAVLANRemovesItsNetwork(t *testing.T) {
	coll, s, _ := switchNetwork(t)
	ctx := context.Background()
	if _, err := coll.Vlans(ctx); err != nil {
		t.Fatal(err)
	}
	v := store.Vlan{ID: 30, Name: "IOT", Network: network("192.168.30.1/24", "192.168.30.100", "192.168.30.250", model.NetworkAccessLocal)}
	if _, _, err := coll.SaveVlan(ctx, v, false); err != nil {
		t.Fatal(err)
	}
	done, err := coll.DeleteVlan(ctx, 30, false)
	if err != nil || len(done) != 1 || s.applied[1].Kind != model.ChangeKindNetworkDelete {
		t.Fatalf("deleted: %+v %v", done, err)
	}
	if _, err := coll.Apply(ctx, collector.ChangeRequest{
		NodeID: "dev:" + string(fwMAC), Kind: model.ChangeKindNetworkDelete, Value: collector.NetworkDelete{Vlan: 1},
	}); !errors.Is(err, collector.ErrInvalidChange) {
		t.Fatalf("the LAN: %v", err)
	}
}

// A device's address is fixed on the router whose network holds it, and
// freed again; an address outside its networks is refused.
func TestReservingADevicesAddress(t *testing.T) {
	coll, s, st := switchNetwork(t)
	ctx := context.Background()
	phone := ""
	for _, n := range coll.State().Topology.Nodes {
		if n.MAC == string(phoneMAC) {
			phone = n.ID
		}
	}
	if phone == "" {
		t.Fatal("no phone on the map")
	}
	if _, err := coll.Reserve(ctx, phone, "192.168.1.41"); err != nil {
		t.Fatal(err)
	}
	r := s.applied[0].Value.(model.DhcpReservation)
	if s.applied[0].Kind != model.ChangeKindReservationSet || r.MAC != phoneMAC || r.IP != "192.168.1.41" {
		t.Fatalf("reserved: %+v", s.applied[0])
	}
	if _, err := coll.Reserve(ctx, phone, "10.9.9.9"); !errors.Is(err, collector.ErrNotChangeable) {
		t.Fatalf("outside: %v", err)
	}
	if _, err := coll.Reserve(ctx, phone, "192.168.1.1"); !errors.Is(err, collector.ErrNotChangeable) {
		t.Fatalf("the gateway: %v", err)
	}
	if _, err := coll.Unreserve(ctx, phone); err != nil || s.applied[1].Kind != model.ChangeKindReservationDelete {
		t.Fatalf("freed: %v", err)
	}
	if _, err := coll.Unreserve(ctx, phone); !errors.Is(err, collector.ErrNotChangeable) {
		t.Fatalf("again: %v", err)
	}
	// The router made them, for the phone: both show in the phone's history.
	h, err := st.ConfigChanges(ctx, phone, 10)
	if err != nil || len(h) < 2 || h[0].Kind != string(model.ChangeKindReservationDelete) || h[0].TargetID != phone || h[0].NodeID != "dev:"+string(fwMAC) {
		t.Fatalf("history: %+v %v", h, err)
	}
}

// A named device gets <name>.<domain> on the DNS server when its address is
// fixed; renaming it, turning the name off and freeing the address follow.
func TestDNSNames(t *testing.T) {
	coll, s, st := switchNetwork(t)
	ctx := context.Background()
	phone := ""
	for _, n := range coll.State().Topology.Nodes {
		if n.MAC == string(phoneMAC) {
			phone = n.ID
		}
	}
	name := func() collector.DNSName {
		t.Helper()
		got, err := coll.DNSNameOf(ctx, phone)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := name(); got.Missing != "no_fixed_ip" {
		t.Fatalf("before: %+v", got)
	}
	// Known by its address only: no name, even with a fixed address.
	if _, err := coll.Reserve(ctx, phone, "192.168.1.41"); err != nil || len(s.applied) != 1 {
		t.Fatalf("reserve: %v %+v", err, s.applied)
	}
	if got := name(); got.Missing != "no_name" {
		t.Fatalf("no name: %+v", got)
	}
	if _, err := st.UpdateInventory(ctx, phone, store.InventoryUpdate{Alias: model.Ptr("Living Room TV")}); err != nil {
		t.Fatal(err)
	}
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	if err := coll.SyncDNSName(ctx, phone); err != nil {
		t.Fatal(err)
	}
	last := func() model.Change { return s.applied[len(s.applied)-1] }
	if v, ok := last().Value.(model.DnsRecord); !ok || v.Name != "living-room-tv.lan" || v.IP != "192.168.1.41" {
		t.Fatalf("named: %+v", last())
	}
	n := len(s.applied)
	if err := coll.SyncDNSName(ctx, phone); err != nil || len(s.applied) != n {
		t.Fatalf("again: nothing to do (%v, %+v)", err, s.applied[n:])
	}
	// The user's name: the old one goes, the new one comes.
	got, err := coll.SetDNSName(ctx, phone, "tv")
	if err != nil || got.Name != "tv.lan" || !got.Custom {
		t.Fatalf("custom: %+v %v", got, err)
	}
	if d, ok := s.applied[n].Value.(collector.DnsDelete); !ok || d.Name != "living-room-tv.lan" {
		t.Fatalf("old name removed first: %+v", s.applied[n:])
	}
	if _, err := coll.SetDNSName(ctx, phone, "Not A Label"); !errors.Is(err, collector.ErrInvalidChange) {
		t.Fatalf("invalid: %v", err)
	}
	// Turned off, then on with a domain set in Omini.
	if got, err := coll.SetDNSName(ctx, phone, "-"); err != nil || got.Name != "" || !got.Off {
		t.Fatalf("off: %+v %v", got, err)
	}
	if d, ok := last().Value.(collector.DnsDelete); !ok || d.Name != "tv.lan" {
		t.Fatalf("off removes it: %+v", last())
	}
	if err := st.SetSetting(ctx, collector.DNSDomainSetting, "home.arpa"); err != nil {
		t.Fatal(err)
	}
	if got, err := coll.SetDNSName(ctx, phone, ""); err != nil || got.Name != "living-room-tv.home.arpa" {
		t.Fatalf("back on: %+v %v", got, err)
	}
	// Freeing the address takes the name away.
	if _, err := coll.Unreserve(ctx, phone); err != nil {
		t.Fatal(err)
	}
	if d, ok := last().Value.(collector.DnsDelete); !ok || d.Name != "living-room-tv.home.arpa" {
		t.Fatalf("freed: %+v", last())
	}
}

// A router whose network another server hands out (a Pi-hole) is never asked
// for a reservation: it would do nothing.
func TestNoReservationOnARouterWithoutDHCP(t *testing.T) {
	coll, s, _ := switchNetwork(t)
	ctx := context.Background()
	s.mu.Lock()
	s.devices[0].Networks[0].Dhcp.Enabled = false
	s.mu.Unlock()
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	phone := ""
	for _, n := range coll.State().Topology.Nodes {
		if n.MAC == string(phoneMAC) {
			phone = n.ID
		}
	}
	if _, err := coll.Reserve(ctx, phone, "192.168.1.41"); !errors.Is(err, collector.ErrNotChangeable) {
		t.Fatalf("reserved on a router without DHCP: %v %+v", err, s.applied)
	}
}

// Another address for Omini's own host would move Omini at its next lease
// renewal: refused unless destructive changes are allowed; its current one is
// fine.
func TestOminisOwnHostIsNotMoved(t *testing.T) {
	coll, s, _ := switchNetwork(t)
	ctx := context.Background()
	s.mu.Lock()
	s.devices[0].Arp = append(s.devices[0].Arp, model.ArpEntry{IP: "192.168.1.3", MAC: ominiMAC})
	s.mu.Unlock()
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	omini := coll.OminiNode()
	if omini == "" {
		t.Fatal("Omini is not on the map")
	}
	if _, err := coll.Reserve(ctx, omini, "192.168.1.60"); !errors.Is(err, collector.ErrInvalidChange) {
		t.Fatalf("moved Omini: %v", err)
	}
	if _, err := coll.Reserve(ctx, omini, "192.168.1.3"); err != nil {
		t.Fatalf("its own address: %v", err)
	}
	if err := coll.SetAllowDestructive(ctx, true); err != nil {
		t.Fatal(err)
	}
	if _, err := coll.Reserve(ctx, omini, "192.168.1.61"); err != nil {
		t.Fatalf("allowed: %v", err)
	}
}
