package insights

import (
	"sort"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// keysOf runs one rule and returns the keys it found, sorted, with their
// severities.
func keysOf(t *testing.T, rule string, in Input) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, i := range Evaluate(in) {
		if i.Rule == rule {
			out[i.Key] = i.Severity
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestServiceStopped(t *testing.T) {
	srv := topology.Node{
		ID: "dev:srv", Kind: topology.KindDevice, Label: "srv", Online: true,
		Device: &model.Device{Services: []model.Service{
			{Name: "unbound", Enabled: model.Ptr(true), Running: model.Ptr(false), Description: model.Ptr("DNS resolver")},
			{Name: "sshd", Enabled: model.Ptr(true), Running: model.Ptr(true)},
			{Name: "ntpd", Enabled: model.Ptr(false), Running: model.Ptr(false)}, // off on purpose
			{Name: "cron", Running: model.Ptr(false)},                            // not said to be enabled
			{Name: "dhcpd", Enabled: model.Ptr(true)},                            // running unknown
		}},
	}
	off := topology.Node{
		ID: "dev:off", Kind: topology.KindDevice, Label: "off",
		Device: &model.Device{Services: []model.Service{{Name: "x", Enabled: model.Ptr(true), Running: model.Ptr(false)}}},
	}
	in := Input{Now: time.Unix(0, 0), Topology: topology.Topology{Nodes: []topology.Node{srv, off}}}
	got := keysOf(t, "service_stopped", in)
	if len(got) != 1 || got["service_stopped|dev:srv|unbound"] != Warning {
		t.Fatalf("got %v", got)
	}
	for _, i := range Evaluate(in) {
		if i.Rule == "service_stopped" && (i.Params["service"] != "unbound" || i.Params["description"] != "DNS resolver") {
			t.Fatalf("params %v", i.Params)
		}
	}
}

func TestVPNDown(t *testing.T) {
	now := time.Unix(1_790_000_000, 0).UTC()
	fw := topology.Node{
		ID: "dev:fw", Kind: topology.KindDevice, Label: "fw", Online: true,
		Device: &model.Device{VpnPeers: []model.VpnPeer{
			{Name: "office", Protocol: "wireguard", Connected: model.Ptr(false)},                                                // connected an hour ago (memory)
			{Name: "branch", Protocol: "ipsec", Connected: model.Ptr(false), LastHandshake: model.Ptr(now.Add(-3 * time.Hour))}, // handshake 3 h ago
			{Name: "phone", Protocol: "wireguard", Connected: model.Ptr(false), LastHandshake: model.Ptr(now.Add(-72 * time.Hour))},
			{Name: "laptop", Protocol: "wireguard", Connected: model.Ptr(false)}, // never connected
			{Name: "up", Protocol: "wireguard", Connected: model.Ptr(true)},
			{Name: "old", Protocol: "wireguard", Connected: model.Ptr(false)}, // connected two days ago
		}},
	}
	in := Input{
		Now: now, Topology: topology.Topology{Nodes: []topology.Node{fw}},
		History: History{VPNConnected: map[string]time.Time{
			"dev:fw|office": now.Add(-time.Hour), "dev:fw|old": now.Add(-48 * time.Hour), "dev:fw|up": now,
		}},
	}
	got := sortedKeys(keysOf(t, "vpn_down", in))
	want := []string{"vpn_down|dev:fw|branch", "vpn_down|dev:fw|office"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v", got)
	}
}

func TestRebootPending(t *testing.T) {
	dev := func(id string, f *model.Firmware) topology.Node {
		return topology.Node{ID: id, Kind: topology.KindDevice, Label: id, Online: true, Device: &model.Device{Firmware: f}}
	}
	in := Input{Now: time.Unix(0, 0), Topology: topology.Topology{Nodes: []topology.Node{
		dev("dev:a", &model.Firmware{NeedsReboot: model.Ptr(true)}),                                    // no update pending
		dev("dev:b", &model.Firmware{NeedsReboot: model.Ptr(true), UpdateAvailable: model.Ptr(true)}),  // and an update
		dev("dev:c", &model.Firmware{NeedsReboot: model.Ptr(false), UpdateAvailable: model.Ptr(true)}), // no restart asked
		dev("dev:d", nil),
	}}}
	got := keysOf(t, "reboot_pending", in)
	if len(got) != 2 || got["reboot_pending|dev:a"] != Info || got["reboot_pending|dev:b"] != Info {
		t.Fatalf("got %v", got)
	}
	// update_pending keeps its own logic.
	if u := keysOf(t, "update_pending", in); len(u) != 2 {
		t.Fatalf("update_pending: %v", u)
	}
}

func TestWifiOpen(t *testing.T) {
	open := model.Ptr(model.WifiNetworkSecurityOpen)
	ap := func(id string, integration int64, nets ...model.WifiNetwork) topology.Node {
		return topology.Node{
			ID: id, Kind: topology.KindDevice, Role: "ap", Label: id, Online: true, IntegrationID: integration,
			Device: &model.Device{WifiNetworks: nets},
		}
	}
	in := Input{Now: time.Unix(0, 0), Topology: topology.Topology{Nodes: []topology.Node{
		// A controller reports its site's SSIDs on each access point.
		ap("dev:ap2", 7, model.WifiNetwork{SSID: "Cafe", Security: open}, model.WifiNetwork{SSID: "Guest", Security: open, Vlan: model.Ptr(uint16(50))}),
		ap("dev:ap1", 7, model.WifiNetwork{SSID: "Cafe", Security: open}, model.WifiNetwork{SSID: "Guest", Security: open, Vlan: model.Ptr(uint16(50))}),
		ap("dev:ap3", 8,
			model.WifiNetwork{SSID: "Home", Security: model.Ptr(model.WifiNetworkSecurityWpa2)},
			model.WifiNetwork{SSID: "Old", Security: open, Enabled: model.Ptr(false)},
			model.WifiNetwork{SSID: "Lan", Security: open, Vlan: model.Ptr(uint16(1))}, // the untagged LAN: no guest network
		),
	}}}
	got := keysOf(t, "wifi_open", in)
	want := map[string]string{"wifi_open|7|Cafe": Warning, "wifi_open|7|Guest": Info, "wifi_open|8|Lan": Warning}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, s := range want {
		if got[k] != s {
			t.Errorf("%s: %q, want %q", k, got[k], s)
		}
	}
	for _, i := range Evaluate(in) {
		if i.Key == "wifi_open|7|Cafe" && (i.NodeID != "dev:ap1" || i.Params["ssid"] != "Cafe") {
			t.Errorf("on %s with %v", i.NodeID, i.Params)
		}
	}
}

func TestHotSensor(t *testing.T) {
	kind := func(k model.TemperatureKind) *model.TemperatureKind { return &k }
	nas := topology.Node{
		ID: "dev:nas", Kind: topology.KindDevice, Label: "nas", Online: true,
		Device: &model.Device{Temperatures: []model.Temperature{
			{Sensor: "ada0", Kind: kind(model.TemperatureKindDisk), Celsius: 58},
			{Sensor: "ada1", Kind: kind(model.TemperatureKindDisk), Celsius: 55}, // at the limit, not above
			{Sensor: "board", Kind: kind(model.TemperatureKindBoard), Celsius: 81.2},
			{Sensor: "CPU 0", Kind: kind(model.TemperatureKindCPU), Celsius: 99}, // hot_cpu's
			{Sensor: "x", Celsius: 99}, // no kind
		}},
	}
	got := sortedKeys(keysOf(t, "hot_sensor", Input{Now: time.Unix(0, 0), Topology: topology.Topology{Nodes: []topology.Node{nas}}}))
	if len(got) != 2 || got[0] != "hot_sensor|dev:nas|ada0" || got[1] != "hot_sensor|dev:nas|board" {
		t.Fatalf("got %v", got)
	}
}

func TestHighSwap(t *testing.T) {
	dev := func(id string, swap *float64) topology.Node {
		return topology.Node{ID: id, Kind: topology.KindDevice, Label: id, Online: true, Device: &model.Device{SwapPct: swap}}
	}
	in := Input{Now: time.Unix(0, 0), Topology: topology.Topology{Nodes: []topology.Node{
		dev("dev:a", model.Ptr(72.0)), dev("dev:b", model.Ptr(50.0)), dev("dev:c", nil),
	}}}
	if got := keysOf(t, "high_swap", in); len(got) != 1 || got["high_swap|dev:a"] != Warning {
		t.Fatalf("got %v", got)
	}
}

// A gateway not tied by name to a WAN interface is told on its router; one a
// WAN node carries is told there only.
func TestWANGatewayWithoutWANInterface(t *testing.T) {
	gw := func(name, iface string, s model.GatewayStatus) model.Gateway {
		return model.Gateway{Name: name, Interface: model.Ptr(iface), Status: s}
	}
	dev := &model.Device{
		Interfaces: []model.Interface{{Name: "igb0", Wan: model.Ptr(true)}, {Name: "igb1"}},
		Gateways: []model.Gateway{
			gw("WAN_DHCP", "igb0", model.GatewayStatusDown),  // on the WAN node
			gw("LTE", "ue0", model.GatewayStatusDown),        // an interface not marked WAN
			gw("VPN_GW", "wg0", model.GatewayStatusDegraded), // degraded
			gw("SPARE", "igb2", model.GatewayStatusUp),
			{Name: "NOIF", Status: model.GatewayStatusDown}, // no interface at all
		},
	}
	fw := topology.Node{
		ID: "dev:fw", Kind: topology.KindDevice, Label: "fw", Online: true, Device: dev,
		Reports: []topology.Report{{IntegrationID: 1, Device: dev}},
	}
	wan := topology.Node{ID: "wan:dev:fw:igb0", Kind: topology.KindWAN, Label: "WAN", WAN: &topology.WANLink{
		Interface: "igb0", Gateways: []model.Gateway{dev.Gateways[0]},
	}}
	in := Input{Now: time.Unix(0, 0), Topology: topology.Topology{Nodes: []topology.Node{fw, wan}}}
	down := sortedKeys(keysOf(t, "wan_down", in))
	want := []string{"wan_down|dev:fw|LTE", "wan_down|dev:fw|NOIF", "wan_down|wan:dev:fw:igb0|WAN_DHCP"}
	if len(down) != len(want) {
		t.Fatalf("down: %v", down)
	}
	for i := range want {
		if down[i] != want[i] {
			t.Fatalf("down: %v", down)
		}
	}
	if d := keysOf(t, "wan_degraded", in); len(d) != 1 || d["wan_degraded|dev:fw|VPN_GW"] != Warning {
		t.Fatalf("degraded: %v", d)
	}
}
