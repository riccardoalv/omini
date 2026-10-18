package insights

import (
	"net/netip"
	"slices"
	"strings"

	"github.com/riccardoalv/omini/internal/classify"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

func init() { Rules["iot_with_trusted"] = iotWithTrusted }

// iotTypes are the devices that seldom get updates and often talk to the
// cloud: on a network of their own they cannot reach the rest. Printers,
// game consoles, desk phones and phones are left out: they usually live on
// the main network, and telling about them would be noise.
var iotTypes = map[string]bool{
	classify.Camera: true, classify.TV: true, classify.MediaPlayer: true, classify.Speaker: true,
	classify.SmartHome: true, classify.Appliance: true, classify.AirConditioner: true, classify.SolarInverter: true,
}

// trustedTypes hold what is worth keeping away from them.
var trustedTypes = map[string]bool{
	classify.Server: true, classify.NAS: true, classify.Hypervisor: true,
	classify.VirtualMachine: true, classify.Computer: true,
}

// segNet is one local network: a router's LAN or VLAN subnet.
type segNet struct {
	prefix netip.Prefix
	vlan   uint16
	router string
}

// iotWithTrusted: IoT devices share a network (a VLAN, else a subnet) with
// servers and computers. One insight per network, and only once the network
// already uses VLANs: without them there is nowhere to move them.
func iotWithTrusted(in Input, idx index) []Insight {
	nets, vlans := segNetworks(in.Topology)
	for id := range in.Vlans {
		vlans = vlans || id > 1
	}
	if !vlans || len(nets) == 0 {
		return nil
	}
	type group struct {
		net          segNet
		iot, trusted []string
	}
	groups := map[netip.Prefix]*group{}
	for _, n := range in.Topology.Nodes {
		isIoT, isTrusted := iotTypes[n.Type], trustedTypes[n.Type]
		if (!isIoT && !isTrusted) || n.Hidden {
			continue
		}
		ip, err := netip.ParseAddr(n.IP)
		if err != nil {
			continue
		}
		net, ok := networkOf(nets, ip)
		if !ok {
			continue
		}
		g := groups[net.prefix]
		if g == nil {
			g = &group{net: net}
			groups[net.prefix] = g
		}
		if isIoT {
			g.iot = append(g.iot, idx.label(n.ID))
		} else {
			g.trusted = append(g.trusted, idx.label(n.ID))
		}
	}
	var out []Insight
	for p, g := range groups {
		if len(g.iot) == 0 || len(g.trusted) == 0 {
			continue
		}
		slices.Sort(g.iot)
		slices.Sort(g.trusted)
		params := map[string]any{
			"network": p.String(), "iot": len(g.iot), "trusted": len(g.trusted),
			"iot_devices": strings.Join(g.iot, ", "), "trusted_devices": strings.Join(g.trusted, ", "),
		}
		if g.net.vlan > 0 {
			params["vlan"] = int(g.net.vlan)
			if name := in.Vlans[g.net.vlan]; name != "" {
				params["vlan_name"] = name
			}
		}
		out = append(out, Insight{
			Key: "iot_with_trusted:" + p.String(), Rule: "iot_with_trusted", Severity: Info,
			NodeID: g.net.router, Params: params,
		})
	}
	slices.SortFunc(out, func(a, b Insight) int { return strings.Compare(a.Key, b.Key) })
	return out
}

// segNetworks are the private IPv4 LAN subnets the routers and firewalls
// report, with their VLAN, and whether any VLAN besides 1 is in use.
func segNetworks(topo topology.Topology) ([]segNet, bool) {
	var nets []segNet
	seen := map[netip.Prefix]bool{}
	vlans := false
	add := func(s string, vlan uint16, router string) {
		p, err := netip.ParsePrefix(s)
		if err != nil || !p.Addr().Is4() || !p.Addr().IsPrivate() || p.Bits() > 30 {
			return
		}
		p = p.Masked()
		if !seen[p] {
			seen[p] = true
			nets = append(nets, segNet{prefix: p, vlan: vlan, router: router})
		}
	}
	for _, n := range topo.Nodes {
		d := n.Device
		if d == nil {
			continue
		}
		for _, v := range d.Vlans {
			vlans = vlans || v.ID > 1
		}
		for _, i := range d.Interfaces {
			v := model.Deref(i.Vlan)
			vlans = vlans || v > 1
			if model.Deref(i.Wan) || (n.Role != "router" && n.Role != "firewall") {
				continue
			}
			for _, ip := range i.IPs {
				add(ip, v, n.ID)
			}
		}
		for _, nw := range d.Networks {
			vlans = vlans || nw.Vlan > 1
			add(model.Deref(nw.Gateway), nw.Vlan, n.ID)
		}
		if n.Role == "router" || n.Role == "firewall" {
			for _, v := range d.Vlans {
				add(model.Deref(v.Subnet), v.ID, n.ID)
			}
		}
	}
	// The most specific network first, for networkOf.
	slices.SortFunc(nets, func(a, b segNet) int { return b.prefix.Bits() - a.prefix.Bits() })
	return nets, vlans
}

func networkOf(nets []segNet, ip netip.Addr) (segNet, bool) {
	for _, n := range nets {
		if n.prefix.Contains(ip) {
			return n, true
		}
	}
	return segNet{}, false
}
