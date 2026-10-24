package collector

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

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
		if e, ok := known[n.ID]; ok {
			// Known already: recorded under its new key before the old one
			// left the map (a firewall that got a new card).
			if old := sameDeviceBefore(n, e, inventory, onMap, moved); old != "" {
				moved[old] = n.ID
			}
			continue
		}
		if old := previousID(n, known, onMap, moved); old != "" {
			moved[old] = n.ID
		}
	}
	return moved
}

// previousID is the id a device node had before its key changed: the MAC of
// one of its MACs, else (a device that had no MAC for a key, keyed by its
// address) the same integration's node with the same address.
func previousID(n topology.Node, known map[string]store.InventoryEntry, onMap map[string]bool, moved map[string]string) string {
	free := func(old string) bool {
		e, ok := known[old]
		_, taken := moved[old]
		return old != n.ID && ok && e.Kind == string(topology.KindDevice) && !onMap[old] && !taken
	}
	for _, m := range macsOf(n.Device) {
		if old := "dev:" + string(m); free(old) {
			return old
		}
	}
	if n.IP == "" || n.IntegrationID == 0 {
		return ""
	}
	prefix := fmt.Sprintf("dev:%d:", n.IntegrationID)
	for id, e := range known {
		if strings.HasPrefix(id, prefix) && e.IP == n.IP && free(id) {
			return id
		}
	}
	return ""
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

// sameDeviceBefore is an older device entry that is this node under its key
// before its card changed: a device first seen earlier, no longer on the map,
// with the same name and the same address (a firewall's LAN moved to a new
// card: new MAC, same name and gateway address).
func sameDeviceBefore(n topology.Node, e store.InventoryEntry, inventory []store.InventoryEntry, onMap map[string]bool, moved map[string]string) string {
	if n.IP == "" || n.Label == "" {
		return ""
	}
	for _, o := range inventory {
		if _, taken := moved[o.ID]; taken || o.ID == n.ID || onMap[o.ID] || o.Kind != string(topology.KindDevice) {
			continue
		}
		if o.IP == n.IP && strings.EqualFold(o.Label, e.Label) && o.FirstSeen.Before(e.FirstSeen) {
			return o.ID
		}
	}
	return ""
}
