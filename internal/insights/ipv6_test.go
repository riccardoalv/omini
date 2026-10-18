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
