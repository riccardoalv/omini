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

// lagUplinks: a switch learns MACs on its port-channel (Trk1, Po11) while
// LLDP names the member ports (49, 50): the port-channel is the uplink too,
// so what is learned on it is behind the device at the other end.
func (b *builder) lagUplinks() {
	for id, srcs := range b.sources {
		for _, d := range srcs {
			for _, i := range d.Interfaces {
				if len(i.Members) == 0 {
					continue
				}
				if _, ok := b.uplinks[portKey{id, i.Name}]; ok {
					continue
				}
				for _, m := range i.Members {
					if peer, ok := b.uplinks[portKey{id, m}]; ok {
						b.uplinks[portKey{id, i.Name}] = peer
						break
					}
				}
			}
		}
	}
}

// addVPNLinks joins a router to another site's router over a site-to-site
// VPN: a peer whose endpoint is one of that router's addresses, or whose
// network holds its address. The other site keeps its own internet as its
// root; the tunnel is a link between the two (drawn dashed).
func (b *builder) addVPNLinks(managed []string) {
	addrs := map[string][]netip.Addr{}
	for _, id := range managed {
		for _, d := range b.sources[id] {
			for _, s := range append(append([]string{model.Deref(d.Host)}, d.IPs...), ifaceIPs(d)...) {
				if p, err := netip.ParsePrefix(s); err == nil {
					addrs[id] = append(addrs[id], p.Addr())
				} else if a, err := netip.ParseAddr(s); err == nil {
					addrs[id] = append(addrs[id], a)
				}
			}
		}
	}
	for _, id := range managed {
		for _, d := range b.sources[id] {
			for _, peer := range d.VpnPeers {
				endpoint, _ := netip.ParseAddr(model.Deref(peer.Endpoint))
				network, netErr := netip.ParsePrefix(model.Deref(peer.Address))
				for _, other := range managed {
					if other == id {
						continue
					}
					for _, a := range addrs[other] {
						if (endpoint.IsValid() && a == endpoint) || (netErr == nil && network.Bits() < 32 && network.Contains(a)) {
							b.addEdge(id, "", other, "", EdgeVPN, 0)
							goto next
						}
					}
				}
			next:
			}
		}
	}
}

func ifaceIPs(d *model.Device) []string {
	var out []string
	for _, i := range d.Interfaces {
		out = append(out, i.IPs...)
	}
	return out
}
