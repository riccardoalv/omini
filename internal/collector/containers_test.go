package collector

import (
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

func tcp(port, target uint16) model.ContainerPort {
	p := model.ContainerPortProtocolTcp
	return model.ContainerPort{Port: port, Protocol: &p, TargetPort: &target}
}

func udp(port uint16) model.ContainerPort {
	p := model.ContainerPortProtocolUdp
	return model.ContainerPort{Port: port, Protocol: &p}
}

// containerNode is a container device as the topology has it: no address of
// its own, linked to its host by a neighbor of protocol "other".
func containerNode(host, hostIP, name, image, stack string, ports ...model.ContainerPort) (topology.Node, topology.Edge) {
	other := model.NeighborProtocolOther
	id := "dev:docker:" + hostIP + ":" + name
	running, replicas := uint64(1), uint64(1)
	state := "running"
	ct := &model.Container{Name: &name, Image: &image, State: &state, Ports: ports, Running: &running, Replicas: &replicas}
	if stack != "" {
		ct.Stack = &stack
	}
	n := topology.Node{
		ID: id, Kind: topology.KindDevice, Label: name, Online: true,
		Device: &model.Device{
			Key: "docker:" + hostIP + ":" + name, Name: name, Container: ct,
			Neighbors: []model.Neighbor{{LocalPort: "ingress", Protocol: &other, RemoteIP: &hostIP, RemotePlatform: new("Docker")}},
		},
	}
	return n, topology.Edge{ID: "e:" + host + "|" + id, Source: host, Target: id, Kind: topology.EdgeLLDP}
}

// master is the maintainer's Swarm manager: a Proxmox VM whose scan found apps.
func master() topology.Node {
	return classified(topology.Node{
		ID: "dev:bc:24:11:8d:d7:e5", Kind: topology.KindDevice, Label: "master", Online: true, IP: "192.168.1.42",
		Vendor: "Proxmox Server Solutions", OpenPorts: []int{22, 80, 81, 443, 3001, 8096, 9000},
		Web: []topology.WebApp{
			{Port: 81, URL: "http://192.168.1.42:81/", Title: "Login"},
			{Port: 3001, URL: "http://192.168.1.42:3001/", Title: "Uptime Kuma"},
			{Port: 8096, URL: "http://192.168.1.42:8096/", Title: "Jellyfin"},
			{Port: 9000, URL: "http://192.168.1.42:9000/", Title: "Portainer"},
		},
	})
}

func byID(topo topology.Topology) map[string]topology.Node {
	m := map[string]topology.Node{}
	for _, n := range topo.Nodes {
		m[n.ID] = n
	}
	return m
}

func TestAppCarriesTheContainerThatPublishesItsPort(t *testing.T) {
	host := master()
	jf, e1 := containerNode(host.ID, host.IP, "homelab_jellyfin", "lscr.io/linuxserver/jellyfin:latest", "homelab", tcp(8096, 8096))
	pt, e2 := containerNode(host.ID, host.IP, "homelab_portainer", "portainer/portainer-ce:2.21", "homelab", tcp(9000, 9000), tcp(8000, 8000))
	topo := topology.Topology{Nodes: []topology.Node{host, jf, pt}, Edges: []topology.Edge{e1, e2}}
	expandApps(&topo)
	attachContainers(&topo)

	nodes := byID(topo)
	if _, ok := nodes[jf.ID]; ok {
		t.Fatal("a container whose app the scan found is no node of its own")
	}
	if _, ok := nodes[pt.ID]; ok {
		t.Fatal("Portainer's container is its app")
	}
	for _, e := range topo.Edges {
		if e.Source == jf.ID || e.Target == jf.ID || e.Source == pt.ID || e.Target == pt.ID {
			t.Fatalf("edge to a container left: %+v", e)
		}
	}
	app := nodes["app:"+host.ID+":8096"]
	if app.Product != "jellyfin" || app.Container == nil || model.Deref(app.Container.Name) != "homelab_jellyfin" ||
		model.Deref(app.Container.Stack) != "homelab" {
		t.Fatalf("Jellyfin app = %+v (container %+v)", app, app.Container)
	}
	if c := nodes["app:"+host.ID+":9000"].Container; c == nil || model.Deref(c.Name) != "homelab_portainer" {
		t.Fatalf("Portainer app container = %+v", c)
	}
	if c := nodes["app:"+host.ID+":3001"].Container; c != nil {
		t.Fatalf("Uptime Kuma runs in no container Docker reports, got %+v", c)
	}
	apps := 0
	for _, n := range topo.Nodes {
		if n.Kind == topology.KindApp {
			apps++
		}
	}
	if apps != 3 { // Uptime Kuma, Jellyfin, Portainer: nothing duplicated
		t.Fatalf("want 3 apps, got %d: %+v", apps, topo.Nodes)
	}
}

func TestContainerWithoutAppBecomesAnAppOfItsImage(t *testing.T) {
	host := master()
	npm, e1 := containerNode(host.ID, host.IP, "homelab_nginx-proxy-manager", "jc21/nginx-proxy-manager:latest", "homelab",
		tcp(80, 80), tcp(443, 443), tcp(81, 81))
	st, e2 := containerNode(host.ID, host.IP, "homelab_speedtest", "registry.local/team/speedy:1", "homelab", tcp(3000, 3000))
	alloy, e3 := containerNode(host.ID, host.IP, "homelab_alloy", "grafana/alloy:latest", "homelab", udp(1514))
	topo := topology.Topology{Nodes: []topology.Node{host, npm, st, alloy}, Edges: []topology.Edge{e1, e2, e3}}
	expandApps(&topo)
	attachContainers(&topo)
	nodes := byID(topo)

	// The scan found a page on 81 it could not name: that is the app's page.
	a := nodes["app:"+host.ID+":81"]
	if a.Kind != topology.KindApp || a.Product != "nginx-proxy-manager" || a.Label != "Nginx Proxy Manager" ||
		a.ParentID != host.ID || len(a.Web) != 1 || a.Web[0].URL != "http://192.168.1.42:81/" || a.Container == nil || !a.Online {
		t.Fatalf("NPM app = %+v", a)
	}
	for _, p := range []string{"80", "443"} {
		if _, ok := nodes["app:"+host.ID+":"+p]; ok {
			t.Fatalf("a container is one app, got one on %s too", p)
		}
	}
	// An image the catalog does not know: named after its service; no web link,
	// since the scan saw no page on its port.
	s := nodes["app:"+host.ID+":3000"]
	if s.Product != "" || s.Label != "speedtest" || len(s.Web) != 0 {
		t.Fatalf("speedtest app = %+v", s)
	}
	// UDP only: an app without a web link.
	al := nodes["app:"+host.ID+":1514"]
	if al.Product != "alloy" || len(al.Web) != 0 || al.Container == nil {
		t.Fatalf("alloy app = %+v", al)
	}
	edges := 0
	for _, e := range topo.Edges {
		if e.Source == host.ID && (e.Target == a.ID || e.Target == s.ID || e.Target == al.ID) {
			edges++
		}
	}
	if edges != 3 {
		t.Fatalf("each container app hangs from its host, got %d edges: %+v", edges, topo.Edges)
	}
	for _, id := range []string{npm.ID, st.ID, alloy.ID} {
		if _, ok := nodes[id]; ok {
			t.Fatalf("%s should be an app, not a device", id)
		}
	}
}

func TestSwarmServiceFoundOnAnotherNodeOfTheMesh(t *testing.T) {
	manager := master()
	worker := classified(topology.Node{
		ID: "dev:bc:24:11:00:00:02", Kind: topology.KindDevice, Label: "worker", Online: true, IP: "192.168.1.43",
		OpenPorts: []int{8096}, Web: []topology.WebApp{{Port: 8096, URL: "http://192.168.1.43:8096/", Title: "Jellyfin"}},
	})
	manager.Web, manager.OpenPorts = nil, nil // the manager's scan found nothing
	jf, e1 := containerNode(manager.ID, manager.IP, "homelab_jellyfin", "jellyfin/jellyfin", "homelab", tcp(8096, 8096))
	jf.Device.Container.Addresses = []string{"192.168.1.43"}
	topo := topology.Topology{Nodes: []topology.Node{manager, worker, jf}, Edges: []topology.Edge{e1}}
	expandApps(&topo)
	attachContainers(&topo)
	nodes := byID(topo)
	if c := nodes["app:"+worker.ID+":8096"].Container; c == nil || model.Deref(c.Name) != "homelab_jellyfin" {
		t.Fatalf("the worker's Jellyfin is served by the service, got %+v", c)
	}
	if _, ok := nodes["app:"+manager.ID+":8096"]; ok {
		t.Fatal("found on the mesh: no second app on the manager")
	}
	if _, ok := nodes[jf.ID]; ok {
		t.Fatal("the service is its app")
	}
}

func TestContainerWithItsOwnAddressStaysADevice(t *testing.T) {
	host := master()
	pihole, e1 := containerNode(host.ID, host.IP, "pihole", "pihole/pihole", "", tcp(80, 80))
	pihole.Device.MACs = []model.MACAddress{"02:42:c0:a8:01:50"}
	pihole.Device.IPs = []string{"192.168.1.80"}
	plain, e2 := containerNode(host.ID, host.IP, "worker", "busybox", "")
	topo := topology.Topology{Nodes: []topology.Node{host, pihole, plain}, Edges: []topology.Edge{e1, e2}}
	attachContainers(&topo)
	nodes := byID(topo)
	if n, ok := nodes[pihole.ID]; !ok || n.Kind != topology.KindDevice {
		t.Fatal("a macvlan container stays a device")
	}
	if _, ok := nodes[plain.ID]; !ok {
		t.Fatal("a container without published ports is left alone")
	}
	if len(topo.Nodes) != 3 {
		t.Fatalf("nothing added, got %+v", topo.Nodes)
	}
}

func TestStoppedServiceAppIsOffline(t *testing.T) {
	host := master()
	c, e := containerNode(host.ID, host.IP, "homelab_tool", "registry.local/tool", "homelab", tcp(7000, 7000))
	zero := uint64(0)
	c.Device.Container.Running = &zero
	topo := topology.Topology{Nodes: []topology.Node{host, c}, Edges: []topology.Edge{e}}
	attachContainers(&topo)
	if a := byID(topo)["app:"+host.ID+":7000"]; a.Online || a.Kind != topology.KindApp {
		t.Fatalf("a service with no replica running is an offline app, got %+v", a)
	}
}
