package api

import (
	"cmp"
	"net/http"
	"net/netip"
	"slices"

	"github.com/riccardoalv/omini/internal/insights"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// The "DNS and DHCP" screen: the checks of the network scan, and the pools
// and leases the routers report, from the current map.

type dhcpView struct {
	Checks []dhcpCheckView `json:"checks"`
	Pools  []poolView      `json:"pools"`
	Leases []leaseView     `json:"leases"`
}

type dhcpCheckView struct {
	Interface string      `json:"interface"`
	Error     string      `json:"error,omitempty"`
	Offers    []offerView `json:"offers"`
}

type offerView struct {
	model.DhcpOffer
	Known  bool   `json:"known"`             // a router, or a device serving DHCP
	NodeID string `json:"node_id,omitempty"` // the server on the map
}

type poolView struct {
	NodeID string `json:"node_id"`
	model.DhcpPool
}

type leaseView struct {
	NodeID string `json:"node_id"`          // the server that gave it
	Client string `json:"client,omitempty"` // the client on the map
	model.DhcpLease
}

type dnsServerView struct {
	model.DnsCheck
	NodeID string `json:"node_id,omitempty"`
}

func (s *Server) servicesView(w http.ResponseWriter, _ *http.Request) {
	topo := s.Collector.State().Topology
	writeJSON(w, http.StatusOK, map[string]any{
		"dhcp": dhcpOf(topo), "dns": map[string]any{"servers": dnsOf(topo)},
		"generated_at": s.Collector.State().GeneratedAt,
	})
}

func nodesBy(topo topology.Topology) (byIP map[string]string, byMAC map[string]string) {
	byIP, byMAC = map[string]string{}, map[string]string{}
	for _, n := range topo.Nodes {
		if n.Kind == topology.KindApp {
			continue
		}
		if _, ok := byIP[n.IP]; n.IP != "" && !ok {
			byIP[n.IP] = n.ID
		}
		if _, ok := byMAC[n.MAC]; n.MAC != "" && !ok {
			byMAC[n.MAC] = n.ID
		}
	}
	// Then IPv6: a server reached by its IPv6 address is that node too.
	for _, n := range topo.Nodes {
		for _, ip := range topology.GlobalIPv6(n.IPs6) {
			if _, ok := byIP[ip]; !ok && n.Kind != topology.KindApp {
				byIP[ip] = n.ID
			}
		}
	}
	return byIP, byMAC
}

func dhcpOf(topo topology.Topology) dhcpView {
	known := insights.KnownDHCPServers(topo)
	byIP, byMAC := nodesBy(topo)
	v := dhcpView{Checks: []dhcpCheckView{}, Pools: []poolView{}, Leases: []leaseView{}}
	for _, c := range topo.DhcpChecks {
		cv := dhcpCheckView{Interface: c.Interface, Error: model.Deref(c.Error), Offers: []offerView{}}
		for _, o := range c.Offers {
			cv.Offers = append(cv.Offers, offerView{DhcpOffer: o, Known: known[o.ServerIP], NodeID: byIP[o.ServerIP]})
		}
		v.Checks = append(v.Checks, cv)
	}
	for _, n := range topo.Nodes {
		d := n.Device
		if n.Kind != topology.KindDevice || d == nil {
			continue
		}
		for _, p := range d.DhcpPools {
			v.Pools = append(v.Pools, poolView{NodeID: n.ID, DhcpPool: p})
		}
		for _, l := range d.DhcpLeases {
			v.Leases = append(v.Leases, leaseView{NodeID: n.ID, Client: byMAC[string(l.MAC)], DhcpLease: l})
		}
	}
	slices.SortFunc(v.Leases, func(a, b leaseView) int { return cmpIP(a.IP, b.IP) })
	return v
}

func dnsOf(topo topology.Topology) []dnsServerView {
	byIP, _ := nodesBy(topo)
	out := []dnsServerView{}
	seen := map[string]bool{}
	for _, c := range topo.DnsChecks {
		if !seen[c.Server] {
			seen[c.Server] = true
			out = append(out, dnsServerView{DnsCheck: c, NodeID: byIP[c.Server]})
		}
	}
	slices.SortFunc(out, func(a, b dnsServerView) int { return cmpIP(a.Server, b.Server) })
	return out
}

func cmpIP(a, b string) int {
	x, errA := netip.ParseAddr(a)
	y, errB := netip.ParseAddr(b)
	if errA != nil || errB != nil {
		return cmp.Compare(a, b)
	}
	return x.Compare(y)
}
