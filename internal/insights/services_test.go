package insights

import (
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

func TestDHCPAndDNSRules(t *testing.T) {
	fw := topology.Node{
		ID: "dev:fw", Kind: topology.KindDevice, Label: "fw", Role: "firewall", IP: "192.168.1.1",
		Device: &model.Device{Key: "fw"},
	}
	pihole := topology.Node{ID: "mac:53", Kind: topology.KindClient, Label: "pihole", IP: "192.168.1.53"}
	topo := topology.Topology{
		Nodes: []topology.Node{fw, pihole},
		DhcpChecks: []model.DhcpCheck{
			{Interface: "eth0", Offers: []model.DhcpOffer{
				{ServerIP: "192.168.1.1"},
				{ServerIP: "192.168.1.250", ServerMAC: model.Ptr(model.MACAddress("00:11:22:33:44:55"))},
			}},
			{Interface: "eth1"}, // nobody answered
			{Interface: "wlan0", Error: model.Ptr("no raw sockets")}, // could not check: no alert
		},
		DnsChecks: []model.DnsCheck{
			{
				Server: "192.168.1.1", External: &model.DnsQuery{Name: "example.com", Ok: true, RttMs: model.Ptr(12.0)},
				Local: &model.DnsQuery{Name: "nas.home.lan", Ok: false, Error: model.Ptr("not found")},
			},
			{Server: "192.168.1.53", External: &model.DnsQuery{Name: "example.com", Ok: false, Error: model.Ptr("timeout")}},
			{Server: "192.168.1.54", External: &model.DnsQuery{Name: "example.com", Ok: true, RttMs: model.Ptr(800.0)}},
		},
	}
	got := map[string][]Insight{}
	for _, i := range Evaluate(Input{Topology: topo, Now: time.Unix(0, 0)}) {
		got[i.Rule] = append(got[i.Rule], i)
	}
	if l := got["dhcp_no_offer"]; len(l) != 1 || l[0].Params["iface"] != "eth1" || l[0].Severity != Critical {
		t.Errorf("no offer: %+v", l)
	}
	if l := got["dhcp_rogue"]; len(l) != 1 || l[0].Params["ip"] != "192.168.1.250" || l[0].Params["mac"] != "00:11:22:33:44:55" {
		t.Errorf("rogue (the firewall is known): %+v", l)
	}
	if l := got["dns_down"]; len(l) != 1 || l[0].NodeID != "mac:53" || l[0].Params["error"] != "timeout" {
		t.Errorf("down (about the server's node): %+v", l)
	}
	if l := got["dns_slow"]; len(l) != 1 || l[0].Params["server"] != "192.168.1.54" || l[0].NodeID != "" {
		t.Errorf("slow (not on the map): %+v", l)
	}
	if l := got["dns_local"]; len(l) != 1 || l[0].Params["name"] != "nas.home.lan" || l[0].NodeID != "dev:fw" {
		t.Errorf("local: %+v", l)
	}
}

// One server answering is the network's DHCP, whoever it is.
func TestASingleDHCPServerIsNotRogue(t *testing.T) {
	topo := topology.Topology{DhcpChecks: []model.DhcpCheck{{Interface: "eth0", Offers: []model.DhcpOffer{{ServerIP: "10.0.0.9"}}}}}
	for _, i := range Evaluate(Input{Topology: topo, Now: time.Unix(0, 0)}) {
		if i.Rule == "dhcp_rogue" {
			t.Fatalf("rogue: %+v", i)
		}
	}
}

func TestVLANsOutsideTheCatalog(t *testing.T) {
	sw := topology.Node{ID: "dev:sw", Kind: topology.KindDevice, Label: "sw", Device: &model.Device{
		Key: "sw", Vlans: []model.Vlan{{ID: 1}, {ID: 30, Name: model.Ptr("lab")}},
	}}
	in := Input{Topology: topology.Topology{Nodes: []topology.Node{sw}}, Now: time.Unix(0, 0)}
	if got := vlanUnknown(in, index{}); got != nil {
		t.Fatalf("no catalog yet: %+v", got)
	}
	in.Vlans = map[uint16]string{1: "default"}
	got := vlanUnknown(in, index{})
	if len(got) != 1 || got[0].Params["vlan"] != uint16(30) || got[0].Params["name"] != "lab" {
		t.Fatalf("unknown: %+v", got)
	}
}

// TestNoAnswerToOminiWhileTheServerRuns: no offer reached Omini, but the
// router serves DHCP and its server runs: a warning about Omini's request,
// not "no DHCP server".
func TestNoAnswerToOminiWhileTheServerRuns(t *testing.T) {
	fw := topology.Node{ID: "dev:fw", Kind: topology.KindDevice, Label: "OPNsense", Online: true, Device: &model.Device{
		DhcpLeases: []model.DhcpLease{{IP: "192.168.1.20", MAC: "aa:00:00:00:00:20"}},
		Services: []model.Service{
			{Name: "dpinger/WAN_DHCP6", Description: model.Ptr("Gateway WAN_DHCP6"), Running: model.Ptr(true)},
			{Name: "kea-dhcp/v4", Description: model.Ptr("KEA DHCPv4 server"), Running: model.Ptr(true)},
		},
	}}
	topo := topology.Topology{Nodes: []topology.Node{fw}, DhcpChecks: []model.DhcpCheck{{Interface: "ens18"}}}
	got := byRule(Evaluate(Input{Topology: topo}))
	if l := got["dhcp_no_offer_omini"]; len(l) != 1 || l[0].Severity != Warning || l[0].Params["server"] != "OPNsense" || len(got["dhcp_no_offer"]) != 0 {
		t.Fatalf("running: %+v", got)
	}
	// Its server stopped: no DHCP server answers at all.
	fw.Device.Services[1].Running = model.Ptr(false)
	got = byRule(Evaluate(Input{Topology: topology.Topology{Nodes: []topology.Node{fw}, DhcpChecks: topo.DhcpChecks}}))
	if len(got["dhcp_no_offer"]) != 1 || len(got["dhcp_no_offer_omini"]) != 0 {
		t.Fatalf("stopped: %+v", got)
	}
}
