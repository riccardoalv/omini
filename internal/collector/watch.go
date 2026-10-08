package collector

import (
	"context"
	"log/slog"
	"time"

	"github.com/riccardoalv/omini/internal/insights"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

// presenceSince is the setting holding when presence tracking started: devices
// found in the first minutes after it are the network as it was, not new.
const presenceSince = "presence_since"

// NewDeviceGrace is how long after tracking starts devices count as already
// there (the first scans take a few rounds to find everything).
const NewDeviceGrace = 15 * time.Minute

// offlineAfter is how long a device must be missing before it is marked as
// gone: several polls, so a phone that skips one does not flap.
func (c *Collector) offlineAfter() time.Duration {
	return max(3*c.opts.Interval, 5*time.Minute)
}

// tracked reports whether a node is a device whose presence is followed.
func tracked(n topology.Node) bool {
	return n.Kind == topology.KindClient || n.Kind == topology.KindDevice || n.Kind == topology.KindUnmanaged
}

// watch records the presence timeline and the traffic history, and turns the
// insights into alerts. It never fails the rebuild: problems are logged.
func (c *Collector) watch(ctx context.Context, topo topology.Topology, inventory []store.InventoryEntry,
	statuses []Status, now time.Time,
) []store.Alert {
	since := c.trackingSince(ctx, now)
	if err := c.trackPresence(ctx, topo, inventory, now); err != nil {
		slog.Warn("could not record presence", "err", err)
	}
	if err := c.store.RecordTraffic(ctx, trafficSamples(topo), now); err != nil {
		slog.Warn("could not record traffic history", "err", err)
	}

	in := insights.Input{Topology: topo, Now: now}
	names := map[int64]string{}
	if all, err := c.store.ListIntegrations(ctx); err == nil {
		for _, it := range all {
			names[it.ID] = it.Name
		}
	}
	for _, s := range statuses {
		in.Integrations = append(in.Integrations, insights.Integration{
			ID: s.IntegrationID, Name: names[s.IntegrationID], OK: s.OK, Error: s.Error,
		})
	}
	seenMAC := firstSeenByMAC(inventory)
	for _, e := range inventory {
		if (e.Kind == string(topology.KindClient) || e.Kind == string(topology.KindDevice)) &&
			firstSeen(e, seenMAC).After(since.Add(NewDeviceGrace)) {
			in.NewDevices = append(in.NewDevices, insights.NewDevice{NodeID: e.ID, FirstSeen: e.FirstSeen})
		}
	}
	found := insights.Evaluate(in)
	current := make([]store.Alert, 0, len(found))
	for _, f := range found {
		current = append(current, store.Alert{Key: f.Key, Rule: f.Rule, Severity: f.Severity, NodeID: f.NodeID, Params: f.Params})
	}
	changes, err := c.store.SyncAlerts(ctx, current, now)
	if err != nil {
		slog.Warn("could not update alerts", "err", err)
	} else if len(changes) > 0 && c.opts.OnAlerts != nil {
		c.opts.OnAlerts(changes)
	}
	open, err := c.store.ListAlerts(ctx, time.Time{})
	if err != nil {
		slog.Warn("could not read alerts", "err", err)
		return []store.Alert{}
	}
	return open
}

// firstSeenByMAC is when each MAC was first seen, whatever node it was: a
// machine seen as a client that an integration starts reporting becomes a
// device node with another id, and is not new for that.
func firstSeenByMAC(inventory []store.InventoryEntry) map[string]time.Time {
	out := map[string]time.Time{}
	for _, e := range inventory {
		if e.MAC == "" || e.FirstSeen.IsZero() {
			continue
		}
		if t, ok := out[e.MAC]; !ok || e.FirstSeen.Before(t) {
			out[e.MAC] = e.FirstSeen
		}
	}
	return out
}

// firstSeen is when an inventory entry's machine was first seen (by its MAC).
func firstSeen(e store.InventoryEntry, byMAC map[string]time.Time) time.Time {
	if t, ok := byMAC[e.MAC]; ok && t.Before(e.FirstSeen) {
		return t
	}
	return e.FirstSeen
}

// trackingSince returns when presence tracking started, starting it now the
// first time.
func (c *Collector) trackingSince(ctx context.Context, now time.Time) time.Time {
	v, ok, err := c.store.GetSetting(ctx, presenceSince)
	if err == nil && ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
	}
	if err := c.store.SetSetting(ctx, presenceSince, now.UTC().Format(time.RFC3339)); err != nil {
		slog.Warn("could not save when presence tracking started", "err", err)
	}
	return now
}

// trackPresence adds a join event for every device that appeared and a leave
// event (at the time it was last seen) for every device missing for longer
// than offlineAfter.
func (c *Collector) trackPresence(ctx context.Context, topo topology.Topology, inventory []store.InventoryEntry, now time.Time) error {
	c.mu.Lock()
	if c.presence == nil {
		state, err := c.store.PresenceState(ctx)
		if err != nil {
			c.mu.Unlock()
			return err
		}
		c.presence = map[string]bool{}
		for id, e := range state {
			c.presence[id] = e.Kind == "join"
		}
	}
	c.mu.Unlock()

	byID := make(map[string]store.InventoryEntry, len(inventory))
	for _, e := range inventory {
		byID[e.ID] = e
	}
	present := map[string]bool{}
	seenMAC := firstSeenByMAC(inventory)
	var events []store.PresenceEvent
	c.mu.Lock()
	for _, n := range topo.Nodes {
		if !n.Online || !tracked(n) {
			continue
		}
		present[n.ID] = true
		here, known := c.presence[n.ID]
		if here {
			continue
		}
		e := byID[n.ID]
		// The first sighting ever: the inventory entry was just created.
		first := !known && !e.FirstSeen.IsZero() && now.Sub(firstSeen(e, seenMAC)) < time.Minute
		events = append(events, store.PresenceEvent{NodeID: n.ID, Kind: "join", At: now, First: first})
		c.presence[n.ID] = true
	}
	for id, here := range c.presence {
		if !here || present[id] {
			continue
		}
		last, ok := byID[id]
		if !ok { // deleted from the inventory: forget it
			delete(c.presence, id)
			continue
		}
		if now.Sub(last.LastSeen) < c.offlineAfter() {
			continue
		}
		events = append(events, store.PresenceEvent{NodeID: id, Kind: "leave", At: last.LastSeen})
		c.presence[id] = false
	}
	c.mu.Unlock()
	return c.store.AddPresence(ctx, events, now)
}

// trafficSamples are the current rates of every node: per interface, and a
// Wi-Fi client's own traffic (interface "").
func trafficSamples(topo topology.Topology) []store.TrafficSample {
	var out []store.TrafficSample
	for _, n := range topo.Nodes {
		for name, r := range n.Traffic {
			out = append(out, store.TrafficSample{NodeID: n.ID, Iface: name, RxBps: r.RxBps, TxBps: r.TxBps})
		}
		if n.Flow != nil && n.Online {
			out = append(out, store.TrafficSample{NodeID: n.ID, RxBps: n.Flow.RxBps, TxBps: n.Flow.TxBps})
		}
	}
	return out
}
