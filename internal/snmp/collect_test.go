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
		if d.CPUCount == nil {
			d.CPUCount = model.Ptr(uint64(1)) // the fake agent's one processor row
		}
	}
	if d.MemPct != nil && d.MemTotalBytes == nil { // the fake agent's 1000 KiB of RAM
		d.MemTotalBytes = model.Ptr(uint64(1000 * 1024))
		d.MemUsedBytes = model.Ptr(uint64(*d.MemPct*10) * 1024)
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
		// ifAdminStatus is always there: a port is enabled unless said otherwise.
		if iface.Enabled == nil {
			iface.Enabled = model.Ptr(true)
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

// A Q-BRIDGE MAC table is indexed by filtering database id, which is not
// always the VLAN id: dot1qVlanFdbId maps it back.
func TestFdbIDIsMappedToItsVLAN(t *testing.T) {
	sw := model.Device{
		Key: "sw", Name: "sw",
		Interfaces: []model.Interface{{Name: "1/1"}, {Name: "1/2"}},
		Fdb: []model.FdbEntry{
			{MAC: "b0:7b:25:18:1b:6d", Port: "1/1", Vlan: model.Ptr[uint16](20)},
			{MAC: "b0:7b:25:18:1b:6e", Port: "1/2", Vlan: model.Ptr[uint16](1)},
		},
	}
	vlans := func(opts snmptest.Options) map[model.MACAddress]uint16 {
		agent := snmptest.Start(t, "public", snmptest.FromDevice(sw, opts))
		d, err := snmp.Collect(context.Background(), target(agent, "public"))
		if err != nil {
			t.Fatal(err)
		}
		out := map[model.MACAddress]uint16{}
		for _, f := range d.Fdb {
			out[f.MAC] = model.Deref(f.Vlan)
		}
		return out
	}
	want := map[model.MACAddress]uint16{"b0:7b:25:18:1b:6d": 20, "b0:7b:25:18:1b:6e": 1}
	if got := vlans(snmptest.Options{FdbIDs: map[uint16]int{1: 1, 20: 3}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("with dot1qVlanFdbId: %v", got)
	}
	// Without dot1qVlanCurrentTable the fdbId is taken as the VLAN id.
	if got := vlans(snmptest.Options{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("without it: %v", got)
	}
}

// One lost packet must not hide an agent from the probe.
func TestProbeRetriesALostPacket(t *testing.T) {
	sw := demo.Network(time.Now())[1]
	agent := snmptest.Start(t, "homelab", snmptest.FromDevice(sw, snmptest.Options{}))
	agent.Drop(1)
	if community, ok := snmp.Probe(context.Background(), target(agent, ""), []string{"homelab"}); !ok || community != "homelab" {
		t.Fatalf("probe = %q %v", community, ok)
	}
}

// Without LLDP the key must not depend on interface order: the bridge
// address when the switch has one, else its lowest Ethernet MAC.
func TestKeyWithoutLLDPIsStable(t *testing.T) {
	sw := model.Device{Key: "sw", Name: "sw", Interfaces: []model.Interface{
		{Name: "vlan1", Type: model.Ptr(model.InterfaceTypeVlan), MAC: model.Ptr[model.MACAddress]("02:00:00:00:00:01")},
		{Name: "1/1", Type: model.Ptr(model.InterfaceTypeEthernet), MAC: model.Ptr[model.MACAddress]("00:11:22:33:44:66")},
		{Name: "1/2", Type: model.Ptr(model.InterfaceTypeEthernet), MAC: model.Ptr[model.MACAddress]("00:11:22:33:44:55")},
	}}
	key := func(extra ...gosnmp.SnmpPDU) string {
		agent := snmptest.Start(t, "public", append(snmptest.FromDevice(sw, snmptest.Options{}), extra...))
		d, err := snmp.Collect(context.Background(), target(agent, "public"))
		if err != nil {
			t.Fatal(err)
		}
		return d.Key
	}
	if k := key(); k != "00:11:22:33:44:55" {
		t.Fatalf("lowest Ethernet MAC: %s", k)
	}
	bridge := gosnmp.SnmpPDU{Name: ".1.3.6.1.2.1.17.1.1.0", Type: gosnmp.OctetString, Value: []byte{0, 0x11, 0x22, 0x33, 0x44, 0x50}}
	if k := key(bridge); k != "00:11:22:33:44:50" {
		t.Fatalf("bridge address: %s", k)
	}
	// With LLDP the chassis MAC stays the key.
	sw.Key = "00:11:22:33:44:77"
	if k := key(bridge); k != "00:11:22:33:44:77" {
		t.Fatalf("LLDP chassis: %s", k)
	}
}

// A processor table that times out must not hide the memory.
func TestMemoryIsReadWhenTheCPUTableFails(t *testing.T) {
	d := model.Device{Key: "x", Name: "srv", CPUPct: model.Ptr(12.0), MemPct: model.Ptr(40.0)}
	agent := snmptest.Start(t, "public", snmptest.FromDevice(d, snmptest.Options{}))
	agent.Mute("1.3.6.1.2.1.25.3.3.1.2") // hrProcessorLoad
	got, err := snmp.Collect(context.Background(), target(agent, "public"))
	if err != nil {
		t.Fatal(err)
	}
	if got.CPUPct != nil || model.Deref(got.MemPct) != 40 {
		t.Fatalf("cpu %v, memory %v", got.CPUPct, got.MemPct)
	}
}

// Every hrProcessorLoad row is a processor: their number is the CPU count.
func TestCPUCountIsTheNumberOfProcessorRows(t *testing.T) {
	d := model.Device{Key: "x", Name: "nas", CPUPct: model.Ptr(30.0), CPUCount: model.Ptr(uint64(4))}
	agent := snmptest.Start(t, "public", snmptest.FromDevice(d, snmptest.Options{}))
	got, err := snmp.Collect(context.Background(), target(agent, "public"))
	if err != nil {
		t.Fatal(err)
	}
	if model.Deref(got.CPUCount) != 4 || model.Deref(got.CPUPct) != 30 {
		t.Fatalf("cpu count %v, load %v", got.CPUCount, got.CPUPct)
	}
	// No processor table: no count either.
	agent = snmptest.Start(t, "public", snmptest.FromDevice(model.Device{Key: "y", Name: "sw"}, snmptest.Options{}))
	if got, err = snmp.Collect(context.Background(), target(agent, "public")); err != nil || got.CPUCount != nil {
		t.Fatalf("cpu count without the table: %v (%v)", got.CPUCount, err)
	}
}

// RAM in bytes is hrStorageRAM's size and used times its allocation units.
func TestMemoryBytesFromTheRAMRow(t *testing.T) {
	const gib = 1 << 30
	d := model.Device{Key: "x", Name: "nas", MemTotalBytes: model.Ptr(uint64(8 * gib)), MemUsedBytes: model.Ptr(uint64(2 * gib))}
	agent := snmptest.Start(t, "public", snmptest.FromDevice(d, snmptest.Options{}))
	got, err := snmp.Collect(context.Background(), target(agent, "public"))
	if err != nil {
		t.Fatal(err)
	}
	if model.Deref(got.MemTotalBytes) != 8*gib || model.Deref(got.MemUsedBytes) != 2*gib || model.Deref(got.MemPct) != 25 {
		t.Fatalf("memory total %v, used %v, pct %v", got.MemTotalBytes, got.MemUsedBytes, got.MemPct)
	}
}

// A NAS's volumes are hrStorageFixedDisk rows; RAM, virtual memory, empty
// rows and pseudo file systems are not disks.
func TestStorageFromFixedDisks(t *testing.T) {
	const gib = 1 << 30
	disks := []model.Storage{
		{Mount: "/", TotalBytes: model.Ptr(uint64(2 * gib)), UsedBytes: model.Ptr(uint64(gib))},
		{Mount: "/volume1", TotalBytes: model.Ptr(uint64(4000 * gib)), UsedBytes: model.Ptr(uint64(3800 * gib))},
		{Mount: `C:\ Label:System  Serial Number 1a2b`, TotalBytes: model.Ptr(uint64(100 * gib)), UsedBytes: model.Ptr(uint64(10 * gib))},
		{Mount: "/run"},
		{Mount: "/dev/shm"},
		{Mount: "/snap/core22/1380"},
		{Mount: "/empty"}, // size 0
	}
	for i := range disks[3:6] {
		disks[3+i].TotalBytes, disks[3+i].UsedBytes = model.Ptr(uint64(gib)), model.Ptr(uint64(gib))
	}
	d := model.Device{Key: "x", Name: "nas", MemPct: model.Ptr(50.0), Storage: disks}
	pdus := snmptest.FromDevice(d, snmptest.Options{})
	// Virtual memory (hrStorageVirtualMemory), not a disk.
	for col, v := range map[int]int{4: 1024, 5: 100, 6: 10} {
		pdus = append(pdus, gosnmp.SnmpPDU{Name: fmt.Sprintf(".1.3.6.1.2.1.25.2.3.1.%d.3", col), Type: gosnmp.Integer, Value: v})
	}
	pdus = append(pdus,
		gosnmp.SnmpPDU{Name: ".1.3.6.1.2.1.25.2.3.1.2.3", Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1.2.1.25.2.1.3"},
		gosnmp.SnmpPDU{Name: ".1.3.6.1.2.1.25.2.3.1.3.3", Type: gosnmp.OctetString, Value: "Virtual memory"})
	agent := snmptest.Start(t, "public", pdus)
	got, err := snmp.Collect(context.Background(), target(agent, "public"))
	if err != nil {
		t.Fatal(err)
	}
	want := append([]model.Storage{}, disks[:3]...)
	want[2].Mount = `C:\` // Windows: the label and serial number are not the mount
	if !reflect.DeepEqual(got.Storage, want) {
		t.Fatalf("storage: %+v", got.Storage)
	}
	if model.Deref(got.MemPct) != 50 {
		t.Fatalf("memory: %v", got.MemPct)
	}
}

// IPv6 neighbors are ipNetToPhysicalTable's IPv6 rows, named by their port;
// its IPv4 rows are ARP only for agents without ipNetToMediaTable.
func TestNDPFromIPNetToPhysical(t *testing.T) {
	d := model.Device{
		Key: "x", Name: "router",
		Interfaces: []model.Interface{{Name: "lan"}, {Name: "wan"}},
		Arp:        []model.ArpEntry{{IP: "192.168.1.10", MAC: "aa:bb:cc:00:00:10", Interface: model.Ptr("lan")}},
		Ndp: []model.ArpEntry{
			{IP: "2804:1128:bd79:3e00::42", MAC: "aa:bb:cc:00:00:42", Interface: model.Ptr("lan")},
			{IP: "fe80::1", MAC: "aa:bb:cc:00:00:01", Interface: model.Ptr("wan")},
		},
	}
	pdus := snmptest.FromDevice(d, snmptest.Options{})
	// The same ARP entry in ipNetToPhysicalTable must not be read twice.
	pdus = append(pdus, gosnmp.SnmpPDU{Name: ".1.3.6.1.2.1.4.35.1.4.1.1.4.192.168.1.10", Type: gosnmp.OctetString, Value: []byte{0xaa, 0xbb, 0xcc, 0, 0, 0x10}})
	agent := snmptest.Start(t, "public", pdus)
	got, err := snmp.Collect(context.Background(), target(agent, "public"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Ndp, d.Ndp) {
		t.Fatalf("ndp: %+v", got.Ndp)
	}
	if !reflect.DeepEqual(got.Arp, d.Arp) {
		t.Fatalf("arp: %+v", got.Arp)
	}
	// Without ipNetToMediaTable its IPv4 rows are the ARP.
	d.Arp = nil
	agent = snmptest.Start(t, "public", append(snmptest.FromDevice(d, snmptest.Options{}), pdus[len(pdus)-1]))
	if got, err = snmp.Collect(context.Background(), target(agent, "public")); err != nil {
		t.Fatal(err)
	}
	if want := []model.ArpEntry{{IP: "192.168.1.10", MAC: "aa:bb:cc:00:00:10", Interface: model.Ptr("lan")}}; !reflect.DeepEqual(got.Arp, want) {
		t.Fatalf("arp from ipNetToPhysicalTable: %+v", got.Arp)
	}
}
