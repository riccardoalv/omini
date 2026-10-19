package collector

import (
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// The ISP's router answering at an uplink's IPv6 gateway address is on the
// internet side, as when it answers at the IPv4 one.
func TestUpstreamByIPv6Gateway(t *testing.T) {
	topo := topology.Topology{Nodes: []topology.Node{
		{ID: "wan:fw:pppoe0", Kind: topology.KindWAN, WAN: &topology.WANLink{Gateways: []model.Gateway{
			{Name: "GWv6", Address: model.Ptr("2001:DB8:FFFF::1"), Status: model.GatewayStatusUp},
		}}},
		{ID: "mac:isp", Kind: topology.KindClient, IPs6: []string{"2001:db8:ffff::1", "fe80::1"}},
		{ID: "mac:lan", Kind: topology.KindClient, IP: "192.168.1.5", IPs6: []string{"fe80::1"}},
	}}
	got := upstreamNodes(&topo)
	if !got["mac:isp"] || got["mac:lan"] {
		t.Fatalf("upstream = %v", got)
	}
}
