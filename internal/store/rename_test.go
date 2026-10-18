package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestRenameNodeKeepsWhatWasKept(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := time.Unix(1_800_000_000, 0)
	old, moved := "dev:aa:aa:aa:aa:aa:01", "dev:aa:aa:aa:aa:aa:02"
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.MarkSeen(ctx, []InventoryEntry{
		{ID: old, Kind: "device", Label: "Core switch"},
		{ID: "mac:cc:cc:cc:cc:cc:cc", Kind: "client", Label: "NAS", ParentID: old, Port: "Port 3"},
	}, now.Add(-30*24*time.Hour)))
	alias := "Rack switch"
	_, err := s.UpdateInventory(ctx, old, InventoryUpdate{Alias: &alias})
	must(err)
	must(s.SaveLayout(ctx, map[string]Point{old: {X: 10, Y: 20}}))
	must(s.SetPortLabel(ctx, old, "Port 3", "NAS"))
	_, err = s.AddConfigChange(ctx, ConfigChange{
		NodeID: old, IntegrationID: 1, Kind: "port.enabled", Port: "Port 5",
		After: json.RawMessage("false"), At: now.Add(-time.Hour),
	})
	must(err)
	area, err := s.CreateArea(ctx, Area{Name: "Rack", Color: "blue", Direction: "RIGHT", Width: 100, Height: 100, Members: []string{old, "dev:other"}})
	must(err)

	must(s.RenameNode(ctx, old, moved))

	inv, err := s.ListInventory(ctx)
	must(err)
	byID := map[string]InventoryEntry{}
	for _, e := range inv {
		byID[e.ID] = e
	}
	if _, ok := byID[old]; ok {
		t.Fatalf("the old entry is gone: %+v", inv)
	}
	got := byID[moved]
	if got.Alias != alias || !got.FirstSeen.Equal(now.Add(-30*24*time.Hour)) {
		t.Fatalf("the entry keeps its alias and first sighting: %+v", got)
	}
	if byID["mac:cc:cc:cc:cc:cc:cc"].ParentID != moved {
		t.Fatalf("clients follow their parent: %+v", byID["mac:cc:cc:cc:cc:cc:cc"])
	}
	layout, err := s.GetLayout(ctx)
	must(err)
	if _, ok := layout[moved]; !ok {
		t.Fatalf("the position follows: %+v", layout)
	}
	labels, err := s.PortLabels(ctx)
	must(err)
	if labels[moved]["Port 3"] != "NAS" {
		t.Fatalf("port labels follow: %+v", labels)
	}
	changes, err := s.ConfigChanges(ctx, moved, 10)
	must(err)
	if len(changes) != 1 {
		t.Fatalf("the change history follows: %+v", changes)
	}
	areas, err := s.ListAreas(ctx)
	must(err)
	for _, a := range areas {
		if a.ID == area.ID && (len(a.Members) != 2 || a.Members[0] != moved) {
			t.Fatalf("areas follow: %+v", a.Members)
		}
	}
}

func TestRenameNodeLetsTheNewNodeWin(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	old, moved := "dev:old", "dev:new"
	if err := s.SaveLayout(ctx, map[string]Point{old: {X: 1, Y: 1}, moved: {X: 2, Y: 2}}); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameNode(ctx, old, moved); err != nil {
		t.Fatal(err)
	}
	layout, err := s.GetLayout(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(layout) != 1 || layout[moved] != (Point{X: 2, Y: 2}) {
		t.Fatalf("the new node's own position wins: %+v", layout)
	}
}
