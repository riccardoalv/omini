package insights

import (
	"slices"
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

func accessPoint(id string, ch uint8, auto bool) topology.Node {
	return topology.Node{
		ID: id, Label: id, Kind: topology.KindDevice, Role: "ap", Online: true,
		Changes: []model.ChangeKind{model.ChangeKindWifiChannel},
		Device: &model.Device{Radios: []model.Radio{
			{Name: "wifi0", Band: model.WifiBandA24Ghz, Channel: &ch, Auto: model.Ptr(auto)},
			{Name: "wifi1", Band: model.WifiBandA5Ghz, Channel: model.Ptr[uint8](36)},
		}},
	}
}

func TestWifiChannelConflict(t *testing.T) {
	topo := topology.Topology{Nodes: []topology.Node{accessPoint("hall", 6, false), accessPoint("office", 6, false), accessPoint("attic", 3, true)}}
	got := byRule(Evaluate(Input{Topology: topo}))["wifi_channel_conflict"]
	if len(got) != 1 {
		t.Fatalf("one insight for the network: %+v", got)
	}
	a := got[0]
	if a.Key != "wifi_channel_conflict" || a.Severity != Warning || a.NodeID != "attic" {
		t.Fatalf("insight: %+v", a)
	}
	p := a.Params
	if p["aps"] != "attic (3), hall (6), office (6)" || p["count"] != 3 || p["auto_aps"] != "attic" || p["movable"] != 1 ||
		!slices.Equal(p["nodes"].([]string), []string{"attic", "hall", "office"}) {
		t.Fatalf("params: %+v", p)
	}
}

func TestWifiChannelsFine(t *testing.T) {
	topo := topology.Topology{Nodes: []topology.Node{accessPoint("a", 1, false), accessPoint("b", 6, false), accessPoint("c", 11, true)}}
	if got := byRule(Evaluate(Input{Topology: topo}))["wifi_channel_conflict"]; len(got) != 0 {
		t.Fatalf("1, 6 and 11: %+v", got)
	}
	// 5 GHz is not looked at yet.
	if got := byRule(Evaluate(Input{Topology: topology.Topology{}}))["wifi_channel_conflict"]; len(got) != 0 {
		t.Fatalf("no access points: %+v", got)
	}
}

func TestWifiChannelConflictAllAuto(t *testing.T) {
	topo := topology.Topology{Nodes: []topology.Node{accessPoint("a", 6, true), accessPoint("b", 6, true)}}
	got := byRule(Evaluate(Input{Topology: topo}))["wifi_channel_conflict"]
	if len(got) != 1 || got[0].Params["movable"] != 0 || got[0].Params["auto_aps"] != "a, b" || got[0].Severity != Info {
		t.Fatalf("told as information, with nothing to move: %+v", got)
	}
}
