package store

import (
	"context"
	"errors"
	"testing"
)

func TestAreaCRUD(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	a, err := s.CreateArea(ctx, Area{Name: "  Rack ", Color: "blue", Direction: "RIGHT", X: 10, Y: 20, Width: 300, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == 0 || a.Name != "Rack" {
		t.Fatalf("created %+v", a)
	}

	if len(a.Members) != 0 {
		t.Fatalf("members of a new area: %v", a.Members)
	}
	name, x, members := "Server rack", 50.0, []string{"mac:aa", "ip:192.168.1.52"}
	u, err := s.UpdateArea(ctx, a.ID, AreaUpdate{Name: &name, X: &x, Members: &members})
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != name || u.X != 50 || u.Y != 20 || u.Color != "blue" || u.Width != 300 {
		t.Fatalf("updated %+v", u)
	}

	list, err := s.ListAreas(ctx)
	if err != nil || len(list) != 1 || list[0].Name != u.Name || len(list[0].Members) != 2 || list[0].Members[1] != "ip:192.168.1.52" {
		t.Fatalf("list %+v %v", list, err)
	}

	if err := s.DeleteArea(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteArea(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if _, err := s.UpdateArea(ctx, a.ID, AreaUpdate{Name: &name}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update deleted: %v", err)
	}
}

func TestAreaValidation(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	valid := Area{Name: "Rack", Color: "gray", Direction: "DOWN", Width: 100, Height: 100}

	for name, mutate := range map[string]func(*Area){
		"empty name": func(a *Area) { a.Name = " " },
		"long name":  func(a *Area) { a.Name = string(make([]byte, 61)) },
		"color":      func(a *Area) { a.Color = "#fff" },
		"direction":  func(a *Area) { a.Direction = "LEFT" },
		"too small":  func(a *Area) { a.Width = 10 },
	} {
		a := valid
		mutate(&a)
		if _, err := s.CreateArea(ctx, a); !errors.Is(err, ErrInvalidArea) {
			t.Errorf("%s: expected ErrInvalidArea, got %v", name, err)
		}
	}

	a, err := s.CreateArea(ctx, valid)
	if err != nil {
		t.Fatal(err)
	}
	bad := "pink"
	if _, err := s.UpdateArea(ctx, a.ID, AreaUpdate{Color: &bad}); !errors.Is(err, ErrInvalidArea) {
		t.Fatalf("update with invalid color: %v", err)
	}
}

func TestAreaCustomColor(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	a, err := s.CreateArea(ctx, Area{Name: "Lab", Color: "#1A2B3C", Direction: "RIGHT", Width: 100, Height: 100})
	if err != nil || a.Color != "#1a2b3c" {
		t.Fatalf("hex color: %+v %v", a, err)
	}
	c := "#ff8800"
	if u, err := s.UpdateArea(ctx, a.ID, AreaUpdate{Color: &c}); err != nil || u.Color != c {
		t.Fatalf("update hex color: %+v %v", u, err)
	}
}

func TestAutoAreas(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	vlan := Area{Name: "VLAN 20 · IOT", Color: "#4c8dff", Direction: "RIGHT", Width: 100, Height: 100, Auto: "vlan:20"}
	a, err := s.CreateArea(ctx, vlan)
	if err != nil || a.Auto != "vlan:20" {
		t.Fatalf("create: %+v %v", a, err)
	}
	// A second browser creating the same area gets the existing one.
	vlan.Name = "Other"
	b, err := s.CreateArea(ctx, vlan)
	if err != nil || b.ID != a.ID || b.Name != "VLAN 20 · IOT" {
		t.Fatalf("second create: %+v %v", b, err)
	}
	if _, err := s.CreateArea(ctx, Area{Name: "LAN", Color: "gray", Direction: "DOWN", Width: 100, Height: 100, Auto: "subnet:192.168.1.0/24"}); err != nil {
		t.Fatal(err)
	}
	bad := vlan
	bad.Auto = "rack"
	if _, err := s.CreateArea(ctx, bad); !errors.Is(err, ErrInvalidArea) {
		t.Fatalf("unknown auto key: %v", err)
	}

	// Removing it dismisses it: kept, so it is not created again.
	if err := s.DeleteArea(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteArea(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	list, _ := s.ListAreas(ctx)
	if len(list) != 2 || !list[0].Dismissed || list[1].Dismissed || list[1].Auto != "subnet:192.168.1.0/24" {
		t.Fatalf("list: %+v", list)
	}
	again, err := s.CreateArea(ctx, vlan)
	if err != nil || again.ID != a.ID || !again.Dismissed {
		t.Fatalf("created again: %+v %v", again, err)
	}
}
