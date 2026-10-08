package collector

import (
	"fmt"
	"testing"

	"github.com/riccardoalv/omini/internal/model"
)

func TestSubnetsOf(t *testing.T) {
	got := subnetsOf([]model.Device{{
		Interfaces: []model.Interface{
			{Name: "igc0", Wan: model.Ptr(true), IPs: []string{"100.64.1.2/24", "192.168.100.2/24"}}, // WAN: skipped
			{Name: "igc1", IPs: []string{"192.168.1.1/24", "fe80::1/64"}},
			{Name: "igc1.20", IPs: []string{"192.168.20.1/24"}},
			{Name: "lo0", IPs: []string{"127.0.0.1/8"}},
			{Name: "big", IPs: []string{"10.0.0.1/16"}}, // too large to scan
			{Name: "p2p", IPs: []string{"10.9.9.1/31"}}, // a point-to-point link
			{Name: "pub", IPs: []string{"8.8.8.1/24"}},
			{Name: "bare", IPs: []string{"192.168.5.1"}},
		},
		Vlans: []model.Vlan{{ID: 30, Subnet: model.Ptr("10.0.30.0/24")}, {ID: 20, Subnet: model.Ptr("192.168.20.0/24")}},
	}})
	if fmt.Sprint(got) != "[10.0.30.0/24 192.168.1.0/24 192.168.20.0/24]" {
		t.Fatalf("subnets: %v", got)
	}
}
