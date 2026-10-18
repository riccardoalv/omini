package flows

import (
	"net/netip"
	"slices"
	"strings"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// Directory tells, for an address seen in a flow, whether it is on the local
// network, which device on the map it is, and what name it had when a device
// looked it up. It is rebuilt after every collection round.
type Directory struct {
	nodes    map[netip.Addr]string
	networks []netip.Prefix      // the routers' and devices' own networks (not their WANs)
	wan      map[netip.Addr]bool // the routers' internet-side addresses
	names    Names
}

// Names maps an address to the name a device looked up to reach it: by the
// device that asked (client) first, then by anyone.
type Names struct {
	ByClient map[netip.Addr]map[netip.Addr]string
	Any      map[netip.Addr]string
}

// Name is the name client looked up for addr ("" when none is known).
func (n Names) Name(client, addr netip.Addr) string {
	if m := n.ByClient[client]; m != nil {
		if s := m[addr]; s != "" {
			return s
		}
	}
	return n.Any[addr]
}

// NewDirectory reads the map: every node's addresses, the ARP and NDP tables
// (so a device's IPv6 addresses are known by its MAC), and the networks of
// the devices' interfaces.
func NewDirectory(topo topology.Topology, names Names) *Directory {
	d := &Directory{nodes: map[netip.Addr]string{}, wan: map[netip.Addr]bool{}, names: names}
	byMAC := map[model.MACAddress]string{}
	add := func(s, id string) {
		s, _, _ = strings.Cut(s, "/")
		if a, err := netip.ParseAddr(s); err == nil {
			a = a.Unmap()
			if _, ok := d.nodes[a]; !ok {
				d.nodes[a] = id
			}
		}
	}
	for _, n := range topo.Nodes {
		if n.Kind == topology.KindWAN || n.Kind == topology.KindApp {
			continue
		}
		if m := model.NormMAC(n.MAC); m != "" {
			byMAC[m] = n.ID
		}
		add(n.IP, n.ID)
		for _, a := range topology.GlobalIPv6(n.IPs6) { // NDP, DHCPv6 leases
			add(a, n.ID)
		}
		if n.Device == nil {
			continue
		}
		for _, m := range n.Device.MACs {
			if _, ok := byMAC[m]; !ok {
				byMAC[m] = n.ID
			}
		}
		for _, a := range n.Device.IPs {
			add(a, n.ID)
		}
		for _, i := range n.Device.Interfaces {
			wan := i.Wan != nil && *i.Wan
			for _, s := range i.IPs {
				p, err := netip.ParsePrefix(s)
				if err != nil {
					add(s, n.ID)
					continue
				}
				if wan {
					d.wan[p.Addr().Unmap()] = true
					continue
				}
				add(s, n.ID)
				if p.Bits() > 0 && !p.Addr().IsLoopback() && !p.Addr().IsLinkLocalUnicast() {
					d.networks = append(d.networks, p.Masked())
				}
			}
		}
	}
	for _, n := range topo.Nodes {
		if n.Device == nil {
			continue
		}
		for _, e := range append(append([]model.ArpEntry{}, n.Device.Arp...), n.Device.Ndp...) {
			if id, ok := byMAC[model.NormMAC(string(e.MAC))]; ok {
				add(e.IP, id)
			}
		}
	}
	return d
}

// Node is the device on the map with this address ("" when none).
func (d *Directory) Node(a netip.Addr) string {
	if d == nil {
		return ""
	}
	return d.nodes[a.Unmap()]
}

// WAN reports whether a is a router's internet-side address: traffic there
// is the LAN's traffic again, after NAT.
func (d *Directory) WAN(a netip.Addr) bool { return d != nil && d.wan[a.Unmap()] }

// Local reports whether a is on the local network: a private, link-local or
// unique local address, one of the devices' networks, or a device on the map.
func (d *Directory) Local(a netip.Addr) bool {
	a = a.Unmap()
	if a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLoopback() || a.IsMulticast() || a.IsUnspecified() ||
		a == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
		return true
	}
	if d == nil {
		return false
	}
	if d.wan[a] {
		return false
	}
	if _, ok := d.nodes[a]; ok {
		return true
	}
	for _, p := range d.networks {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// Name is the name client looked up to reach addr ("" when none).
func (d *Directory) Name(client, addr netip.Addr) string {
	if d == nil {
		return ""
	}
	return d.names.Name(client.Unmap(), addr.Unmap())
}

// WANAddrs are the routers' internet-side addresses (where the network is).
func (d *Directory) WANAddrs() []netip.Addr {
	if d == nil {
		return nil
	}
	out := make([]netip.Addr, 0, len(d.wan))
	for a := range d.wan {
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b netip.Addr) int { return a.Compare(b) })
	return out
}
