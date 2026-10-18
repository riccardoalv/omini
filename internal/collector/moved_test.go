package collector_test

import (
	"context"
	"testing"

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
