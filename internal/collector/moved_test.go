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
