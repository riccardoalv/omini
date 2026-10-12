// Package fixip finds the devices that still get their address by DHCP on a
// router Omini can change, and the address a reservation for each would use:
// the current one, or a free one outside the DHCP range when the router keeps
// reservations out of it. It is a pure function of the topology.
package fixip

import (
	"net/netip"
	"slices"
	"strings"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// infra are the device types whose address should not change: what other
// devices and people reach by address.
var infra = []string{"switch", "ap", "server", "nas", "hypervisor", "virtual_machine", "printer", "ups"}

// Infra reports whether a device type is infrastructure.
func Infra(deviceType string) bool { return slices.Contains(infra, deviceType) }

// Candidate is a device on DHCP, without a reservation, on a router that can
// make one.
type Candidate struct {
	NodeID string `json:"node_id"`
	Label  string `json:"label"`
	Type   string `json:"type,omitempty"`
	MAC    string `json:"mac"`
	IP     string `json:"ip"`
	// Router is the node that serves the device's network.
	Router string `json:"router"`
	Infra  bool   `json:"infra"`
	// Proposed is the address the reservation uses instead of IP, when the
	// router refuses one inside its DHCP range and IP is in it.
	Proposed string `json:"proposed,omitempty"`
}

// Address is the address the reservation uses.
func (c Candidate) Address() string {
	if c.Proposed != "" {
		return c.Proposed
	}
	return c.IP
}

// network is a router's network that can take reservations.
type network struct {
	router  string
	prefix  netip.Prefix
	gateway netip.Addr
	dhcp    *model.NetworkDhcp
}

// Candidates are the devices on DHCP (a router has a lease for their MAC)
// with no reservation anywhere, on a network whose router can make one;
// devices with a random MAC (phones hiding theirs) are left out. Infra ones
// first, then by label.
func Candidates(topo topology.Topology) []Candidate {
	var nets []network
	leased := map[model.MACAddress]bool{}
	reserved := map[model.MACAddress]bool{}
	used := map[netip.Addr]bool{}
	for _, n := range topo.Nodes {
		if a, err := netip.ParseAddr(n.IP); err == nil {
			used[a] = true
		}
		if n.Device == nil {
			continue
		}
		for _, l := range n.Device.DhcpLeases {
			leased[model.NormMAC(string(l.MAC))] = true
			if a, err := netip.ParseAddr(l.IP); err == nil {
				used[a] = true
			}
		}
		for _, r := range n.Device.DhcpReservations {
			reserved[model.NormMAC(string(r.MAC))] = true
			if a, err := netip.ParseAddr(r.IP); err == nil {
				used[a] = true
			}
		}
		if !slices.Contains(n.Changes, model.ChangeKindReservationSet) {
			continue
		}
		for _, nw := range n.Device.Networks {
			p, err := netip.ParsePrefix(model.Deref(nw.Gateway))
			if err != nil || !p.Addr().Is4() {
				continue
			}
			nets = append(nets, network{router: n.ID, prefix: p.Masked(), gateway: p.Addr(), dhcp: nw.Dhcp})
		}
	}
	var out []Candidate
	for _, n := range topo.Nodes {
		mac := model.NormMAC(n.MAC)
		ip, err := netip.ParseAddr(n.IP)
		if n.Kind == topology.KindSegment || n.Kind == topology.KindWAN || n.RandomMAC ||
			mac == "" || err != nil || !ip.Is4() || !leased[mac] || reserved[mac] {
			continue
		}
		i := slices.IndexFunc(nets, func(nw network) bool { return nw.prefix.Contains(ip) })
		if i < 0 || nets[i].router == n.ID || ip == nets[i].gateway {
			continue
		}
		c := Candidate{
			NodeID: n.ID, Label: n.Label, Type: n.Type, MAC: string(mac), IP: n.IP,
			Router: nets[i].router, Infra: Infra(n.Type),
		}
		if inRange(nets[i].dhcp, ip) && nets[i].dhcp.ReserveInRange != nil && !*nets[i].dhcp.ReserveInRange {
			if free, ok := outside(nets[i], used); ok {
				c.Proposed = free.String()
				used[free] = true
			}
		}
		out = append(out, c)
	}
	slices.SortStableFunc(out, func(a, b Candidate) int {
		if a.Infra != b.Infra {
			if a.Infra {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label))
	})
	return out
}

func inRange(d *model.NetworkDhcp, ip netip.Addr) bool {
	if d == nil || !d.Enabled {
		return false
	}
	from, err1 := netip.ParseAddr(model.Deref(d.From))
	to, err2 := netip.ParseAddr(model.Deref(d.To))
	return err1 == nil && err2 == nil && ip.Compare(from) >= 0 && ip.Compare(to) <= 0
}

// outside is the first free address of the network after the gateway and
// outside its DHCP range.
func outside(nw network, used map[netip.Addr]bool) (netip.Addr, bool) {
	for a := nw.prefix.Addr().Next(); nw.prefix.Contains(a); a = a.Next() {
		if !nw.prefix.Contains(a.Next()) {
			break // the broadcast address
		}
		if a == nw.gateway || used[a] || inRange(nw.dhcp, a) {
			continue
		}
		return a, true
	}
	return netip.Addr{}, false
}
