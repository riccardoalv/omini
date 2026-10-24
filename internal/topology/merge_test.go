package topology_test

import (
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// hypervisor is a Proxmox node with one VM it knows by its MAC only (no guest
// agent: no address).
func hypervisor(vm model.Device) topology.Source {
	return topology.Source{IntegrationID: 6, Online: true, Devices: []model.Device{
		{
			Key: "aa:00:00:00:00:50", Name: "pve", Host: model.Ptr("192.168.1.50"), Role: model.Ptr(model.DeviceRoleServer),
			MACs: []model.MACAddress{"aa:00:00:00:00:50"},
		},
		vm,
	}}
}

func guest(name, mac string) model.Device {
	return model.Device{
		Key: mac, Name: name, Role: model.Ptr(model.DeviceRoleServer), Model: model.Ptr("QEMU/KVM virtual machine"),
		OSVersion: model.Ptr("Linux"), MACs: []model.MACAddress{model.MACAddress(mac)},
		Interfaces: []model.Interface{{Name: "net0", MAC: mac2(mac), Vlans: &model.PortVlans{Untagged: model.Ptr(uint16(1))}}},
		Neighbors: []model.Neighbor{{
			LocalPort: "net0", Protocol: model.Ptr(model.NeighborProtocolOther),
			RemoteName: model.Ptr("pve"), RemotePort: model.Ptr("vmbr0"), RemotePlatform: model.Ptr("Proxmox VE"),
		}},
	}
}

func mac2(s string) *model.MACAddress { m := model.MACAddress(s); return &m }

// router knows which MAC has each address (ARP).
func router(arp ...model.ArpEntry) topology.Source {
	return topology.Source{IntegrationID: 3, Online: true, Devices: []model.Device{{
		Key: "aa:00:00:00:00:01", Name: "fw", Host: model.Ptr("192.168.1.1"), Role: model.Ptr(model.DeviceRoleFirewall),
		MACs: []model.MACAddress{"aa:00:00:00:00:01"}, Arp: arp,
	}}}
}

// A Docker host has no MAC in the Engine API; its address is the VM's (the
// router's ARP says so), so the two are one node, and its containers hang
// under it.
func TestADeviceWithoutMACIsTheVMWithItsAddress(t *testing.T) {
	docker := topology.Source{IntegrationID: 10, Online: true, Devices: []model.Device{
		{
			Key: "docker:192.168.1.42", Name: "containers", Host: model.Ptr("192.168.1.42"), IPs: []string{"192.168.1.42"},
			Role: model.Ptr(model.DeviceRoleServer), Vendor: model.Ptr("Docker"), OSVersion: model.Ptr("Ubuntu 26.04 LTS"),
			Services: []model.Service{{Name: "homelab_portainer", Running: model.Ptr(true)}},
		},
		{
			Key: "docker:192.168.1.42:nginx", Name: "nginx", Role: model.Ptr(model.DeviceRoleServer),
			Neighbors: []model.Neighbor{{
				LocalPort: "homelab", Protocol: model.Ptr(model.NeighborProtocolOther),
				RemoteIP: model.Ptr("192.168.1.42"), RemoteName: model.Ptr("containers"), RemotePlatform: model.Ptr("Docker"),
			}},
		},
	}}
	topo := topology.Build([]topology.Source{
		router(model.ArpEntry{IP: "192.168.1.42", MAC: "bc:24:11:8d:d7:e5"}),
		hypervisor(guest("master", "bc:24:11:8d:d7:e5")),
		docker,
	})
	var vm *topology.Node
	for i := range topo.Nodes {
		n := &topo.Nodes[i]
		if n.ID == "dev:10:docker:192.168.1.42" {
			t.Fatalf("the Docker host is not a node of its own: %+v", n)
		}
		if n.ID == "dev:bc:24:11:8d:d7:e5" {
			vm = n
		}
	}
	if vm == nil {
		t.Fatalf("no VM node: %+v", topo.Nodes)
	}
	if len(vm.Reports) != 2 || len(vm.Device.Services) != 1 || model.Deref(vm.Device.OSVersion) != "Ubuntu 26.04 LTS" {
		t.Fatalf("the VM carries Docker's report (its system and containers): %d reports, %+v", len(vm.Reports), vm.Device)
	}
	found := false
	for _, e := range topo.Edges {
		if e.Source == vm.ID && e.Target == "dev:10:docker:192.168.1.42:nginx" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the container hangs under the VM: %+v", topo.Edges)
	}
}

// A NAS running as a VM: its own plugin (TrueNAS) knows its pools, sensors and
// version; the hypervisor only knows a VM. The node shows both, its own report
// first, and keeps the hypervisor's NIC (whose VLANs Proxmox changes).
func TestANodeShowsEveryReportOfTheMachine(t *testing.T) {
	nas := topology.Source{IntegrationID: 9, Online: true, Devices: []model.Device{{
		Key: "bc:24:11:b3:cc:ae", Name: "truenas", Host: model.Ptr("192.168.1.51"), IPs: []string{"192.168.1.51"},
		Role: model.Ptr(model.DeviceRoleServer), Vendor: model.Ptr("iXsystems"), Model: model.Ptr("TrueNAS"),
		OSVersion: model.Ptr("TrueNAS 25.04.2"), MACs: []model.MACAddress{"bc:24:11:b3:cc:ae"},
		Interfaces:   []model.Interface{{Name: "enp6s18", MAC: mac2("bc:24:11:b3:cc:ae"), IPs: []string{"192.168.1.51/24"}}},
		Storage:      []model.Storage{{Mount: "tank", TotalBytes: model.Ptr(uint64(4e12)), UsedBytes: model.Ptr(uint64(3.9e12))}},
		Temperatures: []model.Temperature{{Sensor: "sda", Kind: model.Ptr(model.TemperatureKindDisk), Celsius: 41}},
	}}}
	topo := topology.Build([]topology.Source{hypervisor(guest("TrueNAS", "bc:24:11:b3:cc:ae")), nas})
	var n *topology.Node
	for i := range topo.Nodes {
		if topo.Nodes[i].ID == "dev:bc:24:11:b3:cc:ae" {
			n = &topo.Nodes[i]
		}
	}
	if n == nil {
		t.Fatalf("no NAS node: %+v", topo.Nodes)
	}
	d := n.Device
	if model.Deref(d.OSVersion) != "TrueNAS 25.04.2" || len(d.Storage) != 1 || len(d.Temperatures) != 1 || n.IntegrationID != 9 {
		t.Fatalf("the NAS's own report comes first: integration %d, %+v", n.IntegrationID, d)
	}
	names := map[string]bool{}
	for _, i := range d.Interfaces {
		names[i.Name] = true
	}
	if !names["enp6s18"] || !names["net0"] {
		t.Fatalf("both its system's port and the VM's NIC are shown: %+v", d.Interfaces)
	}
	if len(d.Neighbors) != 1 || n.Reports[0].Device.Storage != nil {
		t.Fatalf("the VM's link to its host stays, and the reports stay as they came: %+v", d.Neighbors)
	}
}

// A switch whose collection failed keeps its last MAC table: a MAC only it
// lists (a card that was replaced) is no client any more; one the router's
// ARP still sees stays.
func TestAnOfflineSwitchKeepsNoGhosts(t *testing.T) {
	sw := topology.Source{IntegrationID: 4, Online: false, Devices: []model.Device{{
		Key: "1c:2a:a3:2f:57:f7", Name: "switch", Host: model.Ptr("192.168.1.96"), Role: model.Ptr(model.DeviceRoleSwitch),
		MACs: []model.MACAddress{"1c:2a:a3:2f:57:f7"},
		Fdb: []model.FdbEntry{
			{MAC: "58:9c:fc:10:8f:2c", Port: "Port 1"}, // the firewall's old card
			{MAC: "aa:00:00:00:00:10", Port: "Port 3"}, // the NAS, still there
		},
	}}}
	topo := topology.Build([]topology.Source{
		router(model.ArpEntry{IP: "192.168.1.10", MAC: "aa:00:00:00:00:10"}),
		sw,
	})
	ids := map[string]bool{}
	for _, n := range topo.Nodes {
		ids[n.ID] = true
	}
	if ids["mac:58:9c:fc:10:8f:2c"] {
		t.Fatalf("a MAC only an offline switch lists is no client: %+v", topo.Nodes)
	}
	if !ids["mac:aa:00:00:00:00:10"] {
		t.Fatalf("a client the router's ARP sees stays: %+v", topo.Nodes)
	}
}

// A switch that got another address from DHCP: its integration still asks the
// old one and fails, but the router knows where its MAC answers now.
func TestAnOfflineDeviceShowsWhereItIsNow(t *testing.T) {
	sw := topology.Source{IntegrationID: 4, Online: false, Devices: []model.Device{{
		Key: "1c:2a:a3:2f:57:f7", Name: "switch", Host: model.Ptr("192.168.1.96"), IPs: []string{"192.168.1.96"},
		Role: model.Ptr(model.DeviceRoleSwitch), MACs: []model.MACAddress{"1c:2a:a3:2f:57:f7"},
	}}}
	topo := topology.Build([]topology.Source{
		router(model.ArpEntry{IP: "192.168.1.110", MAC: "1c:2a:a3:2f:57:f7"}),
		sw,
	})
	for _, n := range topo.Nodes {
		if n.ID == "dev:1c:2a:a3:2f:57:f7" {
			if n.IP != "192.168.1.110" || n.Online {
				t.Fatalf("the offline switch shows its address now: %+v", n)
			}
			return
		}
	}
	t.Fatalf("no switch node: %+v", topo.Nodes)
}

// After DHCP leases were cleared, a desktop got the address the hypervisor
// had, and a local DNS name still says "proxmox" for it: the name is the
// hypervisor's, not the desktop's.
func TestAStaleDNSNameDoesNotNameAnotherDevice(t *testing.T) {
	fw := router(model.ArpEntry{IP: "192.168.1.40", MAC: "00:e0:4c:f8:02:2e"})
	fw.Devices[0].DnsRecords = []model.DnsRecord{{Name: "proxmox.internal", IP: "192.168.1.40"}}
	fw.Devices[0].DhcpLeases = []model.DhcpLease{{IP: "192.168.1.40", MAC: "00:e0:4c:f8:02:2e", Hostname: model.Ptr("DESKTOP-P1KTQAF")}}
	pve := hypervisor(guest("vm", "bc:24:11:00:00:01"))
	pve.Devices[0].Name = "proxmox" // the hypervisor, now at another address
	topo := topology.Build([]topology.Source{fw, pve})
	for _, n := range topo.Nodes {
		if n.ID == "mac:00:e0:4c:f8:02:2e" {
			if n.Label != "DESKTOP-P1KTQAF" {
				t.Fatalf("the desktop keeps its own name: %+v", n)
			}
			return
		}
	}
	t.Fatalf("no desktop node: %+v", topo.Nodes)
}
