package topology_test

import (
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// A phone on a mesh unit's Wi-Fi, the unit on a switch port. One round the
// unit's list misses the phone (a timeout): the switch still sees the phone
// on the port towards the unit, which once made "an unmanaged switch" with
// the phone on a cable. It stays on the unit's Wi-Fi until something says
// for sure that it moved: another unit's list, or a switch port of its own.
func TestClientStaysWhereItWasSeenForSure(t *testing.T) {
	const (
		phone  = model.MACAddress("da:a1:19:00:00:01")
		laptop = model.MACAddress("b0:7b:25:00:00:01")
		unitA  = "30:16:9d:00:00:0a"
		unitB  = "30:16:9d:00:00:0b"
	)
	unit := func(mac string, clients ...model.MACAddress) model.Device {
		d := model.Device{
			Key: mac, Name: "halo-" + mac[len(mac)-1:], MACs: []model.MACAddress{model.MACAddress(mac)},
			Role: model.Ptr(model.DeviceRoleAp),
		}
		for _, c := range clients {
			d.WirelessClients = append(d.WirelessClients, model.WirelessClient{
				MAC: c, Interface: model.Ptr("Casa · 5 GHz"), SSID: model.Ptr("Casa"), Band: model.Ptr(model.WifiBandA5Ghz),
			})
		}
		return d
	}
	sw := func(fdb ...model.FdbEntry) model.Device {
		return model.Device{
			Key: "1c:00:00:00:00:01", Name: "sw", MACs: []model.MACAddress{"1c:00:00:00:00:01"},
			Role: model.Ptr(model.DeviceRoleSwitch), Fdb: fdb,
		}
	}
	fw := model.Device{
		Key: "00:e0:4c:00:00:01", Name: "fw", MACs: []model.MACAddress{"00:e0:4c:00:00:01"}, Role: model.Ptr(model.DeviceRoleFirewall),
		Arp: []model.ArpEntry{{IP: "192.168.1.50", MAC: phone}, {IP: "192.168.1.60", MAC: laptop}},
	}
	round := func(before map[model.MACAddress]topology.Attachment, devices ...model.Device) (topology.Topology, map[string]topology.Node) {
		topo := topology.BuildWith([]topology.Source{{IntegrationID: 1, Online: true, Devices: append(devices, fw)}},
			topology.Options{Attached: before})
		return topo, byID(topo)
	}
	onPort := func(m model.MACAddress, port string) model.FdbEntry { return model.FdbEntry{MAC: m, Port: port} }

	// 1. On unit A's Wi-Fi: seen for sure, remembered.
	topo, _ := round(nil, unit(unitA, phone), unit(unitB), sw(onPort(unitA, "5"), onPort(unitB, "6"), onPort(phone, "5")))
	memory := topo.Attached
	if a := memory[phone]; !a.WiFi || a.Node != "dev:"+unitA || a.SSID != "Casa" {
		t.Fatalf("remembered: %+v", a)
	}

	// 2. Unit A's list misses it; the switch sees it behind unit A, with a
	// laptop: no unmanaged switch with the phone on a cable.
	topo, nodes := round(memory, unit(unitA), unit(unitB),
		sw(onPort(unitA, "5"), onPort(unitB, "6"), onPort(phone, "5"), onPort(laptop, "5")))
	p := nodes["mac:"+string(phone)]
	if p.ParentID != "dev:"+unitA || p.SSID != "Casa" || p.Band != "5ghz" {
		t.Fatalf("the phone moved to %q (%+v)", p.ParentID, p)
	}
	for _, e := range topo.Edges {
		if e.Target == p.ID && e.Kind != topology.EdgeWifi {
			t.Fatalf("the phone's link is %s, want Wi-Fi", e.Kind)
		}
	}
	for k, v := range topo.Attached {
		memory[k] = v
	}

	// 3. It roams to unit B: for sure, it moves.
	_, nodes = round(memory, unit(unitA), unit(unitB, phone), sw(onPort(unitA, "5"), onPort(unitB, "6"), onPort(phone, "6")))
	if p := nodes["mac:"+string(phone)]; p.ParentID != "dev:"+unitB {
		t.Fatalf("after roaming the phone hangs from %q", p.ParentID)
	}

	// 4. Plugged alone into a switch port: on a cable for sure, it moves.
	topo, nodes = round(memory, unit(unitA), unit(unitB), sw(onPort(unitA, "5"), onPort(unitB, "6"), onPort(phone, "12")))
	if p := nodes["mac:"+string(phone)]; p.ParentID != "dev:1c:00:00:00:00:01" || p.Port != "12" {
		t.Fatalf("on a cable the phone hangs from %q %q", p.ParentID, p.Port)
	}
	if a := topo.Attached[phone]; a.WiFi || a.Port != "12" {
		t.Fatalf("remembered on the cable: %+v", a)
	}
}
