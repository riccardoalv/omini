package insights

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

func TestMoreRules(t *testing.T) {
	now := time.Unix(1_790_000_000, 0).UTC()
	sw := topology.Node{
		ID: "dev:sw", Kind: topology.KindDevice, Role: "switch", Label: "core", Online: true,
		Web: []topology.WebApp{{Port: 80, URL: "http://192.168.1.2/"}},
		Device: &model.Device{
			Interfaces: []model.Interface{
				{Name: "Port 1", Up: model.Ptr(true), Duplex: model.Ptr(model.InterfaceDuplexHalf)},
				{Name: "Port 2", Up: model.Ptr(true), Duplex: model.Ptr(model.InterfaceDuplexFull)},
				{Name: "SFP 1", Up: model.Ptr(true), Transceiver: &model.Transceiver{RxPowerDBM: model.Ptr(-23.4)}},
				{Name: "SFP 2", Up: model.Ptr(true), Transceiver: &model.Transceiver{RxPowerDBM: model.Ptr(-12.0), RxPowerLowDBM: model.Ptr(-10.0)}},
				{Name: "SFP 3", Up: model.Ptr(true), Transceiver: &model.Transceiver{RxPowerDBM: model.Ptr(-5.0)}},
			},
		},
	}
	fw := topology.Node{
		ID: "dev:fw", Kind: topology.KindDevice, Role: "firewall", Label: "fw", Online: true,
		Web: []topology.WebApp{{Port: 443, URL: "https://192.168.1.1/"}},
		Device: &model.Device{
			DhcpPools:      []model.DhcpPool{{Network: "192.168.1.0/24", Total: model.Ptr(uint64(200)), Used: model.Ptr(uint64(190))}, {Network: "10.0.0.0/24", Total: model.Ptr(uint64(200)), Used: model.Ptr(uint64(20))}},
			FirewallStates: &model.FirewallStates{Current: model.Ptr(uint64(95_000)), Limit: model.Ptr(uint64(100_000))},
		},
	}
	tv := topology.Node{ID: "mac:tv", Kind: topology.KindClient, Label: "TV", Online: true}
	cam := topology.Node{ID: "mac:cam", Kind: topology.KindClient, Label: "camera", Online: true, OpenPorts: []int{23, 80}}
	pve := topology.Node{ID: "mac:pve", Kind: topology.KindClient, Label: "pve", Online: true, Product: "proxmox"}
	// A Proxmox already read by a plugin of another type (not a built-in scan): no suggestion.
	pve2 := topology.Node{ID: "dev:pve2", Kind: topology.KindDevice, Label: "pve2", Online: true, Product: "proxmox", IntegrationID: 9, Device: &model.Device{}}
	vm := topology.Node{ID: "mac:vm", Kind: topology.KindClient, Label: "vm", Online: true, Brand: "proxmox"}
	ap := topology.Node{ID: "mac:ap", Kind: topology.KindClient, Label: "halo", Online: true, Brand: "mercusys"}
	in := Input{
		Now: now,
		Topology: topology.Topology{
			Nodes: []topology.Node{sw, fw, tv, cam, pve, pve2, vm, ap},
			Edges: []topology.Edge{
				{ID: "e1", Source: "dev:sw", SourcePort: "Port 3", Target: "mac:tv", Kind: topology.EdgeFDB, SpeedMbps: 100},
				{ID: "e2", Source: "dev:sw", SourcePort: "Port 4", Target: "mac:cam", Kind: topology.EdgeFDB, SpeedMbps: 1000},
				{ID: "e3", Source: "dev:fw", SourcePort: "igc1", Target: "dev:sw", Kind: topology.EdgeLLDP, SpeedMbps: 100},
			},
		},
		History: History{
			Flaps:   map[string]int{"dev:sw|Port 2": 4, "dev:sw|Port 1": 1},
			Reboots: map[string]time.Time{"dev:fw": now.Add(-10 * time.Minute), "dev:sw": now.Add(-2 * time.Hour)},
		},
		Detectors: []Detector{
			{Type: "proxmox", Name: "Proxmox VE", Products: []string{"proxmox"}},
			{Type: "mercusys", Name: "Mercusys Halo", Brands: []string{"mercusys"}},
		},
		Configured: map[string]bool{"mercusys": true},
	}
	for i := range 5 {
		in.NewDevices = append(in.NewDevices, NewDevice{NodeID: "mac:tv", FirstSeen: now.Add(-time.Duration(i) * time.Minute)})
	}
	got := byRule(Evaluate(in))

	check := func(rule string, n int) []Insight {
		t.Helper()
		if len(got[rule]) != n {
			t.Fatalf("%s: %d insights, want %d: %+v", rule, len(got[rule]), n, got[rule])
		}
		return got[rule]
	}
	// The TV at 100 Mbps; the 100 Mbps uplink between network devices is slow_uplink's.
	if fe := check("fast_ethernet", 1); fe[0].NodeID != "mac:tv" || fe[0].Severity != Warning {
		t.Fatalf("fast ethernet: %+v", fe)
	}
	// The Proxmox host (product), not the VM (only its MAC's brand); Mercusys is set up.
	if ia := check("integration_available", 1); ia[0].NodeID != "mac:pve" || ia[0].Params["integration"] != "Proxmox VE" {
		t.Fatalf("integration available: %+v", ia)
	}
	if fl := check("link_flapping", 1); fl[0].Params["iface"] != "Port 2" {
		t.Fatalf("flapping: %+v", fl)
	}
	if hd := check("half_duplex", 1); hd[0].Params["iface"] != "Port 1" {
		t.Fatalf("half duplex: %+v", hd)
	}
	if rb := check("device_rebooted", 1); rb[0].NodeID != "dev:fw" {
		t.Fatalf("rebooted: %+v", rb)
	}
	// SFP 1 below -20 dBm, SFP 2 below its module's own threshold.
	check("sfp_low_rx", 2)
	if dp := check("dhcp_pool_full", 1); dp[0].Params["pct"] != 95.0 {
		t.Fatalf("dhcp pool: %+v", dp)
	}
	check("firewall_states_full", 1)
	// Telnet on the camera; the switch's admin page only over HTTP (the firewall has HTTPS).
	is := check("insecure_service", 2)
	for _, i := range is {
		if i.NodeID == "dev:fw" {
			t.Fatalf("an HTTPS admin page flagged: %+v", i)
		}
	}
	if b := check("new_devices_burst", 1); b[0].Params["count"] != 5 || b[0].Severity != Warning {
		t.Fatalf("burst: %+v", b)
	}
}

// An IP phone announces itself by LLDP: a 100 Mbps link to it is not a slow
// uplink between network devices (it is fast_ethernet's).
func TestAnLLDPPhoneIsNotInfrastructure(t *testing.T) {
	sw := topology.Node{ID: "dev:sw", Kind: topology.KindDevice, Role: "switch", Label: "sw", Online: true, Device: &model.Device{}}
	phone := topology.Node{ID: "lldp:phone", Kind: topology.KindUnmanaged, Label: "SEPF87B20A1B2C3", Model: "Cisco IP Phone 8845", Online: true}
	ap := topology.Node{ID: "lldp:ap", Kind: topology.KindUnmanaged, Label: "U6-Lite", Model: "U6-Lite", Online: true}
	in := Input{Topology: topology.Topology{
		Nodes: []topology.Node{sw, phone, ap},
		Edges: []topology.Edge{
			{ID: "1", Source: "dev:sw", SourcePort: "Port 20", Target: "lldp:phone", Kind: topology.EdgeLLDP, SpeedMbps: 100},
			{ID: "2", Source: "dev:sw", SourcePort: "Port 21", Target: "lldp:ap", Kind: topology.EdgeLLDP, SpeedMbps: 100},
		},
	}}
	got := byRule(Evaluate(in))
	if len(got["slow_uplink"]) != 1 || got["slow_uplink"][0].NodeID != "lldp:ap" {
		t.Fatalf("slow uplinks: %+v", got["slow_uplink"])
	}
	if len(got["fast_ethernet"]) != 1 || got["fast_ethernet"][0].NodeID != "lldp:phone" {
		t.Fatalf("fast ethernet: %+v", got["fast_ethernet"])
	}
}

// A firewall whose page was read on port 80 but that also serves 443 (it
// redirects to HTTPS): not an HTTP-only admin page.
func TestHTTPSPortMeansNotHTTPOnly(t *testing.T) {
	fw := topology.Node{
		ID: "dev:fw", Kind: topology.KindDevice, Role: "firewall", Label: "OPNsense", Online: true, Device: &model.Device{},
		Web: []topology.WebApp{{Port: 80, URL: "http://192.168.1.1/"}}, OpenPorts: []int{22, 53, 80, 443},
	}
	got := byRule(Evaluate(Input{Topology: topology.Topology{Nodes: []topology.Node{fw}}}))
	if len(got["insecure_service"]) != 0 {
		t.Fatalf("flagged: %+v", got["insecure_service"])
	}
}

func TestDiscoveryLimited(t *testing.T) {
	if got := byRule(Evaluate(Input{})); len(got["discovery_limited"]) != 0 {
		t.Fatal("raised with nothing limited")
	}
	got := byRule(Evaluate(Input{DiscoveryLimited: []string{"host_network", "multicast"}}))
	if l := got["discovery_limited"]; len(l) != 1 || l[0].Params["limits"] != "host_network,multicast" || l[0].Severity != Warning {
		t.Fatalf("limited: %+v", l)
	}
}

// Devices made with a 100 Mbps port (cameras, UPS cards, air conditioners,
// badge readers...) raise no fast Ethernet alert; a computer or a TV does.
func TestFastEthernetSkipsDevicesMadeFor100Mbps(t *testing.T) {
	sw := topology.Node{ID: "dev:sw", Kind: topology.KindDevice, Role: "switch", Label: "sw", Online: true, Device: &model.Device{}}
	nodes := []topology.Node{sw}
	var edges []topology.Edge
	for i, typ := range []string{"camera", "ups", "air_conditioner", "smart_home", "appliance", "solar_inverter", "ip_phone", "computer", "tv"} {
		id := "mac:" + typ
		nodes = append(nodes, topology.Node{ID: id, Kind: topology.KindClient, Label: typ, Type: typ, Online: true})
		edges = append(edges, topology.Edge{
			ID: id, Source: "dev:sw", SourcePort: fmt.Sprint("Port ", i+1), Target: id, Kind: topology.EdgeFDB, SpeedMbps: 100,
		})
	}
	got := byRule(Evaluate(Input{Topology: topology.Topology{Nodes: nodes, Edges: edges}}))
	var alerted []string
	for _, f := range got["fast_ethernet"] {
		alerted = append(alerted, f.NodeID)
	}
	sort.Strings(alerted)
	if want := []string{"mac:computer", "mac:tv"}; !reflect.DeepEqual(alerted, want) {
		t.Fatalf("fast Ethernet alerts on %v, want %v", alerted, want)
	}
}
