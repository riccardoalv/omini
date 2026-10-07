package topology_test

import (
	"slices"
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

func mac(s string) *model.MACAddress { m := model.MACAddress(s); return &m }

// scanned is what the network scan reports for a small home network.
func scanned() model.Device {
	return model.Device{
		Key: "00:e0:4c:68:00:02", Name: "opnsense.home.lan", Host: model.Ptr("192.168.1.1"),
		Role: model.Ptr(model.DeviceRoleRouter), MACs: []model.MACAddress{"00:e0:4c:68:00:02"},
		Hosts: []model.Host{
			{IP: "192.168.1.1", MAC: mac("00:e0:4c:68:00:02"), OpenPorts: []uint16{53, 443}, Hostnames: []string{"opnsense.home.lan"}},
			{IP: "192.168.1.10", MAC: mac("00:11:32:aa:00:01"), Hostnames: []string{"10.1.168.192.in-addr.arpa", "nas.home.lan"}, OpenPorts: []uint16{5000, 445}},
			{IP: "192.168.1.40", MAC: mac("da:a1:19:00:00:01"), Hostnames: []string{"Living-Room.local"}, Services: []string{"_airplay._tcp"}, Model: model.Ptr("AppleTV11,1")},
			{IP: "192.168.1.50", MAC: mac("f4:f5:d8:00:00:06"), Manufacturer: model.Ptr("Samsung Electronics"), Model: model.Ptr("QN55Q60")},
			{IP: "10.0.20.5", Hostnames: []string{"camera.iot.lan"}, OpenPorts: []uint16{554}},
		},
	}
}

func byID(t topology.Topology) map[string]topology.Node {
	out := map[string]topology.Node{}
	for _, n := range t.Nodes {
		out[n.ID] = n
	}
	return out
}

func TestScannedHostsBecomeClientsOfTheGateway(t *testing.T) {
	topo := topology.Build([]topology.Source{{IntegrationID: 1, Online: true, Devices: []model.Device{scanned()}}})
	nodes := byID(topo)
	gw := "dev:00:e0:4c:68:00:02"

	if _, ok := nodes["mac:00:e0:4c:68:00:02"]; ok {
		t.Fatal("the gateway must not appear again as a client")
	}
	if g := nodes[gw]; !slices.Equal(g.OpenPorts, []int{53, 443}) {
		t.Errorf("gateway not enriched with its scan results: %+v", g)
	}

	nas := nodes["mac:00:11:32:aa:00:01"]
	if nas.ParentID != gw || nas.Label != "nas" || nas.Hostname != "nas.home.lan" || nas.Vendor != "Synology" ||
		!slices.Equal(nas.OpenPorts, []int{445, 5000}) {
		t.Errorf("NAS = %+v", nas)
	}

	tv := nodes["mac:da:a1:19:00:00:01"]
	if tv.Label != "Living-Room" || tv.Model != "AppleTV11,1" || !slices.Equal(tv.Services, []string{"_airplay._tcp"}) || !tv.RandomMAC {
		t.Errorf("Apple TV = %+v", tv)
	}
	if s := nodes["mac:f4:f5:d8:00:00:06"]; s.Vendor != "Samsung Electronics" || s.Label != "192.168.1.50" {
		t.Errorf("UPnP manufacturer should win over the MAC vendor: %+v", s)
	}

	cam := nodes["ip:10.0.20.5"]
	if cam.ParentID != gw || cam.Label != "camera" || !slices.Equal(cam.OpenPorts, []int{554}) {
		t.Errorf("host known only by IP = %+v", cam)
	}
}

func TestScanAndIntegrationDescribingTheSameRouterMerge(t *testing.T) {
	plugin := model.Device{
		Key: "00:e0:4c:68:00:02", Name: "opnsense", Role: model.Ptr(model.DeviceRoleFirewall),
		MACs:       []model.MACAddress{"00:e0:4c:68:00:02"},
		Arp:        []model.ArpEntry{{IP: "192.168.1.10", MAC: "00:11:32:aa:00:01"}},
		DhcpLeases: []model.DhcpLease{{IP: "192.168.1.10", MAC: "00:11:32:aa:00:01", Hostname: model.Ptr("synology")}},
	}
	topo := topology.Build([]topology.Source{
		{IntegrationID: 1, Online: true, Devices: []model.Device{plugin}},
		{IntegrationID: 2, Online: true, Devices: []model.Device{scanned()}},
	})
	nodes := byID(topo)
	devices := 0
	for _, n := range nodes {
		if n.Kind == topology.KindDevice {
			devices++
		}
	}
	if devices != 1 {
		t.Fatalf("router appears %d times", devices)
	}
	if nas := nodes["mac:00:11:32:aa:00:01"]; nas.Hostname != "synology" || !slices.Equal(nas.OpenPorts, []int{445, 5000}) {
		t.Errorf("DHCP hostname should win and scan details be kept: %+v", nas)
	}
}

func TestClientsGetTheVendorFromTheMAC(t *testing.T) {
	sw := model.Device{Key: "aa:00:00:00:00:01", Name: "sw", Fdb: []model.FdbEntry{{MAC: "b8:27:eb:12:34:56", Port: "3"}}}
	nodes := byID(topology.Build([]topology.Source{{IntegrationID: 1, Online: true, Devices: []model.Device{sw}}}))
	if v := nodes["mac:b8:27:eb:12:34:56"].Vendor; v != "Raspberry Pi Foundation" {
		t.Fatalf("vendor = %q", v)
	}
}

func TestMachineIdentifiersAreNotUsedAsNames(t *testing.T) {
	gw := model.Device{Key: "aa:00:00:00:00:01", Name: "gw", Hosts: []model.Host{
		{IP: "192.168.1.54", MAC: mac("bc:24:11:00:00:54"), Hostnames: []string{"2fe55d4dad28419eac9d65a1b2c3d4e5.local", "homeassistant.local"}},
		{IP: "192.168.1.55", MAC: mac("bc:24:11:00:00:55"), Hostnames: []string{"0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0.local"}},
	}}
	nodes := byID(topology.Build([]topology.Source{{IntegrationID: 1, Online: true, Devices: []model.Device{gw}}}))
	if n := nodes["mac:bc:24:11:00:00:54"]; n.Label != "homeassistant" {
		t.Errorf("label = %q, want homeassistant", n.Label)
	}
	if n := nodes["mac:bc:24:11:00:00:55"]; n.Label != "192.168.1.55" {
		t.Errorf("a UUID must not become the label, got %q", n.Label)
	}
}
