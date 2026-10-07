package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omini.db")
	for range 2 {
		s, err := Open(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
	}
}

func TestIntegrationCRUDAndSnapshotCascade(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	created, err := s.CreateIntegration(ctx, Integration{
		Name: "core switch", Type: "snmp", Enabled: true,
		Config: integration.Config{"host": "192.168.1.2", "port": float64(161)},
	})
	if err != nil {
		t.Fatal(err)
	}
	created.Name = "sw-core"
	created.Enabled = false
	updated, err := s.UpdateIntegration(ctx, created)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "sw-core" || updated.Enabled || updated.Config.String("host") != "192.168.1.2" {
		t.Fatalf("unexpected update result: %+v", updated)
	}

	err = s.SaveSnapshot(ctx, Snapshot{
		IntegrationID: created.ID, CollectedAt: time.Now(), OK: true,
		Devices: []model.Device{{Key: "k", Name: "sw-core"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	snaps, _ := s.ListSnapshots(ctx)
	if len(snaps) != 1 || snaps[0].Devices[0].Name != "sw-core" {
		t.Fatalf("unexpected snapshots: %+v", snaps)
	}

	if err := s.DeleteIntegration(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if snaps, _ := s.ListSnapshots(ctx); len(snaps) != 0 {
		t.Fatal("snapshot should be deleted with its integration")
	}
	if _, err := s.GetIntegration(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMarkSeenKeepsUserFields(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	first := time.Unix(1_700_000_000, 0)

	entry := InventoryEntry{ID: "mac:aa:bb:cc:00:00:01", Kind: "client", Label: "phone", MAC: "aa:bb:cc:00:00:01", IP: "192.168.1.50"}
	if err := s.MarkSeen(ctx, []InventoryEntry{entry}, first); err != nil {
		t.Fatal(err)
	}
	alias, pinned := "Ricardo's phone", true
	if _, err := s.UpdateInventory(ctx, entry.ID, InventoryUpdate{Alias: &alias, Pinned: &pinned}); err != nil {
		t.Fatal(err)
	}

	entry.IP = "" // not observed this time: keep the previous value
	if err := s.MarkSeen(ctx, []InventoryEntry{entry}, first.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	all, _ := s.ListInventory(ctx)
	got := all[0]
	if got.Alias != alias || !got.Pinned || got.IP != "192.168.1.50" {
		t.Fatalf("user fields or previous values lost: %+v", got)
	}
	if !got.FirstSeen.Equal(first) || !got.LastSeen.Equal(first.Add(time.Hour)) {
		t.Fatalf("unexpected timestamps: first=%v last=%v", got.FirstSeen, got.LastSeen)
	}
}

func TestClassificationOverrides(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if err := s.MarkSeen(ctx, []InventoryEntry{{ID: "mac:aa", Kind: "client", Label: "x"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	phone, android := "phone", "android"
	e, err := s.UpdateInventory(ctx, "mac:aa", InventoryUpdate{DeviceType: &phone, Icon: &android})
	if err != nil || e.DeviceType != "phone" || e.Icon != "android" {
		t.Fatalf("override not saved: %+v, %v", e, err)
	}
	empty := ""
	e, _ = s.UpdateInventory(ctx, "mac:aa", InventoryUpdate{DeviceType: &empty})
	if e.DeviceType != "" || e.Icon != "android" {
		t.Fatalf("empty must reset only that field: %+v", e)
	}
}

func TestHideDevice(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if err := s.MarkSeen(ctx, []InventoryEntry{{ID: "mac:aa", Kind: "client", Label: "x"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	yes, no := true, false
	e, err := s.UpdateInventory(ctx, "mac:aa", InventoryUpdate{Hidden: &yes})
	if err != nil || !e.Hidden || e.Pinned {
		t.Fatalf("hide: %+v, %v", e, err)
	}
	// Seen again: still hidden (user fields are never overwritten).
	if err := s.MarkSeen(ctx, []InventoryEntry{{ID: "mac:aa", Kind: "client", Label: "x"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.ListInventory(ctx); !list[0].Hidden {
		t.Fatal("a hidden device must stay hidden when seen again")
	}
	if e, _ = s.UpdateInventory(ctx, "mac:aa", InventoryUpdate{Hidden: &no}); e.Hidden {
		t.Fatal("unhide failed")
	}
}
