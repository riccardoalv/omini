package collector

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

// RoundSetting is the settings key of the time between collection rounds, in
// seconds (unset: Options.Interval, from OMINI_POLL_INTERVAL).
const RoundSetting = "round_interval_s"

// Limits of the round interval.
const (
	MinRoundInterval = 15 * time.Second
	MaxRoundInterval = 24 * time.Hour
)

// Round describes the last collection round: every enabled integration, one
// at a time, from the edge of the network to its center.
type Round struct {
	StartedAt  time.Time `json:"started_at"`
	DurationMs int64     `json:"duration_ms"`
	// Order is the integrations in the order they were collected.
	Order     []int64 `json:"order"`
	IntervalS int     `json:"interval_s"`
}

// RoundInterval is the time between collection rounds.
func (c *Collector) RoundInterval(ctx context.Context) time.Duration {
	if v, ok, err := c.store.GetSetting(ctx, RoundSetting); err == nil && ok {
		if s, err := strconv.Atoi(v); err == nil && s > 0 {
			return time.Duration(s) * time.Second
		}
	}
	return c.opts.Interval
}

// SetRoundInterval changes the time between collection rounds (0 restores the default).
func (c *Collector) SetRoundInterval(ctx context.Context, d time.Duration) error {
	value := ""
	if d > 0 {
		value = strconv.Itoa(int(d / time.Second))
	}
	if err := c.store.SetSetting(ctx, RoundSetting, value); err != nil {
		return err
	}
	select {
	case c.reschedule <- struct{}{}:
	default:
	}
	return nil
}

// roundOrder sorts integrations from the edge of the network to its center,
// so what an access point says about its clients is collected before what a
// switch, then the router, see of them: a round is one coherent picture, and
// the map is built once, at its end. An integration's level is the depth (on
// the last map) of its device closest to the root; without a map yet, its
// devices' roles say it (access points and servers, then switches, then
// routers). Built-in discovery (network scan, nmap, flows) goes last: it sees
// the whole network from the center.
func (c *Collector) roundOrder(ins []store.Integration) []store.Integration {
	c.mu.RLock()
	topo := c.state.Topology
	snaps := make(map[int64][]model.Device, len(c.snaps))
	for id, s := range c.snaps {
		snaps[id] = s.Devices
	}
	c.mu.RUnlock()

	depth := nodeDepths(topo)
	level := map[int64]int{}
	for _, n := range topo.Nodes {
		if n.Kind != topology.KindDevice || n.IntegrationID == 0 {
			continue
		}
		if cur, ok := level[n.IntegrationID]; !ok || depth[n.ID] < cur {
			level[n.IntegrationID] = depth[n.ID]
		}
	}
	core := func(in store.Integration) bool {
		impl, err := c.reg.Get(in.Type)
		return err == nil && impl.Info().Kind == integration.KindCore
	}
	levelOf := func(in store.Integration) int {
		if l, ok := level[in.ID]; ok {
			return l
		}
		l := -1
		for _, d := range snaps[in.ID] {
			r := roleLevel(model.Deref(d.Role))
			if l < 0 || r < l {
				l = r
			}
		}
		if l < 0 {
			return 2
		}
		return l
	}
	out := append([]store.Integration(nil), ins...)
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := core(out[i]), core(out[j])
		if ci != cj {
			return !ci
		}
		li, lj := levelOf(out[i]), levelOf(out[j])
		if li != lj {
			return li > lj // deeper first
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// roleLevel: how far from the root a device of that role usually sits.
func roleLevel(r model.DeviceRole) int {
	switch r {
	case model.DeviceRoleFirewall, model.DeviceRoleRouter:
		return 1
	case model.DeviceRoleSwitch:
		return 2
	default: // access points, servers, unknown
		return 3
	}
}

// nodeDepths is each node's distance from a root (a node nothing hangs from).
func nodeDepths(topo topology.Topology) map[string]int {
	children := map[string][]string{}
	hasParent := map[string]bool{}
	for _, e := range topo.Edges {
		children[e.Source] = append(children[e.Source], e.Target)
		hasParent[e.Target] = true
	}
	depth := map[string]int{}
	var queue []string
	for _, n := range topo.Nodes {
		if !hasParent[n.ID] {
			depth[n.ID] = 0
			queue = append(queue, n.ID)
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, c := range children[id] {
			if _, seen := depth[c]; !seen {
				depth[c] = depth[id] + 1
				queue = append(queue, c)
			}
		}
	}
	return depth
}

// DefaultInterval is the round interval used when none is set.
func (c *Collector) DefaultInterval() time.Duration { return c.opts.Interval }
