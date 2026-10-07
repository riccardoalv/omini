package topology_test

import (
	"encoding/json"
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
