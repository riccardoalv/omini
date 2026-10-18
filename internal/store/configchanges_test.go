package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestConfigChanges(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := time.Unix(1_800_000_000, 0)
	add := func(c ConfigChange) {
		t.Helper()
		if _, err := s.AddConfigChange(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	add(ConfigChange{
		NodeID: "dev:sw", IntegrationID: 1, Kind: "port.enabled", Port: "Port 5",
		Before: json.RawMessage("true"), After: json.RawMessage("false"), At: now.Add(-400 * 24 * time.Hour),
	})
	add(ConfigChange{
		NodeID: "dev:sw", IntegrationID: 1, Kind: "port.enabled", Port: "Port 5",
		Before: json.RawMessage("true"), After: json.RawMessage("false"), At: now.Add(-time.Hour),
	})
	add(ConfigChange{
		NodeID: "dev:sw", IntegrationID: 1, Kind: "port.enabled", Port: "Port 5",
		After: json.RawMessage("true"), Error: "the switch did not answer", At: now,
	})
	add(ConfigChange{NodeID: "dev:other", IntegrationID: 2, Kind: "port.enabled", After: json.RawMessage("true"), At: now})

	list, err := s.ConfigChanges(ctx, "dev:sw", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("a change older than a year is forgotten: %+v", list)
	}
	if l := list[0]; l.Error == "" || l.Before != nil || string(l.After) != "true" || !l.At.Equal(now) {
		t.Errorf("newest first, before unknown: %+v", l)
	}
	if l := list[1]; l.Port != "Port 5" || string(l.Before) != "true" || string(l.After) != "false" {
		t.Errorf("older: %+v", l)
	}
	if list, _ := s.ConfigChanges(ctx, "dev:sw", 1); len(list) != 1 {
		t.Errorf("limit: %+v", list)
	}
}

// A change made by the router for a device is in both histories.
func TestConfigChangesForAnotherDevice(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := time.Unix(1_800_000_000, 0)
	if _, err := s.AddConfigChange(ctx, ConfigChange{
		NodeID: "dev:fw", TargetID: "mac:phone", IntegrationID: 1, Kind: "reservation.set",
		After: json.RawMessage(`{"mac":"aa:00:00:00:00:01","ip":"192.168.1.41"}`), At: now,
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"dev:fw", "mac:phone"} {
		list, err := s.ConfigChanges(ctx, id, 10)
		if err != nil || len(list) != 1 || list[0].NodeID != "dev:fw" || list[0].TargetID != "mac:phone" {
			t.Fatalf("%s: %+v %v", id, list, err)
		}
	}
	if list, _ := s.ConfigChanges(ctx, "dev:sw", 10); len(list) != 0 {
		t.Fatalf("another device: %+v", list)
	}
}
