package insights

import (
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// An IPv6 gateway that is down or degraded alerts like an IPv4 one, with its
// round-trip time and loss.
func TestIPv6GatewayAlerts(t *testing.T) {
	topo := topology.Topology{Nodes: []topology.Node{{
		ID: "wan:fw:pppoe0", Kind: topology.KindWAN, Label: "WAN", WAN: &topology.WANLink{Interface: "pppoe0", Gateways: []model.Gateway{
			{Name: "WAN_GW", Address: model.Ptr("100.64.0.1"), Status: model.GatewayStatusUp},
			{Name: "WAN_GWv6", Address: model.Ptr("fe80::1%pppoe0"), Status: model.GatewayStatusDegraded, RttMs: model.Ptr(180.0), LossPct: model.Ptr(20.0)},
		}},
	}}}
	got := byRule(wanStatus(Input{Topology: topo}, index{}))
	d := got["wan_degraded"]
	if len(d) != 1 || d[0].Params["gateway"] != "WAN_GWv6" || d[0].Params["rtt_ms"] != 180.0 || d[0].Params["loss_pct"] != 20.0 {
		t.Fatalf("alerts = %+v", got)
	}
}

// Two machines answering for the same global IPv6 address are a duplicate,
// as with IPv4; the same link-local address on two machines is not (each
// network has its fe80::1).
func TestDuplicateIPv6(t *testing.T) {
	client := func(id, mac string, ips6 ...string) topology.Node {
		return topology.Node{ID: id, Kind: topology.KindClient, Label: id, Online: true, MAC: mac, IPs6: ips6}
	}
	topo := topology.Topology{Nodes: []topology.Node{
		client("mac:a", "aa:00:00:00:00:01", "2001:db8:1::10", "fe80::1"),
		client("mac:b", "aa:00:00:00:00:02", "2001:db8:1::10", "fe80::1"),
		client("mac:c", "aa:00:00:00:00:03", "fd00::5", "fe80::2"),
		client("mac:d", "aa:00:00:00:00:04", "fd00::6", "fe80::2"),
	}}
	idx := index{byID: map[string]*topology.Node{}}
	for i := range topo.Nodes {
		idx.byID[topo.Nodes[i].ID] = &topo.Nodes[i]
	}
	got := duplicateIP(Input{Topology: topo}, idx)
	if len(got) != 1 || got[0].Params["ip"] != "2001:db8:1::10" || got[0].NodeID != "mac:a" {
		t.Fatalf("duplicates = %+v", got)
	}
}
