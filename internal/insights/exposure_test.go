package insights

import (
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

func TestPortForwards(t *testing.T) {
	gw := topology.Node{
		ID: "dev:gw", Kind: topology.KindDevice, Label: "gw", Role: "router", Online: true,
		Device: &model.Device{PortForwards: []model.PortForward{
			{Protocol: model.PortForwardProtocolTcp, ExternalPort: 443, InternalIP: model.Ptr("192.168.1.10"), Description: model.Ptr("Nextcloud")},
			{
				Protocol: model.PortForwardProtocolUdp, ExternalPort: 27015, ExternalPortEnd: model.Ptr[uint16](27020),
				InternalIP: model.Ptr("192.168.1.50"), InternalPort: model.Ptr[uint16](7015), Upnp: model.Ptr(true),
			},
			{Protocol: model.PortForwardProtocolTcpUdp, ExternalPort: 51820, InternalIP: model.Ptr("fd00::5"), InternalPort: model.Ptr[uint16](51820), Source: model.Ptr("203.0.113.0/24")},
			{Protocol: model.PortForwardProtocolTcp, ExternalPort: 22, InternalIP: model.Ptr("192.168.1.10"), Enabled: model.Ptr(false)},
		}},
	}
	nas := topology.Node{ID: "mac:nas", Kind: topology.KindClient, Label: "nas", IP: "192.168.1.10", Online: true}
	got := byRule(Evaluate(Input{Topology: topology.Topology{Nodes: []topology.Node{gw, nas}}}))

	fw := got["port_forward"]
	if len(fw) != 2 {
		t.Fatalf("two enabled forwards by hand: %+v", fw)
	}
	a, b := fw[1], fw[0] // sorted by key: tcp/udp before tcp|
	if a.Key != "port_forward|dev:gw|tcp|443" || a.Severity != Info || a.NodeID != "dev:gw" {
		t.Fatalf("forward: %+v", a)
	}
	if p := a.Params; p["node"] != "gw" || p["protocol"] != "tcp" || p["ports"] != "443" || p["internal"] != "192.168.1.10:443" ||
		p["device"] != "nas" || p["target"] != "nas" || p["description"] != "Nextcloud" {
		t.Fatalf("params: %+v", p)
	}
	if p := b.Params; p["internal"] != "[fd00::5]:51820" || p["target"] != "fd00::5" || p["source"] != "203.0.113.0/24" || p["device"] != nil {
		t.Fatalf("IPv6 to an unknown device: %+v", p)
	}

	up := got["upnp_forward"]
	if len(up) != 1 || up[0].Severity != Warning || up[0].Key != "upnp_forward|dev:gw|udp|27015" {
		t.Fatalf("one opened by UPnP: %+v", up)
	}
	if p := up[0].Params; p["ports"] != "27015-27020" || p["internal"] != "192.168.1.50:7015-7020" || p["target"] != "192.168.1.50" {
		t.Fatalf("a range: %+v", p)
	}
}
