package snmp_test

import (
	"context"
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/snmp"
	"github.com/riccardoalv/omini/internal/snmp/snmptest"
)

func TestBridgeID(t *testing.T) {
	if got := snmp.BridgeID([]byte{0x10, 0x00, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x01}); got != "4096.aa:bb:cc:dd:ee:01" {
		t.Fatal(got)
	}
	if snmp.BridgeID([]byte{1, 2}) != "" {
		t.Fatal("a short ID is no ID")
	}
}

// TestReadsSpanningTreeAndLAGs: each port's STP state, the root the switch
// sees, and the LAG its members belong to.
func TestReadsSpanningTreeAndLAGs(t *testing.T) {
	st := func(s model.PortStpState) *model.PortStp { return &model.PortStp{State: &s} }
	sw := model.Device{
		Key: "x", Name: "sw-core", StpRoot: model.Ptr("4096.aa:bb:cc:dd:ee:01"),
		Interfaces: []model.Interface{
			{Name: "ge-0/0/1", Up: model.Ptr(true), Stp: st(model.PortStpStateForwarding)},
			{Name: "ge-0/0/2", Up: model.Ptr(true), Stp: st(model.PortStpStateBlocking)},
			{Name: "ae0", Up: model.Ptr(true)},
			{Name: "ge-0/0/3", Up: model.Ptr(true), Lag: model.Ptr("ae0")},
			{Name: "ge-0/0/4", Up: model.Ptr(false), Lag: model.Ptr("ae0")},
		},
	}
	agent := snmptest.Start(t, "public", snmptest.FromDevice(sw, snmptest.Options{}))
	d, err := snmp.Collect(context.Background(), target(agent, "public"))
	if err != nil {
		t.Fatal(err)
	}
	ports := map[string]model.Interface{}
	for _, i := range d.Interfaces {
		ports[i.Name] = i
	}
	if s := ports["ge-0/0/2"].Stp; s == nil || *s.State != model.PortStpStateBlocking {
		t.Errorf("blocking: %+v", s)
	}
	if s := ports["ge-0/0/1"].Stp; s == nil || *s.State != model.PortStpStateForwarding {
		t.Errorf("forwarding: %+v", s)
	}
	if model.Deref(d.StpRoot) != "4096.aa:bb:cc:dd:ee:01" {
		t.Errorf("root: %v", model.Deref(d.StpRoot))
	}
	if model.Deref(ports["ge-0/0/4"].Lag) != "ae0" || model.Deref(ports["ae0"].Type) != model.InterfaceTypeLag ||
		len(ports["ae0"].Members) != 2 {
		t.Errorf("lag: %+v %+v", ports["ge-0/0/4"], ports["ae0"])
	}
}

// TestSpanningTreeRoles: root (dot1dStpRootPort), designated (this switch is
// the segment's designated bridge), alternate and backup (blocking, another
// bridge or another port of this one is), disabled.
func TestSpanningTreeRoles(t *testing.T) {
	stp := func(s model.PortStpState, r model.PortStpRole) *model.PortStp {
		return &model.PortStp{State: &s, Role: &r}
	}
	fwd, blk := model.PortStpStateForwarding, model.PortStpStateBlocking
	sw := model.Device{
		Key: "aa:bb:cc:00:00:01", Name: "sw",
		Interfaces: []model.Interface{
			{Name: "p1", Stp: stp(fwd, model.PortStpRoleRoot)},
			{Name: "p2", Stp: stp(fwd, model.PortStpRoleDesignated)},
			{Name: "p3", Stp: stp(blk, model.PortStpRoleAlternate)},
			{Name: "p4", Stp: stp(blk, model.PortStpRoleBackup)},
			{Name: "p5", Stp: stp(model.PortStpStateDisabled, model.PortStpRoleDisabled)},
			{Name: "p6", Stp: &model.PortStp{State: &fwd}}, // no designated bridge: no role
		},
	}
	agent := snmptest.Start(t, "public", snmptest.FromDevice(sw, snmptest.Options{}))
	d, err := snmp.Collect(context.Background(), target(agent, "public"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]model.PortStpRole{
		"p1": model.PortStpRoleRoot, "p2": model.PortStpRoleDesignated, "p3": model.PortStpRoleAlternate,
		"p4": model.PortStpRoleBackup, "p5": model.PortStpRoleDisabled, "p6": "",
	}
	for _, i := range d.Interfaces {
		if i.Stp == nil || model.Deref(i.Stp.Role) != want[i.Name] {
			t.Errorf("%s: %+v, want role %q", i.Name, i.Stp, want[i.Name])
		}
	}
}
