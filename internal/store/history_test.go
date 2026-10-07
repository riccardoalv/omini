package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestTrafficHistoryMinutesAndHours(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	start := time.Unix(1_790_000_000, 0).UTC().Truncate(time.Hour)
	rec := func(at time.Time, rx uint64) {
		t.Helper()
		if err := st.RecordTraffic(ctx, []TrafficSample{{NodeID: "dev:fw", Iface: "igc0", RxBps: rx, TxBps: rx / 2}}, at); err != nil {
			t.Fatal(err)
		}
	}
	rec(start.Add(10*time.Second), 100)
	rec(start.Add(40*time.Second), 300) // same minute: replaces the first
	rec(start.Add(time.Minute), 500)

	got, err := st.TrafficHistory(ctx, "dev:fw", "igc0", start.Add(-time.Hour), start.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].RxBps != 300 || got[1].RxBps != 500 || got[1].TxBps != 250 {
		t.Fatalf("minutes: %+v", got)
	}

	// A week asked: hourly points, average and peak of the minutes.
	hours, err := st.TrafficHistory(ctx, "dev:fw", "igc0", start.Add(-7*24*time.Hour), start.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(hours) != 1 || hours[0].RxBps != 400 || hours[0].RxMaxBps != 500 {
		t.Fatalf("hours: %+v", hours)
	}

	// A day later the minutes are gone, the hour is kept.
	rec(start.Add(25*time.Hour), 1)
	got, _ = st.TrafficHistory(ctx, "dev:fw", "igc0", start.Add(time.Hour), start.Add(25*time.Hour))
	if len(got) != 1 || got[0].RxBps != 1 {
		t.Fatalf("old minutes kept: %+v", got)
	}
	hours, _ = st.TrafficHistory(ctx, "dev:fw", "igc0", start.Add(-48*time.Hour), start.Add(25*time.Hour))
	if len(hours) != 2 || hours[0].RxBps != 400 {
		t.Fatalf("hours after a day: %+v", hours)
	}
}

func TestPresenceTimeline(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	now := time.Unix(1_790_000_000, 0).UTC()
	if err := st.MarkSeen(ctx, []InventoryEntry{{ID: "mac:aa", Kind: "client", Label: "Phone", MAC: "aa"}}, now); err != nil {
		t.Fatal(err)
	}
	err := st.AddPresence(ctx, []PresenceEvent{
		{NodeID: "mac:aa", Kind: "join", At: now.Add(-2 * time.Hour), First: true},
		{NodeID: "mac:aa", Kind: "leave", At: now.Add(-time.Hour)},
		{NodeID: "mac:bb", Kind: "join", At: now.Add(-time.Hour)},
		{NodeID: "mac:aa", Kind: "join", At: now},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	state, err := st.PresenceState(ctx)
	if err != nil || state["mac:aa"].Kind != "join" || state["mac:bb"].Kind != "join" || len(state) != 2 {
		t.Fatalf("state: %+v %v", state, err)
	}
	list, err := st.ListPresence(ctx, PresenceQuery{NodeID: "mac:aa"})
	if err != nil || len(list) != 3 || list[0].Kind != "join" || list[0].Label != "Phone" || !list[2].First {
		t.Fatalf("timeline: %+v %v", list, err)
	}
	page, _ := st.ListPresence(ctx, PresenceQuery{BeforeID: list[0].ID, Limit: 1})
	if len(page) != 1 || page[0].NodeID != "mac:bb" {
		t.Fatalf("page: %+v", page)
	}
	firsts, _ := st.ListPresence(ctx, PresenceQuery{First: true})
	if len(firsts) != 1 || firsts[0].NodeID != "mac:aa" {
		t.Fatalf("first sightings: %+v", firsts)
	}
}

func TestAlertsOpenRefreshResolve(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	now := time.Unix(1_790_000_000, 0).UTC()
	disk := Alert{Key: "disk_full|dev:fw|/", Rule: "disk_full", Severity: "warning", NodeID: "dev:fw", Params: map[string]any{"pct": 82.0}}
	cpu := Alert{Key: "high_cpu|dev:fw", Rule: "high_cpu", Severity: "warning", NodeID: "dev:fw", Params: map[string]any{}}

	changes, err := st.SyncAlerts(ctx, []Alert{disk, cpu}, now)
	if err != nil || len(changes) != 2 || !changes[0].Opened {
		t.Fatalf("open: %+v %v", changes, err)
	}
	// The disk fills up (now critical), the CPU calms down.
	disk.Severity, disk.Params = "critical", map[string]any{"pct": 93.0}
	changes, err = st.SyncAlerts(ctx, []Alert{disk}, now.Add(time.Minute))
	if err != nil || len(changes) != 1 || changes[0].Opened || changes[0].Alert.Rule != "high_cpu" {
		t.Fatalf("resolve: %+v %v", changes, err)
	}
	open, _ := st.ListAlerts(ctx, time.Time{})
	if len(open) != 1 || open[0].Severity != "critical" || open[0].Params["pct"] != 93.0 || !open[0].OpenedAt.Equal(now) {
		t.Fatalf("open alerts: %+v", open)
	}
	all, _ := st.ListAlerts(ctx, now.Add(-time.Hour))
	if len(all) != 2 {
		t.Fatalf("with resolved: %+v", all)
	}
	if err := st.DismissAlert(ctx, open[0].ID, true); err != nil {
		t.Fatal(err)
	}
	open, _ = st.ListAlerts(ctx, time.Time{})
	if !open[0].Dismissed {
		t.Fatal("not dismissed")
	}
	if err := st.DismissAlert(ctx, 999, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing alert: %v", err)
	}
	// The CPU alert comes back: a new alert, not the resolved one.
	changes, _ = st.SyncAlerts(ctx, []Alert{disk, cpu}, now.Add(2*time.Minute))
	if len(changes) != 1 || !changes[0].Opened || changes[0].Alert.Rule != "high_cpu" {
		t.Fatalf("reopen: %+v", changes)
	}
}
