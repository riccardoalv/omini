package collector

import (
	"strings"

	"github.com/riccardoalv/omini/internal/topology"
)

// attachVMs places the guests of a hypervisor under it. A Proxmox guest (a VM
// or container, whatever runs in it: Ubuntu, TrueNAS, Home Assistant...) is
// recognized by its MAC (Proxmox's OUI), which does not say which host
// runs it: the inference is only made when the network has a single Proxmox
// host. With several hosts the VMs stay where the network data put them
// (a Proxmox integration can tell them apart later).
func attachVMs(topo *topology.Topology) {
	var hosts []string
	for _, n := range topo.Nodes {
		if n.Product == "proxmox" && n.Kind != topology.KindApp {
			if n.Device != nil && n.Device.Vendor != nil && *n.Device.Vendor == "Proxmox" {
				return // a Proxmox integration places the guests
			}
			hosts = append(hosts, n.ID)
		}
	}
	if len(hosts) != 1 {
		return
	}
	host := hosts[0]
	vms := map[string]bool{}
	for i := range topo.Nodes {
		n := &topo.Nodes[i]
		// A device an integration reports (a Proxmox integration places its
		// guests itself; a firewall running as a VM stays the root) is left alone.
		if n.ID == host || n.Kind == topology.KindApp || n.Device != nil || !proxmoxGuest(n) {
			continue
		}
		vms[n.ID] = true
		n.ParentID, n.Port = host, ""
	}
	if len(vms) == 0 {
		return
	}
	// Replace the VM's uplink (to the switch, AP or router) with a link to its host.
	edges := topo.Edges[:0]
	for _, e := range topo.Edges {
		if !vms[e.Target] {
			edges = append(edges, e)
		}
	}
	for id := range vms {
		edges = append(edges, topology.Edge{ID: "e:" + host + "|" + id, Source: host, Target: id, Kind: topology.EdgeInferred})
	}
	topo.Edges = edges
}

// proxmoxGuest reports whether a node's MAC was assigned by Proxmox.
func proxmoxGuest(n *topology.Node) bool {
	return strings.Contains(strings.ToLower(n.Vendor), "proxmox")
}
