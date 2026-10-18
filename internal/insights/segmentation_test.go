package insights

import (
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// segRouter is a firewall with a LAN on VLAN 1 and an IoT VLAN 30.
func segRouter(vlan30 bool) topology.Node {
	nets := []model.Network{{Vlan: 1, Gateway: model.Ptr("192.168.1.1/24")}}
	if vlan30 {
		nets = append(nets, model.Network{Vlan: 30, Gateway: model.Ptr("192.168.30.1/24")})
	}
	return topology.Node{
		ID: "dev:fw", Kind: topology.KindDevice, Label: "fw", Role: "firewall", IP: "192.168.1.1",
		Device: &model.Device{Networks: nets},
	}
}

func client(id, typ, ip string) topology.Node {
	return topology.Node{ID: id, Kind: topology.KindClient, Label: id, Type: typ, IP: ip, Online: true}
}

func TestIoTWithTrustedDevices(t *testing.T) {
	topo := topology.Topology{Nodes: []topology.Node{
		segRouter(true),
		client("cam", "camera", "192.168.1.40"),
		client("tv", "tv", "192.168.1.41"),
		client("nas", "nas", "192.168.1.10"),
		client("pc", "computer", "192.168.1.20"),
		client("printer", "printer", "192.168.1.50"),
		client("plug", "smart_home", "192.168.30.5"), // already on its own VLAN
	}}
	got := byRule(Evaluate(Input{Topology: topo, Vlans: map[uint16]string{1: "LAN", 30: "IoT"}}))["iot_with_trusted"]
	if len(got) != 1 {
		t.Fatalf("one insight for the LAN, none for the IoT VLAN: %+v", got)
	}
	p := got[0].Params
	if got[0].Severity != Info || got[0].NodeID != "dev:fw" || got[0].Key != "iot_with_trusted:192.168.1.0/24" {
		t.Fatalf("an info about the router's network: %+v", got[0])
	}
	if p["network"] != "192.168.1.0/24" || p["vlan"] != 1 || p["vlan_name"] != "LAN" ||
		p["iot"] != 2 || p["trusted"] != 2 || p["iot_devices"] != "cam, tv" || p["trusted_devices"] != "nas, pc" {
		t.Fatalf("params: %+v", p)
	}
}

func TestIoTWithTrustedNeedsVLANs(t *testing.T) {
	topo := topology.Topology{Nodes: []topology.Node{
		segRouter(false),
		client("cam", "camera", "192.168.1.40"),
		client("pc", "computer", "192.168.1.20"),
	}}
	if got := byRule(Evaluate(Input{Topology: topo}))["iot_with_trusted"]; len(got) != 0 {
		t.Fatalf("a network without VLANs has nowhere to move them: %+v", got)
	}
}

func TestIoTWithTrustedNeedsBoth(t *testing.T) {
	topo := topology.Topology{Nodes: []topology.Node{
		segRouter(true),
		client("cam", "camera", "192.168.1.40"),
		client("phone", "phone", "192.168.1.60"),
		client("pc", "computer", "192.168.30.20"),
	}}
	if got := byRule(Evaluate(Input{Topology: topo}))["iot_with_trusted"]; len(got) != 0 {
		t.Fatalf("IoT without a server or computer beside it is fine: %+v", got)
	}
}

func TestIoTWithTrustedSkipsHiddenAndUsesCorrectedTypes(t *testing.T) {
	cam := client("cam", "camera", "192.168.1.40")
	cam.Hidden = true
	pi := client("pi", "server", "192.168.1.30") // the user said this "TV" is a server
	tv := client("tv", "tv", "192.168.1.41")
	topo := topology.Topology{Nodes: []topology.Node{segRouter(true), cam, pi, tv}}
	got := byRule(Evaluate(Input{Topology: topo}))["iot_with_trusted"]
	if len(got) != 1 || got[0].Params["iot_devices"] != "tv" || got[0].Params["trusted_devices"] != "pi" {
		t.Fatalf("hidden devices are left out, types as corrected count: %+v", got)
	}
}
