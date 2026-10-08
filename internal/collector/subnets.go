package collector

import (
	"net/netip"
	"sort"

	"github.com/riccardoalv/omini/internal/model"
)

// minLearnedBits: larger subnets (more than /22, 1024 addresses) are not
// scanned from what routers report.
const minLearnedBits = 22

// knownSubnets are the private IPv4 subnets the other integrations' devices
// report: the LAN interfaces and VLANs of routers and firewalls (WANs aside).
func (c *Collector) knownSubnets(except int64) []netip.Prefix {
	c.mu.RLock()
	var devices []model.Device
	for id, s := range c.snaps {
		if id != except {
			devices = append(devices, s.Devices...)
		}
	}
	c.mu.RUnlock()
	return subnetsOf(devices)
}

func subnetsOf(devices []model.Device) []netip.Prefix {
	seen := map[netip.Prefix]bool{}
	add := func(s string) {
		p, err := netip.ParsePrefix(s)
		if err != nil || !p.Addr().Is4() || !p.Addr().IsPrivate() || p.Bits() < minLearnedBits || p.Bits() > 30 {
			return
		}
		seen[p.Masked()] = true
	}
	for _, d := range devices {
		for _, i := range d.Interfaces {
			if model.Deref(i.Wan) {
				continue
			}
			for _, ip := range i.IPs {
				add(ip)
			}
		}
		for _, v := range d.Vlans {
			add(model.Deref(v.Subnet))
		}
	}
	out := make([]netip.Prefix, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Addr().Less(out[j].Addr()) || (out[i].Addr() == out[j].Addr() && out[i].Bits() < out[j].Bits())
	})
	return out
}
