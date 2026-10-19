package collector

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/riccardoalv/omini/internal/appicons"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// attachContainers makes a container that publishes ports on its host the
// app the network scan found there: one node per self-hosted app. An app on
// host H, port P carries the container (or Swarm service) that publishes P on
// H, or on one of the addresses its ports also answer on (a Swarm routing
// mesh); the container is then no node of its own. A container whose ports
// show no app becomes an app of its own under its host, named and drawn
// after its image. A container with an address of its own (macvlan, ipvlan)
// stays a device: its apps come from scanning it. Matching is by host and
// port only: no runtime is known here.
func attachContainers(topo *topology.Topology) {
	byID := make(map[string]*topology.Node, len(topo.Nodes))
	for i := range topo.Nodes {
		byID[topo.Nodes[i].ID] = &topo.Nodes[i]
	}
	type at struct {
		ip   string
		port int
	}
	onHost := map[string]map[int]*topology.Node{} // host node → port → app
	onIP := map[at][]*topology.Node{}
	for i := range topo.Nodes {
		a := &topo.Nodes[i]
		if a.Kind != topology.KindApp {
			continue
		}
		port, err := strconv.Atoi(a.Port)
		if err != nil {
			continue
		}
		if onHost[a.ParentID] == nil {
			onHost[a.ParentID] = map[int]*topology.Node{}
		}
		onHost[a.ParentID][port] = a
		if a.IP != "" {
			onIP[at{a.IP, port}] = append(onIP[at{a.IP, port}], a)
		}
	}
	gone := map[string]bool{}
	var added []topology.Node
	var edges []topology.Edge
	for i := range topo.Nodes {
		c := &topo.Nodes[i]
		if c.Kind == topology.KindApp || c.Device == nil || c.Device.Container == nil ||
			len(c.Device.Container.Ports) == 0 || ownAddress(c.Device) {
			continue
		}
		host := containerHost(topo, byID, c)
		if host == nil {
			continue
		}
		ct := c.Device.Container
		matched := false
		for _, p := range ct.Ports {
			if !webProtocol(p) {
				continue
			}
			var apps []*topology.Node
			if a := onHost[host.ID][int(p.Port)]; a != nil {
				apps = append(apps, a)
			}
			for _, ip := range ct.Addresses {
				apps = append(apps, onIP[at{ip, int(p.Port)}]...)
			}
			for _, a := range apps {
				if a.Container == nil {
					a.Container = ct
				}
				matched = true
			}
		}
		gone[c.ID] = true
		if matched {
			continue
		}
		app := containerApp(c, host)
		if byID[app.ID] != nil || slices.ContainsFunc(added, func(n topology.Node) bool { return n.ID == app.ID }) {
			continue // the same service reported by two managers
		}
		added = append(added, app)
		edges = append(edges, topology.Edge{
			ID: "e:" + host.ID + "|" + app.ID, Source: host.ID, Target: app.ID,
			SourcePort: app.Port, Kind: topology.EdgeInferred,
		})
	}
	if len(gone) == 0 {
		return
	}
	topo.Nodes = slices.DeleteFunc(topo.Nodes, func(n topology.Node) bool { return gone[n.ID] })
	topo.Edges = slices.DeleteFunc(topo.Edges, func(e topology.Edge) bool { return gone[e.Source] || gone[e.Target] })
	topo.Nodes = append(topo.Nodes, added...)
	topo.Edges = append(topo.Edges, edges...)
}

// ownAddress: the container is reachable at an address of its own.
func ownAddress(d *model.Device) bool {
	return len(d.MACs) > 0 || len(d.IPs) > 0 || model.Deref(d.Host) != ""
}

// webProtocol: a port a web app can be on (TCP, or not said).
func webProtocol(p model.ContainerPort) bool {
	return p.Protocol == nil || *p.Protocol == model.ContainerPortProtocolTcp
}

// containerHost is the node the container hangs from (its neighbor of
// protocol "other"): the other end of its link, the one at the neighbor's
// address when it has several.
func containerHost(topo *topology.Topology, byID map[string]*topology.Node, c *topology.Node) *topology.Node {
	want := map[string]bool{}
	for _, nb := range c.Device.Neighbors {
		if model.Deref(nb.Protocol) == model.NeighborProtocolOther && nb.RemoteIP != nil {
			want[*nb.RemoteIP] = true
		}
	}
	var first *topology.Node
	for _, e := range topo.Edges {
		other := ""
		switch c.ID {
		case e.Source:
			other = e.Target
		case e.Target:
			other = e.Source
		default:
			continue
		}
		n := byID[other]
		if n == nil || n.Kind == topology.KindApp {
			continue
		}
		if want[n.IP] {
			return n
		}
		if first == nil {
			first = n
		}
	}
	return first
}

// containerApp is the app node of a container no app was found for: on its
// first port the scan found a web page on, else an open one, else its first
// TCP port; its icon is its image's when the catalog knows it.
func containerApp(c *topology.Node, host *topology.Node) topology.Node {
	ct := c.Device.Container
	ports := slices.Clone(ct.Ports)
	slices.SortStableFunc(ports, func(a, b model.ContainerPort) int {
		return cmp.Compare(portRank(host, a), portRank(host, b))
	})
	p := ports[0]
	app, _ := appicons.FromImage(model.Deref(ct.Image))
	var web []topology.WebApp
	if webProtocol(p) {
		w := topology.WebApp{Port: int(p.Port), App: app}
		// Only a page the scan saw: a published port is not always a web
		// interface (syslog, MQTT...), and a link that leads nowhere is worse
		// than none.
		if i := slices.IndexFunc(host.Web, func(w topology.WebApp) bool { return w.Port == int(p.Port) }); i >= 0 {
			w.URL, w.Title = host.Web[i].URL, host.Web[i].Title
		}
		if w.URL != "" {
			web = []topology.WebApp{w}
		}
	}
	label := containerLabel(ct, c.Label)
	if app != "" {
		label = appicons.DisplayName(app, "")
	}
	port := strconv.Itoa(int(p.Port))
	return topology.Node{
		ID: "app:" + host.ID + ":" + port, Kind: topology.KindApp, Label: label,
		Role: "app", Type: "app", Online: host.Online && containerUp(ct), ParentID: host.ID, Port: port,
		IP: host.IP, Product: app, Web: web, Container: ct,
	}
}

// portRank orders a container's ports for its app: a web page found by the
// scan, an open port, another TCP port, then the rest.
func portRank(host *topology.Node, p model.ContainerPort) int {
	switch {
	case !webProtocol(p):
		return 3
	case slices.ContainsFunc(host.Web, func(w topology.WebApp) bool { return w.Port == int(p.Port) }):
		return 0
	case slices.Contains(host.OpenPorts, int(p.Port)):
		return 1
	}
	return 2
}

// containerLabel names a container app without a catalog icon: its name
// without its stack ("homelab_speedtest" → "speedtest").
func containerLabel(ct *model.Container, fallback string) string {
	name := cmp.Or(model.Deref(ct.Name), fallback)
	if stack := model.Deref(ct.Stack); stack != "" {
		name = cmp.Or(strings.TrimPrefix(name, stack+"_"), name)
	}
	return name
}

// containerUp: the container runs (a service: at least one replica).
func containerUp(ct *model.Container) bool {
	if ct.Running != nil {
		return *ct.Running > 0
	}
	s := model.Deref(ct.State)
	return s == "" || s == "running"
}
