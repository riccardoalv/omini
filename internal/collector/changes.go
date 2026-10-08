package collector

import (
	"time"

	"github.com/riccardoalv/omini/internal/insights"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/plugins"
	"github.com/riccardoalv/omini/internal/topology"
)

// changeTracker remembers, between rounds, what the alerts need to see change:
// ports going up and down, devices restarting.
type changeTracker struct {
	up      map[string]bool        // "node|iface" → up at the last round
	flaps   map[string][]time.Time // "node|iface" → when it changed state
	uptime  map[string]uint64      // node → uptime at the last round
	reboots map[string]time.Time   // node → when it restarted
}

// observeChanges compares this round with the last one: a port that changed
// state counts as a flap (kept an hour), a device whose uptime went back
// restarted (kept RebootWindow).
func (c *Collector) observeChanges(topo topology.Topology, now time.Time) insights.History {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &c.changes
	if t.up == nil {
		t.up, t.flaps = map[string]bool{}, map[string][]time.Time{}
		t.uptime, t.reboots = map[string]uint64{}, map[string]time.Time{}
	}
	for _, n := range topo.Nodes {
		if n.Kind != topology.KindDevice || n.Device == nil || !n.Online {
			continue
		}
		for _, i := range n.Device.Interfaces {
			if i.Up == nil || (i.Type != nil && *i.Type != model.InterfaceTypeEthernet) {
				continue
			}
			key := n.ID + "|" + i.Name
			if was, ok := t.up[key]; ok && was != *i.Up {
				t.flaps[key] = append(t.flaps[key], now)
			}
			t.up[key] = *i.Up
		}
		if u := n.Device.UptimeS; u != nil {
			if was, ok := t.uptime[n.ID]; ok && *u < was {
				t.reboots[n.ID] = now.Add(-time.Duration(*u) * time.Second)
			}
			t.uptime[n.ID] = *u
		}
	}
	h := insights.History{Flaps: map[string]int{}, Reboots: map[string]time.Time{}}
	for key, times := range t.flaps {
		kept := times[:0]
		for _, at := range times {
			if now.Sub(at) <= time.Hour {
				kept = append(kept, at)
			}
		}
		if len(kept) == 0 {
			delete(t.flaps, key)
			continue
		}
		t.flaps[key] = kept
		h.Flaps[key] = len(kept)
	}
	for id, at := range t.reboots {
		if now.Sub(at) > insights.RebootWindow {
			delete(t.reboots, id)
			continue
		}
		h.Reboots[id] = at
	}
	return h
}

// detectors: what each integration of the plugin store can read, by the
// identification of the devices (catalog "detect").
func detectors() []insights.Detector {
	var out []insights.Detector
	for _, e := range plugins.Catalog() {
		if e.Detect == nil {
			continue
		}
		out = append(out, insights.Detector{
			Type: e.ID, Name: e.Name, Products: e.Detect.Products, Brands: e.Detect.Brands, OS: e.Detect.OS,
		})
	}
	return out
}
