package snmptest

import (
	"fmt"
	"net"
	"strings"

	"github.com/gosnmp/gosnmp"

	"github.com/riccardoalv/omini/internal/model"
)

// Options controls how a device is exposed by FromDevice.
type Options struct {
	// Enterprise is the IANA enterprise number used in sysObjectID (e.g. 14988 for MikroTik).
	Enterprise int
}

// FromDevice renders a model device as the standard MIB tables a real agent
// would expose (SNMPv2-MIB, IF-MIB, IP-MIB, Q-BRIDGE-MIB, LLDP-MIB,
// HOST-RESOURCES-MIB). Wireless clients and DHCP leases have no standard MIB
// and are not rendered.
func FromDevice(d model.Device, opts Options) []gosnmp.SnmpPDU {
	var out []gosnmp.SnmpPDU
	add := func(typ gosnmp.Asn1BER, value any, format string, args ...any) {
		out = append(out, gosnmp.SnmpPDU{Name: "." + fmt.Sprintf(format, args...), Type: typ, Value: value})
	}
	role := model.Deref(d.Role)
	routed := role == model.DeviceRoleRouter || role == model.DeviceRoleFirewall

	// SNMPv2-MIB system group.
	add(gosnmp.OctetString, model.Deref(d.Model), "1.3.6.1.2.1.1.1.0")
	add(gosnmp.ObjectIdentifier, fmt.Sprintf(".1.3.6.1.4.1.%d.1", opts.Enterprise), "1.3.6.1.2.1.1.2.0")
	add(gosnmp.TimeTicks, uint32(model.Deref(d.UptimeS)*100), "1.3.6.1.2.1.1.3.0")
	add(gosnmp.OctetString, d.Name, "1.3.6.1.2.1.1.5.0")
	services := 2 // datalink (bridging)
	forwarding := 2
	if routed {
		services, forwarding = 4|8|64, 1
	}
	add(gosnmp.Integer, services, "1.3.6.1.2.1.1.7.0")
	add(gosnmp.Integer, forwarding, "1.3.6.1.2.1.4.1.0")

	// IF-MIB: ifIndex = position + 1.
	ifIndex := map[string]int{}
	for i, iface := range d.Interfaces {
		idx := i + 1
		ifIndex[iface.Name] = idx
		add(gosnmp.Integer, idx, "1.3.6.1.2.1.2.2.1.1.%d", idx)
		add(gosnmp.OctetString, iface.Name, "1.3.6.1.2.1.2.2.1.2.%d", idx)
		add(gosnmp.Integer, ianaType(model.Deref(iface.Type)), "1.3.6.1.2.1.2.2.1.3.%d", idx)
		speed := model.Deref(iface.SpeedMbps) * 1_000_000
		add(gosnmp.Gauge32, uint32(min(speed, 4_294_967_295)), "1.3.6.1.2.1.2.2.1.5.%d", idx)
		add(gosnmp.OctetString, macBytes(model.Deref(iface.MAC)), "1.3.6.1.2.1.2.2.1.6.%d", idx)
		add(gosnmp.Integer, 1, "1.3.6.1.2.1.2.2.1.7.%d", idx)
		oper := 2
		if model.Deref(iface.Up) {
			oper = 1
		}
		add(gosnmp.Integer, oper, "1.3.6.1.2.1.2.2.1.8.%d", idx)
		add(gosnmp.Counter32, uint32(model.Deref(iface.RxBytes)), "1.3.6.1.2.1.2.2.1.10.%d", idx)
		add(gosnmp.Counter32, uint32(model.Deref(iface.RxErrors)), "1.3.6.1.2.1.2.2.1.14.%d", idx)
		add(gosnmp.Counter32, uint32(model.Deref(iface.TxBytes)), "1.3.6.1.2.1.2.2.1.16.%d", idx)
		add(gosnmp.Counter32, uint32(model.Deref(iface.TxErrors)), "1.3.6.1.2.1.2.2.1.20.%d", idx)
		add(gosnmp.OctetString, iface.Name, "1.3.6.1.2.1.31.1.1.1.1.%d", idx)
		add(gosnmp.Counter64, model.Deref(iface.RxBytes), "1.3.6.1.2.1.31.1.1.1.6.%d", idx)
		add(gosnmp.Counter64, model.Deref(iface.TxBytes), "1.3.6.1.2.1.31.1.1.1.10.%d", idx)
		add(gosnmp.Gauge32, uint32(model.Deref(iface.SpeedMbps)), "1.3.6.1.2.1.31.1.1.1.15.%d", idx)
		add(gosnmp.OctetString, model.Deref(iface.Description), "1.3.6.1.2.1.31.1.1.1.18.%d", idx)
		// BRIDGE-MIB: bridge port number == ifIndex.
		add(gosnmp.Integer, idx, "1.3.6.1.2.1.17.1.4.1.2.%d", idx)
	}

	// IF-MIB ifStackTable: a link aggregation over its member ports.
	for _, iface := range d.Interfaces {
		for _, m := range iface.Members {
			add(gosnmp.Integer, 1, "1.3.6.1.2.1.31.1.2.1.3.%d.%d", ifIndex[iface.Name], ifIndex[m])
		}
	}

	// IP-MIB addresses and ARP.
	for _, ip := range d.IPs {
		add(gosnmp.Integer, 1, "1.3.6.1.2.1.4.20.1.2.%s", ip)
	}
	for _, a := range d.Arp {
		idx := ifIndex[model.Deref(a.Interface)]
		add(gosnmp.OctetString, macBytes(a.MAC), "1.3.6.1.2.1.4.22.1.2.%d.%s", idx, a.IP)
		add(gosnmp.Integer, 3, "1.3.6.1.2.1.4.22.1.4.%d.%s", idx, a.IP) // dynamic
	}

	// Q-BRIDGE-MIB forwarding table: index fdbId(VLAN).mac.
	for _, f := range d.Fdb {
		vlan := uint16(1)
		if f.Vlan != nil {
			vlan = *f.Vlan
		}
		idx := fmt.Sprintf("%d.%s", vlan, macOID(f.MAC))
		add(gosnmp.Integer, ifIndex[f.Port], "1.3.6.1.2.1.17.7.1.2.2.1.2.%s", idx)
		add(gosnmp.Integer, 3, "1.3.6.1.2.1.17.7.1.2.2.1.3.%s", idx) // learned
	}

	// LLDP-MIB: local chassis/ports and remote systems.
	if mac := model.NormMAC(d.Key); mac != "" {
		add(gosnmp.Integer, 4, "1.0.8802.1.1.2.1.3.1.0")
		add(gosnmp.OctetString, macBytes(mac), "1.0.8802.1.1.2.1.3.2.0")
	}
	for _, iface := range d.Interfaces {
		idx := ifIndex[iface.Name]
		add(gosnmp.Integer, 5, "1.0.8802.1.1.2.1.3.7.1.2.%d", idx) // interfaceName
		add(gosnmp.OctetString, iface.Name, "1.0.8802.1.1.2.1.3.7.1.3.%d", idx)
		add(gosnmp.OctetString, iface.Name, "1.0.8802.1.1.2.1.3.7.1.4.%d", idx)
	}
	for i, nb := range d.Neighbors {
		idx := fmt.Sprintf("0.%d.%d", ifIndex[nb.LocalPort], i+1)
		if nb.RemoteMAC != nil {
			add(gosnmp.Integer, 4, "1.0.8802.1.1.2.1.4.1.1.4.%s", idx)
			add(gosnmp.OctetString, macBytes(*nb.RemoteMAC), "1.0.8802.1.1.2.1.4.1.1.5.%s", idx)
		}
		add(gosnmp.Integer, 5, "1.0.8802.1.1.2.1.4.1.1.6.%s", idx)
		add(gosnmp.OctetString, model.Deref(nb.RemotePort), "1.0.8802.1.1.2.1.4.1.1.7.%s", idx)
		add(gosnmp.OctetString, model.Deref(nb.RemoteName), "1.0.8802.1.1.2.1.4.1.1.9.%s", idx)
		add(gosnmp.OctetString, model.Deref(nb.RemotePlatform), "1.0.8802.1.1.2.1.4.1.1.10.%s", idx)
		if len(nb.Capabilities) > 0 {
			add(gosnmp.OctetString, capBits(nb.Capabilities), "1.0.8802.1.1.2.1.4.1.1.12.%s", idx)
		}
		if ip := net.ParseIP(model.Deref(nb.RemoteIP)).To4(); ip != nil {
			add(gosnmp.Integer, 2, "1.0.8802.1.1.2.1.4.2.1.3.%s.1.4.%d.%d.%d.%d", idx, ip[0], ip[1], ip[2], ip[3])
		}
	}

	// HOST-RESOURCES-MIB: CPU load and RAM usage.
	if d.CPUPct != nil {
		add(gosnmp.Integer, int(*d.CPUPct), "1.3.6.1.2.1.25.3.3.1.2.1")
	}
	if d.MemPct != nil {
		add(gosnmp.ObjectIdentifier, ".1.3.6.1.2.1.25.2.1.2", "1.3.6.1.2.1.25.2.3.1.2.1")
		add(gosnmp.Integer, 1024, "1.3.6.1.2.1.25.2.3.1.4.1")
		add(gosnmp.Integer, 1000, "1.3.6.1.2.1.25.2.3.1.5.1")
		add(gosnmp.Integer, int(*d.MemPct*10), "1.3.6.1.2.1.25.2.3.1.6.1")
	}
	return out
}

// capBits encodes LLDP capabilities as BITS (other = the first byte's top bit).
func capBits(caps []model.NeighborCapability) []byte {
	order := []model.NeighborCapability{"other", "repeater", "bridge", "ap", "router", "telephone", "docsis", "station"}
	b := []byte{0, 0}
	for i, c := range order {
		for _, x := range caps {
			if x == c {
				b[i/8] |= 0x80 >> (i % 8)
			}
		}
	}
	return b
}

func ianaType(t model.InterfaceType) int {
	switch t {
	case model.InterfaceTypeEthernet:
		return 6
	case model.InterfaceTypeWireless:
		return 71
	case model.InterfaceTypeLoopback:
		return 24
	case model.InterfaceTypeBridge:
		return 209
	case model.InterfaceTypeVlan:
		return 135
	case model.InterfaceTypeLag:
		return 161
	case model.InterfaceTypeTunnel:
		return 131
	}
	return 1 // other
}

func macBytes(m model.MACAddress) []byte {
	if m == "" {
		return []byte{}
	}
	hw, err := net.ParseMAC(string(m))
	if err != nil {
		return []byte{}
	}
	return hw
}

func macOID(m model.MACAddress) string {
	parts := make([]string, 0, 6)
	for _, b := range macBytes(m) {
		parts = append(parts, fmt.Sprint(b))
	}
	return strings.Join(parts, ".")
}
