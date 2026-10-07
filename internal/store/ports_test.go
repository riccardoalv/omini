package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPortLabels(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if err := s.SetPortLabel(ctx, "dev:sw", "ge1", " Uplink to rack "); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPortLabel(ctx, "dev:sw", "ge2", "TV room"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPortLabel(ctx, "dev:sw", "ge2", "Living room"); err != nil {
		t.Fatal(err)
	}
	labels, err := s.PortLabels(ctx)
	if err != nil || labels["dev:sw"]["ge1"] != "Uplink to rack" || labels["dev:sw"]["ge2"] != "Living room" {
		t.Fatalf("labels %v %v", labels, err)
	}
	if err := s.SetPortLabel(ctx, "dev:sw", "ge1", ""); err != nil {
		t.Fatal(err)
	}
	if labels, _ = s.PortLabels(ctx); len(labels["dev:sw"]) != 1 {
		t.Fatalf("an empty description removes it: %v", labels)
	}
	if err := s.SetPortLabel(ctx, "dev:sw", "ge3", strings.Repeat("x", 81)); !errors.Is(err, ErrInvalidPortLabel) {
		t.Fatalf("too long: %v", err)
	}
}
