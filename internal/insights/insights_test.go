package insights

import (
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

func byRule(list []Insight) map[string][]Insight {
	out := map[string][]Insight{}
	for _, i := range list {
		out[i.Rule] = append(out[i.Rule], i)
	}
	return out
}

func kind(k model.TemperatureKind) *model.TemperatureKind { return &k }

func iface(t model.InterfaceType) *model.InterfaceType { return &t }

func TestRules(t *testing.T) {
	now := time.Unix(1_790_000_000, 0).UTC()
	fw := topology.Node{
		ID: "dev:fw", Kind: topology.KindDevice, Role: "firewall", Label: "OPNsense", Online: true, IntegrationID: 2,
		Device: &model.Device{
			CPUPct: model.Ptr(97.0), MemPct: model.Ptr(91.0),
			Firmware: &model.Firmware{UpdateAvailable: model.Ptr(true), Latest: model.Ptr("25.7.1"), Updates: model.Ptr(uint64(12))},
			Storage: []model.Storage{
				{Mount: "/", TotalBytes: model.Ptr(uint64(100)), UsedBytes: model.Ptr(uint64(93))},
				{Mount: "/var", TotalBytes: model.Ptr(uint64(100)), UsedBytes: model.Ptr(uint64(81))},
				{Mount: "/tmp", TotalBytes: model.Ptr(uint64(100)), UsedBytes: model.Ptr(uint64(10))},
			},
			Temperatures: []model.Temperature{
				{Sensor: "CPU 0", Celsius: 72, Kind: kind(model.TemperatureKindCPU)},
				{Sensor: "ada0", Celsius: 90, Kind: kind(model.TemperatureKindDisk)}, // a disk: not the CPU
			},
			Interfaces: []model.Interface{
				{Name: "igc0", SpeedMbps: model.Ptr(uint64(1000)), Up: model.Ptr(true)},
				{Name: "bridge0", SpeedMbps: model.Ptr(uint64(1000)), Type: iface(model.InterfaceTypeBridge)},
			},
		},
		Traffic: map[string]topology.Rate{
			"igc0":    {RxBps: 900_000_000, TxBps: 10, RxErrors: 4},
			"bridge0": {RxBps: 950_000_000}, // not a physical port
		},
	}
	sw := topology.Node{ID: "dev:sw", Kind: topology.KindDevice, Role: "switch", Label: "Switch", Online: false, IntegrationID: 3, Device: &model.Device{}}
	ap := topology.Node{ID: "dev:ap", Kind: topology.KindDevice, Role: "ap", Label: "AP", Online: true, Device: &model.Device{}}
	topo := topology.Topology{
		Nodes: []topology.Node{
			fw, sw, ap,
			{ID: "wan:dev:fw|pppoe0", Kind: topology.KindWAN, Label: "WAN", WAN: &topology.WANLink{
				Interface: "pppoe0", Gateways: []model.Gateway{{Name: "GW", Status: model.GatewayStatusDegraded, LossPct: model.Ptr(12.0)}},
			}},
			{
				ID: "mac:01", Kind: topology.KindClient, Label: "Phone", Online: true, IP: "192.168.1.10", MAC: "aa:00:00:00:00:01",
				ParentID: "dev:ap", SignalDBM: model.Ptr(int64(-81)), SSID: "Home",
			},
			{ID: "mac:02", Kind: topology.KindClient, Label: "TV", Online: true, IP: "192.168.1.10", MAC: "aa:00:00:00:00:02"},
			{ID: "mac:03", Kind: topology.KindClient, Label: "Old", Online: false, IP: "192.168.1.11", MAC: "aa:00:00:00:00:03"},
			{ID: "mac:04", Kind: topology.KindClient, Label: "Old2", Online: true, IP: "192.168.1.11", MAC: "aa:00:00:00:00:04"},
			{ID: "seg:sw|Port 5", Kind: topology.KindSegment, Label: "Unmanaged", ParentID: "dev:sw", Port: "Port 5", MACCount: 4},
			{ID: "lldp:x", Kind: topology.KindUnmanaged, Label: "core-sw", ParentID: "dev:sw", Port: "Port 8"},
		},
		Edges: []topology.Edge{
			{ID: "e1", Source: "dev:fw", Target: "dev:ap", SourcePort: "igc1", SpeedMbps: 100, Kind: topology.EdgeLLDP},
			{ID: "e2", Source: "dev:fw", Target: "dev:sw", SourcePort: "igc0", SpeedMbps: 1000, Kind: topology.EdgeLLDP},
			{ID: "e3", Source: "dev:ap", Target: "mac:01", SpeedMbps: 54, Kind: topology.EdgeWifi},
		},
	}
	got := byRule(Evaluate(Input{
		Topology: topo,
		Integrations: []Integration{
			{ID: 2, Name: "OPNsense", OK: true},
			{ID: 3, Name: "Horaco", OK: false, Error: "timeout"},
			{ID: 4, Name: "Mercusys", OK: false, Error: "login refused"},
		},
		NewDevices: []NewDevice{{NodeID: "mac:02", FirstSeen: now.Add(-time.Hour)}, {NodeID: "mac:04", FirstSeen: now.Add(-48 * time.Hour)}},
		Now:        now,
	}))

	check := func(rule string, n int, sev string) []Insight {
		t.Helper()
		if len(got[rule]) != n {
			t.Fatalf("%s: %d insights, want %d: %+v", rule, len(got[rule]), n, got[rule])
		}
		if n > 0 && sev != "" && got[rule][0].Severity != sev {
			t.Fatalf("%s: severity %s, want %s", rule, got[rule][0].Severity, sev)
		}
		return got[rule]
	}
	if off := check("device_offline", 1, Critical); off[0].NodeID != "dev:sw" || off[0].Params["error"] != "timeout" {
		t.Fatalf("offline: %+v", off)
	}
	// Horaco's failure is the switch being offline; Mercusys has nothing on the map.
	if f := check("integration_failed", 1, Critical); f[0].Params["integration"] != "Mercusys" {
		t.Fatalf("integration: %+v", f)
	}
	check("wan_degraded", 1, Warning)
	if d := check("duplicate_ip", 1, Warning); d[0].Params["ip"] != "192.168.1.10" {
		t.Fatalf("duplicate: %+v", d) // the offline Old does not count
	}
	if u := check("update_pending", 1, Warning); u[0].Params["latest"] != "25.7.1" {
		t.Fatalf("update: %+v", u)
	}
	disks := check("disk_full", 2, "")
	if disks[0].Severity != Critical || disks[0].Params["mount"] != "/" || disks[1].Severity != Warning {
		t.Fatalf("disks: %+v", disks)
	}
	if h := check("hot_cpu", 1, Warning); h[0].Params["celsius"] != 72.0 {
		t.Fatalf("hot: %+v", h)
	}
	check("high_cpu", 1, Critical)
	check("high_memory", 1, Warning)
	if s := check("slow_uplink", 1, Warning); s[0].NodeID != "dev:ap" || s[0].Params["speed_mbps"] != uint64(100) {
		t.Fatalf("slow: %+v", s)
	}
	if e := check("interface_errors", 1, Warning); e[0].Params["iface"] != "igc0" {
		t.Fatalf("errors: %+v", e)
	}
	if w := check("weak_wifi", 1, Warning); w[0].Params["ap"] != "AP" {
		t.Fatalf("wifi: %+v", w)
	}
	if s := check("saturated_link", 1, Warning); s[0].Params["iface"] != "igc0" || s[0].Params["pct"] != 90.0 {
		t.Fatalf("saturated: %+v", s)
	}
	check("unmanaged_switch", 1, Info)
	check("unknown_neighbor", 1, Info)
	if n := check("new_device", 1, Info); n[0].NodeID != "mac:02" || n[0].Params["mac"] != "aa:00:00:00:00:02" {
		t.Fatalf("new: %+v", n)
	}
}

func TestEvaluateOrderAndKeys(t *testing.T) {
	list := Evaluate(Input{
		Topology: topology.Topology{Nodes: []topology.Node{
			{ID: "seg:a", Kind: topology.KindSegment, Label: "a"},
			{ID: "dev:b", Kind: topology.KindDevice, Label: "b", Device: &model.Device{}},
		}},
		Now: time.Unix(0, 0),
	})
	if len(list) != 2 || list[0].Severity != Critical || list[1].Severity != Info {
		t.Fatalf("order: %+v", list)
	}
	if list[0].Key != "device_offline|dev:b" || list[1].Key != "unmanaged_switch|seg:a" {
		t.Fatalf("keys: %+v", list)
	}
	// A quiet network: nothing to say.
	if got := Evaluate(Input{Now: time.Unix(0, 0)}); len(got) != 0 {
		t.Fatalf("empty: %+v", got)
	}
}
