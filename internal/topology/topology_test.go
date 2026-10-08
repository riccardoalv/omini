package topology_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/demo"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

var demoTime = time.Unix(1_790_000_000, 0)

func buildDemo(t *testing.T) (topology.Topology, map[string]topology.Node, map[string]topology.Edge) {
	t.Helper()
	topo := topology.Build([]topology.Source{{IntegrationID: 1, Online: true, Devices: demo.Network(demoTime)}})
	nodes := map[string]topology.Node{}
	for _, n := range topo.Nodes {
		nodes[n.ID] = n
	}
	edges := map[string]topology.Edge{}
	for _, e := range topo.Edges {
		edges[e.Source+">"+e.Target] = e
	}
	return topo, nodes, edges
}

const (
	fw = "dev:00:e0:4c:68:00:02"
	sw = "dev:1c:2a:a3:10:00:01"
	ap = "dev:74:ac:b9:20:00:01"
)

func TestManagedDevicesAndLinks(t *testing.T) {
	_, nodes, edges := buildDemo(t)

	for _, id := range []string{fw, sw, ap} {
		if n, ok := nodes[id]; !ok || n.Kind != topology.KindDevice || !n.Online {
			t.Fatalf("managed device %s missing or wrong: %+v", id, n)
		}
	}

	// Firewall has no LLDP: located through the switch MAC table (port 1),
	// with its own port taken from its ARP table.
	up, ok := edges[fw+">"+sw]
	if !ok {
		t.Fatalf("missing firewall -> switch edge; edges: %v", keys(edges))
	}
	if up.Kind != topology.EdgeInferred || up.SourcePort != "igc1" || up.TargetPort != "1" || up.SpeedMbps != 2500 {
		t.Errorf("unexpected uplink: %+v", up)
	}

	// Switch <-> AP reported by LLDP on both sides: one edge with both ports.
	link, ok := edges[sw+">"+ap]
	if !ok {
		t.Fatalf("missing switch -> AP edge; edges: %v", keys(edges))
	}
	if link.Kind != topology.EdgeLLDP || link.SourcePort != "3" || link.TargetPort != "eth0" || link.SpeedMbps != 1000 {
		t.Errorf("unexpected LLDP link: %+v", link)
	}
}

func TestClientsArePlacedOnTheEdgeMostPort(t *testing.T) {
	_, nodes, _ := buildDemo(t)
	cases := map[string]struct{ parent, port string }{
		"mac:00:11:32:aa:00:01": {sw, "2"}, // NAS
		"mac:04:5d:4b:50:00:01": {sw, "6"}, // TV
		"mac:d8:bb:c1:60:00:01": {sw, "7"}, // desktop
	}
	for id, want := range cases {
		n := nodes[id]
		if n.ParentID != want.parent || n.Port != want.port {
			t.Errorf("%s (%s) attached to %s:%s, want %s:%s", id, n.Label, n.ParentID, n.Port, want.parent, want.port)
		}
	}
	if nas := nodes["mac:00:11:32:aa:00:01"]; nas.Label != "nas" || nas.IP != "192.168.1.10" {
		t.Errorf("NAS not enriched from DHCP/ARP: %+v", nas)
	}
}

func TestWifiClientsAttachToTheAP(t *testing.T) {
	_, nodes, edges := buildDemo(t)
	count := 0
	for _, n := range nodes {
		if n.Kind == topology.KindClient && n.ParentID == ap {
			count++
			if n.SSID != "homelab" || n.SignalDBM == nil {
				t.Errorf("wifi client without SSID/signal: %+v", n)
			}
			if e := edges[ap+">"+n.ID]; e.Kind != topology.EdgeWifi {
				t.Errorf("wifi client edge kind = %q", e.Kind)
			}
		}
	}
	if count != 10 {
		t.Errorf("AP has %d wifi clients, want 10", count)
	}
	// Seen on the switch uplink port too, but the Wi-Fi table wins.
	if n := nodes["mac:f0:18:98:00:00:02"]; n.ParentID != ap || n.Port != "wifi1" {
		t.Errorf("macbook attached to %s:%s", n.ParentID, n.Port)
	}
	if !nodes["mac:da:a1:19:00:00:01"].RandomMAC {
		t.Error("iphone MAC should be flagged as randomized")
	}
}

func TestUnmanagedSegments(t *testing.T) {
	_, nodes, _ := buildDemo(t)
	for port, want := range map[string]int{"4": 4, "5": 5} { // proxmox + 3 VMs; mercusys + 4 clients
		seg, ok := nodes["seg:"+sw+":"+port]
		if !ok {
			t.Fatalf("missing segment on port %s", port)
		}
		if seg.MACCount != want || seg.ParentID != sw || seg.Port != port {
			t.Errorf("segment on port %s: %+v", port, seg)
		}
	}
	if n := nodes["mac:a8:a1:59:40:00:01"]; n.ParentID != "seg:"+sw+":4" {
		t.Errorf("proxmox host should be inside the segment, got parent %q", n.ParentID)
	}
}

func TestOnlyPresentDevicesBecomeClients(t *testing.T) {
	_, nodes, _ := buildDemo(t)
	if _, ok := nodes["mac:00:24:e4:99:00:01"]; ok {
		t.Error("a device known only from an old DHCP lease must not be on the map")
	}
	for _, mac := range []string{"00:e0:4c:68:00:02", "1c:2a:a3:10:00:01", "74:ac:b9:20:00:01"} {
		if _, ok := nodes["mac:"+mac]; ok {
			t.Errorf("infrastructure MAC %s duplicated as a client", mac)
		}
	}
}

func TestEveryNodeIsReachableFromTheFirewall(t *testing.T) {
	topo, _, _ := buildDemo(t)
	children := map[string][]string{}
	for _, e := range topo.Edges {
		children[e.Source] = append(children[e.Source], e.Target)
	}
	seen := map[string]bool{fw: true}
	queue := []string{fw}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range children[cur] {
			if !seen[c] {
				seen[c] = true
				queue = append(queue, c)
			}
		}
	}
	for _, n := range topo.Nodes {
		if !seen[n.ID] {
			t.Errorf("node %s (%s) not reachable from the firewall following edge direction", n.ID, n.Label)
		}
	}
}

func TestDeterministic(t *testing.T) {
	a, _, _ := buildDemo(t)
	b, _, _ := buildDemo(t)
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatal("Build output is not deterministic")
	}
}

func TestSameDeviceFromTwoIntegrationsIsMerged(t *testing.T) {
	devices := demo.Network(demoTime)
	// A second integration reporting the firewall with less data (e.g. SNMP).
	thin := model.Device{Key: "opnsense-snmp", Name: "opnsense", MACs: []model.MACAddress{"00:e0:4c:68:00:01"}}
	topo := topology.Build([]topology.Source{
		{IntegrationID: 1, Online: true, Devices: devices},
		{IntegrationID: 2, Online: true, Devices: []model.Device{thin}},
	})
	count := 0
	for _, n := range topo.Nodes {
		if n.Kind == topology.KindDevice && n.Label == "opnsense" {
			count++
			if len(n.Device.Arp) == 0 {
				t.Error("merged node should keep the richer data")
			}
		}
	}
	if count != 1 {
		t.Fatalf("firewall appears %d times, want 1", count)
	}
}

func TestUnknownLLDPNeighborBecomesUnmanagedNode(t *testing.T) {
	sw := model.Device{Key: "aa:00:00:00:00:01", Name: "sw", Neighbors: []model.Neighbor{{
		LocalPort: "ge5", RemoteName: model.Ptr("mystery-switch"), RemoteMAC: model.Ptr(model.MACAddress("aa:00:00:00:00:99")),
	}}}
	topo := topology.Build([]topology.Source{{IntegrationID: 1, Online: true, Devices: []model.Device{sw}}})
	var got []string
	for _, n := range topo.Nodes {
		got = append(got, string(n.Kind)+":"+n.Label)
	}
	want := []string{"device:sw", "unmanaged:mystery-switch"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("nodes = %v, want %v", got, want)
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestDirectLinksGetThePortSpeed(t *testing.T) {
	// A firewall seen through its API: the modem alone on igb0 (WAN), three
	// devices on igb1 (LAN, a switch is in between).
	fw := model.Device{
		Key: "00:0d:b9:00:00:01", Name: "fw", Role: model.Ptr(model.DeviceRoleFirewall),
		MACs: []model.MACAddress{"00:0d:b9:00:00:01"},
		Interfaces: []model.Interface{
			{Name: "igb0", Up: model.Ptr(true), SpeedMbps: model.Ptr(uint64(2500))},
			{Name: "igb1", Up: model.Ptr(true), SpeedMbps: model.Ptr(uint64(1000))},
		},
		Arp: []model.ArpEntry{
			{IP: "203.0.113.1", MAC: "aa:00:00:00:00:01", Interface: model.Ptr("igb0")},
			{IP: "192.168.1.10", MAC: "aa:00:00:00:00:02", Interface: model.Ptr("igb1")},
			{IP: "192.168.1.11", MAC: "aa:00:00:00:00:03", Interface: model.Ptr("igb1")},
			{IP: "192.168.1.12", MAC: "aa:00:00:00:00:04", Interface: model.Ptr("igb1")},
		},
	}
	topo := topology.Build([]topology.Source{{IntegrationID: 1, Online: true, Devices: []model.Device{fw}}})
	speeds := map[string]uint64{}
	for _, e := range topo.Edges {
		speeds[e.Target] = e.SpeedMbps
	}
	if speeds["mac:aa:00:00:00:00:01"] != 2500 {
		t.Errorf("modem alone on the WAN port: speed %d, want 2500", speeds["mac:aa:00:00:00:00:01"])
	}
	for _, m := range []string{"02", "03", "04"} {
		if s := speeds["mac:aa:00:00:00:00:"+m]; s != 0 {
			t.Errorf("device %s shares the LAN port: speed %d, want unknown", m, s)
		}
	}
}

func TestWANsAreParentsOfTheFirewall(t *testing.T) {
	up, down := model.GatewayStatusUp, model.GatewayStatusDown
	fw := model.Device{
		Key: "58:9c:fc:00:00:01", Name: "fw", Role: model.Ptr(model.DeviceRoleFirewall),
		MACs: []model.MACAddress{"58:9c:fc:00:00:01"},
		Interfaces: []model.Interface{
			{Name: "re0", Up: model.Ptr(true), SpeedMbps: model.Ptr(uint64(2500))},
			{Name: "vlan0.2000", Up: model.Ptr(true), Parent: model.Ptr("re0")},
			{
				Name: "pppoe1", Description: model.Ptr("WAN"), Up: model.Ptr(true), Wan: model.Ptr(true),
				Parent: model.Ptr("vlan0.2000"), SpeedMbps: model.Ptr(uint64(2500)), IPs: []string{"100.64.0.20/32"},
			},
			{Name: "igb3", Description: model.Ptr("LTE"), Up: model.Ptr(true), Wan: model.Ptr(true), SpeedMbps: model.Ptr(uint64(1000))},
			{Name: "igb1", Up: model.Ptr(true), SpeedMbps: model.Ptr(uint64(1000))},
		},
		Gateways: []model.Gateway{
			{Name: "WAN_PPPOE", Interface: model.Ptr("pppoe1"), Status: up},
			{Name: "LTE_GW", Interface: model.Ptr("igb3"), Status: down},
		},
		Arp: []model.ArpEntry{
			{IP: "192.168.100.1", MAC: "aa:00:00:00:00:01", Interface: model.Ptr("re0")}, // ISP modem
			{IP: "192.168.1.10", MAC: "aa:00:00:00:00:02", Interface: model.Ptr("igb1")},
		},
	}
	topo := topology.Build([]topology.Source{{IntegrationID: 1, Online: true, Devices: []model.Device{fw}}})
	nodes := map[string]topology.Node{}
	for _, n := range topo.Nodes {
		nodes[n.ID] = n
	}
	parents := map[string][]string{}
	speed := map[string]uint64{}
	for _, e := range topo.Edges {
		parents[e.Target] = append(parents[e.Target], e.Source)
		speed[e.Source+">"+e.Target] = e.SpeedMbps
	}
	fwID := "dev:58:9c:fc:00:00:01"
	pppoe, lte := "wan:"+fwID+":pppoe1", "wan:"+fwID+":igb3"

	w := nodes[pppoe]
	if w.Kind != topology.KindWAN || w.Label != "WAN" || !w.Online || w.WAN.Port != "re0" ||
		w.WAN.SpeedMbps != 2500 || len(w.WAN.Gateways) != 1 || w.WAN.IPs[0] != "100.64.0.20/32" {
		t.Fatalf("PPPoE WAN node = %+v %+v", w, w.WAN)
	}
	if l := nodes[lte]; l.Label != "LTE" || l.Online {
		t.Fatalf("LTE WAN (gateway down) = %+v", l)
	}
	if got := parents[fwID]; len(got) != 2 {
		t.Fatalf("the firewall must have both WANs as parents, got %v", got)
	}
	if speed[pppoe+">"+fwID] != 2500 || speed[lte+">"+fwID] != 1000 {
		t.Fatalf("WAN link speeds: %v", speed)
	}
	if got := parents["mac:aa:00:00:00:00:01"]; len(got) != 1 || got[0] != pppoe {
		t.Fatalf("the modem on the WAN port belongs to the WAN: %v", got)
	}
	if got := parents["mac:aa:00:00:00:00:02"]; len(got) != 1 || got[0] != fwID {
		t.Fatalf("LAN devices stay under the firewall: %v", got)
	}
}

func TestDeviceWithoutMACMergesByIP(t *testing.T) {
	fw := model.Device{
		Key: "58:9c:fc:00:00:01", Name: "OPNsense", Host: model.Ptr("192.168.1.1"),
		MACs: []model.MACAddress{"58:9c:fc:00:00:01"}, IPs: []string{"192.168.1.1"},
		Role: model.Ptr(model.DeviceRoleFirewall),
	}
	scan := model.Device{ // a scan that could not read MACs
		Key: "gw:192.168.1.1", Name: "Gateway", Host: model.Ptr("192.168.1.1"),
		Role: model.Ptr(model.DeviceRoleRouter), Hosts: []model.Host{{IP: "192.168.1.20"}},
	}
	topo := topology.Build([]topology.Source{
		{IntegrationID: 1, Online: true, Devices: []model.Device{fw}},
		{IntegrationID: 2, Online: true, Devices: []model.Device{scan}},
	})
	devices := 0
	for _, n := range topo.Nodes {
		if n.Kind == topology.KindDevice {
			devices++
		}
	}
	if devices != 1 {
		t.Fatalf("one firewall expected, got %d device nodes: %+v", devices, topo.Nodes)
	}
}

// A switch read through its web interface (MAC table, no LLDP) behind a
// firewall that only has ARP: the switch hangs under the firewall by its
// uplink port, and what the switch sees only on that port stays with the
// firewall instead of becoming a segment.
func TestSwitchUplinkFoundFromItsMACTable(t *testing.T) {
	const (
		fwMAC  = "58:9c:fc:00:00:01"
		swMAC  = "1c:2a:a3:00:00:01"
		vm     = "bc:24:11:00:00:01" // a VM on the firewall's bridge, seen on the uplink
		pc     = "00:e0:4c:00:00:01" // directly on Port 1
		hubA   = "30:16:9d:00:00:01" // three behind an unmanaged switch on Port 5
		hubB   = "30:16:9d:00:00:02"
		hubC   = "30:16:9d:00:00:03"
		bridge = "bridge0"
	)
	arp := []model.ArpEntry{}
	for i, m := range []string{swMAC, vm, pc, hubA, hubB, hubC} {
		arp = append(arp, model.ArpEntry{IP: "192.168.1." + string(rune('2'+i)), MAC: model.MACAddress(m), Interface: model.Ptr(bridge)})
	}
	fwDev := model.Device{
		Key: fwMAC, Name: "OPNsense", Host: model.Ptr("192.168.1.1"), Role: model.Ptr(model.DeviceRoleFirewall),
		MACs: []model.MACAddress{fwMAC}, IPs: []string{"192.168.1.1"}, Arp: arp,
		Interfaces: []model.Interface{{Name: bridge, SpeedMbps: model.Ptr(uint64(10000)), Up: model.Ptr(true)}},
	}
	port := func(n string, speed uint64) model.Interface {
		return model.Interface{Name: n, SpeedMbps: model.Ptr(speed), Up: model.Ptr(true)}
	}
	fdb := func(m, p string) model.FdbEntry { return model.FdbEntry{MAC: model.MACAddress(m), Port: p} }
	swDev := model.Device{
		Key: swMAC, Name: "HC-SWTGW218AS", Host: model.Ptr("192.168.1.2"), Role: model.Ptr(model.DeviceRoleSwitch),
		MACs:       []model.MACAddress{swMAC},
		Interfaces: []model.Interface{port("Port 1", 1000), port("Port 5", 2500), port("Port 9", 10000)},
		Fdb: []model.FdbEntry{
			fdb(fwMAC, "Port 9"), fdb(vm, "Port 9"), fdb(pc, "Port 1"),
			fdb(hubA, "Port 5"), fdb(hubB, "Port 5"), fdb(hubC, "Port 5"),
		},
	}
	topo := topology.Build([]topology.Source{
		{IntegrationID: 1, Online: true, Devices: []model.Device{fwDev}},
		{IntegrationID: 2, Online: true, Devices: []model.Device{swDev}},
	})
	nodes := map[string]topology.Node{}
	for _, n := range topo.Nodes {
		nodes[n.ID] = n
	}
	edges := map[string]topology.Edge{}
	for _, e := range topo.Edges {
		edges[e.Source+">"+e.Target] = e
	}
	fwID, swID := "dev:"+fwMAC, "dev:"+swMAC
	up, ok := edges[fwID+">"+swID]
	if !ok || up.TargetPort != "Port 9" || up.SpeedMbps != 10000 {
		t.Fatalf("firewall → switch: %+v (found %v)", up, ok)
	}
	if _, ok := nodes["seg:"+swID+":Port 9"]; ok {
		t.Fatal("the uplink must not become a segment")
	}
	if p := nodes["mac:"+vm].ParentID; p != fwID {
		t.Errorf("the VM seen only on the uplink belongs to the firewall, got %q", p)
	}
	if n := nodes["mac:"+pc]; n.ParentID != swID || n.Port != "Port 1" {
		t.Errorf("pc: parent %q port %q", n.ParentID, n.Port)
	}
	if seg, ok := nodes["seg:"+swID+":Port 5"]; !ok || seg.MACCount != 3 {
		t.Errorf("an unmanaged switch on Port 5 expected: %+v", seg)
	}
}

// A phone asleep drops out of the switch's MAC table while the firewall still
// has it in ARP: it stays on the switch port where it was last learned.
func TestClientKeepsItsLastSwitchPort(t *testing.T) {
	const (
		fwMAC = "58:9c:fc:00:00:01"
		swMAC = "1c:2a:a3:00:00:01"
		phone = "92:34:c8:00:00:01"
	)
	fwDev := model.Device{
		Key: fwMAC, Name: "OPNsense", Host: model.Ptr("192.168.1.1"), Role: model.Ptr(model.DeviceRoleFirewall),
		MACs: []model.MACAddress{fwMAC},
		Arp: []model.ArpEntry{
			{IP: "192.168.1.2", MAC: swMAC, Interface: model.Ptr("bridge0")},
			{IP: "192.168.1.41", MAC: phone, Interface: model.Ptr("bridge0")},
		},
	}
	swDev := model.Device{
		Key: swMAC, Name: "switch", Role: model.Ptr(model.DeviceRoleSwitch), MACs: []model.MACAddress{swMAC},
		Fdb: []model.FdbEntry{{MAC: fwMAC, Port: "Port 9"}}, // the phone is not there right now
	}
	sources := []topology.Source{
		{IntegrationID: 1, Online: true, Devices: []model.Device{fwDev}},
		{IntegrationID: 2, Online: true, Devices: []model.Device{swDev}},
	}
	parent := func(topo topology.Topology) (string, string) {
		for _, n := range topo.Nodes {
			if n.ID == "mac:"+phone {
				return n.ParentID, n.Port
			}
		}
		return "", ""
	}
	if p, _ := parent(topology.Build(sources)); p != "dev:"+fwMAC {
		t.Fatalf("without memory the phone is where ARP sees it, got %q", p)
	}
	remembered := topology.Options{LastSeen: map[model.MACAddress]topology.PortRef{
		phone: {Node: "dev:" + swMAC, Port: "Port 2"},
	}}
	if p, port := parent(topology.BuildWith(sources, remembered)); p != "dev:"+swMAC || port != "Port 2" {
		t.Fatalf("the phone keeps its last switch port, got %q %q", p, port)
	}
	gone := topology.Options{LastSeen: map[model.MACAddress]topology.PortRef{
		phone: {Node: "dev:aa:aa:aa:aa:aa:aa", Port: "Port 2"}, // a switch no longer on the map
	}}
	if p, _ := parent(topology.BuildWith(sources, gone)); p != "dev:"+fwMAC {
		t.Fatalf("a memory of a device that is gone is ignored, got %q", p)
	}
}

// A Wi-Fi client carries its band, link rate and current traffic from its AP.
func TestWifiClientBandAndTraffic(t *testing.T) {
	const apMAC, phone = "30:16:9d:00:00:01", "02:23:ab:00:00:01"
	band := model.WifiBand("5ghz")
	ap := model.Device{
		Key: apMAC, Name: "Bedroom", Role: model.Ptr(model.DeviceRoleAp), MACs: []model.MACAddress{apMAC},
		WirelessClients: []model.WirelessClient{{
			MAC: phone, Interface: model.Ptr("5 GHz"), Band: &band,
			RxBps: model.Ptr(uint64(2_000_000)), TxBps: model.Ptr(uint64(100_000)), TxRateMbps: model.Ptr(866.7),
		}},
	}
	topo := topology.Build([]topology.Source{{IntegrationID: 1, Online: true, Devices: []model.Device{ap}}})
	for _, n := range topo.Nodes {
		if n.ID != "mac:"+phone {
			continue
		}
		if n.Band != "5ghz" || n.LinkMbps != 866.7 || n.Flow == nil || n.Flow.RxBps != 2_000_000 || n.Flow.TxBps != 100_000 {
			t.Fatalf("phone: band %q link %v flow %+v", n.Band, n.LinkMbps, n.Flow)
		}
		return
	}
	t.Fatal("phone not on the map")
}

// Port 5 → unmanaged switch → {Wi-Fi AP, laptop by cable}: the switch sees
// the laptop on the AP's port, but the AP — which lists its clients — does
// not have it. Both hang from an unmanaged segment on that port.
func TestUnmanagedSwitchBesideAnAccessPoint(t *testing.T) {
	const (
		swMAC  = "1c:2a:a3:00:00:01"
		apMAC  = "30:16:9d:00:00:01"
		wired  = "00:e0:4c:00:00:01" // the laptop's cable
		phone  = "02:23:ab:00:00:01" // a Wi-Fi client of the AP
		fwMAC  = "58:9c:fc:00:00:01"
		swID   = "dev:" + swMAC
		apID   = "dev:" + apMAC
		segID  = "seg:" + swID + ":Port 5"
		bridge = "bridge0"
	)
	band := model.WifiBand("5ghz")
	build := func(apClients []model.WirelessClient, apFdb []model.FdbEntry) map[string]topology.Node {
		fw := model.Device{
			Key: fwMAC, Name: "fw", Role: model.Ptr(model.DeviceRoleFirewall), MACs: []model.MACAddress{fwMAC},
			Arp: []model.ArpEntry{
				{IP: "192.168.1.2", MAC: swMAC, Interface: model.Ptr(bridge)},
				{IP: "192.168.1.3", MAC: apMAC, Interface: model.Ptr(bridge)},
				{IP: "192.168.1.108", MAC: wired, Interface: model.Ptr(bridge)},
				{IP: "192.168.1.169", MAC: phone, Interface: model.Ptr(bridge)},
			},
		}
		sw := model.Device{
			Key: swMAC, Name: "sw", Role: model.Ptr(model.DeviceRoleSwitch), MACs: []model.MACAddress{swMAC},
			Interfaces: []model.Interface{{Name: "Port 5", SpeedMbps: model.Ptr(uint64(2500)), Up: model.Ptr(true)}},
			Fdb: []model.FdbEntry{
				{MAC: fwMAC, Port: "Port 9"},
				{MAC: apMAC, Port: "Port 5"},
				{MAC: wired, Port: "Port 5"},
				{MAC: phone, Port: "Port 5"},
			},
		}
		ap := model.Device{
			Key: apMAC, Name: "Bedroom", Role: model.Ptr(model.DeviceRoleAp), MACs: []model.MACAddress{apMAC},
			WirelessClients: apClients, Fdb: apFdb,
		}
		topo := topology.Build([]topology.Source{
			{IntegrationID: 1, Online: true, Devices: []model.Device{fw}},
			{IntegrationID: 2, Online: true, Devices: []model.Device{sw}},
			{IntegrationID: 3, Online: true, Devices: []model.Device{ap}},
		})
		nodes := map[string]topology.Node{}
		for _, n := range topo.Nodes {
			nodes[n.ID] = n
		}
		return nodes
	}
	phoneOnAP := []model.WirelessClient{{MAC: phone, Band: &band, Interface: model.Ptr("Home · 5 GHz")}}

	nodes := build(phoneOnAP, nil)
	seg, ok := nodes[segID]
	if !ok || seg.ParentID != swID || seg.Port != "Port 5" {
		t.Fatalf("an unmanaged segment on Port 5 expected: %+v", seg)
	}
	if nodes[apID].ParentID != segID || nodes["mac:"+wired].ParentID != segID {
		t.Fatalf("AP and laptop under the segment: AP %q, laptop %q", nodes[apID].ParentID, nodes["mac:"+wired].ParentID)
	}
	if nodes["mac:"+phone].ParentID != apID {
		t.Fatalf("the Wi-Fi client stays on the AP: %q", nodes["mac:"+phone].ParentID)
	}

	// The AP lists the laptop as its wired client: it is behind the AP.
	nodes = build(phoneOnAP, []model.FdbEntry{{MAC: wired, Port: "LAN"}})
	if _, ok := nodes[segID]; ok || nodes["mac:"+wired].ParentID != apID {
		t.Fatalf("a client the AP lists stays behind it: %q", nodes["mac:"+wired].ParentID)
	}

	// An AP that lists no clients: nothing to compare, the old guess stands.
	nodes = build(nil, nil)
	if _, ok := nodes[segID]; ok || nodes["mac:"+wired].ParentID != apID {
		t.Fatalf("without a client list the laptop is behind the AP: %q", nodes["mac:"+wired].ParentID)
	}
}

// The switch's MAC table forgets the router for a while: its uplink stays
// where the router was last learned, so what is behind the router does not
// turn into an unmanaged segment on that port.
func TestUplinkKeptWhileTheRouterIsForgotten(t *testing.T) {
	const (
		fwMAC = "58:9c:fc:00:00:01"
		swMAC = "1c:2a:a3:00:00:01"
		swID  = "dev:" + swMAC
	)
	vms := []string{"bc:24:11:00:00:01", "bc:24:11:00:00:02", "bc:24:11:00:00:03"}
	arp := []model.ArpEntry{{IP: "192.168.1.2", MAC: swMAC, Interface: model.Ptr("bridge0")}}
	fdb := []model.FdbEntry{} // the router itself is not in the table right now
	for i, m := range vms {
		arp = append(arp, model.ArpEntry{IP: fmt.Sprintf("192.168.1.%d", 10+i), MAC: model.MACAddress(m), Interface: model.Ptr("bridge0")})
		fdb = append(fdb, model.FdbEntry{MAC: model.MACAddress(m), Port: "Port 9"})
	}
	sources := []topology.Source{
		{IntegrationID: 1, Online: true, Devices: []model.Device{{
			Key: fwMAC, Name: "fw", Role: model.Ptr(model.DeviceRoleFirewall), MACs: []model.MACAddress{fwMAC}, Arp: arp,
		}}},
		{IntegrationID: 2, Online: true, Devices: []model.Device{{
			Key: swMAC, Name: "sw", Role: model.Ptr(model.DeviceRoleSwitch), MACs: []model.MACAddress{swMAC}, Fdb: fdb,
		}}},
	}
	opts := topology.Options{LastSeen: map[model.MACAddress]topology.PortRef{fwMAC: {Node: swID, Port: "Port 9"}}}
	for _, n := range topology.BuildWith(sources, opts).Nodes {
		if n.ID == "seg:"+swID+":Port 9" {
			t.Fatal("the uplink became an unmanaged segment")
		}
	}
}

// A MAC only the switch's table has (no IP, no ARP, no scan), on the port of
// an access point that lists its clients without it, is not shown: nothing
// else says it is there — and it must not invent an unmanaged switch.
func TestMACOnlySeenBehindAnAccessPointIsNotShown(t *testing.T) {
	const (
		fwMAC = "58:9c:fc:00:00:01"
		swMAC = "1c:2a:a3:00:00:01"
		apMAC = "30:16:9d:00:00:01"
		tuya  = "00:33:7a:00:00:01"
		phone = "02:23:ab:00:00:01"
	)
	band := model.WifiBand("2.4ghz")
	topo := topology.Build([]topology.Source{
		{IntegrationID: 1, Online: true, Devices: []model.Device{{
			Key: fwMAC, Name: "fw", Role: model.Ptr(model.DeviceRoleFirewall), MACs: []model.MACAddress{fwMAC},
			Arp: []model.ArpEntry{
				{IP: "192.168.1.2", MAC: swMAC, Interface: model.Ptr("bridge0")},
				{IP: "192.168.1.3", MAC: apMAC, Interface: model.Ptr("bridge0")},
			},
		}}},
		{IntegrationID: 2, Online: true, Devices: []model.Device{{
			Key: swMAC, Name: "sw", Role: model.Ptr(model.DeviceRoleSwitch), MACs: []model.MACAddress{swMAC},
			Fdb: []model.FdbEntry{{MAC: fwMAC, Port: "Port 9"}, {MAC: apMAC, Port: "Port 2"}, {MAC: tuya, Port: "Port 2"}},
		}}},
		{IntegrationID: 3, Online: true, Devices: []model.Device{{
			Key: apMAC, Name: "Living Room", Role: model.Ptr(model.DeviceRoleAp), MACs: []model.MACAddress{apMAC},
			WirelessClients: []model.WirelessClient{{MAC: phone, Band: &band}},
		}}},
	})
	for _, n := range topo.Nodes {
		if n.ID == "mac:"+tuya || n.Kind == topology.KindSegment {
			t.Fatalf("unexpected %s %q", n.Kind, n.ID)
		}
	}
}

// A Proxmox integration hangs each guest under its host with an "other"
// neighbor; the host itself is still placed on its switch port.
func TestGuestUnderHostAndHostOnItsPort(t *testing.T) {
	hostMAC := model.MACAddress("bc:24:11:00:00:01")
	vmMAC := model.MACAddress("bc:24:11:00:00:99")
	switchDev := model.Device{
		Key: "sw", Name: "sw", Role: model.Ptr(model.DeviceRoleSwitch), MACs: []model.MACAddress{"1c:2a:a3:10:00:01"},
		Fdb: []model.FdbEntry{{MAC: hostMAC, Port: "Port 1"}, {MAC: vmMAC, Port: "Port 1"}},
	}
	host := model.Device{Key: string(hostMAC), Name: "pve", Role: model.Ptr(model.DeviceRoleServer), MACs: []model.MACAddress{hostMAC}}
	vm := model.Device{
		Key: string(vmMAC), Name: "ubuntu", Role: model.Ptr(model.DeviceRoleServer), MACs: []model.MACAddress{vmMAC},
		Neighbors: []model.Neighbor{{Protocol: model.Ptr(model.NeighborProtocolOther), LocalPort: "net0", RemoteMAC: &hostMAC, RemotePort: model.Ptr("vmbr0")}},
	}
	topo := topology.Build([]topology.Source{
		{IntegrationID: 1, Online: true, Devices: []model.Device{switchDev}},
		{IntegrationID: 2, Online: true, Devices: []model.Device{host, vm}},
	})
	links := map[string]bool{}
	for _, e := range topo.Edges {
		links[e.Source+"@"+e.SourcePort+">"+e.Target+"@"+e.TargetPort] = true
		links[e.Target+"@"+e.TargetPort+">"+e.Source+"@"+e.SourcePort] = true
	}
	swID, hostID, vmID := "dev:1c:2a:a3:10:00:01", "dev:"+string(hostMAC), "dev:"+string(vmMAC)
	if !links[swID+"@Port 1>"+hostID+"@"] {
		t.Fatalf("the host is not on the switch's Port 1: %+v", topo.Edges)
	}
	if !links[hostID+"@vmbr0>"+vmID+"@net0"] {
		t.Fatalf("the guest is not under its host: %+v", topo.Edges)
	}
	if len(topo.Edges) != 2 {
		t.Fatalf("the guest has another link too: %+v", topo.Edges)
	}
}

// A scanned network stands for the hosts found there: when none of them is
// left under it (they were all placed elsewhere, or none answered), it is
// not drawn — an empty "Network 192.168.100.0/24" floating on the map.
func TestEmptyScannedNetworkIsNotDrawn(t *testing.T) {
	empty := model.Device{Key: "net:192.168.100.0/24", Name: "Network 192.168.100.0/24", Role: model.Ptr(model.DeviceRoleUnknown)}
	full := model.Device{
		Key: "net:10.9.0.0/24", Name: "Network 10.9.0.0/24", Role: model.Ptr(model.DeviceRoleUnknown),
		Hosts: []model.Host{{IP: "10.9.0.5", MAC: model.Ptr(model.MACAddress("aa:00:00:00:00:05"))}},
	}
	topo := topology.Build([]topology.Source{{IntegrationID: 2, Online: true, Devices: []model.Device{empty, full}}})
	var labels []string
	for _, n := range topo.Nodes {
		if n.Kind == topology.KindDevice {
			labels = append(labels, n.Label)
		}
	}
	if !reflect.DeepEqual(labels, []string{"Network 10.9.0.0/24"}) {
		t.Fatalf("devices on the map: %v", labels)
	}
}

// A Proxmox host comes from its API without any MAC: it takes the one its
// address has in the firewall's ARP table, so it is the machine the switch
// sees on Port 1 — one node, on that port — instead of a second one floating.
func TestDeviceWithoutMACTakesItsARPMAC(t *testing.T) {
	const pveMAC = "22:28:4d:00:00:50"
	fw := model.Device{
		Key: "58:9c:fc:00:00:01", Name: "OPNsense", Host: model.Ptr("192.168.1.1"),
		MACs: []model.MACAddress{"58:9c:fc:00:00:01"}, Role: model.Ptr(model.DeviceRoleFirewall),
		Arp: []model.ArpEntry{{IP: "192.168.1.50", MAC: pveMAC}},
	}
	sw := model.Device{
		Key: "1c:2a:a3:00:00:01", Name: "switch", Host: model.Ptr("192.168.1.96"),
		MACs: []model.MACAddress{"1c:2a:a3:00:00:01"}, Role: model.Ptr(model.DeviceRoleSwitch),
		Fdb: []model.FdbEntry{{MAC: pveMAC, Port: "Port 1"}, {MAC: "58:9c:fc:00:00:01", Port: "Port 8"}},
	}
	pve := model.Device{Key: "proxmox:pve", Name: "pve", Host: model.Ptr("192.168.1.50"), Role: model.Ptr(model.DeviceRoleServer)}
	topo := topology.Build([]topology.Source{
		{IntegrationID: 1, Online: true, Devices: []model.Device{fw}},
		{IntegrationID: 2, Online: true, Devices: []model.Device{sw}},
		{IntegrationID: 3, Online: true, Devices: []model.Device{pve}},
	})
	var withIP []string
	for _, n := range topo.Nodes {
		if n.IP == "192.168.1.50" {
			withIP = append(withIP, n.ID)
		}
	}
	if !reflect.DeepEqual(withIP, []string{"dev:3:proxmox:pve"}) {
		t.Fatalf("nodes with the host's address: %v", withIP)
	}
	found := false
	for _, e := range topo.Edges {
		if e.Target == "dev:3:proxmox:pve" && e.Source == "dev:1c:2a:a3:00:00:01" && e.SourcePort == "Port 1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the host hangs from the switch's Port 1: %+v", topo.Edges)
	}
}

// A Proxmox cluster: two nodes reported without MACs (the API has none), each
// running a guest that names its node only by address and name. Every guest
// hangs from the node that runs it, and every node from its own switch port.
func TestProxmoxClusterGuestsUnderTheirNode(t *testing.T) {
	const (
		fwMAC  = "58:9c:fc:00:00:01"
		pve1M  = "22:28:4d:00:00:10"
		pve2M  = "22:28:4d:00:00:11"
		vmMAC  = "bc:24:11:00:00:a1"
		ctMAC  = "bc:24:11:00:00:b2"
		swID   = "dev:1c:2a:a3:00:00:01"
		pve1ID = "dev:7:proxmox:pve1"
		pve2ID = "dev:7:proxmox:pve2"
	)
	fw := model.Device{
		Key: fwMAC, Name: "OPNsense", Host: model.Ptr("192.168.1.1"), MACs: []model.MACAddress{fwMAC},
		Role: model.Ptr(model.DeviceRoleFirewall),
		Arp: []model.ArpEntry{
			{IP: "192.168.1.10", MAC: pve1M},
			{IP: "192.168.1.11", MAC: pve2M},
			{IP: "192.168.1.20", MAC: vmMAC},
			{IP: "192.168.1.21", MAC: ctMAC},
		},
	}
	sw := model.Device{
		Key: "1c:2a:a3:00:00:01", Name: "switch", Host: model.Ptr("192.168.1.96"),
		MACs: []model.MACAddress{"1c:2a:a3:00:00:01"}, Role: model.Ptr(model.DeviceRoleSwitch),
		Fdb: []model.FdbEntry{
			{MAC: fwMAC, Port: "Port 8"},
			{MAC: pve1M, Port: "Port 1"},
			{MAC: vmMAC, Port: "Port 1"},
			{MAC: pve2M, Port: "Port 2"},
			{MAC: ctMAC, Port: "Port 2"},
		},
	}
	node := func(name, ip string) model.Device {
		return model.Device{Key: "proxmox:" + name, Name: name, Host: model.Ptr(ip), Role: model.Ptr(model.DeviceRoleServer), Vendor: model.Ptr("Proxmox")}
	}
	guest := func(name, mac, ip, host, hostIP string) model.Device {
		return model.Device{
			Key: mac, Name: name, Host: model.Ptr(ip), MACs: []model.MACAddress{model.MACAddress(mac)},
			Role: model.Ptr(model.DeviceRoleServer), CPUPct: model.Ptr(12.5), MemPct: model.Ptr(40.0),
			Neighbors: []model.Neighbor{{
				LocalPort: "net0", Protocol: model.Ptr(model.NeighborProtocolOther),
				RemoteName: model.Ptr(host), RemotePort: model.Ptr("vmbr0"), RemoteIP: model.Ptr(hostIP),
			}},
		}
	}
	topo := topology.Build([]topology.Source{
		{IntegrationID: 3, Online: true, Devices: []model.Device{fw}},
		{IntegrationID: 5, Online: true, Devices: []model.Device{sw}},
		{IntegrationID: 7, Online: true, Devices: []model.Device{
			node("pve1", "192.168.1.10"), guest("vm-a", vmMAC, "192.168.1.20", "pve1", "192.168.1.10"),
			node("pve2", "192.168.1.11"), guest("ct-b", ctMAC, "192.168.1.21", "pve2", "192.168.1.11"),
		}},
	})
	parent := map[string]string{}
	for _, e := range topo.Edges {
		parent[e.Target] = e.Source + "|" + e.SourcePort
	}
	want := map[string]string{
		"dev:" + vmMAC: pve1ID + "|vmbr0",
		"dev:" + ctMAC: pve2ID + "|vmbr0",
		pve1ID:         swID + "|Port 1",
		pve2ID:         swID + "|Port 2",
	}
	for child, p := range want {
		if parent[child] != p {
			t.Errorf("%s hangs from %q, want %q", child, parent[child], p)
		}
	}
	for _, n := range topo.Nodes {
		if n.Kind == topology.KindClient && (n.MAC == vmMAC || n.MAC == ctMAC || n.MAC == pve1M || n.MAC == pve2M) {
			t.Errorf("a second node for %s: %s", n.MAC, n.ID)
		}
	}
}

// The switch forgot the Proxmox host's MAC for a while (quiet, aged out) and
// the firewall's ARP has no entry for it: the host stays on the port where it
// was last learned instead of floating apart with its guests.
func TestDeviceStaysOnTheLastPortItWasLearnedOn(t *testing.T) {
	const pveMAC = "22:28:4d:00:00:50"
	swID := "dev:1c:2a:a3:00:00:01"
	sw := model.Device{
		Key: "1c:2a:a3:00:00:01", Name: "switch", Host: model.Ptr("192.168.1.96"),
		MACs: []model.MACAddress{"1c:2a:a3:00:00:01"}, Role: model.Ptr(model.DeviceRoleSwitch),
		Fdb: []model.FdbEntry{{MAC: "aa:00:00:00:00:09", Port: "Port 9"}},
	}
	scan := model.Device{
		Key: "net", Name: "Network", Role: model.Ptr(model.DeviceRoleUnknown),
		Hosts: []model.Host{{IP: "192.168.1.50", MAC: model.Ptr(model.MACAddress(pveMAC))}},
	}
	pve := model.Device{Key: "proxmox:pve", Name: "pve", Host: model.Ptr("192.168.1.50"), Role: model.Ptr(model.DeviceRoleServer)}
	topo := topology.BuildWith([]topology.Source{
		{IntegrationID: 2, Online: true, Devices: []model.Device{sw}},
		{IntegrationID: 3, Online: true, Devices: []model.Device{scan}},
		{IntegrationID: 7, Online: true, Devices: []model.Device{pve}},
	}, topology.Options{LastSeen: map[model.MACAddress]topology.PortRef{pveMAC: {Node: swID, Port: "Port 1"}}})
	for _, e := range topo.Edges {
		if e.Target == "dev:7:proxmox:pve" {
			if e.Source != swID || e.SourcePort != "Port 1" {
				t.Fatalf("host under %s %s, want the switch's Port 1", e.Source, e.SourcePort)
			}
			return
		}
	}
	t.Fatalf("the host hangs from nothing: %+v", topo.Edges)
}

// Nothing places the device (no MAC table, no ARP, nothing remembered), but
// its address is in a network of the firewall: it hangs from that interface.
func TestDeviceHangsFromTheRouterOfItsNetwork(t *testing.T) {
	fw := model.Device{
		Key: "58:9c:fc:00:00:01", Name: "OPNsense", Host: model.Ptr("192.168.1.1"),
		MACs: []model.MACAddress{"58:9c:fc:00:00:01"}, Role: model.Ptr(model.DeviceRoleFirewall),
		Interfaces: []model.Interface{
			{Name: "igc0", Wan: model.Ptr(true), IPs: []string{"192.168.100.2/24"}},
			{Name: "igc1", IPs: []string{"192.168.1.1/24"}},
			{Name: "igc1_vlan20", IPs: []string{"192.168.20.1/24"}},
		},
	}
	pve := model.Device{Key: "proxmox:pve", Name: "pve", Host: model.Ptr("192.168.20.50"), Role: model.Ptr(model.DeviceRoleServer)}
	topo := topology.Build([]topology.Source{
		{IntegrationID: 1, Online: true, Devices: []model.Device{fw}},
		{IntegrationID: 7, Online: true, Devices: []model.Device{pve}},
	})
	for _, e := range topo.Edges {
		if e.Target == "dev:7:proxmox:pve" && e.Source == "dev:58:9c:fc:00:00:01" && e.SourcePort == "igc1_vlan20" {
			return
		}
	}
	t.Fatalf("the host does not hang from the firewall's VLAN 20: %+v", topo.Edges)
}

// The firewall runs as a VM on the Proxmox host that hangs from its own LAN
// switch: no link from the firewall to the host (it would make the host hang
// from two places); the firewall names its host instead.
func TestFirewallRunningAsAVMIsNotABranchOfItsHost(t *testing.T) {
	fw := model.Device{
		Key: "58:9c:fc:00:00:01", Name: "OPNsense", Host: model.Ptr("192.168.1.1"),
		MACs: []model.MACAddress{"58:9c:fc:00:00:01"}, Role: model.Ptr(model.DeviceRoleFirewall),
		Interfaces: []model.Interface{
			{Name: "pppoe0", Wan: model.Ptr(true), IPs: []string{"203.0.113.7/32"}},
			{Name: "lan", IPs: []string{"192.168.1.1/24"}},
		},
		Neighbors: []model.Neighbor{{
			LocalPort: "net0", Protocol: model.Ptr(model.NeighborProtocolOther),
			RemoteName: model.Ptr("pve"), RemoteIP: model.Ptr("192.168.1.50"),
		}},
	}
	pve := model.Device{Key: "proxmox:pve", Name: "pve", Host: model.Ptr("192.168.1.50"), Role: model.Ptr(model.DeviceRoleServer)}
	topo := topology.Build([]topology.Source{
		{IntegrationID: 1, Online: true, Devices: []model.Device{fw}},
		{IntegrationID: 7, Online: true, Devices: []model.Device{pve}},
	})
	for _, e := range topo.Edges {
		if e.Kind == topology.EdgeLLDP && ((e.Source == "dev:58:9c:fc:00:00:01" && e.Target == "dev:7:proxmox:pve") || (e.Target == "dev:58:9c:fc:00:00:01" && e.Source == "dev:7:proxmox:pve")) {
			t.Fatalf("a link between the firewall and its host: %+v", e)
		}
	}
	for _, n := range topo.Nodes {
		if n.ID == "dev:58:9c:fc:00:00:01" && n.RunsOn != "dev:7:proxmox:pve" {
			t.Fatalf("runs on %q", n.RunsOn)
		}
	}
}

// A VM reported by its hypervisor without IPs (no guest agent) takes the
// address the scan found for its MAC: one node, not the VM plus an "IP".
func TestDeviceWithoutIPTakesTheAddressOfItsMAC(t *testing.T) {
	const vm = "bc:24:11:00:00:a1"
	guest := model.Device{Key: vm, Name: "haos", MACs: []model.MACAddress{vm}, Role: model.Ptr(model.DeviceRoleServer)}
	scan := model.Device{
		Key: "gw", Name: "Gateway", Host: model.Ptr("192.168.1.1"), IPs: []string{"192.168.1.1"}, Role: model.Ptr(model.DeviceRoleRouter),
		Hosts: []model.Host{{IP: "192.168.1.54", MAC: model.Ptr(model.MACAddress(vm)), OpenPorts: []uint16{8123}}},
	}
	topo := topology.Build([]topology.Source{
		{IntegrationID: 2, Online: true, Devices: []model.Device{scan}},
		{IntegrationID: 7, Online: true, Devices: []model.Device{guest}},
	})
	var with []string
	for _, n := range topo.Nodes {
		if n.IP == "192.168.1.54" || n.MAC == vm {
			with = append(with, n.ID)
		}
	}
	if !reflect.DeepEqual(with, []string{"dev:" + vm}) {
		t.Fatalf("nodes of the VM: %v", with)
	}
}

// A lab router running as a VM, its "WAN" an address in the firewall's LAN
// (double NAT): not the center of the network. It keeps its link to its host
// and gets no WAN node; the main firewall does.
func TestNestedRouterIsNotTheCenter(t *testing.T) {
	fw := model.Device{
		Key: "58:9c:fc:00:00:01", Name: "fw", Host: model.Ptr("192.168.1.1"),
		MACs: []model.MACAddress{"58:9c:fc:00:00:01"}, Role: model.Ptr(model.DeviceRoleFirewall),
		Interfaces: []model.Interface{
			{Name: "wan", Wan: model.Ptr(true), IPs: []string{"203.0.113.7/24"}},
			{Name: "lan", IPs: []string{"192.168.1.1/24"}},
		},
	}
	lab := model.Device{
		Key: "bc:24:11:00:00:99", Name: "lab-router", Host: model.Ptr("192.168.1.99"),
		MACs: []model.MACAddress{"bc:24:11:00:00:99"}, Role: model.Ptr(model.DeviceRoleRouter),
		Interfaces: []model.Interface{
			{Name: "wan", Wan: model.Ptr(true), IPs: []string{"192.168.1.99/24"}},
			{Name: "lan", IPs: []string{"10.99.0.1/24"}},
		},
		Neighbors: []model.Neighbor{{LocalPort: "net0", Protocol: model.Ptr(model.NeighborProtocolOther), RemoteName: model.Ptr("pve"), RemoteIP: model.Ptr("192.168.1.50")}},
	}
	pve := model.Device{Key: "proxmox:pve", Name: "pve", Host: model.Ptr("192.168.1.50"), Role: model.Ptr(model.DeviceRoleServer)}
	topo := topology.Build([]topology.Source{
		{IntegrationID: 1, Online: true, Devices: []model.Device{fw}},
		{IntegrationID: 2, Online: true, Devices: []model.Device{lab}},
		{IntegrationID: 7, Online: true, Devices: []model.Device{pve}},
	})
	wans, labUnderHost := 0, false
	for _, n := range topo.Nodes {
		if n.Kind == topology.KindWAN {
			wans++
		}
	}
	for _, e := range topo.Edges {
		if e.Source == "dev:7:proxmox:pve" && e.Target == "dev:bc:24:11:00:00:99" {
			labUnderHost = true
		}
	}
	if wans != 1 || !labUnderHost {
		t.Fatalf("WAN nodes %d (want 1, the firewall's); lab router under its host: %v", wans, labUnderHost)
	}
}

// The firewall's own integration reports it as a firewall with its WAN; the
// hypervisor's reports the same machine (same MAC) as one more VM — a server
// with a link to its host. Merged, it is still the center: no link to the host.
func TestFirewallReportedAsAVMByItsHost(t *testing.T) {
	const fwMAC = "58:9c:fc:00:00:01"
	fw := model.Device{
		Key: fwMAC, Name: "OPNsense", Host: model.Ptr("192.168.1.1"),
		MACs: []model.MACAddress{fwMAC}, Role: model.Ptr(model.DeviceRoleFirewall),
		Interfaces: []model.Interface{
			{Name: "pppoe1", Wan: model.Ptr(true), IPs: []string{"100.64.67.216/32"}},
			{Name: "lan", IPs: []string{"192.168.1.1/24"}},
		},
	}
	asVM := model.Device{
		Key: fwMAC, Name: "OPNsense", MACs: []model.MACAddress{fwMAC}, Role: model.Ptr(model.DeviceRoleServer),
		Neighbors: []model.Neighbor{{LocalPort: "net0", Protocol: model.Ptr(model.NeighborProtocolOther), RemoteName: model.Ptr("pve"), RemoteIP: model.Ptr("192.168.1.50"), RemotePort: model.Ptr("vmbr1")}},
	}
	pve := model.Device{Key: "proxmox:pve", Name: "pve", Host: model.Ptr("192.168.1.50"), Role: model.Ptr(model.DeviceRoleServer)}
	topo := topology.Build([]topology.Source{
		{IntegrationID: 3, Online: true, Devices: []model.Device{fw}},
		{IntegrationID: 7, Online: true, Devices: []model.Device{pve, asVM}},
	})
	for _, e := range topo.Edges {
		if e.Kind == topology.EdgeLLDP && e.Target == "dev:7:proxmox:pve" {
			t.Fatalf("the firewall links to its host: %+v", e)
		}
	}
}
