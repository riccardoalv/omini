package collector

import (
	"strconv"
	"sync"
	"time"

	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

// trafficMeter turns interface byte counters into rates: the difference
// between two collections of the same integration, divided by the time
// between them (an average over the polling interval).
type trafficMeter struct {
	mu    sync.Mutex
	prev  map[string]sample                             // integration|device|interface
	rates map[int64]map[string]map[string]topology.Rate // integration → device key → interface
}

type sample struct {
	rx, tx       uint64
	rxErr, txErr uint64
	at           time.Time
}

func newTrafficMeter() *trafficMeter {
	return &trafficMeter{prev: map[string]sample{}, rates: map[int64]map[string]map[string]topology.Rate{}}
}

// observe records a new snapshot. A failed collection clears the
// integration's rates: its data is stale.
func (t *trafficMeter) observe(snap store.Snapshot) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !snap.OK {
		delete(t.rates, snap.IntegrationID)
		return
	}
	byDevice := map[string]map[string]topology.Rate{}
	for _, d := range snap.Devices {
		for _, i := range d.Interfaces {
			if i.RxBytes == nil || i.TxBytes == nil {
				continue
			}
			key := strconv.FormatInt(snap.IntegrationID, 10) + "|" + d.Key + "|" + i.Name
			now := sample{rx: *i.RxBytes, tx: *i.TxBytes, rxErr: deref(i.RxErrors), txErr: deref(i.TxErrors), at: snap.CollectedAt}
			prev, ok := t.prev[key]
			t.prev[key] = now
			secs := now.at.Sub(prev.at).Seconds()
			// Counters that went down were reset (reboot, wrap): skip one round.
			if !ok || secs <= 0 || now.rx < prev.rx || now.tx < prev.tx {
				continue
			}
			rx, tx := float64(now.rx-prev.rx)*8/secs, float64(now.tx-prev.tx)*8/secs
			// More than twice what the port can carry is a counter glitch (a
			// reset not seen as one, a device answering with another's counters),
			// never traffic: skip it rather than report a link at 1800 %.
			if s := float64(deref(i.SpeedMbps)) * 1e6; s > 0 && max(rx, tx) > 2*s {
				continue
			}
			if byDevice[d.Key] == nil {
				byDevice[d.Key] = map[string]topology.Rate{}
			}
			byDevice[d.Key][i.Name] = topology.Rate{
				RxBps:    uint64(rx),
				TxBps:    uint64(tx),
				RxErrors: grew(prev.rxErr, now.rxErr),
				TxErrors: grew(prev.txErr, now.txErr),
			}
		}
	}
	t.rates[snap.IntegrationID] = byDevice
}

// attach puts the rates on the nodes of the devices they belong to.
func (t *trafficMeter) attach(topo *topology.Topology) {
	t.mu.Lock()
	defer t.mu.Unlock()
	index := map[string]int{}
	for i, n := range topo.Nodes {
		if n.Kind != topology.KindDevice {
			continue
		}
		index[n.ID] = i
		if n.Device != nil {
			index[n.Device.Key] = i
			for _, m := range n.Device.MACs {
				index[string(m)] = i
			}
		}
	}
	for _, devices := range t.rates {
		for key, ifaces := range devices {
			i, ok := index[key]
			if !ok {
				i, ok = index["dev:"+key]
			}
			if !ok {
				continue
			}
			n := &topo.Nodes[i]
			if n.Traffic == nil {
				n.Traffic = map[string]topology.Rate{}
			}
			for name, r := range ifaces {
				n.Traffic[name] = r
			}
		}
	}
}

func deref(v *uint64) uint64 {
	if v == nil {
		return 0
	}
	return *v
}

// grew is how much a counter grew (0 when it was reset).
func grew(prev, now uint64) uint64 {
	if now < prev {
		return 0
	}
	return now - prev
}
