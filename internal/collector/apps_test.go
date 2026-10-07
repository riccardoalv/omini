package collector

import (
	"slices"
	"testing"

	"github.com/riccardoalv/omini/internal/topology"
)

func classified(n topology.Node) topology.Node {
	classifyNode(&n)
	return n
}

func TestUbuntuVMRunningAppsGetsOneNodePerApp(t *testing.T) {
	vm := classified(topology.Node{
		ID: "mac:bc:24:11:8d:d7:e5", Kind: topology.KindClient, Online: true, IP: "192.168.1.42",
		Vendor: "Proxmox Server Solutions", OpenPorts: []int{22, 8080, 8096},
		Banners: []string{"SSH-2.0-OpenSSH_10.2p1 Ubuntu-2ubuntu3.6"},
		Titles:  []string{"Jellyfin", "qBittorrent WebUI"},
		Web: []topology.WebApp{
			{Port: 8080, URL: "http://192.168.1.42:8080/", Title: "qBittorrent WebUI"},
			{Port: 8096, URL: "http://192.168.1.42:8096/", Title: "Jellyfin"},
		},
	})
	if vm.Product != "jellyfin" {
		t.Fatalf("before expansion the classifier sees Jellyfin, got %q", vm.Product)
	}
	topo := topology.Topology{Nodes: []topology.Node{vm}}
	expandApps(&topo)

	host := topo.Nodes[0]
	if host.Product != "" || host.OS != "ubuntu" || host.Type != "virtual_machine" {
		t.Fatalf("host should be an Ubuntu VM, got product=%q os=%q type=%q", host.Product, host.OS, host.Type)
	}
	if len(topo.Nodes) != 3 || len(topo.Edges) != 2 {
		t.Fatalf("want 2 app nodes, got nodes=%d edges=%d", len(topo.Nodes), len(topo.Edges))
	}
	qb, jf := topo.Nodes[1], topo.Nodes[2]
	if qb.Kind != topology.KindApp || qb.Product != "qbittorrent" || qb.Label != "qBittorrent" || qb.ParentID != vm.ID || qb.Port != "8080" {
		t.Errorf("qBittorrent node = %+v", qb)
	}
	if jf.Product != "jellyfin" || jf.Label != "Jellyfin" || jf.Web[0].URL != "http://192.168.1.42:8096/" {
		t.Errorf("Jellyfin node = %+v", jf)
	}
}

func TestApplianceKeepsItsOwnLogo(t *testing.T) {
	for _, n := range []topology.Node{
		{ID: "a", Kind: topology.KindClient, Titles: []string{"TrueNAS"}, Web: []topology.WebApp{{Port: 443, Title: "TrueNAS"}}},
		{
			ID: "b", Kind: topology.KindClient, Titles: []string{"Home Assistant"}, OpenPorts: []int{8123},
			Web: []topology.WebApp{{Port: 8123, Title: "Home Assistant"}},
		},
		{
			ID: "c", Kind: topology.KindClient, Titles: []string{"proxmox - Proxmox Virtual Environment"}, OpenPorts: []int{8006},
			Web: []topology.WebApp{{Port: 8006, Title: "proxmox - Proxmox Virtual Environment"}},
		},
	} {
		topo := topology.Topology{Nodes: []topology.Node{classified(n)}}
		expandApps(&topo)
		if len(topo.Nodes) != 1 || topo.Nodes[0].Product == "" {
			t.Errorf("%s: appliance must keep its product and get no app node: %+v", n.Titles[0], topo.Nodes)
		}
	}
}

func TestApplianceWithAnExtraApp(t *testing.T) {
	// Proxmox host that also exposes Portainer: Proxmox stays, Portainer becomes a child.
	n := classified(topology.Node{
		ID: "p", Kind: topology.KindClient, OpenPorts: []int{8006, 9443},
		Titles: []string{"Proxmox Virtual Environment", "Portainer"},
		Web:    []topology.WebApp{{Port: 8006, Title: "Proxmox Virtual Environment"}, {Port: 9443, Title: "Portainer"}},
	})
	topo := topology.Topology{Nodes: []topology.Node{n}}
	expandApps(&topo)
	if topo.Nodes[0].Product != "proxmox" || len(topo.Nodes) != 2 || topo.Nodes[1].Product != "portainer" {
		t.Fatalf("nodes = %+v", topo.Nodes)
	}
}

func TestAppRecognizedByPortWhenTitleIsUseless(t *testing.T) {
	n := classified(topology.Node{
		ID: "x", Kind: topology.KindClient, OpenPorts: []int{22, 8989},
		Banners: []string{"SSH-2.0-OpenSSH_9.2p1 Debian-2+deb12u3"},
		Web:     []topology.WebApp{{Port: 8989, Title: ""}},
	})
	topo := topology.Topology{Nodes: []topology.Node{n}}
	expandApps(&topo)
	if len(topo.Nodes) != 2 || topo.Nodes[1].Product != "sonarr" || topo.Nodes[1].Label != "Sonarr" {
		t.Fatalf("nodes = %+v", topo.Nodes)
	}
}

func TestExporterOnAnotherPortDoesNotMakeTheHostAnAppliance(t *testing.T) {
	vm := classified(topology.Node{
		ID: "vm", Kind: topology.KindClient, Vendor: "Proxmox Server Solutions", OpenPorts: []int{22, 8081, 8989},
		Banners: []string{"SSH-2.0-OpenSSH_10.2p1 Ubuntu-2ubuntu3.6"},
		Titles:  []string{"OPNsense Exporter", "Login - Sonarr"},
		Web:     []topology.WebApp{{Port: 8081, Title: "OPNsense Exporter"}, {Port: 8989, Title: "Login - Sonarr"}},
	})
	if vm.Product != "opnsense" {
		t.Fatalf("precondition: the title fools the classifier, got %q", vm.Product)
	}
	topo := topology.Topology{Nodes: []topology.Node{vm}}
	expandApps(&topo)
	host := topo.Nodes[0]
	if host.Product != "" || host.OS != "ubuntu" || host.Type == "firewall" {
		t.Fatalf("host = product %q os %q type %q", host.Product, host.OS, host.Type)
	}
	labels := []string{topo.Nodes[1].Label, topo.Nodes[2].Label}
	if !slices.Equal(labels, []string{"OPNsense Exporter", "Sonarr"}) {
		t.Fatalf("app labels = %v", labels)
	}
}
