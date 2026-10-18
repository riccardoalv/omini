package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/store"
)

type personJSON struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Color    string   `json:"color"`
	Devices  []string `json:"devices"`
	Home     bool     `json:"home"`
	LastSeen *string  `json:"last_seen"`
}

func TestPeopleCRUD(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	var ana personJSON
	if code := h.do("POST", "/api/people", map[string]any{"name": "Ana", "color": "#112233"}, &ana); code != http.StatusCreated || ana.ID == 0 || ana.Devices == nil {
		t.Fatalf("create: %d %+v", code, ana)
	}
	if code := h.do("POST", "/api/people", map[string]any{"name": "ana"}, nil); code != http.StatusConflict {
		t.Fatalf("same name: %d", code)
	}
	if code := h.do("POST", "/api/people", map[string]any{"name": ""}, nil); code != http.StatusBadRequest {
		t.Fatalf("no name: %d", code)
	}
	path := fmt.Sprintf("/api/people/%d", ana.ID)
	var got personJSON
	if code := h.do("PATCH", path, map[string]any{"name": "Ana Lima"}, &got); code != http.StatusOK || got.Name != "Ana Lima" || got.Color != "#112233" {
		t.Fatalf("rename: %d %+v", code, got)
	}
	if code := h.do("PATCH", "/api/people/999", map[string]any{"name": "X"}, nil); code != http.StatusNotFound {
		t.Fatalf("rename a missing person: %d", code)
	}

	// The owner is set from the inventory; an unknown person is refused.
	ctx := context.Background()
	if err := h.store.MarkSeen(ctx, []store.InventoryEntry{{ID: "mac:00:11:22:00:00:30", Kind: "client", Label: "phone"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	dev := "/api/inventory/" + url.PathEscape("mac:00:11:22:00:00:30")
	var e map[string]any
	if code := h.do("PATCH", dev, map[string]any{"owner_id": ana.ID}, &e); code != http.StatusOK || e["owner_id"] != float64(ana.ID) {
		t.Fatalf("owner: %d %v", code, e)
	}
	if code := h.do("PATCH", dev, map[string]any{"owner_id": 999}, nil); code != http.StatusBadRequest {
		t.Fatalf("unknown owner: %d", code)
	}
	var list []personJSON
	if code := h.do("GET", "/api/people", nil, &list); code != http.StatusOK || len(list) != 1 ||
		len(list[0].Devices) != 1 || list[0].Devices[0] != "mac:00:11:22:00:00:30" || list[0].LastSeen == nil || list[0].Home {
		t.Fatalf("list: %d %+v", code, list)
	}
	e = nil // decoding merges into a map
	if code := h.do("PATCH", dev, map[string]any{"owner_id": nil}, &e); code != http.StatusOK || e["owner_id"] != nil {
		t.Fatalf("clear owner: %d %v", code, e)
	}
	if code := h.do("DELETE", path, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code := h.do("DELETE", path, nil, nil); code != http.StatusNotFound {
		t.Fatalf("delete again: %d", code)
	}
}

// TestPausePerson: every device of the person that can be paused is, each
// on its own; the router (network gear) is skipped.
func TestPausePerson(t *testing.T) {
	h := newHarness(t, nil, fakeDHCP(t))
	h.login()
	h.do("POST", "/api/integrations", map[string]any{"type": "fakedhcp", "config": map[string]any{"host": "192.168.1.1"}}, nil)
	if err := h.coll.CollectNow(t.Context()); err != nil {
		t.Fatal(err)
	}
	tv, fw := "", "dev:58:9c:fc:00:00:01"
	for _, n := range h.coll.State().Topology.Nodes {
		if n.MAC == "00:11:22:00:00:20" {
			tv = n.ID
		}
	}
	var p personJSON
	h.do("POST", "/api/people", map[string]any{"name": "Ana"}, &p)
	for _, id := range []string{tv, fw} {
		if code := h.do("PATCH", "/api/inventory/"+url.PathEscape(id), map[string]any{"owner_id": p.ID}, nil); code != http.StatusOK {
			t.Fatalf("owner of %s: %d", id, code)
		}
	}
	var res []struct {
		NodeID string         `json:"node_id"`
		Status string         `json:"status"`
		Change map[string]any `json:"change"`
	}
	if code := h.do("POST", fmt.Sprintf("/api/people/%d/pause", p.ID), nil, &res); code != http.StatusOK || len(res) != 2 {
		t.Fatalf("pause: %d %+v", code, res)
	}
	for _, r := range res {
		switch r.NodeID {
		case tv:
			if r.Status != "done" || r.Change == nil {
				t.Fatalf("the TV is paused: %+v", r)
			}
		case fw:
			if r.Status != "skipped" || r.Change != nil {
				t.Fatalf("the router is never paused: %+v", r)
			}
		default:
			t.Fatalf("another device: %+v", r)
		}
	}
	// Each device has the change in its own history.
	var hist []map[string]any
	if code := h.do("GET", "/api/nodes/"+url.PathEscape(tv)+"/changes", nil, &hist); code != http.StatusOK || len(hist) == 0 {
		t.Fatalf("history: %d %v", code, hist)
	}
	// The fake router reports nothing paused: nothing to resume.
	if code := h.do("POST", fmt.Sprintf("/api/people/%d/resume", p.ID), nil, &res); code != http.StatusOK || len(res) != 2 ||
		res[0].Status != "skipped" || res[1].Status != "skipped" {
		t.Fatalf("resume: %d %+v", code, res)
	}
	if code := h.do("POST", "/api/people/999/pause", nil, nil); code != http.StatusNotFound {
		t.Fatalf("a missing person: %d", code)
	}
}

func TestInternetPerPerson(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	ctx := context.Background()
	now := time.Now()
	if err := h.store.MarkSeen(ctx, []store.InventoryEntry{
		{ID: "mac:00:11:22:00:00:41", Kind: "client", Label: "phone"},
		{ID: "mac:00:11:22:00:00:42", Kind: "client", Label: "laptop"},
		{ID: "mac:00:11:22:00:00:43", Kind: "client", Label: "tv"},
	}, now); err != nil {
		t.Fatal(err)
	}
	var ana, bob personJSON
	h.do("POST", "/api/people", map[string]any{"name": "Ana"}, &ana)
	h.do("POST", "/api/people", map[string]any{"name": "Bob"}, &bob)
	for _, id := range []string{"mac:00:11:22:00:00:41", "mac:00:11:22:00:00:42"} {
		h.do("PATCH", "/api/inventory/"+url.PathEscape(id), map[string]any{"owner_id": ana.ID}, nil)
	}
	if err := h.store.SaveInternet(ctx, now, []store.Usage{
		{Node: "mac:00:11:22:00:00:41", RX: 100, TX: 10},
		{Node: "mac:00:11:22:00:00:42", RX: 50, TX: 5},
		{Node: "mac:00:11:22:00:00:43", RX: 1000},
	}, nil); err != nil {
		t.Fatal(err)
	}
	var usage struct {
		People []struct {
			ID     int64
			Name   string
			RX, TX uint64
		} `json:"people"`
	}
	if code := h.do("GET", "/api/internet?range=today", nil, &usage); code != http.StatusOK {
		t.Fatalf("internet: %d", code)
	}
	if len(usage.People) != 1 || usage.People[0].ID != ana.ID || usage.People[0].RX != 150 || usage.People[0].TX != 15 {
		t.Fatalf("Ana sums her devices, Bob used nothing: %+v", usage.People)
	}
}
