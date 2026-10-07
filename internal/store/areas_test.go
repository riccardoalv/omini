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
