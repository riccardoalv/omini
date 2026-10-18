package snmp_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/snmp"
	"github.com/riccardoalv/omini/internal/snmp/snmptest"
)

func TestDisableAndEnableAPort(t *testing.T) {
	ctx := context.Background()
	sw := model.Device{Key: "sw", Name: "sw", Interfaces: []model.Interface{
		{Name: "ge-0/0/1", Up: model.Ptr(true)},
		{Name: "ge-0/0/2", Up: model.Ptr(true), Enabled: model.Ptr(false)},
	}}
	agent := snmptest.Start(t, "public", snmptest.FromDevice(sw, snmptest.Options{}))
	agent.AllowWrite("private")

	enabled := func() map[string]bool {
		d, err := snmp.Collect(ctx, target(agent, "public"))
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, i := range d.Interfaces {
			out[i.Name] = i.Enabled != nil && *i.Enabled
		}
		return out
	}
	if e := enabled(); !e["ge-0/0/1"] || e["ge-0/0/2"] {
		t.Fatalf("ifAdminStatus read: %v", e)
	}
	if err := snmp.SetPortEnabled(ctx, target(agent, "private"), "ge-0/0/1", false); err != nil {
		t.Fatal(err)
	}
	if err := snmp.SetPortEnabled(ctx, target(agent, "private"), "ge-0/0/2", true); err != nil {
		t.Fatal(err)
	}
	if e := enabled(); e["ge-0/0/1"] || !e["ge-0/0/2"] {
		t.Fatalf("after the change: %v", e)
	}

	if err := snmp.SetPortEnabled(ctx, target(agent, "public"), "ge-0/0/1", true); !errors.Is(err, snmp.ErrNoWrite) {
		t.Fatalf("a read-only community: %v", err)
	}
	if err := snmp.SetPortEnabled(ctx, target(agent, "private"), "ge-0/0/9", true); err == nil {
		t.Fatal("an unknown port must fail")
	}
}

// Two interfaces with the same name: which one to change is unknown.
func TestRefusesAnAmbiguousPortName(t *testing.T) {
	sw := model.Device{Key: "sw", Name: "sw", Interfaces: []model.Interface{
		{Name: "port"}, {Name: "uplink"}, {Name: "port"},
	}}
	agent := snmptest.Start(t, "public", snmptest.FromDevice(sw, snmptest.Options{}))
	agent.AllowWrite("private")
	err := snmp.SetPortEnabled(context.Background(), target(agent, "private"), "port", false)
	if err == nil || !strings.Contains(err.Error(), "ifIndex 1 3") {
		t.Fatalf("an ambiguous name must be refused, got %v", err)
	}
	if err := snmp.SetPortEnabled(context.Background(), target(agent, "private"), "uplink", false); err != nil {
		t.Fatal(err)
	}
}
