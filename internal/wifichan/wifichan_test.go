package wifichan

import (
	"slices"
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

func radio(label string, ch, clients int) Radio {
	return Radio{NodeID: "dev:" + label, Label: label, Radio: "wifi0", Channel: ch, Clients: clients}
}

func skip(r Radio, why string) Radio {
	r.Skip = why
	return r
}

func moves(steps []Step) map[string][2]int {
	out := map[string][2]int{}
	for _, s := range steps {
		out[s.Label] = [2]int{s.From, s.To}
	}
	return out
}

func TestAlreadyFineHasNoPlan(t *testing.T) {
	rs := []Radio{radio("a", 1, 3), radio("b", 6, 0), radio("c", 11, 9)}
	if Conflict(rs) {
		t.Fatal("1, 6 and 11 are no conflict")
	}
	if steps, skipped := Plan(rs); len(steps) != 0 || len(skipped) != 0 {
		t.Fatalf("nothing to do: %+v %+v", steps, skipped)
	}
	if Conflict([]Radio{radio("a", 6, 0)}) || Conflict(nil) {
		t.Fatal("one access point on 6 is fine")
	}
}

func TestOneOffChannelIsAConflict(t *testing.T) {
	rs := []Radio{radio("a", 3, 1)}
	if !Conflict(rs) {
		t.Fatal("channel 3 is not 1, 6 or 11")
	}
	steps, _ := Plan(rs)
	if got := moves(steps); len(got) != 1 || got["a"] != [2]int{3, 1} {
		t.Fatalf("to the nearest free channel: %+v", steps)
	}
}

// Two on the same channel: the one with fewer clients moves.
func TestTheOneWithFewestClientsMoves(t *testing.T) {
	rs := []Radio{radio("busy", 6, 12), radio("quiet", 6, 2)}
	if !Conflict(rs) {
		t.Fatal("both on 6")
	}
	steps, _ := Plan(rs)
	if got := moves(steps); len(got) != 1 || got["quiet"] != [2]int{6, 1} {
		t.Fatalf("only the quiet one moves: %+v", steps)
	}
	if steps[0].Clients != 2 {
		t.Fatalf("its clients: %+v", steps[0])
	}
}

// Overlapping 3 and 5: both off the three channels, each to the nearest.
func TestOverlappingThreeAndFive(t *testing.T) {
	rs := []Radio{radio("a", 3, 4), radio("b", 5, 1)}
	if !Conflict(rs) {
		t.Fatal("3 and 5 overlap")
	}
	steps, _ := Plan(rs)
	got := moves(steps)
	if len(got) != 2 || got["b"] != [2]int{5, 6} || got["a"] != [2]int{3, 1} {
		t.Fatalf("plan: %+v", steps)
	}
	if steps[0].Label != "b" {
		t.Fatalf("fewest clients first: %+v", steps)
	}
}

// One on 6 and one overlapping it on 4: the one on 6 stays.
func TestKeepsTheOneAlreadyFree(t *testing.T) {
	steps, _ := Plan([]Radio{radio("a", 6, 0), radio("b", 4, 9)})
	if got := moves(steps); len(got) != 1 || got["b"] != [2]int{4, 1} {
		t.Fatalf("only b moves: %+v", steps)
	}
}

// Four access points share the three channels evenly: two on one channel at
// most, and no more than needed move.
func TestFourAccessPointsShareEvenly(t *testing.T) {
	rs := []Radio{radio("a", 6, 4), radio("b", 6, 3), radio("c", 6, 2), radio("d", 6, 1)}
	steps, _ := Plan(rs)
	got := moves(steps)
	if len(got) != 2 || got["d"] != [2]int{6, 1} || got["c"] != [2]int{6, 11} {
		t.Fatalf("the two quietest move: %+v", steps)
	}
	even := []Radio{radio("a", 1, 0), radio("b", 6, 0), radio("c", 11, 0), radio("d", 6, 0)}
	if Conflict(even) {
		t.Fatal("four on 1, 6, 11 and 6 is as even as it gets")
	}
	if !Conflict([]Radio{radio("a", 1, 0), radio("b", 1, 0), radio("c", 1, 0), radio("d", 11, 0)}) {
		t.Fatal("three of four on one channel is a conflict")
	}
}

// Every radio on automatic channel: nothing to move, no button.
func TestAllAutoHasNoSteps(t *testing.T) {
	rs := []Radio{skip(radio("a", 6, 1), SkipAuto), skip(radio("b", 6, 2), SkipAuto)}
	if !Conflict(rs) {
		t.Fatal("still a conflict")
	}
	steps, skipped := Plan(rs)
	if len(steps) != 0 || len(skipped) != 2 || skipped[0].Skip != SkipAuto {
		t.Fatalf("nothing movable: %+v %+v", steps, skipped)
	}
}

// A mesh unit is never moved: the other one goes around it.
func TestMeshIsLeftAlone(t *testing.T) {
	rs := []Radio{skip(radio("mesh", 6, 0), SkipMesh), radio("ap", 6, 10)}
	steps, skipped := Plan(rs)
	if got := moves(steps); len(got) != 1 || got["ap"] != [2]int{6, 1} {
		t.Fatalf("the access point moves although it has more clients: %+v", steps)
	}
	if len(skipped) != 1 || skipped[0].Label != "mesh" || skipped[0].Skip != SkipMesh {
		t.Fatalf("skipped: %+v", skipped)
	}
}

// A radio that cannot move on 3 takes both 1 and 6: the other goes to 11.
func TestAFixedRadioCountsWhereItOverlaps(t *testing.T) {
	rs := []Radio{skip(radio("old", 3, 0), SkipUnsupported), radio("ap", 1, 0)}
	steps, _ := Plan(rs)
	if got := moves(steps); len(got) != 1 || got["ap"] != [2]int{1, 11} {
		t.Fatalf("plan: %+v", steps)
	}
}

func TestRadiosFromTheTopology(t *testing.T) {
	b24, b5 := model.WifiBandA24Ghz, model.WifiBandA5Ghz
	w40 := uint16(40)
	ap := func(id string, d model.Device, changes ...model.ChangeKind) topology.Node {
		return topology.Node{ID: id, Label: id, Kind: topology.KindDevice, Online: true, Device: &d, Changes: changes}
	}
	ch := model.ChangeKindWifiChannel
	topo := topology.Topology{Nodes: []topology.Node{
		ap("ap1", model.Device{
			Radios: []model.Radio{
				{Name: "wifi0", Band: b24, Channel: model.Ptr[uint8](6)},
				{Name: "wifi1", Band: b5, Channel: model.Ptr[uint8](36)},
			},
			WirelessClients: []model.WirelessClient{
				{MAC: "aa:00:00:00:00:01", Band: &b24},
				{MAC: "aa:00:00:00:00:02", Band: &b24},
				{MAC: "aa:00:00:00:00:03", Band: &b5},
				{MAC: "aa:00:00:00:00:04"},
			},
		}, ch),
		// Only 2.4 GHz: clients without a band are on it.
		ap("ap2", model.Device{
			Radios:          []model.Radio{{Name: "ra0", Band: b24, Channel: model.Ptr[uint8](6), Auto: model.Ptr(true)}},
			WirelessClients: []model.WirelessClient{{MAC: "aa:00:00:00:00:05"}},
		}, ch),
		ap("ap3", model.Device{Radios: []model.Radio{{Name: "r", Band: b24, Channel: model.Ptr[uint8](1)}}}),
		ap("ap4", model.Device{WirelessUplink: model.Ptr(true), Radios: []model.Radio{{Name: "r", Band: b24, Channel: model.Ptr[uint8](11)}}}, ch),
		ap("ap5", model.Device{Radios: []model.Radio{{Name: "r", Band: b24, Channel: model.Ptr[uint8](11), WidthMhz: &w40}}}, ch),
		ap("off", model.Device{Radios: []model.Radio{{Name: "r", Band: b24, Channel: model.Ptr[uint8](3), Enabled: model.Ptr(false)}}}, ch),
		ap("unknown", model.Device{Radios: []model.Radio{{Name: "r", Band: b24}}}, ch),
	}}
	got := Radios(topo)
	want := []Radio{
		{NodeID: "ap1", Label: "ap1", Radio: "wifi0", Channel: 6, Clients: 2},
		{NodeID: "ap2", Label: "ap2", Radio: "ra0", Channel: 6, Clients: 1, Skip: SkipAuto},
		{NodeID: "ap3", Label: "ap3", Radio: "r", Channel: 1, Skip: SkipUnsupported},
		{NodeID: "ap4", Label: "ap4", Radio: "r", Channel: 11, Skip: SkipMesh},
		{NodeID: "ap5", Label: "ap5", Radio: "r", Channel: 11, Skip: SkipWidth},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("radios:\n%+v\nwant\n%+v", got, want)
	}
	if Describe(got[:2]) != "ap1 (6), ap2 (6)" {
		t.Fatalf("describe: %q", Describe(got[:2]))
	}
}
