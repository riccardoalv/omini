package insights

import (
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

func flowHome() topology.Topology {
	return topology.Topology{Nodes: []topology.Node{
		{ID: "fw", Kind: topology.KindDevice, Label: "OPNsense", Type: "firewall"},
		{ID: "adguard", Kind: topology.KindDevice, Label: "AdGuard", Device: &model.Device{DnsLookups: []model.DnsLookup{{Client: "192.168.1.42", Domain: "example.com"}}}},
		{ID: "tv", Kind: topology.KindClient, Label: "TV", Type: "tv"},
		{ID: "nas", Kind: topology.KindClient, Label: "NAS", Type: "nas"},
		{ID: "laptop", Kind: topology.KindClient, Label: "Laptop", Type: "computer"},
	}}
}

func TestDNSBypass(t *testing.T) {
	in := Input{Topology: flowHome(), Flows: &Flows{DNSOutside: map[string][]string{
		"tv": {"dns.google", "1.1.1.1"}, "adguard": {"1.1.1.1"}, "fw": {"9.9.9.9"},
	}}}
	got := byRule(Evaluate(in))["dns_bypass"]
	if len(got) != 1 || got[0].NodeID != "tv" || got[0].Params["servers"] != "1.1.1.1, dns.google" {
		t.Fatalf("dns_bypass = %+v", got)
	}
	// Without a DNS server of its own on the network, going outside is normal.
	in.Topology.Nodes[1].Device = nil
	if got := byRule(Evaluate(in))["dns_bypass"]; len(got) != 0 {
		t.Fatalf("no local DNS: %+v", got)
	}
}

func TestUnusualUpload(t *testing.T) {
	in := Input{Topology: flowHome(), Flows: &Flows{Upload: map[string]Upload{
		"tv":     {LastHour: 900 << 20, Average: 10 << 20, Hours: 168},  // unusual
		"laptop": {LastHour: 900 << 20, Average: 400 << 20, Hours: 168}, // its usual
		"nas":    {LastHour: 5 << 30, Average: 1 << 20, Hours: 168},     // a server: backups
		"fw":     {LastHour: 300 << 20, Average: 1 << 20, Hours: 10},    // too little history
	}}}
	got := byRule(Evaluate(in))["upload_unusual"]
	if len(got) != 1 || got[0].NodeID != "tv" || got[0].Params["mb"] != uint64(900) || got[0].Params["usual_mb"] != uint64(10) {
		t.Fatalf("upload_unusual = %+v", got)
	}
}

func TestLANScan(t *testing.T) {
	in := Input{Topology: flowHome(), Flows: &Flows{Scans: map[string]int{"laptop": 60, "fw": 80, "gone": 70}}}
	got := byRule(Evaluate(in))["lan_scan"]
	if len(got) != 1 || got[0].NodeID != "laptop" || got[0].Params["targets"] != 60 {
		t.Fatalf("lan_scan = %+v", got)
	}
	for _, r := range []string{"lan_scan", "dns_bypass", "upload_unusual"} {
		if len(byRule(Evaluate(Input{Topology: flowHome()}))[r]) != 0 {
			t.Fatalf("%s fired without flows", r)
		}
	}
}
