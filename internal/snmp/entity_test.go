package snmp_test

import (
	"context"
	"testing"

	"github.com/gosnmp/gosnmp"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/snmp"
	"github.com/riccardoalv/omini/internal/snmp/snmptest"
)

func ports(d model.Device) map[string]model.Interface {
	out := map[string]model.Interface{}
	for _, i := range d.Interfaces {
		out[i.Name] = i
	}
	return out
}

// TestEntityChassisAndTransceivers: the chassis' model, serial number and
// versions, and each SFP's vendor, part and serial on its port.
func TestEntityChassisAndTransceivers(t *testing.T) {
	sw := model.Device{
		Key: "x", Name: "sw-core", Model: model.Ptr("WS-C3850-24T"), Vendor: model.Ptr("Cisco"),
		Serial: model.Ptr("FOC1234X0AB"), OSVersion: model.Ptr("16.12.4"), Firmware: &model.Firmware{Current: model.Ptr("16.12.2r")},
		Interfaces: []model.Interface{
			{Name: "Gi1/0/1"},
			{Name: "Te1/1/1", Transceiver: &model.Transceiver{
				Vendor: model.Ptr("CISCO-FINISAR"), Part: model.Ptr("SFP-10G-SR"), Serial: model.Ptr("FNS17250ABC"),
			}},
		},
	}
	pdus := snmptest.FromDevice(sw, snmptest.Options{})
	for i := range pdus { // sysDescr says much more than the model
		if pdus[i].Name == ".1.3.6.1.2.1.1.1.0" {
			pdus[i].Value = "Cisco IOS Software [Gibraltar], Catalyst L3 Switch Software"
		}
	}
	agent := snmptest.Start(t, "public", pdus)
	d, err := snmp.Collect(context.Background(), target(agent, "public"))
	if err != nil {
		t.Fatal(err)
	}
	if model.Deref(d.Model) != "WS-C3850-24T" || model.Deref(d.Serial) != "FOC1234X0AB" ||
		model.Deref(d.OSVersion) != "16.12.4" || d.Firmware == nil || model.Deref(d.Firmware.Current) != "16.12.2r" {
		t.Errorf("chassis: model=%v serial=%v os=%v firmware=%+v", model.Deref(d.Model), model.Deref(d.Serial),
			model.Deref(d.OSVersion), d.Firmware)
	}
	p := ports(d)
	x := p["Te1/1/1"].Transceiver
	if x == nil || model.Deref(x.Vendor) != "CISCO-FINISAR" || model.Deref(x.Part) != "SFP-10G-SR" || model.Deref(x.Serial) != "FNS17250ABC" {
		t.Errorf("transceiver: %+v", x)
	}
	if p["Gi1/0/1"].Transceiver != nil {
		t.Errorf("a copper port has no transceiver: %+v", p["Gi1/0/1"].Transceiver)
	}
}

// TestEntityTransceiverNeedsTheAliasTable: without entAliasMappingTable a
// module is not tied to a port by guessing.
func TestEntityTransceiverNeedsTheAliasTable(t *testing.T) {
	sw := model.Device{
		Key: "x", Name: "sw",
		Interfaces: []model.Interface{{Name: "sfp1", Transceiver: &model.Transceiver{Serial: model.Ptr("S1")}}},
	}
	var pdus []gosnmp.SnmpPDU
	for _, p := range snmptest.FromDevice(sw, snmptest.Options{}) {
		if len(p.Name) < 26 || p.Name[:26] != ".1.3.6.1.2.1.47.1.3.2.1.2." {
			pdus = append(pdus, p)
		}
	}
	agent := snmptest.Start(t, "public", pdus)
	d, err := snmp.Collect(context.Background(), target(agent, "public"))
	if err != nil {
		t.Fatal(err)
	}
	if x := ports(d)["sfp1"].Transceiver; x != nil {
		t.Errorf("transceiver without a mapping: %+v", x)
	}
}

// TestProfilesWinOverEntity: a profile's model, serial and versions replace
// the chassis'.
func TestProfilesWinOverEntity(t *testing.T) {
	dev := model.Device{
		Key: "x", Name: "mt", Model: model.Ptr("RouterOS RB5009"), Serial: model.Ptr("ENTITY-SERIAL"),
		OSVersion: model.Ptr("entity-os"), Interfaces: []model.Interface{{Name: "ether1"}},
	}
	pdus := snmptest.FromDevice(dev, snmptest.Options{Enterprise: 14988})
	pdus = append(pdus,
		pdu("1.3.6.1.4.1.14988.1.1.7.8.0", gosnmp.OctetString, []byte("RB5009UG+S+")),
		pdu("1.3.6.1.4.1.14988.1.1.7.3.0", gosnmp.OctetString, []byte("HD0123")),
		pdu("1.3.6.1.4.1.14988.1.1.4.4.0", gosnmp.OctetString, []byte("7.16.1")),
	)
	agent := snmptest.Start(t, "public", pdus)
	d, err := snmp.Collect(context.Background(), target(agent, "public"))
	if err != nil {
		t.Fatal(err)
	}
	if model.Deref(d.Model) != "RB5009UG+S+" || model.Deref(d.Serial) != "HD0123" || model.Deref(d.OSVersion) != "7.16.1" {
		t.Errorf("model=%v serial=%v os=%v", model.Deref(d.Model), model.Deref(d.Serial), model.Deref(d.OSVersion))
	}
}
