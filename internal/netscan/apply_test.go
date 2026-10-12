package netscan

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/snmp/snmptest"
)

// TestDisablesAPortOfAnSNMPDevice: a switch found by the scan has a port
// disabled with the write community, and comes back read again.
func TestDisablesAPortOfAnSNMPDevice(t *testing.T) {
	sw := model.Device{Key: "x", Name: "sw-core", Interfaces: []model.Interface{{Name: "ge-0/0/1", Up: model.Ptr(true)}}}
	agent := snmptest.Start(t, "homelab", snmptest.FromDevice(sw, snmptest.Options{}))
	agent.AllowWrite("homelab-rw")
	s := &Integration{
		ARPPath: "testdata/arp", RoutePath: "testdata/route", Ports: []int{},
		SettleTime: 10 * time.Millisecond, NoMulticast: true, NoPing: true, NoWebTitles: true,
		SNMPPort: agent.Port, SNMPTimeout: 300 * time.Millisecond,
		Locals: func() []localNet { return nil },
	}
	cfg := integration.Config{"subnets": "127.0.0.0/30", "snmp_communities": "homelab"}
	devices, err := s.Collect(context.Background(), cfg)
	if err != nil || len(devices) != 2 {
		t.Fatalf("scan: %v %v", devices, err)
	}
	change := model.Change{Device: devices[1].Key, Kind: model.ChangeKindPortEnabled, Port: model.Ptr("ge-0/0/1"), Value: false}

	if _, err := s.Apply(context.Background(), cfg, change); err == nil || !strings.Contains(err.Error(), "write community") {
		t.Fatalf("without a write community: %v", err)
	}
	cfg["snmp_write_community"] = "homelab"
	if _, err := s.Apply(context.Background(), cfg, change); err == nil || !strings.Contains(err.Error(), "refused to write") {
		t.Fatalf("with a read-only community: %v", err)
	}
	cfg["snmp_write_community"] = "homelab-rw"
	got, err := s.Apply(context.Background(), cfg, change)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != devices[1].Key || *got[0].Interfaces[0].Enabled {
		t.Fatalf("read again after the change: %+v", got)
	}
	unknown := change
	unknown.Device = "not-read"
	if _, err := s.Apply(context.Background(), cfg, unknown); err == nil {
		t.Fatal("a device the scan did not read must fail")
	}
}

// TestChangesTheVLANsOfAnSNMPSwitch: port.vlans, vlan.set and vlan.delete
// through Q-BRIDGE-MIB, with the core's values.
func TestChangesTheVLANsOfAnSNMPSwitch(t *testing.T) {
	sw := model.Device{
		Key: "x", Name: "sw-core", Vlans: []model.Vlan{{ID: 1}, {ID: 10}},
		Interfaces: []model.Interface{{Name: "ge-0/0/1", Up: model.Ptr(true), Vlans: &model.PortVlans{Untagged: model.Ptr[uint16](1)}}},
	}
	agent := snmptest.Start(t, "homelab", snmptest.FromDevice(sw, snmptest.Options{}))
	agent.AllowWrite("homelab-rw")
	s := &Integration{
		ARPPath: "testdata/arp", RoutePath: "testdata/route", Ports: []int{},
		SettleTime: 10 * time.Millisecond, NoMulticast: true, NoPing: true, NoWebTitles: true,
		SNMPPort: agent.Port, SNMPTimeout: 300 * time.Millisecond,
		Locals: func() []localNet { return nil },
	}
	cfg := integration.Config{"subnets": "127.0.0.0/30", "snmp_communities": "homelab", "snmp_write_community": "homelab-rw"}
	devices, err := s.Collect(context.Background(), cfg)
	if err != nil || len(devices) != 2 {
		t.Fatalf("scan: %v %v", devices, err)
	}
	key := devices[1].Key
	type vlan struct {
		ID   uint16 `json:"id"`
		Name string `json:"name,omitempty"`
	}
	if _, err := s.Apply(context.Background(), cfg, model.Change{Device: key, Kind: model.ChangeKindVlanSet, Value: vlan{ID: 30, Name: "Guests"}}); err != nil {
		t.Fatal(err)
	}
	want := model.PortVlans{Untagged: model.Ptr[uint16](30), Tagged: []uint16{10}}
	got, err := s.Apply(context.Background(), cfg, model.Change{Device: key, Kind: model.ChangeKindPortVlans, Port: model.Ptr("ge-0/0/1"), Value: want})
	if err != nil {
		t.Fatal(err)
	}
	if v := got[0].Interfaces[0].Vlans; v == nil || model.Deref(v.Untagged) != 30 || len(v.Tagged) != 1 || v.Tagged[0] != 10 {
		t.Fatalf("port read again: %+v", v)
	}
	if _, err := s.Apply(context.Background(), cfg, model.Change{Device: key, Kind: model.ChangeKindPortVlans, Port: model.Ptr("ge-0/0/1"), Value: model.PortVlans{Untagged: model.Ptr[uint16](1)}}); err != nil {
		t.Fatal(err)
	}
	got, err = s.Apply(context.Background(), cfg, model.Change{Device: key, Kind: model.ChangeKindVlanDelete, Value: vlan{ID: 30}})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range got[0].Vlans {
		if v.ID == 30 {
			t.Fatal("VLAN 30 is still there")
		}
	}
}
