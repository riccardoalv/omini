package snmp

import (
	"net"
	"sort"
	"strings"

	"github.com/riccardoalv/omini/internal/model"
)

// CISCO-CDP-MIB cdpCacheTable: the neighbors Cisco Discovery Protocol saw,
// on Cisco devices and the ones that speak it too.
const (
	oidCdpCache       = "1.3.6.1.4.1.9.9.23.1.2.1.1" // cdpCacheEntry; index: ifIndex.deviceIndex
	cdpColAddressType = 3                            // ip(1), ipv6(20)
	cdpColAddress     = 4
	cdpColDeviceID    = 6
	cdpColDevicePort  = 7
	cdpColPlatform    = 8
	cdpColCaps        = 9 // a 32-bit map, router(0x01) ... phone(0x80)
)

// cdpCapabilities maps cdpCacheCapabilities bits to the model's.
var cdpCapabilities = []struct {
	bit uint32
	cap model.NeighborCapability
}{
	{0x01, model.NeighborCapabilityRouter},
	{0x02 | 0x04 | 0x08, model.NeighborCapabilityBridge}, // transparent, source-route bridge, switch
	{0x10, model.NeighborCapabilityStation},              // host
	{0x40, model.NeighborCapabilityRepeater},
	{0x80, model.NeighborCapabilityTelephone},
}

// collectCDP adds the CDP neighbors of the ports LLDP said nothing about.
func collectCDP(c *client, d *model.Device, ifNames map[string]string) error {
	rows, err := c.walk(oidCdpCache)
	if err != nil || len(rows) == 0 {
		return err
	}
	lldp := map[string]bool{}
	for _, nb := range d.Neighbors {
		lldp[nb.LocalPort] = true
	}
	t := table(rows)
	for idx := range t[cdpColDeviceID] {
		ifIdx, _, ok := strings.Cut(idx, ".")
		port := ifNames[ifIdx]
		if !ok || port == "" || lldp[port] {
			continue
		}
		nb := model.Neighbor{
			LocalPort:  port,
			Protocol:   model.Ptr(model.NeighborProtocolCdp),
			RemoteName: nonEmpty(strings.TrimSpace(pduString(t[cdpColDeviceID][idx]))),
			RemotePort: nonEmpty(strings.TrimSpace(pduString(t[cdpColDevicePort][idx]))),
		}
		if p := strings.TrimSpace(pduString(t[cdpColPlatform][idx])); p != "" {
			first, _, _ := strings.Cut(p, "\n")
			nb.RemotePlatform = model.Ptr(first)
		}
		addr := pduBytes(t[cdpColAddress][idx])
		switch typ, _ := pduInt(t[cdpColAddressType][idx]); {
		case typ == 1 && len(addr) == 4, typ == 20 && len(addr) == 16:
			nb.RemoteIP = model.Ptr(net.IP(addr).String())
		}
		if b := pduBytes(t[cdpColCaps][idx]); len(b) == 4 {
			bits := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
			for _, c := range cdpCapabilities {
				if bits&c.bit != 0 {
					nb.Capabilities = append(nb.Capabilities, c.cap)
				}
			}
		}
		d.Neighbors = append(d.Neighbors, nb)
	}
	sort.SliceStable(d.Neighbors, func(a, b int) bool { return d.Neighbors[a].LocalPort < d.Neighbors[b].LocalPort })
	return nil
}
