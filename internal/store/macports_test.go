package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestMACPortsSurviveAndExpire(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "omini.db")
	st, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_790_000_000, 0).UTC()
	err = st.SaveMACPorts(ctx, []MACPort{
		{MAC: "aa:00:00:00:00:01", NodeID: "dev:sw", Port: "Port 2", SeenAt: now},
		{MAC: "aa:00:00:00:00:02", NodeID: "dev:sw", Port: "Port 5", SeenAt: now.Add(-5 * time.Hour)},
	}, now.Add(-6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	st.Close()

	// Reopened (a restart): the ports are still there.
	st, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.MACPorts(ctx, now.Add(-6*time.Hour))
	if err != nil || len(got) != 2 || got[0].Port != "Port 2" || !got[0].SeenAt.Equal(now) {
		t.Fatalf("after a restart: %+v %v", got, err)
	}

	// The phone moved to Port 9; an hour later the old entry is forgotten.
	later := now.Add(time.Hour + time.Minute)
	err = st.SaveMACPorts(ctx, []MACPort{{MAC: "aa:00:00:00:00:01", NodeID: "dev:sw", Port: "Port 9", SeenAt: later}},
		later.Add(-6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got, _ = st.MACPorts(ctx, time.Time{})
	if len(got) != 1 || got[0].Port != "Port 9" {
		t.Fatalf("moved and expired: %+v", got)
	}
}
