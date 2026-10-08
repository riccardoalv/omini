package topology_test

import (
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

func lldp(port string, mac model.MACAddress, name, platform string, caps ...model.NeighborCapability) model.Neighbor {
	nb := model.Neighbor{LocalPort: port, Protocol: model.Ptr(model.NeighborProtocolLldp), RemoteName: model.Ptr(name), Capabilities: caps}
	if mac != "" {
		nb.RemoteMAC = &mac
	}
	if platform != "" {
		nb.RemotePlatform = model.Ptr(platform)
	}
	return nb
}

// A desk phone on port 7 with a PC behind it: the phone is a client (an IP
// phone), its port is not an uplink, and the PC hangs from the phone.
func TestDeskPhoneWithAPCBehindIt(t *testing.T) {
	const phone, pc, other = "24:9a:d8:2a:eb:3a", "b0:7b:25:18:1b:6d", "b0:7b:25:00:00:09"
	sw := model.Device{
		Key: "1c:00:00:00:00:01", Name: "sw", MACs: []model.MACAddress{"1c:00:00:00:00:01"}, Role: model.Ptr(model.DeviceRoleSwitch),
		Neighbors: []model.Neighbor{lldp("7", phone, "SEP249AD82AEB3A", "SIP-T54W 96.86.0.70",
			model.NeighborCapabilityBridge, model.NeighborCapabilityTelephone)},
		Fdb: []model.FdbEntry{{MAC: phone, Port: "7"}, {MAC: pc, Port: "7"}, {MAC: other, Port: "9"}},
	}
	nodes := byID(topology.Build([]topology.Source{{IntegrationID: 1, Online: true, Devices: []model.Device{sw}}}))
	p := nodes["ext:"+phone]
	if p.Kind != topology.KindClient || p.Type != "ip_phone" {
		t.Fatalf("phone: %+v", p)
	}
	if c := nodes["mac:"+pc]; c.ParentID != p.ID {
		t.Fatalf("the PC hangs from %q, want the phone", c.ParentID)
	}
	if _, dup := nodes["mac:"+phone]; dup {
		t.Fatal("the phone is drawn twice")
	}
}

// A controller that passes on no capabilities: the phone is still known by
// what it says it is (model, maker).
func TestDeskPhoneWithoutCapabilities(t *testing.T) {
	const phone = "24:9a:d8:00:00:01"
	sw := model.Device{
		Key: "1c:00:00:00:00:01", Name: "sw", MACs: []model.MACAddress{"1c:00:00:00:00:01"}, Role: model.Ptr(model.DeviceRoleSwitch),
		Neighbors: []model.Neighbor{lldp("Port 3", phone, "yealink-000001", "Yealink SIP-T54W")},
	}
	nodes := byID(topology.Build([]topology.Source{{IntegrationID: 1, Online: true, Devices: []model.Device{sw}}}))
	if p := nodes["ext:"+phone]; p.Kind != topology.KindClient || p.Type != "ip_phone" {
		t.Fatalf("phone: %+v", p)
	}
}

// A server running lldpd announces itself as a station: a client, not an
// unknown piece of network gear.
func TestLLDPStationIsAClient(t *testing.T) {
	const srv = "52:54:00:00:00:01"
	sw := model.Device{
		Key: "1c:00:00:00:00:01", Name: "sw", MACs: []model.MACAddress{"1c:00:00:00:00:01"}, Role: model.Ptr(model.DeviceRoleSwitch),
		Neighbors: []model.Neighbor{
			lldp("ge-0/0/1", srv, "web01", "Ubuntu 24.04.3 LTS", model.NeighborCapabilityStation),
			lldp("ge-0/0/2", "1c:00:00:00:00:02", "acc", "", model.NeighborCapabilityBridge),
		},
	}
	nodes := byID(topology.Build([]topology.Source{{IntegrationID: 1, Online: true, Devices: []model.Device{sw}}}))
	if n := nodes["ext:"+srv]; n.Kind != topology.KindClient {
		t.Fatalf("server: %+v", n)
	}
	if n := nodes["ext:1c:00:00:00:00:02"]; n.Kind != topology.KindUnmanaged {
		t.Fatalf("a bridge stays network gear: %+v", n)
	}
}

// The gateway MACs of a firewall pair (CARP/VRRP) are not devices: no client
// for them, and no fake segment on the port towards the firewall.
func TestVirtualRouterMACsAreNotClients(t *testing.T) {
	sw := model.Device{
		Key: "1c:00:00:00:00:01", Name: "sw", MACs: []model.MACAddress{"1c:00:00:00:00:01"}, Role: model.Ptr(model.DeviceRoleSwitch),
		Fdb: []model.FdbEntry{
			{MAC: "00:00:5e:00:01:0a", Port: "Po11"},
			{MAC: "00:00:5e:00:01:14", Port: "Po11"},
			{MAC: "00:00:5e:00:01:1e", Port: "Po11"},
			{MAC: "00:00:5e:00:02:0a", Port: "Po11"},
		},
	}
	for _, n := range topology.Build([]topology.Source{{IntegrationID: 1, Online: true, Devices: []model.Device{sw}}}).Nodes {
		if n.Kind == topology.KindClient || n.Kind == topology.KindSegment {
			t.Fatalf("a virtual router MAC became %s %s", n.Kind, n.ID)
		}
	}
}

// The firewall's WAN port sits on a switch access port in VLAN 900, next to
// the ISP's ONT: the ONT is that WAN's modem, under the WAN node — but a
// router that shares its WAN MAC with its LAN claims no VLAN.
func TestModemInTheWANVLANHangsFromTheWAN(t *testing.T) {
	const ont, fwWAN, pc = "00:18:82:6b:51:74", "3c:6a:a7:91:99:5c", "b0:7b:25:00:00:01"
	fw := model.Device{
		Key: "3c:6a:a7:91:99:50", Name: "fw", MACs: []model.MACAddress{"3c:6a:a7:91:99:50"}, Role: model.Ptr(model.DeviceRoleFirewall),
		Interfaces: []model.Interface{
			{Name: "igc0", MAC: model.Ptr(model.MACAddress(fwWAN))},
			{Name: "pppoe0", Parent: model.Ptr("igc0"), Wan: model.Ptr(true), IPs: []string{"203.0.113.45/32"}, Up: model.Ptr(true)},
			{Name: "lan", IPs: []string{"10.10.10.1/24"}, Vlan: model.Ptr(uint16(10))},
		},
	}
	sw := model.Device{
		Key: "1c:00:00:00:00:01", Name: "core", MACs: []model.MACAddress{"1c:00:00:00:00:01"}, Role: model.Ptr(model.DeviceRoleSwitch),
		Fdb: []model.FdbEntry{
			{MAC: fwWAN, Port: "Tw1/0/1", Vlan: model.Ptr(uint16(900))},
			{MAC: ont, Port: "Tw1/0/2", Vlan: model.Ptr(uint16(900))},
			{MAC: pc, Port: "Tw1/0/5", Vlan: model.Ptr(uint16(10))},
		},
	}
	nodes := byID(topology.Build([]topology.Source{{IntegrationID: 1, Online: true, Devices: []model.Device{fw, sw}}}))
	if p := nodes["mac:"+ont].ParentID; p != "wan:dev:3c:6a:a7:91:99:50:pppoe0" {
		t.Fatalf("the ONT hangs from %q, want the PPPoE WAN", p)
	}
	if p := nodes["mac:"+pc].ParentID; p != "dev:1c:00:00:00:00:01" {
		t.Fatalf("the PC hangs from %q, want the switch", p)
	}
}

// A router whose WAN port has the same MAC as its LAN (many do): the VLAN
// that MAC is learned in is the LAN's, never taken for an uplink's.
func TestSharedWANMACClaimsNoVLAN(t *testing.T) {
	const mac, pc = "48:a9:8a:2c:a6:ec", "b0:7b:25:00:00:02"
	rt := model.Device{
		Key: mac, Name: "br-gw", MACs: []model.MACAddress{mac}, Role: model.Ptr(model.DeviceRoleRouter),
		Interfaces: []model.Interface{
			{Name: "ether1", MAC: model.Ptr(model.MACAddress(mac)), Wan: model.Ptr(true), IPs: []string{"198.51.100.20/29"}, Up: model.Ptr(true)},
			{Name: "bridge", MAC: model.Ptr(model.MACAddress(mac)), IPs: []string{"10.20.10.1/24"}},
		},
	}
	sw := model.Device{
		Key: "1c:00:00:00:00:01", Name: "br-sw", MACs: []model.MACAddress{"1c:00:00:00:00:01"}, Role: model.Ptr(model.DeviceRoleSwitch),
		Fdb: []model.FdbEntry{
			{MAC: mac, Port: "Port 24", Vlan: model.Ptr(uint16(20))},
			{MAC: pc, Port: "Port 3", Vlan: model.Ptr(uint16(20))},
		},
	}
	nodes := byID(topology.Build([]topology.Source{{IntegrationID: 1, Online: true, Devices: []model.Device{rt, sw}}}))
	if p := nodes["mac:"+pc].ParentID; p != "dev:1c:00:00:00:00:01" {
		t.Fatalf("the PC hangs from %q, want the switch", p)
	}
}
