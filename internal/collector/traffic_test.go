package collector

import (
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

func snapshot(at time.Time, rx, tx uint64, ok bool) store.Snapshot {
	return store.Snapshot{
		IntegrationID: 2, CollectedAt: at, OK: ok,
		Devices: []model.Device{{
			Key: "58:9c:fc:00:00:01", Name: "fw",
			Interfaces: []model.Interface{
				{Name: "re0", RxBytes: model.Ptr(rx), TxBytes: model.Ptr(tx)},
				{Name: "vtnet0"}, // no counters
			},
		}},
	}
}

func TestTrafficRatesBetweenCollections(t *testing.T) {
	m := newTrafficMeter()
	t0 := time.Unix(1_790_000_000, 0)
	topo := func() topology.Topology {
		tp := topology.Topology{Nodes: []topology.Node{{ID: "dev:58:9c:fc:00:00:01", Kind: topology.KindDevice}}}
		m.attach(&tp)
		return tp
	}

	m.observe(snapshot(t0, 1_000_000, 500_000, true))
	if tr := topo().Nodes[0].Traffic; tr != nil {
		t.Fatalf("one collection gives no rate yet: %v", tr)
	}

	// 60 s later: 75 MB received, 7.5 MB sent → 10 Mbit/s and 1 Mbit/s.
	m.observe(snapshot(t0.Add(time.Minute), 76_000_000, 8_000_000, true))
	r := topo().Nodes[0].Traffic["re0"]
	if r.RxBps != 10_000_000 || r.TxBps != 1_000_000 {
		t.Fatalf("rate = %+v", r)
	}
	if _, ok := topo().Nodes[0].Traffic["vtnet0"]; ok {
		t.Fatal("interfaces without counters have no rate")
	}

	// Counters reset (reboot): no rate for one round instead of a huge one.
	m.observe(snapshot(t0.Add(2*time.Minute), 10, 10, true))
	if _, ok := topo().Nodes[0].Traffic["re0"]; ok {
		t.Fatal("a counter reset must not give a rate")
	}

	// A failed collection clears the rates: the data is stale.
	m.observe(snapshot(t0.Add(3*time.Minute), 20, 20, true))
	m.observe(snapshot(t0.Add(4*time.Minute), 0, 0, false))
	if tr := topo().Nodes[0].Traffic; tr != nil {
		t.Fatalf("stale rates: %v", tr)
	}
}
