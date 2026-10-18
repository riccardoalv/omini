package topology_test

import (
	"slices"
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

const routerMAC = "00:e0:4c:68:00:02"

// router6 is a router that sees its LAN over IPv4 (ARP) and IPv6 (NDP).
func router6() model.Device {
	return model.Device{
		Key: routerMAC, Name: "router", Role: model.Ptr(model.DeviceRoleRouter), MACs: []model.MACAddress{routerMAC},
		Interfaces: []model.Interface{{
			Name: "igc1", MAC: mac(routerMAC),
			IPs: []string{"192.168.1.1/24", "2001:db8:1::1/64", "fe80::1/64"},
		}},
		Arp: []model.ArpEntry{{IP: "192.168.1.20", MAC: "00:11:32:aa:00:01", Interface: model.Ptr("igc1")}},
		Ndp: []model.ArpEntry{
			{IP: "fe80::211:32ff:feaa:1%igc1", MAC: "00:11:32:aa:00:01", Interface: model.Ptr("igc1")},
			{IP: "2001:0db8:0001:0000:0000:0000:0000:0020", MAC: "00:11:32:aa:00:01", Interface: model.Ptr("igc1")},
			// A phone on IPv6 alone (its IPv4 lease expired, ARP forgot it).
			{IP: "2001:db8:1::99", MAC: "f4:f5:d8:00:00:06", Interface: model.Ptr("igc1")},
			{IP: "fe80::1", MAC: "f4:f5:d8:00:00:06", Interface: model.Ptr("igc1")},
		},
	}
}

func build6(devices ...model.Device) map[string]topology.Node {
	return byID(topology.Build([]topology.Source{{IntegrationID: 1, Online: true, Devices: devices}}))
}

// NDP ties IPv6 addresses to a MAC like ARP does for IPv4: the node carries
// them, compact, global first and link-local last.
func TestNDPGivesANodeItsIPv6Addresses(t *testing.T) {
	nodes := build6(router6())
	nas := nodes["mac:00:11:32:aa:00:01"]
	if nas.IP != "192.168.1.20" || !slices.Equal(nas.IPs6, []string{"2001:db8:1::20", "fe80::211:32ff:feaa:1"}) {
		t.Fatalf("NAS = %q %v", nas.IP, nas.IPs6)
	}
	r := nodes["dev:"+routerMAC]
	if !slices.Equal(r.IPs6, []string{"2001:db8:1::1", "fe80::1"}) {
		t.Fatalf("router's own IPv6 = %v", r.IPs6)
	}
}

// A device seen in NDP alone is present, under the router like one in ARP,
// and keeps no IPv4 address it does not have.
func TestNDPCountsForPresence(t *testing.T) {
	nodes := build6(router6())
	phone, ok := nodes["mac:f4:f5:d8:00:00:06"]
	if !ok {
		t.Fatal("a device in NDP alone is not on the map")
	}
	if phone.ParentID != "dev:"+routerMAC || phone.Port != "igc1" || phone.IP != "" ||
		!slices.Equal(phone.IPs6, []string{"2001:db8:1::99", "fe80::1"}) {
		t.Fatalf("phone = %+v", phone)
	}
}

// One machine, one node: a device reported without a MAC takes the one its
// global IPv6 address has in NDP; a link-local address (the same fe80::1 is
// on many machines) never ties two of them.
func TestGlobalIPv6MergesLinkLocalNever(t *testing.T) {
	global := model.Device{Key: "nas", Name: "nas", Host: model.Ptr("2001:db8:1::20")}
	local := model.Device{Key: "tv", Name: "tv", Host: model.Ptr("fe80::1")}
	nodes := build6(router6(), global, local)
	if _, ok := nodes["mac:00:11:32:aa:00:01"]; ok {
		t.Fatal("the NAS is two nodes: its global IPv6 address did not merge it")
	}
	nas := nodes["dev:1:nas"]
	if nas.MAC != "00:11:32:aa:00:01" || !slices.Contains(nas.IPs6, "fe80::211:32ff:feaa:1") {
		t.Fatalf("NAS = %+v", nas)
	}
	if tv := nodes["dev:1:tv"]; tv.MAC != "" {
		t.Fatalf("merged by a link-local address: %+v", tv)
	}
	if _, ok := nodes["mac:f4:f5:d8:00:00:06"]; !ok {
		t.Fatal("the phone whose link-local address the TV shares is gone")
	}

	// Two integrations reporting a machine by the same link-local address
	// alone are two machines; by the same global one, one.
	a := model.Device{Key: "a", Name: "a", Host: model.Ptr("fe80::5")}
	b := model.Device{Key: "b", Name: "b", Host: model.Ptr("fe80::5")}
	c := model.Device{Key: "c", Name: "c", Host: model.Ptr("2001:db8:1::5")}
	d := model.Device{Key: "d", Name: "d", Host: model.Ptr("2001:db8:1::5")}
	topo := topology.Build([]topology.Source{
		{IntegrationID: 1, Online: true, Devices: []model.Device{a, c}},
		{IntegrationID: 2, Online: true, Devices: []model.Device{b, d}},
	})
	got := byID(topo)
	if _, ok := got["dev:2:b"]; !ok {
		t.Error("merged by a link-local address")
	}
	if _, ok := got["dev:2:d"]; ok {
		t.Error("not merged by a global IPv6 address")
	}
}

// A host a scan saw by its IPv6 address alone is the node NDP gives that
// address to, not another one.
func TestScanByIPv6EnrichesTheNDPClient(t *testing.T) {
	scan := model.Device{
		Key: "scan", Name: "scan",
		Hosts: []model.Host{{IP: "2001:db8:1::99", Hostnames: []string{"phone.home.lan"}, OpenPorts: []uint16{62078}}},
	}
	nodes := build6(router6(), scan)
	if _, ok := nodes["ip:2001:db8:1::99"]; ok {
		t.Fatal("the scanned IPv6 host is a second node")
	}
	if p := nodes["mac:f4:f5:d8:00:00:06"]; p.Hostname != "phone.home.lan" || !slices.Equal(p.OpenPorts, []int{62078}) {
		t.Fatalf("phone = %+v", p)
	}
}

// A DHCPv6 lease gives its address to the node and its name names it, like
// an IPv4 lease; it never takes the IPv4 address's place, and alone it makes
// nothing present.
func TestDHCPv6Leases(t *testing.T) {
	r := router6()
	r.DhcpLeases = []model.DhcpLease{
		{IP: "2001:db8:1::abc", MAC: "f4:f5:d8:00:00:06", Hostname: model.Ptr("galaxy-s23")},
		{IP: "192.168.1.20", MAC: "00:11:32:aa:00:01", Hostname: model.Ptr("nas")},
		{IP: "2001:db8:1::21", MAC: "00:11:32:aa:00:01", Hostname: model.Ptr("nas-v6")},
		{IP: "2001:db8:1::77", MAC: "00:24:e4:99:00:01", Hostname: model.Ptr("gone")},
	}
	nodes := build6(r)
	phone := nodes["mac:f4:f5:d8:00:00:06"]
	if phone.Label != "galaxy-s23" || phone.IP != "" || !slices.Equal(phone.IPs6, []string{"2001:db8:1::99", "2001:db8:1::abc", "fe80::1"}) {
		t.Fatalf("phone = %q %q %v", phone.Label, phone.IP, phone.IPs6)
	}
	nas := nodes["mac:00:11:32:aa:00:01"]
	if nas.Label != "nas" || nas.IP != "192.168.1.20" || !slices.Contains(nas.IPs6, "2001:db8:1::21") {
		t.Fatalf("NAS = %q %q %v", nas.Label, nas.IP, nas.IPs6)
	}
	if _, ok := nodes["mac:00:24:e4:99:00:01"]; ok {
		t.Fatal("a DHCPv6 lease alone made a device present")
	}
}

// An IPv6 default gateway is the uplink's gateway like an IPv4 one: on the
// WAN node with its status, and the uplink is down only when every gateway
// is. Its link-local address (as most are) does not make the router look
// nested in another device's LAN.
func TestIPv6GatewaysAreWANGateways(t *testing.T) {
	up, down := model.GatewayStatusUp, model.GatewayStatusDown
	fw := model.Device{
		Key: "58:9c:fc:00:00:01", Name: "fw", Role: model.Ptr(model.DeviceRoleFirewall),
		MACs: []model.MACAddress{"58:9c:fc:00:00:01"},
		Interfaces: []model.Interface{
			{Name: "pppoe0", Up: model.Ptr(true), Wan: model.Ptr(true), IPs: []string{"100.64.0.20/32", "2001:db8:ffff::2/64"}},
			{Name: "lte0", Up: model.Ptr(true), Wan: model.Ptr(true), IPs: []string{"2001:db8:eeee::2/64"}},
			{Name: "igc1", IPs: []string{"192.168.1.1/24", "2001:db8:1::1/64", "fe80::1/64"}},
		},
		Gateways: []model.Gateway{
			{Name: "WAN_GW", Interface: model.Ptr("pppoe0"), Address: model.Ptr("100.64.0.1"), Status: up},
			{Name: "WAN_GWv6", Interface: model.Ptr("pppoe0"), Address: model.Ptr("fe80::1%pppoe0"), Status: down, RttMs: model.Ptr(9.5), LossPct: model.Ptr(100.0)},
			{Name: "LTE_GWv6", Interface: model.Ptr("lte0"), Address: model.Ptr("fe80::1%lte0"), Status: down},
		},
	}
	// A switch on the LAN with the same link-local address as the gateways.
	sw := model.Device{
		Key: "1c:2a:a3:00:00:01", Name: "sw", Role: model.Ptr(model.DeviceRoleSwitch), MACs: []model.MACAddress{"1c:2a:a3:00:00:01"},
		Interfaces: []model.Interface{{Name: "vlan1", IPs: []string{"192.168.1.2/24", "fe80::1/64", "2001:db8:1::2/64"}}},
	}
	nodes := build6(fw, sw)
	wan := nodes["wan:dev:58:9c:fc:00:00:01:pppoe0"]
	if wan.WAN == nil || len(wan.WAN.Gateways) != 2 || !wan.Online {
		t.Fatalf("dual-stack WAN = %+v %+v", wan, wan.WAN)
	}
	if g := wan.WAN.Gateways[1]; model.Deref(g.Address) != "fe80::1%pppoe0" || model.Deref(g.RttMs) != 9.5 {
		t.Fatalf("IPv6 gateway = %+v", g)
	}
	if lte, ok := nodes["wan:dev:58:9c:fc:00:00:01:lte0"]; !ok || lte.Online {
		t.Fatalf("IPv6-only WAN with its gateway down = %+v (present %v)", lte, ok)
	}
}

// A router whose WAN has an address in another router's IPv6 LAN is behind
// it (double NAT, or a routed lab), not the center.
func TestNestedRouterByIPv6(t *testing.T) {
	fw := model.Device{
		Key: "58:9c:fc:00:00:01", Name: "fw", Role: model.Ptr(model.DeviceRoleFirewall), MACs: []model.MACAddress{"58:9c:fc:00:00:01"},
		Interfaces: []model.Interface{
			{Name: "wan", Wan: model.Ptr(true), IPs: []string{"2001:db8:ffff::2/64"}},
			{Name: "lan", IPs: []string{"2001:db8:1::1/64", "fe80::1/64"}},
		},
	}
	lab := model.Device{
		Key: "bc:24:11:00:00:99", Name: "lab", Role: model.Ptr(model.DeviceRoleRouter), MACs: []model.MACAddress{"bc:24:11:00:00:99"},
		Interfaces: []model.Interface{
			{Name: "wan", Wan: model.Ptr(true), IPs: []string{"2001:db8:1::99/64"}},
			{Name: "lan", IPs: []string{"2001:db8:99::1/64"}},
		},
	}
	nodes := build6(fw, lab)
	if _, ok := nodes["wan:dev:bc:24:11:00:00:99:wan"]; ok {
		t.Fatal("the lab router behind the firewall's IPv6 LAN is a center")
	}
	if _, ok := nodes["wan:dev:58:9c:fc:00:00:01:wan"]; !ok {
		t.Fatal("the firewall lost its WAN")
	}
}
