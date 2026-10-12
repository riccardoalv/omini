package fixip

import (
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

func router(id, gateway string, inRange *bool, leases []model.DhcpLease, reservations []model.DhcpReservation) topology.Node {
	return topology.Node{
		ID: id, Kind: topology.KindDevice, Label: id, IP: gateway[:len(gateway)-3],
		Changes: []model.ChangeKind{model.ChangeKindReservationSet},
		Device: &model.Device{
			Networks: []model.Network{{Vlan: 1, Gateway: model.Ptr(gateway), Dhcp: &model.NetworkDhcp{
				Enabled: true, From: model.Ptr(gateway[:len(gateway)-4] + "100"), To: model.Ptr(gateway[:len(gateway)-4] + "199"),
				ReserveInRange: inRange,
			}}},
			DhcpLeases: leases, DhcpReservations: reservations,
		},
	}
}

func client(id, typ, mac, ip string) topology.Node {
	return topology.Node{ID: id, Kind: topology.KindClient, Label: id, Type: typ, MAC: mac, IP: ip}
}

// TestCandidates: devices on DHCP without a reservation, infrastructure
// first; reserved, static, random-MAC and unmanaged networks left out.
func TestCandidates(t *testing.T) {
	topo := topology.Topology{Nodes: []topology.Node{
		router("gw", "192.168.1.1/24", nil,
			[]model.DhcpLease{
				{MAC: "aa:00:00:00:00:01", IP: "192.168.1.120"},
				{MAC: "aa:00:00:00:00:02", IP: "192.168.1.121"},
				{MAC: "aa:00:00:00:00:03", IP: "192.168.1.122"},
				{MAC: "aa:00:00:00:00:04", IP: "192.168.1.123"},
				{MAC: "aa:00:00:00:00:06", IP: "10.9.9.9"},
			},
			[]model.DhcpReservation{{MAC: "aa:00:00:00:00:03", IP: "192.168.1.122"}}),
		client("phone", "phone", "AA-00-00-00-00-01", "192.168.1.120"),
		client("sw-desk", "switch", "aa:00:00:00:00:02", "192.168.1.121"),
		client("nas", "nas", "aa:00:00:00:00:03", "192.168.1.122"),     // reserved
		client("pixel", "phone", "aa:00:00:00:00:04", "192.168.1.123"), // random MAC
		client("tv", "tv", "aa:00:00:00:00:05", "192.168.1.30"),        // static: no lease
		client("far", "server", "aa:00:00:00:00:06", "10.9.9.9"),       // no router for it
	}}
	topo.Nodes[4].RandomMAC = true
	got := Candidates(topo)
	if len(got) != 2 {
		t.Fatalf("candidates: %+v", got)
	}
	if got[0].NodeID != "sw-desk" || !got[0].Infra || got[0].Router != "gw" || got[0].Address() != "192.168.1.121" || got[0].Proposed != "" {
		t.Errorf("infra first, current address kept: %+v", got[0])
	}
	if got[1].NodeID != "phone" || got[1].Infra || got[1].MAC != "aa:00:00:00:00:01" {
		t.Errorf("then the others: %+v", got[1])
	}
}

// TestProposesAnAddressOutsideTheRange: a router that refuses reservations
// in its DHCP range gets the first free address after the gateway, one per
// device.
func TestProposesAnAddressOutsideTheRange(t *testing.T) {
	no := false
	topo := topology.Topology{Nodes: []topology.Node{
		router("pf", "10.0.0.1/24", &no,
			[]model.DhcpLease{
				{MAC: "aa:00:00:00:00:01", IP: "10.0.0.150"},
				{MAC: "aa:00:00:00:00:02", IP: "10.0.0.151"},
				{MAC: "aa:00:00:00:00:03", IP: "10.0.0.3"},
			}, nil),
		client("ap", "ap", "aa:00:00:00:00:01", "10.0.0.150"),
		client("nas", "nas", "aa:00:00:00:00:02", "10.0.0.151"),
		client("cam", "camera", "aa:00:00:00:00:03", "10.0.0.3"), // outside the range already
		client("printer", "printer", "aa:00:00:00:00:09", "10.0.0.2"),
	}}
	got := Candidates(topo)
	if len(got) != 3 {
		t.Fatalf("candidates: %+v", got)
	}
	want := map[string]string{"ap": "10.0.0.4", "nas": "10.0.0.5", "cam": ""}
	for _, c := range got {
		if c.Proposed != want[c.NodeID] {
			t.Errorf("%s: proposed %q, want %q", c.NodeID, c.Proposed, want[c.NodeID])
		}
	}
}
