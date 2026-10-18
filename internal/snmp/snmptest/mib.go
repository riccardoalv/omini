package snmptest

import (
	"fmt"
	"net"
	"slices"
	"strings"

	"github.com/gosnmp/gosnmp"

	"github.com/riccardoalv/omini/internal/model"
)

// Options controls how a device is exposed by FromDevice.
type Options struct {
	// Enterprise is the IANA enterprise number used in sysObjectID (e.g. 14988 for MikroTik).
	Enterprise int
	// BridgePorts numbers the bridge ports (BRIDGE-MIB dot1dBasePort) by
	// interface name; interfaces left out are not bridge ports. Nil: every
	// interface is a bridge port numbered as its ifIndex.
	BridgePorts map[string]int
	// NoBridgePortTable leaves dot1dBasePortIfIndex out (agents that number
	// bridge ports as their ifIndex and do not say so).
	NoBridgePortTable bool
	// FdbIDs gives VLANs a filtering database id other than their VLAN id
	// (VLAN → dot1qVlanFdbId), exposed in dot1qVlanCurrentTable.
	FdbIDs map[uint16]int
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
	// bridgePort is an interface's bridge port (0: not one).
	bridgePort := func(name string) int {
		if opts.BridgePorts != nil {
			return opts.BridgePorts[name]
		}
		return ifIndex[name]
	}
	for i, iface := range d.Interfaces {
		idx := i + 1
		ifIndex[iface.Name] = idx
		add(gosnmp.Integer, idx, "1.3.6.1.2.1.2.2.1.1.%d", idx)
		add(gosnmp.OctetString, iface.Name, "1.3.6.1.2.1.2.2.1.2.%d", idx)
		add(gosnmp.Integer, ianaType(model.Deref(iface.Type)), "1.3.6.1.2.1.2.2.1.3.%d", idx)
		speed := model.Deref(iface.SpeedMbps) * 1_000_000
		add(gosnmp.Gauge32, uint32(min(speed, 4_294_967_295)), "1.3.6.1.2.1.2.2.1.5.%d", idx)
		add(gosnmp.OctetString, macBytes(model.Deref(iface.MAC)), "1.3.6.1.2.1.2.2.1.6.%d", idx)
		admin := 1
		if iface.Enabled != nil && !*iface.Enabled {
			admin = 2
		}
		add(gosnmp.Integer, admin, "1.3.6.1.2.1.2.2.1.7.%d", idx)
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
	}
	// EtherLike-MIB dot3StatsTable: the duplex of the ports that know it, in
	// rows numbered apart from the ifIndex (dot3StatsIndex names the port).
	for i, iface := range d.Interfaces {
		if iface.Duplex == nil {
			continue
		}
		status := 3 // fullDuplex
		if *iface.Duplex == model.InterfaceDuplexHalf {
			status = 2
		}
		add(gosnmp.Integer, ifIndex[iface.Name], "1.3.6.1.2.1.10.7.2.1.1.%d", 100+i)
		add(gosnmp.Integer, status, "1.3.6.1.2.1.10.7.2.1.19.%d", 100+i)
	}
	// BRIDGE-MIB dot1dBasePortIfIndex: bridge port -> ifIndex.
	if !opts.NoBridgePortTable {
		for _, iface := range d.Interfaces {
			if bp := bridgePort(iface.Name); bp > 0 {
				add(gosnmp.Integer, ifIndex[iface.Name], "1.3.6.1.2.1.17.1.4.1.2.%d", bp)
			}
		}
	}

	// IF-MIB ifStackTable: a link aggregation over its member ports, an
	// interface over its parent.
	for _, iface := range d.Interfaces {
		for _, m := range iface.Members {
			add(gosnmp.Integer, 1, "1.3.6.1.2.1.31.1.2.1.3.%d.%d", ifIndex[iface.Name], ifIndex[m])
		}
		if p := model.Deref(iface.Parent); p != "" {
			add(gosnmp.Integer, 1, "1.3.6.1.2.1.31.1.2.1.3.%d.%d", ifIndex[iface.Name], ifIndex[p])
		}
	}

	// IP-MIB addresses and ARP.
	// Interface addresses: IPv4 in ipAddrTable with its mask, IPv6 in
	// ipAddressTable pointing to its ipAddressPrefixTable row.
	onIface := map[string]bool{}
	for _, iface := range d.Interfaces {
		idx := ifIndex[iface.Name]
		for _, s := range iface.IPs {
			ip, prefix, err := net.ParseCIDR(s)
			if err != nil {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				onIface[v4.String()] = true
				add(gosnmp.Integer, idx, "1.3.6.1.2.1.4.20.1.2.%s", v4)
				add(gosnmp.IPAddress, net.IP(prefix.Mask).String(), "1.3.6.1.2.1.4.20.1.3.%s", v4)
				continue
			}
			bits, _ := prefix.Mask.Size()
			add(gosnmp.Integer, idx, "1.3.6.1.2.1.4.34.1.3.2.16.%s", bytesOID(ip))
			add(gosnmp.ObjectIdentifier, fmt.Sprintf(".1.3.6.1.2.1.4.32.1.5.%d.2.16.%s.%d", idx, bytesOID(prefix.IP), bits),
				"1.3.6.1.2.1.4.34.1.5.2.16.%s", bytesOID(ip))
		}
	}
	for _, ip := range d.IPs {
		if !onIface[ip] {
			add(gosnmp.Integer, 1, "1.3.6.1.2.1.4.20.1.2.%s", ip)
		}
	}
	for _, a := range d.Arp {
		idx := ifIndex[model.Deref(a.Interface)]
		add(gosnmp.OctetString, macBytes(a.MAC), "1.3.6.1.2.1.4.22.1.2.%d.%s", idx, a.IP)
		add(gosnmp.Integer, 3, "1.3.6.1.2.1.4.22.1.4.%d.%s", idx, a.IP) // dynamic
	}
	// IPv6 neighbors: ipNetToPhysicalPhysAddress, index ifIndex.2.16.addr.
	for _, a := range d.Ndp {
		if ip := net.ParseIP(a.IP); ip != nil && ip.To4() == nil {
			add(gosnmp.OctetString, macBytes(a.MAC), "1.3.6.1.2.1.4.35.1.4.%d.2.16.%s", ifIndex[model.Deref(a.Interface)], bytesOID(ip))
		}
	}

	// Q-BRIDGE-MIB forwarding table: index fdbId.mac, the fdbId being the
	// VLAN id unless Options.FdbIDs says otherwise (then dot1qVlanCurrentTable
	// maps each VLAN to its fdbId).
	for _, f := range d.Fdb {
		vlan := uint16(1)
		if f.Vlan != nil {
			vlan = *f.Vlan
		}
		fdbID := int(vlan)
		if id, ok := opts.FdbIDs[vlan]; ok {
			fdbID = id
		}
		idx := fmt.Sprintf("%d.%s", fdbID, macOID(f.MAC))
		add(gosnmp.Integer, bridgePort(f.Port), "1.3.6.1.2.1.17.7.1.2.2.1.2.%s", idx)
		add(gosnmp.Integer, 3, "1.3.6.1.2.1.17.7.1.2.2.1.3.%s", idx) // learned
	}
	for vlan, id := range opts.FdbIDs {
		add(gosnmp.Gauge32, uint32(id), "1.3.6.1.2.1.17.7.1.4.2.1.3.0.%d", vlan) //nolint:gosec // test data
	}

	// Q-BRIDGE-MIB VLANs: each VLAN's egress and untagged ports (bitmaps of
	// bridge ports) and each port's PVID.
	vlanIDs := map[uint16]string{}
	for _, v := range d.Vlans {
		vlanIDs[v.ID] = model.Deref(v.Name)
	}
	// BRIDGE-MIB spanning tree and IEEE8023-LAG-MIB aggregates.
	stpCodes := map[model.PortStpState]int{
		model.PortStpStateDisabled: 1, model.PortStpStateBlocking: 2, model.PortStpStateListening: 3,
		model.PortStpStateLearning: 4, model.PortStpStateForwarding: 5,
	}
	for _, iface := range d.Interfaces {
		if iface.Stp != nil && iface.Stp.State != nil {
			add(gosnmp.Integer, stpCodes[*iface.Stp.State], "1.3.6.1.2.1.17.2.15.1.3.%d", bridgePort(iface.Name))
		}
		if lag := model.Deref(iface.Lag); lag != "" {
			add(gosnmp.Integer, ifIndex[lag], "1.2.840.10006.300.43.1.2.1.1.13.%d", ifIndex[iface.Name])
		}
	}
	if root := model.Deref(d.StpRoot); root != "" {
		var prio int
		var mac string
		if _, err := fmt.Sscanf(root, "%d.%s", &prio, &mac); err == nil {
			if hw, err := net.ParseMAC(mac); err == nil {
				add(gosnmp.OctetString, append([]byte{byte(prio >> 8), byte(prio)}, hw...), "1.3.6.1.2.1.17.2.5.0")
			}
		}
	}
	for _, iface := range d.Interfaces {
		if iface.Vlans == nil {
			continue
		}
		if u := iface.Vlans.Untagged; u != nil && bridgePort(iface.Name) > 0 {
			if _, ok := vlanIDs[*u]; !ok {
				vlanIDs[*u] = ""
			}
			add(gosnmp.Gauge32, uint32(*u), "1.3.6.1.2.1.17.7.1.4.5.1.1.%d", bridgePort(iface.Name))
		}
		for _, t := range iface.Vlans.Tagged {
			if _, ok := vlanIDs[t]; !ok {
				vlanIDs[t] = ""
			}
		}
	}
	for id, name := range vlanIDs {
		var egress, untagged []byte
		for _, iface := range d.Interfaces {
			if iface.Vlans == nil {
				continue
			}
			bp := bridgePort(iface.Name)
			if bp < 1 {
				continue
			}
			isUntagged := model.Deref(iface.Vlans.Untagged) == id
			if isUntagged || slices.Contains(iface.Vlans.Tagged, id) {
				egress = setBit(egress, bp)
			}
			if isUntagged {
				untagged = setBit(untagged, bp)
			}
		}
		add(gosnmp.OctetString, name, "1.3.6.1.2.1.17.7.1.4.3.1.1.%d", id)
		add(gosnmp.OctetString, egress, "1.3.6.1.2.1.17.7.1.4.3.1.2.%d", id)
		add(gosnmp.OctetString, untagged, "1.3.6.1.2.1.17.7.1.4.3.1.4.%d", id)
		add(gosnmp.Integer, 1, "1.3.6.1.2.1.17.7.1.4.3.1.5.%d", id)
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
	// One hrProcessorLoad row per processor (CPUCount, else one), all at
	// CPUPct, indexed like Net-SNMP's (hrDeviceIndex 196608 on).
	if d.CPUPct != nil {
		for i := range max(model.Deref(d.CPUCount), 1) {
			add(gosnmp.Integer, int(*d.CPUPct), "1.3.6.1.2.1.25.3.3.1.2.%d", 196608+i)
		}
	}
	// hrStorageTable rows: type, description, allocation units, size, used.
	storage := func(idx int, typ, descr string, units, size, used uint64) {
		add(gosnmp.ObjectIdentifier, ".1.3.6.1.2.1.25.2.1."+typ, "1.3.6.1.2.1.25.2.3.1.2.%d", idx)
		add(gosnmp.OctetString, descr, "1.3.6.1.2.1.25.2.3.1.3.%d", idx)
		add(gosnmp.Integer, int(units), "1.3.6.1.2.1.25.2.3.1.4.%d", idx) //nolint:gosec // test data
		add(gosnmp.Integer, int(size), "1.3.6.1.2.1.25.2.3.1.5.%d", idx)  //nolint:gosec // test data
		add(gosnmp.Integer, int(used), "1.3.6.1.2.1.25.2.3.1.6.%d", idx)  //nolint:gosec // test data
	}
	// RAM in KiB units: the device's bytes, else 1000 units at MemPct.
	switch {
	case d.MemTotalBytes != nil:
		storage(1, "2", "Physical memory", 1024, *d.MemTotalBytes/1024, model.Deref(d.MemUsedBytes)/1024)
	case d.MemPct != nil:
		storage(1, "2", "Physical memory", 1024, 1000, uint64(*d.MemPct*10))
	}
	// Disks: fixed disks in 4 KiB units, indexed like Net-SNMP's (31 on).
	for i, s := range d.Storage {
		storage(31+i, "4", s.Mount, 4096, model.Deref(s.TotalBytes)/4096, model.Deref(s.UsedBytes)/4096)
	}
	return out
}

// setBit sets a bridge port in a Q-BRIDGE port list (port 1 = first byte's top bit).
func setBit(list []byte, port int) []byte {
	for len(list) <= (port-1)/8 {
		list = append(list, 0)
	}
	list[(port-1)/8] |= 0x80 >> ((port - 1) % 8)
	return list
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

func macOID(m model.MACAddress) string { return bytesOID(macBytes(m)) }

// bytesOID writes bytes as OID arcs (an index of a MAC or an address).
func bytesOID(b []byte) string {
	parts := make([]string, 0, len(b))
	for _, x := range b {
		parts = append(parts, fmt.Sprint(x))
	}
	return strings.Join(parts, ".")
}
