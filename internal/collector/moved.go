package collector

import (
	"context"
	"log/slog"
	"slices"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

// movedDevices finds managed devices whose node id changed: an integration
// that picks the device's key differently (a newer plugin, a stable SNMP
// key) gives the same machine another id. A new device node whose MACs
// include the MAC of a device the inventory knows, and that is not on the map
// any more, is that device. It returns old id → new id.
func movedDevices(topo topology.Topology, inventory []store.InventoryEntry) map[string]string {
	known := make(map[string]store.InventoryEntry, len(inventory))
	for _, e := range inventory {
		known[e.ID] = e
	}
	onMap := make(map[string]bool, len(topo.Nodes))
	for _, n := range topo.Nodes {
		onMap[n.ID] = true
	}
	moved := map[string]string{}
	for _, n := range topo.Nodes {
		if n.Kind != topology.KindDevice || n.Device == nil {
			continue
		}
		if _, ok := known[n.ID]; ok {
			continue
		}
		for _, m := range macsOf(n.Device) {
			old := "dev:" + string(m)
			e, ok := known[old]
			if old == n.ID || !ok || e.Kind != string(topology.KindDevice) || onMap[old] {
				continue
			}
			if _, taken := moved[old]; taken {
				continue
			}
			moved[old] = n.ID
			break
		}
	}
	return moved
}

// macsOf lists a device's MACs, lowest first.
func macsOf(d *model.Device) []model.MACAddress {
	seen := map[model.MACAddress]bool{}
	add := func(m model.MACAddress) {
		if m != "" {
			seen[m] = true
		}
	}
	add(model.NormMAC(d.Key))
	for _, m := range d.MACs {
		add(m)
	}
	for _, i := range d.Interfaces {
		if i.MAC != nil {
			add(*i.MAC)
		}
	}
	out := make([]model.MACAddress, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	slices.Sort(out)
	return out
}

// followMovedDevices moves what is kept for a device whose id changed to its
// new id, before the round records what it sees.
func (c *Collector) followMovedDevices(ctx context.Context, topo topology.Topology) {
	inventory, err := c.store.ListInventory(ctx)
	if err != nil {
		slog.Warn("could not read the inventory", "err", err)
		return
	}
	for old, id := range movedDevices(topo, inventory) {
		if err := c.store.RenameNode(ctx, old, id); err != nil {
			slog.Warn("could not move a device's history to its new id", "from", old, "to", id, "err", err)
			continue
		}
		slog.Info("a device has a new id; its history follows", "from", old, "to", id)
	}
}
