package diagnostics

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/insights"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

func TestBundleHidesWhatIdentifiesTheNetwork(t *testing.T) {
	topo := map[string]any{
		"nodes": []any{
			map[string]any{
				"id": "dev:00:11:32:aa:bb:cc", "label": "Ricardo's NAS", "hostname": "ricardo-nas",
				"ip": "192.168.1.10", "mac": "00:11:32:aa:bb:cc", "ssid": "Casa Silva",
				"device": map[string]any{
					"key": "SN12345", "serial": "SN12345", "firmware": "1.0.2.15",
					"interfaces": []any{map[string]any{"name": "igc0", "description": "uplink to Ricardo's office", "ipv6": []any{"2804:14c:1234::10", "fe80::211:32ff:feaa:bbcc"}}},
					"vlans":      []any{map[string]any{"id": 20, "name": "IoT"}},
					"password":   "hunter2",
				},
			},
			map[string]any{"id": "dev:3:SN12345", "parent_id": "mac:da:a1:19:00:00:01", "port_labels": map[string]any{"ether2": "Ricardo's desk"}},
			map[string]any{"id": "mac:04:5d:4b:00:00:09", "reasons": []any{"vendor:Sony", "hostname:living-room-tv"}},
			map[string]any{"id": "ip:192.168.1.1", "web": []any{map[string]any{"port": 443, "url": "https://silva-router.lan/", "title": "Silva Router"}, map[string]any{"port": 80, "url": "http://192.168.1.1/"}}},
			map[string]any{"id": "wan", "wan": map[string]any{"ip": "177.45.10.2", "gateway": "177.45.10.1"}},
		},
	}
	out, err := Bundle(Input{
		Version: "1.4.0", Topology: topo,
		Integrations: []Integration{{ID: 3, Type: "opnsense", Enabled: true, Plugin: "0.5.3", Status: map[string]any{"error": "dial tcp 177.45.10.1:443: timeout"}}},
		Alerts:       []Alert{{Rule: "device_offline", Severity: "critical", NodeID: "dev:00:11:32:aa:bb:cc", Params: map[string]any{"name": "Ricardo's NAS"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	s := string(raw)
	for _, secret := range []string{"Ricardo", "ricardo", "Casa Silva", "SN12345", "hunter2", "aa:bb:cc", "177.45.10", "2804:14c", "feaa:bbcc", "da:a1:19", "living-room", "Silva"} {
		if strings.Contains(s, secret) {
			t.Errorf("bundle still holds %q:\n%s", secret, s)
		}
	}
	for _, kept := range []string{
		`"192.168.1.10"`, `"00:11:32:00:00:01"`, `"igc0"`, `"IoT"`, `"1.0.2.15"`, `"opnsense"`, `"0.5.3"`,
		`"device_offline"`, `"203.0.113.`, `"2001:db8::`, `"fe80::`, `"mac:02:00:00:`, `"ether2"`, `"1.4.0"`, `"vendor:Sony"`, `"hostname:name-`, `"https://name-`, `"http://192.168.1.1/"`,
	} {
		if !strings.Contains(s, kept) {
			t.Errorf("bundle lost %s:\n%s", kept, s)
		}
	}
}

func TestPlaceholdersAreStable(t *testing.T) {
	a := NewAnonymizer()
	if a.MAC("00:11:32:AA:BB:CC") != a.MAC("00-11-32-aa-bb-cc") {
		t.Error("one MAC got two placeholders")
	}
	first := a.Name("nas")
	if a.Name("nas") != first || a.Name("tv") == first {
		t.Error("names are not stable or collide")
	}
	if got := a.Text("dev:00:11:32:aa:bb:cc down, gateway 8.8.8.8 and 8.8.8.8"); got != "dev:00:11:32:00:00:01 down, gateway 203.0.113.2 and 203.0.113.2" {
		t.Errorf("text = %q", got)
	}
	if got := a.Text("at 10:30:00 on 2026-10-13"); got != "at 10:30:00 on 2026-10-13" {
		t.Errorf("a time was taken for an address: %q", got)
	}
}

// TestAlertParamsFromTheRules runs the real rules and checks that none of the
// names they put in an alert's parameters (the device, its parent, the
// access point, the gateway, a sensor, a mount point...) reaches the bundle.
func TestAlertParamsFromTheRules(t *testing.T) {
	now := time.Unix(1_790_000_000, 0).UTC()
	fw := topology.Node{
		ID: "dev:fw", Kind: topology.KindDevice, Role: "firewall", Label: "Silva Firewall", Online: true, IntegrationID: 2,
		Device: &model.Device{
			CPUPct:       model.Ptr(97.0),
			Storage:      []model.Storage{{Mount: "/mnt/ricardo-photos", TotalBytes: model.Ptr(uint64(100)), UsedBytes: model.Ptr(uint64(95))}},
			Temperatures: []model.Temperature{{Sensor: "Silva board sensor", Celsius: 90, Kind: model.Ptr(model.TemperatureKindCPU)}},
			Paused:       []model.PausedDevice{{MAC: "aa:00:00:00:00:01", Rule: model.Ptr("Block Ricardo's kids")}},
		},
	}
	topo := topology.Topology{
		Nodes: []topology.Node{
			fw,
			{ID: "dev:ap", Kind: topology.KindDevice, Role: "ap", Label: "Silva Office AP", Online: true, Device: &model.Device{}},
			{ID: "wan:dev:fw|pppoe0", Kind: topology.KindWAN, Label: "WAN", WAN: &topology.WANLink{
				Interface: "pppoe0", Gateways: []model.Gateway{{Name: "Silva ISP gateway", Status: model.GatewayStatusDown}},
			}},
			{ID: "mac:01", Kind: topology.KindClient, Label: "Ricardo's Phone", Online: true, IP: "192.168.1.10", MAC: "aa:00:00:00:00:01", ParentID: "dev:ap", SignalDBM: model.Ptr(int64(-85)), SSID: "Casa Silva"},
			{ID: "mac:02", Kind: topology.KindClient, Label: "Ricardo's TV", Online: true, IP: "192.168.1.10", MAC: "aa:00:00:00:00:02"},
			{ID: "seg:ap|Port 5", Kind: topology.KindSegment, Label: "Unmanaged", ParentID: "dev:ap", Port: "Port 5", MACCount: 4},
		},
		Edges: []topology.Edge{{ID: "e1", Source: "dev:fw", Target: "dev:ap", SourcePort: "igc1", SpeedMbps: 100, Kind: topology.EdgeLLDP}},
	}
	got := insights.Evaluate(insights.Input{
		Topology: topo, Now: now,
		Integrations: []insights.Integration{{ID: 2, OK: true}, {ID: 9, Name: "Silva Controller", Error: "timeout", Failures: 3}},
	})
	rules := map[string]bool{}
	var alerts []Alert
	for _, i := range got {
		rules[i.Rule] = true
		alerts = append(alerts, Alert{Rule: i.Rule, Severity: i.Severity, NodeID: i.NodeID, Params: i.Params})
	}
	for _, r := range []string{"weak_wifi", "slow_uplink", "unmanaged_switch", "wan_down", "duplicate_ip", "integration_failed", "disk_full", "hot_cpu"} {
		if !rules[r] {
			t.Fatalf("the rules did not raise %s: %v", r, rules)
		}
	}
	out, err := Bundle(Input{Alerts: alerts, Topology: map[string]any{"nodes": []any{fw}}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	s := string(raw)
	for _, secret := range []string{"Silva", "Ricardo", "ricardo"} {
		if strings.Contains(s, secret) {
			t.Errorf("bundle still holds %q:\n%s", secret, s)
		}
	}
	for _, kept := range []string{`"igc1"`, `"Port 5"`, `"192.168.1.10"`, `"weak_wifi"`} {
		if !strings.Contains(s, kept) {
			t.Errorf("bundle lost %s:\n%s", kept, s)
		}
	}
}

func TestBundleLeavesDeviceNotesOut(t *testing.T) {
	topo := map[string]any{"nodes": []any{map[string]any{"id": "dev:00:11:32:aa:bb:cc", "note": "admin password is hunter2"}}}
	out, err := Bundle(Input{Version: "1.6.1", Topology: topo})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "hunter2") || strings.Contains(string(raw), `"note"`) {
		t.Fatalf("a device's note never reaches the bundle: %s", raw)
	}
}

// A node's IPv6 addresses (ips6) and where they came from (NDP, DHCPv6
// leases, AAAA records, IPv6 gateways) become placeholders, the same one
// for an address however it is written.
func TestBundleHidesIPv6Addresses(t *testing.T) {
	topo := topology.Topology{Nodes: []topology.Node{{
		ID: "mac:00:11:32:aa:bb:cc", Kind: topology.KindClient, MAC: "00:11:32:aa:bb:cc",
		IPs6: []string{"2804:14c:1234::10", "fd12:3456::10", "fe80::211:32ff:feaa:bbcc"},
	}, {
		ID: "dev:58:9c:fc:00:00:01", Kind: topology.KindDevice, Device: &model.Device{
			Key: "58:9c:fc:00:00:01", Name: "fw",
			Ndp:        []model.ArpEntry{{IP: "2804:014c:1234:0000:0000:0000:0000:0010", MAC: "00:11:32:aa:bb:cc", Interface: model.Ptr("igc1")}},
			DhcpLeases: []model.DhcpLease{{IP: "2804:14c:1234::77", MAC: "00:11:32:aa:bb:cc"}},
			DnsRecords: []model.DnsRecord{{Name: "nas.lan", IP: "2804:14c:1234::10"}},
			Gateways:   []model.Gateway{{Name: "WAN_GWv6", Address: model.Ptr("fe80::1:2%pppoe0"), Status: model.GatewayStatusUp}},
		},
	}}}
	out, err := Bundle(Input{Version: "1.4.0", Topology: topo})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	s := string(raw)
	for _, secret := range []string{"2804", "fd12:3456", "feaa:bbcc", "fe80::1:2", "aa:bb:cc"} {
		if strings.Contains(s, secret) {
			t.Errorf("bundle still holds %q:\n%s", secret, s)
		}
	}
	for _, kept := range []string{`"ips6"`, `"ndp"`, `"2001:db8::`, `"fd00::`, `"fe80::`, `"igc1"`} {
		if !strings.Contains(s, kept) {
			t.Errorf("bundle lost %s:\n%s", kept, s)
		}
	}
	// The NDP entry, written in full, and the node's compact address are one.
	var b struct {
		Topology struct {
			Nodes []struct {
				IPs6   []string `json:"ips6"`
				Device *struct {
					Ndp []struct {
						IP string `json:"ip"`
					} `json:"ndp"`
				} `json:"device"`
			} `json:"nodes"`
		} `json:"topology"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	var ips6, ndp string
	for _, n := range b.Topology.Nodes {
		if len(n.IPs6) > 0 {
			ips6 = n.IPs6[0]
		}
		if n.Device != nil && len(n.Device.Ndp) > 0 {
			ndp = n.Device.Ndp[0].IP
		}
	}
	if ips6 == "" || ips6 != ndp {
		t.Errorf("placeholders differ: ips6 %q, ndp %q", ips6, ndp)
	}
}
