package insights

import (
	"fmt"
	"net/netip"
	"regexp"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// DNSSlowMs is when a DNS answer counts as slow.
const DNSSlowMs = 500.0

// SlowDNSChecks is how many slow answers in a row make a slow DNS server.
const SlowDNSChecks = 2

func init() {
	Rules["dhcp_no_offer"] = dhcpNoOffer
	Rules["dhcp_rogue"] = dhcpRogue
	Rules["dns_down"] = dnsDown
	Rules["dns_slow"] = dnsSlow
	Rules["dns_local"] = dnsLocal
}

// dhcpNoOffer: the network scan asked for an address and no server answered
// (a check that could not run says nothing).
func dhcpNoOffer(in Input, _ index) []Insight {
	var out []Insight
	server := runningDHCP(in.Topology)
	for _, c := range in.Topology.DhcpChecks {
		if c.Error != nil || len(c.Offers) > 0 {
			continue
		}
		if server != nil {
			// A router serves DHCP and its server runs: the devices get their
			// addresses, only Omini's request goes unanswered. Not worth an
			// alert (maintainer, 2026-10-13).
			continue
		}
		if dhcpOff(in.Topology, in.Topology.DhcpCheckAddrs[c.Interface]) {
			continue // the router says this network has no DHCP: nobody should answer
		}
		out = append(out, Insight{
			Key: "dhcp_no_offer|" + c.Interface, Rule: "dhcp_no_offer", Severity: Critical,
			Params: map[string]any{"iface": c.Interface},
		})
	}
	return out
}

// dhcpOff reports whether a router says the network of these addresses has
// its DHCP turned off.
func dhcpOff(topo topology.Topology, addrs []string) bool {
	var ips []netip.Addr
	for _, s := range addrs {
		if a, err := netip.ParseAddr(s); err == nil {
			ips = append(ips, a)
		}
	}
	for i := range topo.Nodes {
		n := &topo.Nodes[i]
		if n.Device == nil {
			continue
		}
		nets := n.Device.Networks
		for _, r := range n.Reports {
			if r.Device != nil && r.Device != n.Device {
				nets = append(nets, r.Device.Networks...)
			}
		}
		for _, nw := range nets {
			p, err := netip.ParsePrefix(model.Deref(nw.Gateway))
			if err != nil || nw.Dhcp == nil || nw.Dhcp.Enabled {
				continue
			}
			for _, a := range ips {
				if p.Masked().Contains(a) {
					return true
				}
			}
		}
	}
	return false
}

// dhcpService is a DHCP server's name or description (Kea, ISC dhcpd,
// dnsmasq, odhcpd...), not a client or a gateway monitor.
var (
	dhcpService   = regexp.MustCompile(`(?i)dhcp|kea|dnsmasq`)
	notDHCPServer = regexp.MustCompile(`(?i)gateway|dpinger|dhcp6c|client|relay`)
)

// runningDHCP is a device that serves DHCP (it reports leases or pools) and
// reports its DHCP server running.
func runningDHCP(topo topology.Topology) *topology.Node {
	for i := range topo.Nodes {
		n := &topo.Nodes[i]
		d := n.Device
		if d == nil || !n.Online || (len(d.DhcpLeases) == 0 && len(d.DhcpPools) == 0) {
			continue
		}
		for _, s := range d.Services {
			text := s.Name + " " + model.Deref(s.Description)
			if model.Deref(s.Running) && dhcpService.MatchString(text) && !notDHCPServer.MatchString(text) {
				return n
			}
		}
	}
	return nil
}

// dhcpRogue: more than one server answered, and one is not a router or a
// device known to serve DHCP (it reports leases or pools): someone plugged a
// router in the wrong way. The alert is about the server when it is on the map.
func dhcpRogue(in Input, _ index) []Insight {
	known := KnownDHCPServers(in.Topology)
	byIP := nodesByIP(in.Topology)
	var out []Insight
	for _, c := range in.Topology.DhcpChecks {
		if len(c.Offers) < 2 {
			continue
		}
		for _, o := range c.Offers {
			if known[o.ServerIP] {
				continue
			}
			p := map[string]any{"ip": o.ServerIP, "iface": c.Interface}
			if o.ServerMAC != nil {
				p["mac"] = string(*o.ServerMAC)
			}
			out = append(out, about(byIP[o.ServerIP], "dhcp_rogue", Critical, o.ServerIP, p))
		}
	}
	return out
}

func dnsDown(in Input, _ index) []Insight {
	return dnsRule(in, func(c model.DnsCheck) (string, string, map[string]any) {
		if q := c.External; q != nil && !q.Ok {
			return "dns_down", Critical, map[string]any{"name": q.Name, "error": model.Deref(q.Error)}
		}
		return "", "", nil
	})
}

// dnsSlow: a DNS server answered slowly in SlowDNSChecks checks in a row (one
// slow answer is a name it had to look up again, not a slow server).
func dnsSlow(in Input, _ index) []Insight {
	return dnsRule(in, func(c model.DnsCheck) (string, string, map[string]any) {
		if q := c.External; q != nil && q.Ok && model.Deref(q.RttMs) > DNSSlowMs && in.SlowDNS[c.Server] >= SlowDNSChecks {
			return "dns_slow", Warning, map[string]any{"ms": round1(*q.RttMs)}
		}
		return "", "", nil
	})
}

// dnsLocal: the server answers for the internet but not for a local name.
func dnsLocal(in Input, _ index) []Insight {
	return dnsRule(in, func(c model.DnsCheck) (string, string, map[string]any) {
		if c.External != nil && c.External.Ok && c.Local != nil && !c.Local.Ok {
			return "dns_local", Warning, map[string]any{"name": c.Local.Name, "error": model.Deref(c.Local.Error)}
		}
		return "", "", nil
	})
}

// dnsRule runs a check of each DNS server, once per server; the alert is
// about the server's node when it is on the map.
func dnsRule(in Input, check func(model.DnsCheck) (string, string, map[string]any)) []Insight {
	byIP := nodesByIP(in.Topology)
	var out []Insight
	seen := map[string]bool{}
	for _, c := range in.Topology.DnsChecks {
		rule, severity, p := check(c)
		if rule == "" || seen[c.Server] {
			continue
		}
		seen[c.Server] = true
		p["server"] = c.Server
		out = append(out, about(byIP[c.Server], rule, severity, c.Server, p))
	}
	return out
}

// about is an insight about a node when there is one, else about subject.
func about(n *topology.Node, rule, severity, subject string, p map[string]any) Insight {
	if n != nil {
		return node(rule, severity, n, subject, p)
	}
	return Insight{Key: rule + "|" + subject, Rule: rule, Severity: severity, Params: p}
}

func nodesByIP(topo topology.Topology) map[string]*topology.Node {
	byIP := map[string]*topology.Node{}
	for i := range topo.Nodes {
		if n := &topo.Nodes[i]; n.IP != "" && n.Kind != topology.KindApp {
			if _, taken := byIP[n.IP]; !taken {
				byIP[n.IP] = n
			}
		}
	}
	return byIP
}

// KnownDHCPServers are the addresses of the devices expected to serve DHCP:
// routers and firewalls, and devices that report leases or pools.
func KnownDHCPServers(topo topology.Topology) map[string]bool {
	known := map[string]bool{}
	for _, n := range topo.Nodes {
		d := n.Device
		if n.Kind != topology.KindDevice || d == nil {
			continue
		}
		if n.Role == string(model.DeviceRoleRouter) || n.Role == string(model.DeviceRoleFirewall) ||
			len(d.DhcpLeases) > 0 || len(d.DhcpPools) > 0 {
			known[n.IP] = true
			for _, ip := range d.IPs {
				known[ip] = true
			}
		}
	}
	return known
}

func init() { Rules["vlan_unknown"] = vlanUnknown }

// vlanUnknown: a device has a VLAN the network's catalog does not (created on
// the device by hand): Omini is the VLANs' source of truth.
func vlanUnknown(in Input, _ index) []Insight {
	if in.Vlans == nil {
		return nil
	}
	var out []Insight
	for _, n := range devices(in) {
		for _, v := range n.Device.Vlans {
			if _, ok := in.Vlans[v.ID]; !ok {
				out = append(out, node("vlan_unknown", Warning, n, fmt.Sprint(v.ID), map[string]any{
					"vlan": v.ID, "name": model.Deref(v.Name),
				}))
			}
		}
	}
	return out
}
