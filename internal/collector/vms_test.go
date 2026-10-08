package collector

import (
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
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
		topology.Node{ID: "vm", Kind: topology.KindClient, Type: "virtual_machine", Vendor: "Proxmox Server Solutions", ParentID: "gw", Port: "lan"},
		// A guest classified by what runs in it is still a guest.
		topology.Node{ID: "nas", Kind: topology.KindClient, Type: "nas", Product: "truenas", Vendor: "Proxmox Server Solutions", ParentID: "gw"},
		topology.Node{ID: "phone", Kind: topology.KindClient, Type: "phone", ParentID: "gw"},
	)
	topo.Edges = append(topo.Edges,
		topology.Edge{ID: "e:gw|vm", Source: "gw", Target: "vm", SourcePort: "lan"},
		topology.Edge{ID: "e:gw|nas", Source: "gw", Target: "nas"},
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
	if got := parentEdges(topo, "nas"); len(got) != 1 || got[0] != "pve1" {
		t.Fatalf("TrueNAS guest uplinks = %v", got)
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

func TestApplyInventoryMarksHiddenDevices(t *testing.T) {
	topo := topology.Topology{Nodes: []topology.Node{{ID: "tv", Kind: topology.KindClient}, {ID: "phone", Kind: topology.KindClient}}}
	applyInventory(&topo, []store.InventoryEntry{{ID: "tv", Kind: "client", Hidden: true, LastSeen: time.Now()}}, time.Now())
	if !topo.Nodes[0].Hidden || topo.Nodes[1].Hidden {
		t.Fatalf("hidden flags: %+v", topo.Nodes)
	}
}

func TestModelNames(t *testing.T) {
	apple := topology.Node{ID: "a", Kind: topology.KindClient, Model: "iPhone14,2", Label: "iphone"}
	nameModel(&apple)
	if apple.Model != "iPhone 13 Pro" || apple.Label != "iphone" {
		t.Fatalf("apple: %+v", apple)
	}
	android := topology.Node{ID: "b", Kind: topology.KindClient, Hostname: "SM-S911B", Label: "SM-S911B"}
	nameModel(&android)
	if android.Model != "Samsung Galaxy S23" || android.Label != "Samsung Galaxy S23" {
		t.Fatalf("android: %+v", android)
	}
	tv := topology.Node{ID: "c", Kind: topology.KindClient, Hostname: "living-room-tv", Label: "living-room-tv"}
	nameModel(&tv)
	if tv.Model != "" || tv.Label != "living-room-tv" {
		t.Fatalf("unknown names stay: %+v", tv)
	}
}

func TestVMsReportedByAnIntegrationStayPut(t *testing.T) {
	topo := proxmoxNet(1)
	// The firewall runs as a VM (Proxmox MAC) and is reported by its own integration.
	topo.Nodes = append(topo.Nodes, topology.Node{ID: "dev:fw", Kind: topology.KindDevice, Vendor: "Proxmox Server Solutions", Device: &model.Device{Key: "fw"}})
	attachVMs(&topo)
	if got := parentEdges(topo, "dev:fw"); len(got) != 0 {
		t.Fatalf("the firewall was moved under the host: %v", got)
	}
	if got := parentEdges(topo, "vm"); len(got) != 1 || got[0] != "pve1" {
		t.Fatalf("an unreported guest is still inferred: %v", got)
	}
}

func TestNoInferenceWithAProxmoxIntegration(t *testing.T) {
	topo := proxmoxNet(1)
	topo.Nodes[1].Device = &model.Device{Key: "pve1", Vendor: model.Ptr("Proxmox")}
	attachVMs(&topo)
	if got := parentEdges(topo, "vm"); len(got) != 1 || got[0] != "gw" {
		t.Fatalf("guests are placed by the integration, not guessed: %v", got)
	}
}
