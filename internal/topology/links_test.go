package topology

import (
	"testing"

	"github.com/riccardoalv/omini/internal/model"
)

func TestMarkLinks(t *testing.T) {
	blocking := model.PortStpStateBlocking
	topo := Topology{
		Nodes: []Node{
			{ID: "core", Device: &model.Device{Interfaces: []model.Interface{
				{Name: "ae0", Type: model.Ptr(model.InterfaceTypeLag), Up: model.Ptr(true)},
				{Name: "xe-1", Lag: model.Ptr("ae0"), Up: model.Ptr(true), SpeedMbps: model.Ptr[uint64](10000)},
				{Name: "xe-2", Lag: model.Ptr("ae0"), Up: model.Ptr(true), SpeedMbps: model.Ptr[uint64](10000)},
				{Name: "ge-9", Stp: &model.PortStp{State: &blocking}},
			}}},
			{ID: "access", Device: &model.Device{}},
			{ID: "spare", Device: &model.Device{}},
		},
		Edges: []Edge{
			{ID: "a", Source: "core", SourcePort: "ae0", Target: "access", SpeedMbps: 10000},
			{ID: "b", Source: "core", SourcePort: "ge-9", Target: "spare", SpeedMbps: 1000},
		},
	}
	markLinks(&topo)
	if e := topo.Edges[0]; e.Lag != 2 || e.SpeedMbps != 20000 || e.SourcePort != "ae0" || e.Standby {
		t.Errorf("LAG: %+v", e)
	}
	if e := topo.Edges[1]; !e.Standby || e.Lag != 0 {
		t.Errorf("standby: %+v", e)
	}
}
