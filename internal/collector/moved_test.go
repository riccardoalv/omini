package collector_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
)

func TestADeviceWhoseKeyChangedKeepsItsHistory(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.addIntegration(t, "fake", integration.Config{})
	first, stable := model.MACAddress("aa:00:00:00:00:05"), model.MACAddress("aa:00:00:00:00:01")
	sw := model.Device{Key: string(first), Name: "sw", MACs: []model.MACAddress{stable, first}}
	e.fake.set([]model.Device{sw}, nil)
	e.collect(t)
	alias := "Rack switch"
	if _, err := e.st.UpdateInventory(ctx, "dev:"+string(first), store.InventoryUpdate{Alias: &alias}); err != nil {
		t.Fatal(err)
	}

	// A newer integration keys the same switch by another of its MACs.
	sw.Key = string(stable)
	e.fake.set([]model.Device{sw}, nil)
	s := e.collect(t)

	n, ok := node(s, "dev:"+string(stable))
	if !ok || n.Label != alias {
		t.Fatalf("the switch keeps its alias under its new id: %+v (found=%v)", n, ok)
	}
	inv, err := e.st.ListInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range inv {
		if en.ID == "dev:"+string(first) {
			t.Fatalf("the old id is gone from the inventory: %+v", inv)
		}
	}
}

func TestANewDeviceDoesNotTakeAnotherDevicesHistory(t *testing.T) {
	e := setup(t)
	e.addIntegration(t, "fake", integration.Config{})
	a := model.Device{Key: "aa:00:00:00:00:01", Name: "a"}
	b := model.Device{Key: "aa:00:00:00:00:02", Name: "b", MACs: []model.MACAddress{"aa:00:00:00:00:01"}}
	e.fake.set([]model.Device{a}, nil)
	e.collect(t)
	// a is still on the map: b, which also lists a's MAC, is merged into it or
	// stays itself, but never renames a.
	e.fake.set([]model.Device{a, b}, nil)
	s := e.collect(t)
	if _, ok := node(s, "dev:aa:00:00:00:00:01"); !ok {
		t.Fatalf("a device still on the map keeps its id: %+v", s.Topology.Nodes)
	}
}

func TestADeviceKeyedByItsAddressBeforeKeepsItsHistory(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.addIntegration(t, "fake", integration.Config{})
	// An older plugin keyed the DNS server by its address; the newer one by its MAC.
	dns := model.Device{Key: "192.168.1.5", Name: "adguard", Host: model.Ptr("192.168.1.5"), IPs: []string{"192.168.1.5"}}
	e.fake.set([]model.Device{dns}, nil)
	s := e.collect(t)
	var old string
	for _, n := range s.Topology.Nodes {
		if n.Label == "adguard" {
			old = n.ID
		}
	}
	if old == "" {
		t.Fatalf("no node for the DNS server: %+v", s.Topology.Nodes)
	}
	alias := "DNS"
	if _, err := e.st.UpdateInventory(ctx, old, store.InventoryUpdate{Alias: &alias}); err != nil {
		t.Fatal(err)
	}
	dns.Key, dns.MACs = "aa:00:00:00:00:53", []model.MACAddress{"aa:00:00:00:00:53"}
	e.fake.set([]model.Device{dns}, nil)
	s = e.collect(t)
	n, ok := node(s, "dev:aa:00:00:00:00:53")
	if !ok || n.Label != alias {
		t.Fatalf("the DNS server keeps its alias under its MAC: %+v (found=%v, old=%s)", n, ok, old)
	}
}

func TestAContainerOrVMIsNoNewDevice(t *testing.T) {
	e := setup(t)
	e.addIntegration(t, "fake", integration.Config{})
	host := model.Device{Key: "aa:00:00:00:00:42", Name: "docker", Host: model.Ptr("192.168.1.42"), MACs: []model.MACAddress{"aa:00:00:00:00:42"}}
	e.fake.set([]model.Device{host}, nil)
	e.collect(t)
	// After the grace period a stack is deployed: a container its host reports,
	// and a stranger's phone shows up on the network.
	e.clock = e.clock.Add(collector.NewDeviceGrace + time.Minute)
	container := model.Device{Key: "docker:192.168.1.42:web", Name: "web", Neighbors: []model.Neighbor{{
		LocalPort: "bridge", Protocol: model.Ptr(model.NeighborProtocolOther),
		RemoteIP: model.Ptr("192.168.1.42"), RemotePlatform: model.Ptr("Docker"),
	}}}
	phone := model.Device{Key: "aa:00:00:00:00:99", Name: "phone", MACs: []model.MACAddress{"aa:00:00:00:00:99"}}
	e.fake.set([]model.Device{host, container, phone}, nil)
	s := e.collect(t)
	var news []string
	for _, a := range s.Alerts {
		if a.Rule == "new_device" {
			news = append(news, a.NodeID)
		}
	}
	if len(news) != 1 || news[0] != "dev:aa:00:00:00:00:99" {
		t.Fatalf("only the phone is a new device, not the container: %v", news)
	}
}

// A firewall's LAN moved to a new card: for a while both its old MAC and its
// new one were on the map; when the old one leaves, its history (and its
// first sighting) goes to the new one, which is then no new device.
func TestAFirewallOnANewCardKeepsItsHistory(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.addIntegration(t, "fake", integration.Config{})
	oldFW := model.Device{Key: "58:9c:fc:10:8f:2c", Name: "OPNsense.internal", Host: model.Ptr("192.168.1.1"), MACs: []model.MACAddress{"58:9c:fc:10:8f:2c"}}
	newFW := model.Device{Key: "24:8a:07:f7:bb:2a", Name: "OPNsense.internal", Host: model.Ptr("192.168.1.10"), MACs: []model.MACAddress{"24:8a:07:f7:bb:2a"}}
	e.fake.set([]model.Device{oldFW}, nil)
	e.collect(t)
	alias := "Firewall"
	if _, err := e.st.UpdateInventory(ctx, "dev:58:9c:fc:10:8f:2c", store.InventoryUpdate{Alias: &alias}); err != nil {
		t.Fatal(err)
	}
	// Past the grace period the new card shows up next to the old one.
	e.clock = e.clock.Add(collector.NewDeviceGrace + time.Minute)
	e.fake.set([]model.Device{oldFW, newFW}, nil)
	e.collect(t)
	// The old card is gone; the new one answers at the firewall's address.
	newFW.Host = model.Ptr("192.168.1.1")
	e.clock = e.clock.Add(time.Minute)
	e.fake.set([]model.Device{newFW}, nil)
	s := e.collect(t)
	n, ok := node(s, "dev:24:8a:07:f7:bb:2a")
	if !ok || n.Label != alias {
		t.Fatalf("the firewall keeps its alias on its new card: %+v (found=%v)", n, ok)
	}
	for _, a := range s.Alerts {
		if a.Rule == "new_device" && a.NodeID == "dev:24:8a:07:f7:bb:2a" {
			t.Fatalf("the firewall on its new card is no new device: %+v", a)
		}
	}
}

func TestASubnetStandInIsNoNewDevice(t *testing.T) {
	e := setup(t)
	e.addIntegration(t, "fake", integration.Config{})
	e.fake.set([]model.Device{{Key: "aa:00:00:00:00:01", Name: "sw", MACs: []model.MACAddress{"aa:00:00:00:00:01"}}}, nil)
	e.collect(t)
	e.clock = e.clock.Add(collector.NewDeviceGrace + time.Minute)
	// The network scan's stand-in for a subnet: no MAC, no address.
	standIn := model.Device{
		Key: "net:192.168.1.0/24", Name: "Network 192.168.1.0/24",
		Hosts: []model.Host{{IP: "192.168.1.77"}},
	}
	e.fake.set([]model.Device{{Key: "aa:00:00:00:00:01", Name: "sw", MACs: []model.MACAddress{"aa:00:00:00:00:01"}}, standIn}, nil)
	s := e.collect(t)
	for _, a := range s.Alerts {
		if a.Rule == "new_device" && strings.Contains(a.NodeID, "net:") {
			t.Fatalf("a subnet's stand-in is no new device: %+v", a)
		}
	}
}
