package insights

import (
	"fmt"
	"net/netip"
	"strconv"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

func init() {
	Rules["port_forward"] = portForwards
}

// portForwards: what the routers expose to the internet. port_forward (info)
// for each enabled forward, open while it exists; upnp_forward (warning) for
// one a device opened by itself (UPnP or NAT-PMP) instead. Omini never
// changes them.
func portForwards(in Input, idx index) []Insight {
	byIP := map[string]*topology.Node{}
	for i := range in.Topology.Nodes {
		n := &in.Topology.Nodes[i]
		if n.IP != "" && n.Kind != topology.KindApp && byIP[n.IP] == nil {
			byIP[n.IP] = n
		}
	}
	var out []Insight
	for _, n := range devices(in) {
		for _, f := range n.Device.PortForwards {
			if f.Enabled != nil && !*f.Enabled {
				continue
			}
			ip := model.Deref(f.InternalIP)
			p := map[string]any{
				"protocol": string(f.Protocol), "ports": Ports(f.ExternalPort, f.ExternalPortEnd),
				"ip": ip, "internal": internal(f), "target": ip,
				"description": model.Deref(f.Description),
			}
			if s := model.Deref(f.Source); s != "" {
				p["source"] = s
			}
			if dst := byIP[ip]; dst != nil && ip != "" {
				p["device"], p["target"] = idx.label(dst.ID), idx.label(dst.ID)
			}
			if p["target"] == "" {
				p["target"] = "?"
			}
			rule, sev := "port_forward", Info
			if model.Deref(f.Upnp) {
				rule, sev = "upnp_forward", Warning
			}
			subject := string(f.Protocol) + "|" + strconv.Itoa(int(f.ExternalPort))
			out = append(out, node(rule, sev, n, subject, p))
		}
	}
	return out
}

// Ports is a port or a range as text: "443", "8000-8010".
func Ports(first uint16, last *uint16) string {
	if last == nil || *last <= first {
		return strconv.Itoa(int(first))
	}
	return fmt.Sprintf("%d-%d", first, *last)
}

// internal is where a forward goes, "192.168.1.10:443" (a range keeps its
// length; no port: the same as outside).
func internal(f model.PortForward) string {
	ip := model.Deref(f.InternalIP)
	if a, err := netip.ParseAddr(ip); err == nil && a.Is6() {
		ip = "[" + ip + "]"
	}
	first := f.ExternalPort
	if f.InternalPort != nil {
		first = *f.InternalPort
	}
	var last *uint16
	if f.ExternalPortEnd != nil && *f.ExternalPortEnd > f.ExternalPort {
		end := first + (*f.ExternalPortEnd - f.ExternalPort)
		last = &end
	}
	if ip == "" {
		return Ports(first, last)
	}
	return ip + ":" + Ports(first, last)
}
