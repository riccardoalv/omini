package snmp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/riccardoalv/omini/internal/demo"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/snmp"
	"github.com/riccardoalv/omini/internal/snmp/snmptest"
)

func target(a *snmptest.Agent, community string) snmp.Target {
	return snmp.Target{Host: "127.0.0.1", Port: a.Port, Community: community, Timeout: 300 * time.Millisecond}
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
			got, err := snmp.Collect(context.Background(), target(agent, "s3cret"))
			if err != nil {
				t.Fatal(err)
			}
			want := expected(tc.device, tc.vendor, tc.role)
			normalize(&got)
			normalize(&want)
			if !reflect.DeepEqual(got, want) {
				g, _ := json.MarshalIndent(got, "", "  ")
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

func TestProbeFindsTheCommunity(t *testing.T) {
	sw := demo.Network(time.Now())[1]
	agent := snmptest.Start(t, "homelab", snmptest.FromDevice(sw, snmptest.Options{}))

	community, ok := snmp.Probe(context.Background(), target(agent, ""), []string{"public", "homelab"})
	if !ok || community != "homelab" {
		t.Fatalf("probe = %q %v", community, ok)
	}
	if _, ok := snmp.Probe(context.Background(), target(agent, ""), []string{"public"}); ok {
		t.Fatal("a wrong community must not answer")
	}
}

func TestWrongCommunityFails(t *testing.T) {
	sw := demo.Network(time.Now())[1]
	agent := snmptest.Start(t, "right", snmptest.FromDevice(sw, snmptest.Options{}))

	_, err := snmp.Collect(context.Background(), target(agent, "wrong"))
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

	got, err := snmp.Collect(context.Background(), target(agent, "public"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "printer" || model.Deref(got.Model) != "Laser Printer" {
		t.Fatalf("unexpected device: %+v", got)
	}
}

func TestMissingHost(t *testing.T) {
	if _, err := snmp.Collect(context.Background(), snmp.Target{}); err == nil {
		t.Fatal("expected error without host")
	}
}

// TestCiscoStyleSwitch: a port-channel's members (ifStackTable), a desk
// phone's LLDP capabilities, and a MAC table kept per VLAN (community@vlan,
// asked for by the Cisco profile).
func TestCiscoStyleSwitch(t *testing.T) {
	phone := model.MACAddress("24:9a:d8:2a:eb:3a")
	sw := model.Device{
		Name: "core", Key: "00:11:22:33:44:55", Model: model.Ptr("Cisco IOS Software, Catalyst L3 Switch"),
		Interfaces: []model.Interface{
			{Name: "Gi1/0/1", Type: model.Ptr(model.InterfaceTypeEthernet)},
			{Name: "Gi1/0/2", Type: model.Ptr(model.InterfaceTypeEthernet)},
			{Name: "Gi1/0/3", Type: model.Ptr(model.InterfaceTypeEthernet)},
			{Name: "Po1", Type: model.Ptr(model.InterfaceTypeLag), Members: []string{"Gi1/0/1", "Gi1/0/2"}},
		},
		Neighbors: []model.Neighbor{{
			LocalPort: "Gi1/0/3", RemoteMAC: &phone, RemotePort: model.Ptr("WAN PORT"),
			Capabilities: []model.NeighborCapability{model.NeighborCapabilityBridge, model.NeighborCapabilityTelephone},
		}},
	}
	pdus := snmptest.FromDevice(sw, snmptest.Options{Enterprise: 9})
	// CISCO-VTP-MIB vtpVlanState: VLANs 1 and 20 (operational).
	vtp := func(v int) gosnmp.SnmpPDU {
		return gosnmp.SnmpPDU{Name: fmt.Sprintf(".1.3.6.1.4.1.9.9.46.1.3.1.1.2.1.%d", v), Type: gosnmp.Integer, Value: 1}
	}
	agent := snmptest.Start(t, "s3cret", append(pdus, vtp(1), vtp(20)))
	// VLAN 20's MAC table, only in its own community: the PC on Gi1/0/3.
	agent.AddCommunity("s3cret@20", []gosnmp.SnmpPDU{
		{Name: ".1.3.6.1.2.1.17.1.4.1.2.3", Type: gosnmp.Integer, Value: 3},
		{Name: ".1.3.6.1.2.1.17.4.3.1.2.176.123.37.24.27.109", Type: gosnmp.Integer, Value: 3},
		{Name: ".1.3.6.1.2.1.17.4.3.1.3.176.123.37.24.27.109", Type: gosnmp.Integer, Value: 3},
	})
	got, err := snmp.Collect(context.Background(), target(agent, "s3cret"))
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range got.Interfaces {
		if i.Name == "Po1" && !reflect.DeepEqual(i.Members, []string{"Gi1/0/1", "Gi1/0/2"}) {
			t.Errorf("Po1 members: %v", i.Members)
		}
	}
	if len(got.Neighbors) != 1 || !reflect.DeepEqual(got.Neighbors[0].Capabilities,
		[]model.NeighborCapability{model.NeighborCapabilityBridge, model.NeighborCapabilityTelephone}) {
		t.Errorf("neighbors: %+v", got.Neighbors)
	}
	found := false
	for _, f := range got.Fdb {
		if f.MAC == "b0:7b:25:18:1b:6d" && f.Port == "Gi1/0/3" && model.Deref(f.Vlan) == 20 {
			found = true
		}
	}
	if !found {
		t.Errorf("VLAN 20's MAC table not read: %+v", got.Fdb)
	}
}
