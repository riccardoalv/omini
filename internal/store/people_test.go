package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestPeople(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	at := time.Unix(1_800_000_000, 0)
	if err := s.MarkSeen(ctx, []InventoryEntry{
		{ID: "dev:phone", Kind: "client", Label: "phone"},
		{ID: "dev:laptop", Kind: "client", Label: "laptop"},
	}, at); err != nil {
		t.Fatal(err)
	}
	ana, err := s.CreatePerson(ctx, "  Ana   Lima ", "#AABBCC", at)
	if err != nil || ana.Name != "Ana Lima" || ana.Color != "#aabbcc" {
		t.Fatalf("create: %+v %v", ana, err)
	}
	if _, err := s.CreatePerson(ctx, "ana lima", "", at); !errors.Is(err, ErrPersonExists) {
		t.Fatalf("a name is unique, ignoring case: %v", err)
	}
	if _, err := s.CreatePerson(ctx, " ", "", at); !errors.Is(err, ErrPersonName) {
		t.Fatalf("a name is needed: %v", err)
	}
	if _, err := s.CreatePerson(ctx, "Bob", "red", at); !errors.Is(err, ErrPersonColor) {
		t.Fatalf("a color is #rrggbb: %v", err)
	}
	bob, err := s.CreatePerson(ctx, "Bob", "", at)
	if err != nil {
		t.Fatal(err)
	}
	name := "Ana"
	if p, err := s.UpdatePerson(ctx, ana.ID, &name, nil); err != nil || p.Name != "Ana" || p.Color != "#aabbcc" {
		t.Fatalf("rename keeps the color: %+v %v", p, err)
	}
	dup := "BOB"
	if _, err := s.UpdatePerson(ctx, ana.ID, &dup, nil); !errors.Is(err, ErrPersonExists) {
		t.Fatalf("rename to another's name: %v", err)
	}
	if _, err := s.UpdatePerson(ctx, 999, &name, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename a missing person: %v", err)
	}

	// A device has at most one owner: setting another replaces it.
	var u InventoryUpdate
	if err := json.Unmarshal([]byte(`{"owner_id": `+itoa(ana.ID)+`}`), &u); err != nil || !u.Owner.Set {
		t.Fatalf("owner from JSON: %+v %v", u, err)
	}
	if e, err := s.UpdateInventory(ctx, "dev:phone", u); err != nil || e.OwnerID != ana.ID {
		t.Fatalf("set owner: %+v %v", e, err)
	}
	if e, err := s.UpdateInventory(ctx, "dev:phone", InventoryUpdate{Owner: OptionalID{Set: true, ID: bob.ID}}); err != nil || e.OwnerID != bob.ID {
		t.Fatalf("another owner: %+v %v", e, err)
	}
	alias := "Phone"
	if e, err := s.UpdateInventory(ctx, "dev:phone", InventoryUpdate{Alias: &alias}); err != nil || e.OwnerID != bob.ID {
		t.Fatalf("another field keeps the owner: %+v %v", e, err)
	}
	if _, err := s.UpdateInventory(ctx, "dev:laptop", InventoryUpdate{Owner: OptionalID{Set: true, ID: 999}}); !errors.Is(err, ErrNoSuchPerson) {
		t.Fatalf("an unknown owner: %v", err)
	}
	if err := s.MarkSeen(ctx, []InventoryEntry{{ID: "dev:phone", Kind: "client", Label: "phone"}}, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var clear InventoryUpdate
	_ = json.Unmarshal([]byte(`{"owner_id": null}`), &clear)
	if !clear.Owner.Set || clear.Owner.ID != 0 {
		t.Fatalf("null clears: %+v", clear)
	}
	if _, err := s.UpdateInventory(ctx, "dev:laptop", InventoryUpdate{Owner: OptionalID{Set: true, ID: bob.ID}}); err != nil {
		t.Fatal(err)
	}
	if e, err := s.UpdateInventory(ctx, "dev:laptop", clear); err != nil || e.OwnerID != 0 {
		t.Fatalf("clear owner: %+v %v", e, err)
	}

	// Deleting a person leaves their devices without an owner.
	if err := s.DeletePerson(ctx, bob.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePerson(ctx, bob.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
	inv, _ := s.ListInventory(ctx)
	for _, e := range inv {
		if e.OwnerID != 0 {
			t.Fatalf("an owner left behind: %+v", e)
		}
	}
	list, err := s.ListPeople(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "Ana" {
		t.Fatalf("list: %+v %v", list, err)
	}
}

// TestRenameNodeKeepsTheOwner: a device whose key changed keeps its owner.
func TestRenameNodeKeepsTheOwner(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if err := s.MarkSeen(ctx, []InventoryEntry{{ID: "ip:192.168.1.5", Kind: "client", Label: "phone"}}, time.Unix(1_800_000_000, 0)); err != nil {
		t.Fatal(err)
	}
	p, _ := s.CreatePerson(ctx, "Ana", "", time.Unix(1_800_000_000, 0))
	if _, err := s.UpdateInventory(ctx, "ip:192.168.1.5", InventoryUpdate{Owner: OptionalID{Set: true, ID: p.ID}}); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameNode(ctx, "ip:192.168.1.5", "dev:aa:bb:cc:dd:ee:ff"); err != nil {
		t.Fatal(err)
	}
	inv, _ := s.ListInventory(ctx)
	if len(inv) != 1 || inv[0].ID != "dev:aa:bb:cc:dd:ee:ff" || inv[0].OwnerID != p.ID {
		t.Fatalf("renamed: %+v", inv)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
