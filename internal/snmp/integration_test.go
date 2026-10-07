package snmp_test

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/demo"
	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/snmp"
	"github.com/riccardoalv/omini/internal/snmp/snmptest"
)

func newIntegration() *snmp.Integration {
	return &snmp.Integration{Timeout: 300 * time.Millisecond}
}

func config(a *snmptest.Agent, community string) integration.Config {
	return integration.Config{"host": "127.0.0.1", "community": community, "port": float64(a.Port)}
}

// TestCollectRoundTrip exposes each demo device through a fake SNMP agent and
// checks that the integration reads back what the device "has".
func TestCollectRoundTrip(t *testing.T) {
	devices := demo.Network(time.Unix(1_790_000_000, 0))
	cases := []struct {
		device     model.Device
		enterprise int
		vendor     string
		role       model.DeviceRole
	}{
		{devices[0], 12325, "FreeBSD", model.DeviceRoleFirewall}, // sysDescr mentions OPNsense
		{devices[1], 0, "", model.DeviceRoleSwitch},              // unknown enterprise (Horaco)
		{devices[2], 41112, "Ubiquiti", model.DeviceRoleSwitch},  // bridging only: an AP looks like a switch over SNMP
	}
	for _, tc := range cases {
		t.Run(tc.device.Name, func(t *testing.T) {
			agent := snmptest.Start(t, "s3cret", snmptest.FromDevice(tc.device, snmptest.Options{Enterprise: tc.enterprise}))
			got, err := newIntegration().Collect(context.Background(), config(agent, "s3cret"))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("got %d devices, want 1", len(got))
			}
			want := expected(tc.device, tc.vendor, tc.role)
			normalize(&got[0])
			normalize(&want)
			if !reflect.DeepEqual(got[0], want) {
				g, _ := json.MarshalIndent(got[0], "", "  ")
				w, _ := json.MarshalIndent(want, "", "  ")
				t.Fatalf("collected device differs\n--- got\n%s\n--- want\n%s", g, w)
			}
		})
	}
}

// expected is what SNMP can carry for a device: no Wi-Fi clients or DHCP
// leases (no standard MIB), the polled address as host, a guessed role and
// the vendor from sysObjectID.
func expected(d model.Device, vendor string, role model.DeviceRole) model.Device {
	d.Host = model.Ptr("127.0.0.1")
	d.WirelessClients, d.DhcpLeases = nil, nil
	d.Role = &role
	d.Vendor = nil
	if vendor != "" {
		d.Vendor = &vendor
	}
	if d.CPUPct != nil {
		d.CPUPct = model.Ptr(float64(int(*d.CPUPct))) // hrProcessorLoad is an integer
	}
	ifaces := make([]model.Interface, len(d.Interfaces))
	for i, iface := range d.Interfaces {
		// Counters always exist over SNMP, even on ports that are down.
		if iface.RxBytes == nil {
			iface.RxBytes = model.Ptr(uint64(0))
		}
		if iface.TxBytes == nil {
			iface.TxBytes = model.Ptr(uint64(0))
		}
		ifaces[i] = iface
	}
	d.Interfaces = ifaces
	return d
}

func normalize(d *model.Device) {
	sort.Slice(d.Fdb, func(i, j int) bool {
		return d.Fdb[i].Port+string(d.Fdb[i].MAC) < d.Fdb[j].Port+string(d.Fdb[j].MAC)
	})
	sort.Slice(d.Arp, func(i, j int) bool { return d.Arp[i].IP < d.Arp[j].IP })
	sort.Strings(d.IPs)
}

func TestTestConnection(t *testing.T) {
	sw := demo.Network(time.Now())[1]
	agent := snmptest.Start(t, "public", snmptest.FromDevice(sw, snmptest.Options{}))

	msg, err := newIntegration().Test(context.Background(), config(agent, "public"))
	if err != nil {
		t.Fatal(err)
	}
	if msg != "Connected to sw-core" {
		t.Errorf("message = %q", msg)
	}
}

func TestWrongCommunityFails(t *testing.T) {
	sw := demo.Network(time.Now())[1]
	agent := snmptest.Start(t, "right", snmptest.FromDevice(sw, snmptest.Options{}))

	_, err := newIntegration().Collect(context.Background(), config(agent, "wrong"))
	if err == nil {
		t.Fatal("expected an error with the wrong community")
	}
	if !strings.Contains(err.Error(), "check host, community") {
		t.Errorf("error should guide the user, got: %v", err)
	}
}

func TestMinimalAgent(t *testing.T) {
	// Agents that only implement the system group must still produce a device.
	d := model.Device{Key: "x", Name: "printer", Model: model.Ptr("Laser Printer")}
	agent := snmptest.Start(t, "public", snmptest.FromDevice(d, snmptest.Options{}))

	got, err := newIntegration().Collect(context.Background(), config(agent, "public"))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Name != "printer" || model.Deref(got[0].Model) != "Laser Printer" {
		t.Fatalf("unexpected device: %+v", got[0])
	}
}

func TestMissingHost(t *testing.T) {
	if _, err := newIntegration().Collect(context.Background(), integration.Config{}); err == nil {
		t.Fatal("expected error without host")
	}
}
