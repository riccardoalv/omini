package collector

import (
	"testing"

	"github.com/riccardoalv/omini/internal/topology"
)

func proxmoxNet(hosts int) topology.Topology {
	topo := topology.Topology{Nodes: []topology.Node{{ID: "gw", Kind: topology.KindDevice}}}
	for i := range hosts {
		id := []string{"pve1", "pve2"}[i]
		topo.Nodes = append(topo.Nodes, topology.Node{ID: id, Kind: topology.KindClient, Product: "proxmox", ParentID: "gw"})
		topo.Edges = append(topo.Edges, topology.Edge{ID: "e:gw|" + id, Source: "gw", Target: id})
	}
	topo.Nodes = append(topo.Nodes,
		topology.Node{ID: "vm", Kind: topology.KindClient, Type: "virtual_machine", Brand: "proxmox", ParentID: "gw", Port: "lan"},
		topology.Node{ID: "phone", Kind: topology.KindClient, Type: "phone", ParentID: "gw"},
	)
	topo.Edges = append(topo.Edges,
		topology.Edge{ID: "e:gw|vm", Source: "gw", Target: "vm", SourcePort: "lan"},
		topology.Edge{ID: "e:gw|phone", Source: "gw", Target: "phone"},
	)
	return topo
}

func parentEdges(topo topology.Topology, target string) []string {
	var out []string
	for _, e := range topo.Edges {
		if e.Target == target {
			out = append(out, e.Source)
		}
	}
	return out
}

func TestVMsGoUnderTheOnlyProxmoxHost(t *testing.T) {
	topo := proxmoxNet(1)
	attachVMs(&topo)

	vm := topo.Nodes[2]
	if vm.ParentID != "pve1" || vm.Port != "" {
		t.Fatalf("vm parent = %q port = %q", vm.ParentID, vm.Port)
	}
	if got := parentEdges(topo, "vm"); len(got) != 1 || got[0] != "pve1" {
		t.Fatalf("vm uplinks = %v", got)
	}
	if got := parentEdges(topo, "phone"); len(got) != 1 || got[0] != "gw" {
		t.Fatalf("other devices must not move: %v", got)
	}
}

func TestVMsStayWithSeveralProxmoxHosts(t *testing.T) {
	topo := proxmoxNet(2)
	attachVMs(&topo)
	if got := parentEdges(topo, "vm"); len(got) != 1 || got[0] != "gw" {
		t.Fatalf("no inference with two hosts: %v", got)
	}
}

func TestVMsStayWithoutProxmoxHost(t *testing.T) {
	topo := proxmoxNet(0)
	attachVMs(&topo)
	if got := parentEdges(topo, "vm"); len(got) != 1 || got[0] != "gw" {
		t.Fatalf("no host: %v", got)
	}
}
