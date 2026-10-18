package snmp_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/snmp"
	"github.com/riccardoalv/omini/internal/snmp/snmptest"
)

// A Q-BRIDGE switch: an access port in VLAN 10, a trunk (native 1, tagged 10
// and 20) and a port in VLAN 1.
func qbridge(t *testing.T) *snmptest.Agent {
	sw := model.Device{
		Key: "sw", Name: "sw",
		Vlans: []model.Vlan{{ID: 1, Name: model.Ptr("default")}, {ID: 10, Name: model.Ptr("Office")}, {ID: 20, Name: model.Ptr("Cams")}},
		Interfaces: []model.Interface{
			{Name: "1/1", Vlans: &model.PortVlans{Untagged: model.Ptr[uint16](10)}},
			{Name: "1/2", Vlans: &model.PortVlans{Untagged: model.Ptr[uint16](1), Tagged: []uint16{10, 20}}},
			{Name: "1/3", Vlans: &model.PortVlans{Untagged: model.Ptr[uint16](1)}},
		},
	}
	agent := snmptest.Start(t, "public", snmptest.FromDevice(sw, snmptest.Options{}))
	agent.AllowWrite("private")
	return agent
}

func readVlans(t *testing.T, agent *snmptest.Agent) model.Device {
	t.Helper()
	d, err := snmp.Collect(context.Background(), target(agent, "public"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func portVlans(d model.Device, name string) model.PortVlans {
	for _, i := range d.Interfaces {
		if i.Name == name && i.Vlans != nil {
			return *i.Vlans
		}
	}
	return model.PortVlans{}
}

func TestReadsTheVLANsOfAQBridgeSwitch(t *testing.T) {
	d := readVlans(t, qbridge(t))
	var ids []uint16
	for _, v := range d.Vlans {
		ids = append(ids, v.ID)
	}
	if !slices.Equal(ids, []uint16{1, 10, 20}) || model.Deref(d.Vlans[1].Name) != "Office" {
		t.Fatalf("vlans: %+v", d.Vlans)
	}
	if p := portVlans(d, "1/1"); model.Deref(p.Untagged) != 10 || len(p.Tagged) != 0 {
		t.Fatalf("access: %+v", p)
	}
	if p := portVlans(d, "1/2"); model.Deref(p.Untagged) != 1 || !slices.Equal(p.Tagged, []uint16{10, 20}) {
		t.Fatalf("trunk: %+v", p)
	}
}

func TestChangesAPortsVLANs(t *testing.T) {
	ctx := context.Background()
	agent := qbridge(t)
	w := target(agent, "private")
	// The access port becomes a trunk: native 20, tagged 10.
	if err := snmp.SetPortVlans(ctx, w, "1/1", model.PortVlans{Untagged: model.Ptr[uint16](20), Tagged: []uint16{10}}); err != nil {
		t.Fatal(err)
	}
	if p := portVlans(readVlans(t, agent), "1/1"); model.Deref(p.Untagged) != 20 || !slices.Equal(p.Tagged, []uint16{10}) {
		t.Fatalf("1/1: %+v", p)
	}
	// The trunk drops VLAN 20; the others stay as they were.
	if err := snmp.SetPortVlans(ctx, w, "1/2", model.PortVlans{Untagged: model.Ptr[uint16](1), Tagged: []uint16{10}}); err != nil {
		t.Fatal(err)
	}
	d := readVlans(t, agent)
	if p := portVlans(d, "1/2"); !slices.Equal(p.Tagged, []uint16{10}) {
		t.Fatalf("1/2: %+v", p)
	}
	if p := portVlans(d, "1/3"); model.Deref(p.Untagged) != 1 || len(p.Tagged) != 0 {
		t.Fatalf("1/3 changed: %+v", p)
	}
	if err := snmp.SetPortVlans(ctx, w, "1/3", model.PortVlans{Untagged: model.Ptr[uint16](99)}); err == nil {
		t.Fatal("a VLAN the switch does not have must fail")
	}
	if err := snmp.SetPortVlans(ctx, target(agent, "public"), "1/3", model.PortVlans{Untagged: model.Ptr[uint16](10)}); !errors.Is(err, snmp.ErrNoWrite) {
		t.Fatalf("read-only: %v", err)
	}
}

func TestCreatesRenamesAndDeletesAVLAN(t *testing.T) {
	ctx := context.Background()
	agent := qbridge(t)
	w := target(agent, "private")
	if err := snmp.SetVlan(ctx, w, 30, "Guests"); err != nil {
		t.Fatal(err)
	}
	if err := snmp.SetVlan(ctx, w, 10, "Staff"); err != nil {
		t.Fatal(err)
	}
	names := map[uint16]string{}
	for _, v := range readVlans(t, agent).Vlans {
		names[v.ID] = model.Deref(v.Name)
	}
	if names[30] != "Guests" || names[10] != "Staff" {
		t.Fatalf("names: %v", names)
	}
	if err := snmp.DeleteVlan(ctx, w, 30); err != nil {
		t.Fatal(err)
	}
	for _, v := range readVlans(t, agent).Vlans {
		if v.ID == 30 {
			t.Fatal("VLAN 30 is still there")
		}
	}
}

// A switch whose bridge ports are not numbered as their ifIndex: the
// management interface (ifIndex 1) is not a bridge port, 1/1 (ifIndex 2) is
// bridge port 1 and 1/2 (ifIndex 3) is bridge port 2.
func numberedBridge(t *testing.T, opts snmptest.Options) *snmptest.Agent {
	sw := model.Device{
		Key: "sw", Name: "sw",
		Vlans: []model.Vlan{{ID: 1, Name: model.Ptr("default")}, {ID: 10, Name: model.Ptr("Office")}},
		Interfaces: []model.Interface{
			{Name: "mgmt"},
			{Name: "1/1", Vlans: &model.PortVlans{Untagged: model.Ptr[uint16](10)}},
			{Name: "1/2", Vlans: &model.PortVlans{Untagged: model.Ptr[uint16](1)}},
		},
	}
	agent := snmptest.Start(t, "public", snmptest.FromDevice(sw, opts))
	agent.AllowWrite("private")
	return agent
}

func TestChangesTheVLANsOfTheRightBridgePort(t *testing.T) {
	ctx := context.Background()
	agent := numberedBridge(t, snmptest.Options{BridgePorts: map[string]int{"1/1": 1, "1/2": 2}})
	w := target(agent, "private")
	d := readVlans(t, agent)
	if p := portVlans(d, "1/1"); model.Deref(p.Untagged) != 10 {
		t.Fatalf("1/1 read: %+v", p)
	}
	if err := snmp.SetPortVlans(ctx, w, "1/2", model.PortVlans{Untagged: model.Ptr[uint16](10)}); err != nil {
		t.Fatal(err)
	}
	d = readVlans(t, agent)
	if p := portVlans(d, "1/2"); model.Deref(p.Untagged) != 10 {
		t.Fatalf("1/2: %+v", p)
	}
	// mgmt is ifIndex 1, which is not bridge port 1 (that is 1/1).
	if err := snmp.SetPortVlans(ctx, w, "mgmt", model.PortVlans{Untagged: model.Ptr[uint16](1)}); err == nil {
		t.Fatal("a port that is not a bridge port must be refused")
	}
	if p := portVlans(readVlans(t, agent), "1/1"); model.Deref(p.Untagged) != 10 || len(p.Tagged) != 0 {
		t.Fatalf("1/1 changed through mgmt: %+v", p)
	}
}

func TestChangesVLANsWithoutABridgePortTable(t *testing.T) {
	agent := numberedBridge(t, snmptest.Options{NoBridgePortTable: true})
	if err := snmp.SetPortVlans(context.Background(), target(agent, "private"), "1/2", model.PortVlans{Untagged: model.Ptr[uint16](10)}); err != nil {
		t.Fatal(err)
	}
	d := readVlans(t, agent)
	if p := portVlans(d, "1/2"); model.Deref(p.Untagged) != 10 {
		t.Fatalf("1/2: %+v", p)
	}
	if p := portVlans(d, "1/1"); model.Deref(p.Untagged) != 10 {
		t.Fatalf("1/1: %+v", p)
	}
}
