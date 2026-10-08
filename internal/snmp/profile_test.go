package snmp_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/riccardoalv/omini/internal/demo"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/snmp"
	"github.com/riccardoalv/omini/internal/snmp/snmptest"
)

func pdu(oid string, typ gosnmp.Asn1BER, v any) gosnmp.SnmpPDU {
	return gosnmp.SnmpPDU{Name: "." + oid, Type: typ, Value: v}
}

func TestShippedProfilesParse(t *testing.T) {
	ids := map[string]bool{}
	for _, p := range snmp.Profiles() {
		ids[p.ID] = true
	}
	for _, id := range []string{"net-snmp", "mikrotik", "cisco", "juniper", "fortinet", "synology", "qnap", "hpe-aruba"} {
		if !ids[id] {
			t.Errorf("profile %s missing", id)
		}
	}
}

func TestParseProfileErrors(t *testing.T) {
	for raw, want := range map[string]string{
		"name: x\nmatch: {sys_descr: a}":         "id is required",
		"id: x":                                  "match needs",
		"id: x\nmatch: {sys_descr: '('}":         "sys_descr",
		"id: x\nmatch: {sys_descr: a}\nrole: tv": "unknown role",
		"id: x\nmatch: {sys_descr: a}\nfoo: 1":   "field foo not found",
	} {
		if _, err := snmp.ParseProfile([]byte(raw)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", raw, err, want)
		}
	}
}

// A MikroTik router with Net-SNMP-like extras: both profiles apply, the
// MikroTik one (higher priority) last.
func TestProfilesApply(t *testing.T) {
	dev := demo.Network(time.Unix(1_790_000_000, 0))[1]
	pdus := snmptest.FromDevice(dev, snmptest.Options{Enterprise: 14988})
	pdus = append(pdus,
		pdu("1.3.6.1.4.1.14988.1.1.7.8.0", gosnmp.OctetString, []byte("RB5009UG+S+")),
		pdu("1.3.6.1.4.1.14988.1.1.7.3.0", gosnmp.OctetString, []byte("HD0123")),
		pdu("1.3.6.1.4.1.14988.1.1.4.4.0", gosnmp.OctetString, []byte("7.16.1")),
		pdu("1.3.6.1.4.1.14988.1.1.3.11.0", gosnmp.Integer, 512),
		pdu("1.3.6.1.4.1.14988.1.1.7.4.0", gosnmp.OctetString, []byte("7.15")),
		pdu("1.3.6.1.4.1.14988.1.1.7.7.0", gosnmp.OctetString, []byte("7.16.1")),
		// UCD-SNMP-MIB
		pdu("1.3.6.1.4.1.2021.11.11.0", gosnmp.Integer, 85),
		pdu("1.3.6.1.4.1.2021.4.5.0", gosnmp.Integer, 1000),
		pdu("1.3.6.1.4.1.2021.4.6.0", gosnmp.Integer, 300),
		pdu("1.3.6.1.4.1.2021.4.14.0", gosnmp.Integer, 50),
		pdu("1.3.6.1.4.1.2021.4.15.0", gosnmp.Integer, 150),
		pdu("1.3.6.1.4.1.2021.10.1.3.1", gosnmp.OctetString, []byte("0.42")),
		pdu("1.3.6.1.4.1.2021.10.1.3.2", gosnmp.OctetString, []byte("0.30")),
		pdu("1.3.6.1.4.1.2021.10.1.3.3", gosnmp.OctetString, []byte("0.25")),
		pdu("1.3.6.1.4.1.2021.13.16.2.1.3.1", gosnmp.Gauge32, uint(41000)),
		pdu("1.3.6.1.4.1.2021.13.16.2.1.3.2", gosnmp.Gauge32, uint(55500)),
	)
	agent := snmptest.Start(t, "public", pdus)
	d, err := snmp.Collect(context.Background(), target(agent, "public"))
	if err != nil {
		t.Fatal(err)
	}
	if model.Deref(d.Vendor) != "MikroTik" || model.Deref(d.Role) != model.DeviceRoleRouter ||
		model.Deref(d.Model) != "RB5009UG+S+" || model.Deref(d.Serial) != "HD0123" || model.Deref(d.OSVersion) != "7.16.1" {
		t.Fatalf("identity: vendor=%v role=%v model=%v serial=%v os=%v", model.Deref(d.Vendor), model.Deref(d.Role),
			model.Deref(d.Model), model.Deref(d.Serial), model.Deref(d.OSVersion))
	}
	if model.Deref(d.CPUPct) != 15 || model.Deref(d.MemPct) != 50 {
		t.Fatalf("cpu=%v mem=%v", model.Deref(d.CPUPct), model.Deref(d.MemPct))
	}
	if len(d.LoadAvg) != 3 || d.LoadAvg[0] != 0.42 {
		t.Fatalf("load: %v", d.LoadAvg)
	}
	temps := map[string]float64{}
	for _, tp := range d.Temperatures {
		temps[tp.Sensor] = tp.Celsius
	}
	if temps["CPU"] != 51.2 || temps["Sensors (hottest)"] != 55.5 {
		t.Fatalf("temperatures: %+v", d.Temperatures)
	}
	if d.Firmware == nil || model.Deref(d.Firmware.Current) != "7.15" || model.Deref(d.Firmware.Latest) != "7.16.1" {
		t.Fatalf("firmware: %+v", d.Firmware)
	}
}

func TestUserProfilesReplaceAndAdd(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "mine.yaml"), []byte("id: lab-ups\nname: Lab UPS\nmatch: {sys_object_id: [1.3.6.1.4.1.318]}\nvendor: APC\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "cisco.yaml"), []byte("id: cisco\nname: My Cisco\nmatch: {sys_object_id: [1.3.6.1.4.1.9]}\n"), 0o644)
	defer func() { _, _ = snmp.LoadProfiles() }()
	n, err := snmp.LoadProfiles(dir, filepath.Join(dir, "missing"))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]snmp.Profile{}
	for _, p := range snmp.Profiles() {
		byID[p.ID] = p
	}
	if n != len(byID) || byID["lab-ups"].Vendor != "APC" || byID["cisco"].Name != "My Cisco" {
		t.Fatalf("profiles: %d %+v", n, byID)
	}
	_ = os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("id: [\n"), 0o644)
	if _, err := snmp.LoadProfiles(dir); err == nil {
		t.Fatal("a broken profile must be reported")
	}
}
