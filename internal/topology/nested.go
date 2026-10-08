package topology

import (
	"net/netip"

	"github.com/riccardoalv/omini/internal/model"
)

// lanNets are the networks of every managed device's non-WAN interfaces,
// by device: where a nested router's "WAN" may sit.
func (b *builder) lanNets() map[string][]netip.Prefix {
	out := map[string][]netip.Prefix{}
	for id, srcs := range b.sources {
		for _, d := range srcs {
			for _, i := range d.Interfaces {
				if model.Deref(i.Wan) {
					continue
				}
				for _, a := range i.IPs {
					if p, err := netip.ParsePrefix(a); err == nil && p.Bits() >= 8 && p.Bits() < 32 {
						out[id] = append(out[id], p.Masked())
					}
				}
			}
		}
	}
	return out
}

// nestedWAN reports whether a router's WAN interface is in fact a link into
// another managed device's LAN (double NAT: a lab router, an ISP router
// behind the firewall): its address or its gateway is in that device's
// network. Such a router is not the center of the network: no WAN node, its
// uplink is placed like any device's.
func (b *builder) nestedWAN(id string, i model.Interface, gateways []model.Gateway, nets map[string][]netip.Prefix) bool {
	var addrs []netip.Addr
	for _, a := range i.IPs {
		if p, err := netip.ParsePrefix(a); err == nil {
			addrs = append(addrs, p.Addr())
		} else if ip, err := netip.ParseAddr(a); err == nil {
			addrs = append(addrs, ip)
		}
	}
	for _, g := range gateways {
		if model.Deref(g.Interface) != i.Name {
			continue
		}
		if ip, err := netip.ParseAddr(model.Deref(g.Address)); err == nil {
			addrs = append(addrs, ip)
		}
	}
	for other, prefixes := range nets {
		if other == id {
			continue
		}
		for _, p := range prefixes {
			for _, a := range addrs {
				if p.Contains(a) {
					return true
				}
			}
		}
	}
	return false
}

// centers are the routers and firewalls with a real internet uplink (a WAN
// that is not a link into another device's LAN): the center of the network.
func (b *builder) centers() map[string]bool {
	nets := b.lanNets()
	out := map[string]bool{}
	for id, srcs := range b.sources {
		for _, d := range srcs {
			for _, i := range d.Interfaces {
				if model.Deref(i.Wan) && !b.nestedWAN(id, i, d.Gateways, nets) {
					out[id] = true
				}
			}
		}
	}
	return out
}
