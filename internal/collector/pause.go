package collector

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

// Pausing a device's internet: the router of its network blocks its
// addresses toward the internet (pause.set); its own network stays.

// networkGear are the types whose internet is never paused: the network
// itself depends on them.
var networkGear = []string{"router", "firewall", "switch", "ap", "hypervisor"}

// pauseTarget is the router that can pause the device, and its addresses.
func (c *Collector) pauseTarget(nodeID string) (topology.Node, model.PausedDevice, error) {
	n, ok := c.node(nodeID)
	mac := model.NormMAC(n.MAC)
	if !ok || mac == "" {
		return topology.Node{}, model.PausedDevice{}, fmt.Errorf("%w: pausing a device needs its MAC", ErrNotChangeable)
	}
	if c.opts.Self.Owns(n) || n.Kind == topology.KindDevice || slices.Contains(networkGear, n.Type) {
		return topology.Node{}, model.PausedDevice{}, fmt.Errorf("%w: Omini's own host and network devices cannot be paused", ErrNotChangeable)
	}
	ips := addressesOf(c.State().Topology, n, mac)
	for _, r := range c.State().Topology.Nodes {
		if !slices.Contains(r.Changes, model.ChangeKindPauseSet) {
			continue
		}
		for _, s := range ips {
			if ip, err := netip.ParseAddr(s); err == nil && networkHolding(r, ip) != nil {
				name := n.Label
				return r, model.PausedDevice{MAC: mac, IPs: ips, Name: &name}, nil
			}
		}
	}
	return topology.Node{}, model.PausedDevice{}, fmt.Errorf("%w: no router Omini manages serves this device's network", ErrNotChangeable)
}

// addressesOf are a device's current IPv4 addresses: on the map and fixed.
func addressesOf(topo topology.Topology, n topology.Node, mac model.MACAddress) []string {
	var out []string
	if ip, err := netip.ParseAddr(n.IP); err == nil && ip.Is4() {
		out = append(out, ip.String())
	}
	if ip, ok := fixedIP(topo, mac); ok && !slices.Contains(out, ip.String()) {
		out = append(out, ip.String())
	}
	slices.Sort(out)
	return out
}

// CanPause tells whether a device can be paused, and on which router.
func (c *Collector) CanPause(nodeID string) (string, bool) {
	r, _, err := c.pauseTarget(nodeID)
	return r.ID, err == nil
}

// Pause cuts a device's internet on its router.
func (c *Collector) Pause(ctx context.Context, nodeID string) (store.ConfigChange, error) {
	r, v, err := c.pauseTarget(nodeID)
	if err != nil {
		return store.ConfigChange{}, err
	}
	return c.Apply(ctx, ChangeRequest{NodeID: r.ID, Kind: model.ChangeKindPauseSet, Value: v})
}

// Resume gives a paused device its internet back, on every router pausing it.
func (c *Collector) Resume(ctx context.Context, nodeID string) (store.ConfigChange, error) {
	n, ok := c.node(nodeID)
	mac := model.NormMAC(n.MAC)
	if !ok || mac == "" {
		return store.ConfigChange{}, ErrNotChangeable
	}
	var last store.ConfigChange
	found := false
	for _, r := range c.State().Topology.Nodes {
		if r.Device == nil || pausedOf(r, mac) == nil || !slices.Contains(r.Changes, model.ChangeKindPauseDelete) {
			continue
		}
		rec, err := c.Apply(ctx, ChangeRequest{NodeID: r.ID, Kind: model.ChangeKindPauseDelete, Value: MACValue{MAC: mac}})
		if err != nil {
			return rec, err
		}
		last, found = rec, true
	}
	if !found {
		return store.ConfigChange{}, fmt.Errorf("%w: the device is not paused", ErrNotChangeable)
	}
	return last, nil
}

// syncPaused follows paused devices that got another address: the router
// blocks the new one.
func (c *Collector) syncPaused(ctx context.Context) {
	topo := c.State().Topology
	for _, r := range topo.Nodes {
		if r.Device == nil || !slices.Contains(r.Changes, model.ChangeKindPauseSet) {
			continue
		}
		for _, p := range r.Device.Paused {
			if model.Deref(p.Rule) != "" {
				continue // blocked by a rule set by hand: the user keeps it up to date
			}
			mac := model.NormMAC(string(p.MAC))
			id, ok := c.byMAC(string(mac))
			if !ok {
				continue
			}
			n, _ := c.node(id)
			ips := addressesOf(topo, n, mac)
			have := slices.Clone(p.IPs)
			slices.Sort(have)
			if len(ips) == 0 || slices.Equal(ips, have) {
				continue
			}
			v := model.PausedDevice{MAC: mac, IPs: ips, Name: p.Name}
			if _, err := c.Apply(ctx, ChangeRequest{NodeID: r.ID, Kind: model.ChangeKindPauseSet, Value: v}); err != nil {
				slog.Warn("could not follow a paused device's new address", "node", id, "err", err)
			}
		}
	}
}
