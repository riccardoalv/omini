package topology

import (
	"net/netip"
	"slices"

	"github.com/riccardoalv/omini/internal/model"
)

// IPv6 is first-class: a node carries its IPv6 addresses (Node.IPs6) as it
// carries its IPv4 one. They come from the device's own report (its
// addresses and interfaces) and from what the routers see: their neighbor
// tables (NDP, like ARP for IPv4) and DHCPv6 leases. Link-local addresses
// (fe80::/10) are kept on the node, but never tie two reports together: the
// same one (fe80::1) is on many routers, on every network.

// addr6 parses an IPv6 address, alone or with its prefix length
// ("2001:db8::1/64"), without its zone ("fe80::1%eth0").
func addr6(s string) (netip.Addr, bool) {
	if p, err := netip.ParsePrefix(s); err == nil {
		s = p.Addr().String()
	}
	a, err := netip.ParseAddr(s)
	if err != nil || !a.Is6() || a.Is4In6() || a.IsUnspecified() || a.IsLoopback() || a.IsMulticast() {
		return netip.Addr{}, false
	}
	return a.WithZone(""), true
}

// IsIPv6 reports whether s is an IPv6 address (with or without a prefix).
func IsIPv6(s string) bool {
	_, ok := addr6(s)
	return ok
}

// mergeable: an address that tells which machine it is: anything but an
// IPv6 link-local address.
func mergeable(ip string) bool {
	if ip == "" {
		return false
	}
	a, err := netip.ParseAddr(ip)
	return err != nil || !a.Is6() || !a.IsLinkLocalUnicast()
}

// GlobalIPv6 are the addresses of ips6 that are not link-local (global and
// ULA): the ones that name a machine.
func GlobalIPv6(ips6 []string) []string {
	var out []string
	for _, s := range ips6 {
		if a, ok := addr6(s); ok && !a.IsLinkLocalUnicast() {
			out = append(out, s)
		}
	}
	return out
}

// seen6 records an IPv6 address seen for a MAC (in NDP or a DHCPv6 lease).
func (b *builder) seen6(m model.MACAddress, ip string) {
	if m = model.NormMAC(string(m)); m == "" || m.IsGroup() {
		return
	}
	if a, ok := addr6(ip); ok {
		b.v6[m] = append(b.v6[m], a.String())
	}
}

// ips6Of are a node's IPv6 addresses, compact: global and ULA ones first,
// then link-local.
func (b *builder) ips6Of(n *Node) []string {
	set := map[netip.Addr]bool{}
	add := func(s string) {
		if a, ok := addr6(s); ok {
			set[a] = true
		}
	}
	var macs []model.MACAddress
	if n.Kind == KindDevice {
		for _, d := range b.sources[n.ID] {
			add(model.Deref(d.Host))
			for _, s := range append(slices.Clone(d.IPs), ifaceIPs(d)...) {
				add(s)
			}
		}
		macs = deviceMACs(n.Device)
	} else if m := model.NormMAC(n.MAC); m != "" {
		macs = []model.MACAddress{m}
	}
	for _, m := range macs {
		for _, s := range b.v6[m] {
			add(s)
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]netip.Addr, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	slices.SortFunc(out, func(x, y netip.Addr) int {
		if lx, ly := x.IsLinkLocalUnicast(), y.IsLinkLocalUnicast(); lx != ly {
			if lx {
				return 1
			}
			return -1
		}
		return x.Compare(y)
	})
	strs := make([]string, len(out))
	for i, a := range out {
		strs[i] = a.String()
	}
	return strs
}

// assignIPv6 gives every node its IPv6 addresses, and makes the global ones
// find it (a host the scan saw by its IPv6 address is that node). It returns
// the addresses it indexed.
func (b *builder) assignIPv6() map[string]bool {
	ids := make([]string, 0, len(b.nodes))
	for id := range b.nodes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	indexed := map[string]bool{}
	for _, id := range ids {
		n := b.nodes[id]
		if n.Kind == KindWAN || n.Kind == KindSegment {
			continue
		}
		n.IPs6 = b.ips6Of(n)
		for _, ip := range GlobalIPv6(n.IPs6) {
			if _, taken := b.byIP[ip]; !taken {
				b.byIP[ip] = id
				indexed[ip] = true
			}
		}
	}
	return indexed
}

// neighborOf is where a MAC is seen in a router's neighbor tables: ARP, else
// NDP. Neither says for sure where a client is.
func (b *builder) neighborOf(arp map[model.MACAddress]arpSeen, m model.MACAddress) (arpSeen, bool) {
	if a, ok := arp[m]; ok {
		return a, true
	}
	a, ok := b.ndp[m]
	return a, ok
}

// collectNDP records a device's IPv6 neighbors.
func (b *builder) collectNDP(id string, d *model.Device) {
	for _, a := range d.Ndp {
		m := model.NormMAC(string(a.MAC))
		if m == "" || m.IsGroup() {
			continue
		}
		b.seen6(m, a.IP)
		if _, ok := b.ndp[m]; !ok {
			b.ndp[m] = arpSeen{node: id, iface: model.Deref(a.Interface)}
		}
	}
}

// ipKey is how an address is looked up: an IPv6 address in its compact form
// (plugins write 2001:0db8::0020 or fe80::1%eth0 too), anything else as is.
func ipKey(s string) string {
	if a, ok := addr6(s); ok {
		return a.String()
	}
	return s
}
