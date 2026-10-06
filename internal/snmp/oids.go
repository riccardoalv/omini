package snmp

// Standard MIB objects read by the generic SNMP integration.
const (
	// SNMPv2-MIB
	oidSysDescr    = "1.3.6.1.2.1.1.1.0"
	oidSysObjectID = "1.3.6.1.2.1.1.2.0"
	oidSysUpTime   = "1.3.6.1.2.1.1.3.0"
	oidSysName     = "1.3.6.1.2.1.1.5.0"
	oidSysServices = "1.3.6.1.2.1.1.7.0"

	// IP-MIB
	oidIPForwarding    = "1.3.6.1.2.1.4.1.0"
	oidIPAddrIfIndex   = "1.3.6.1.2.1.4.20.1.2" // index: IPv4 address
	oidIPNetToMedia    = "1.3.6.1.2.1.4.22.1"   // ARP: .2 phys address, .4 type; index ifIndex.a.b.c.d
	oidIPNetToPhysical = "1.3.6.1.2.1.4.35.1.4" // ARP (newer): index ifIndex.addrType.len.addr...
	oidIfTable         = "1.3.6.1.2.1.2.2.1"    // IF-MIB ifEntry
	oidIfXTable        = "1.3.6.1.2.1.31.1.1.1" // IF-MIB ifXEntry

	// BRIDGE-MIB / Q-BRIDGE-MIB
	oidBasePortIfIndex = "1.3.6.1.2.1.17.1.4.1.2"   // bridge port -> ifIndex
	oidDot1dTpFdb      = "1.3.6.1.2.1.17.4.3.1"     // .2 port, .3 status; index: mac
	oidDot1qTpFdb      = "1.3.6.1.2.1.17.7.1.2.2.1" // .2 port, .3 status; index: fdbId.mac

	// LLDP-MIB (802.1AB-2005)
	oidLldpLocChassisIDSubtype = "1.0.8802.1.1.2.1.3.1.0"
	oidLldpLocChassisID        = "1.0.8802.1.1.2.1.3.2.0"
	oidLldpLocPort             = "1.0.8802.1.1.2.1.3.7.1"   // .2 id subtype, .3 id, .4 desc; index: localPortNum
	oidLldpRem                 = "1.0.8802.1.1.2.1.4.1.1"   // index: timeMark.localPortNum.remIndex
	oidLldpRemManAddr          = "1.0.8802.1.1.2.1.4.2.1.3" // index: timeMark.localPortNum.remIndex.subtype.len.addr

	// HOST-RESOURCES-MIB
	oidHrProcessorLoad = "1.3.6.1.2.1.25.3.3.1.2"
	oidHrStorage       = "1.3.6.1.2.1.25.2.3.1" // .2 type, .4 alloc units, .5 size, .6 used
	oidHrStorageRAM    = ".1.3.6.1.2.1.25.2.1.2"
)

// LLDP chassis/port id subtypes we care about.
const (
	chassisSubtypeMAC     = 4
	chassisSubtypeNetAddr = 5
	portSubtypeMAC        = 3
	portSubtypeIfName     = 5
	portSubtypeLocal      = 7
)

// enterprises maps IANA private enterprise numbers (from sysObjectID) to vendors.
var enterprises = map[int]string{
	9:     "Cisco",
	11:    "HP",
	171:   "D-Link",
	311:   "Microsoft",
	674:   "Dell",
	1916:  "Extreme Networks",
	2011:  "Huawei",
	2636:  "Juniper",
	3955:  "Linksys",
	4413:  "Broadcom",
	4526:  "Netgear",
	6527:  "Nokia",
	6876:  "VMware",
	8072:  "Net-SNMP",
	10002: "Ubiquiti",
	11863: "TP-Link",
	12325: "FreeBSD",
	12356: "Fortinet",
	14823: "Aruba",
	14988: "MikroTik",
	25053: "Ruckus",
	25506: "H3C",
	30065: "Arista",
	41112: "Ubiquiti",
}

// IANAifType values mapped to the model's interface types.
var ifTypes = map[int64]string{
	6:   "ethernet", // ethernetCsmacd
	62:  "ethernet", // fastEther
	117: "ethernet", // gigabitEthernet
	71:  "wireless", // ieee80211
	24:  "loopback", // softwareLoopback
	209: "bridge",
	135: "vlan", // l2vlan
	136: "vlan", // l3ipvlan
	161: "lag",  // ieee8023adLag
	131: "tunnel",
}
