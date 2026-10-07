package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/store"
)

func TestAlertsPresenceAndHistory(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	ctx := context.Background()
	now := time.Now()

	if _, err := h.store.SyncAlerts(ctx, []store.Alert{
		{Key: "high_cpu|dev:fw", Rule: "high_cpu", Severity: "warning", NodeID: "dev:fw", Params: map[string]any{"pct": 91.0}},
	}, now); err != nil {
		t.Fatal(err)
	}
	var alerts []store.Alert
	if code := h.do("GET", "/api/alerts", nil, &alerts); code != 200 || len(alerts) != 1 || alerts[0].Params["pct"] != 91.0 {
		t.Fatalf("alerts: %d %+v", code, alerts)
	}
	if code := h.do("POST", fmt.Sprintf("/api/alerts/%d/dismiss", alerts[0].ID), map[string]bool{"dismissed": true}, nil); code != http.StatusNoContent {
		t.Fatalf("dismiss: %d", code)
	}
	if code := h.do("POST", "/api/alerts/999/dismiss", map[string]bool{"dismissed": true}, nil); code != http.StatusNotFound {
		t.Fatalf("dismiss missing: %d", code)
	}
	_, _ = h.store.SyncAlerts(ctx, nil, now) // resolved
	if h.do("GET", "/api/alerts", nil, &alerts); len(alerts) != 0 {
		t.Fatalf("open alerts after resolve: %+v", alerts)
	}
	if h.do("GET", "/api/alerts?resolved_hours=24", nil, &alerts); len(alerts) != 1 || alerts[0].ResolvedAt == nil || !alerts[0].Dismissed {
		t.Fatalf("resolved alerts: %+v", alerts)
	}
	if code := h.do("GET", "/api/alerts?resolved_hours=0", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("bad hours: %d", code)
	}

	_ = h.store.AddPresence(ctx, []store.PresenceEvent{
		{NodeID: "mac:aa", Kind: "join", At: now.Add(-time.Hour), First: true},
		{NodeID: "mac:bb", Kind: "join", At: now},
	}, now)
	var events []store.PresenceEvent
	if code := h.do("GET", "/api/presence?node=mac:aa", nil, &events); code != 200 || len(events) != 1 || !events[0].First {
		t.Fatalf("presence: %d %+v", code, events)
	}
	if h.do("GET", "/api/presence?limit=1", nil, &events); len(events) != 1 || events[0].NodeID != "mac:bb" {
		t.Fatalf("presence page: %+v", events)
	}
	if code := h.do("GET", "/api/presence?limit=9999", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("bad limit: %d", code)
	}

	_ = h.store.RecordTraffic(ctx, []store.TrafficSample{{NodeID: "dev:fw", Iface: "igc0", RxBps: 5, TxBps: 7}}, now)
	var points []store.TrafficPoint
	if code := h.do("GET", "/api/history?node=dev:fw&iface=igc0", nil, &points); code != 200 || len(points) != 1 || points[0].TxBps != 7 {
		t.Fatalf("history: %d %+v", code, points)
	}
	if code := h.do("GET", "/api/history?iface=igc0", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("history without node: %d", code)
	}
	if code := h.do("GET", "/api/history?node=dev:fw&hours=99999", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("history hours: %d", code)
	}
}
