package collector_test

import (
	"context"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/collector"
)

// The firewall is backed up after a round, once a day, and after Omini
// changes it; a configuration that changed without Omini is an alert.
func TestConfigurationBackups(t *testing.T) {
	var clock time.Time
	coll, s, st := switchNetworkWith(t, func(o *collector.Options) {
		now := o.Now
		o.Now = func() time.Time {
			if !clock.IsZero() {
				return clock
			}
			return now()
		}
	})
	ctx := context.Background()
	fw := "dev:" + string(fwMAC)
	s.config = "hostname fw\nlan 192.168.1.1/24\n"
	coll.AfterRound(ctx)
	list, err := st.Backups(ctx, fw)
	if err != nil || len(list) != 1 || list[0].ByOmini || list[0].Size != len(s.config) {
		t.Fatalf("first: %+v %v", list, err)
	}
	_, content, err := coll.BackupContent(ctx, list[0].ID)
	if err != nil || content != s.config {
		t.Fatalf("content: %q %v", content, err)
	}
	// Within the day: not asked again.
	coll.AfterRound(ctx)
	if s.backups != 1 {
		t.Fatalf("backups: %d", s.backups)
	}
	// Omini changes the firewall: backed up after the round, by Omini.
	if _, err := coll.Reserve(ctx, "dev:"+string(swMAC), "192.168.1.2"); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.config += "reservation sw 192.168.1.2\n"
	s.mu.Unlock()
	coll.AfterRound(ctx)
	if list, _ = st.Backups(ctx, fw); len(list) != 2 || !list[0].ByOmini {
		t.Fatalf("after Omini's change: %+v", list)
	}
	// A day later, changed by hand: a version not by Omini, and an alert.
	clock = time.Now().Add(25 * time.Hour)
	s.mu.Lock()
	s.config += "rule pass any\n"
	s.mu.Unlock()
	coll.AfterRound(ctx)
	if list, _ = st.Backups(ctx, fw); len(list) != 3 || list[0].ByOmini {
		t.Fatalf("changed outside: %+v", list)
	}
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range coll.State().Alerts {
		found = found || (a.Rule == "config_changed" && a.NodeID == fw)
	}
	if !found {
		t.Fatalf("no config_changed alert: %+v", coll.State().Alerts)
	}
}
