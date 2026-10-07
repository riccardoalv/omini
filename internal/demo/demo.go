// Package demo provides a fictional homelab network, so Omini can be tried
// without any hardware. It also serves as a fixture for topology tests.
package demo

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
)

type Integration struct {
	now func() time.Time
}

func New() *Integration { return &Integration{now: time.Now} }

func (*Integration) Info() integration.Info {
	return integration.Info{
		Type:        "demo",
		Name:        "Demo network",
		Description: "A fictional homelab (firewall, switch, access points, servers and clients) to try Omini without hardware.",
		Kind:        integration.KindCore,
		Fields:      []model.FormField{},
	}
}

func (*Integration) Test(context.Context, integration.Config) (string, error) {
	return "Demo network ready", nil
}

func (i *Integration) Collect(context.Context, integration.Config) ([]model.Device, error) {
	return Network(i.now()), nil
}

type client struct {
	mac      model.MACAddress
	ip       string
	hostname string
}

// Network returns the demo devices as they would be collected at time now.
// Interface counters grow with time, so traffic rates can be computed.
func Network(now time.Time) []model.Device {
	var (
		fwLAN   = model.MACAddress("00:e0:4c:68:00:02")
		fwWAN   = model.MACAddress("00:e0:4c:68:00:01")
		swMAC   = model.MACAddress("1c:2a:a3:10:00:01")
		apMAC   = model.MACAddress("74:ac:b9:20:00:01")
		mercMAC = model.MACAddress("5c:62:8b:30:00:01")
		nas     = client{"00:11:32:aa:00:01", "192.168.1.10", "nas"}
		pve     = client{"a8:a1:59:40:00:01", "192.168.1.20", "pve"}
		tv      = client{"04:5d:4b:50:00:01", "192.168.1.30", "living-room-tv"}
		desktop = client{"d8:bb:c1:60:00:01", "192.168.1.31", "desktop"}
	)
	vms := []client{
		{"bc:24:11:00:00:01", "192.168.1.21", "home-assistant"},
		{"bc:24:11:00:00:02", "192.168.1.22", "pihole"},
		{"bc:24:11:00:00:03", "192.168.1.23", "jellyfin"},
	}
	// Wi-Fi clients of the managed AP (more than 8, so the map collapses them).
	apClients := []struct {
		client
		radio  string
		signal int64
	}{
		{client{"da:a1:19:00:00:01", "192.168.1.101", "iphone-ana"}, "wifi1", -52},
		{client{"f0:18:98:00:00:02", "192.168.1.102", "macbook-ricardo"}, "wifi1", -48},
		{client{"5e:3c:2d:00:00:03", "192.168.1.103", "pixel-8"}, "wifi1", -61},
		{client{"60:fd:a6:00:00:04", "192.168.1.104", "ipad"}, "wifi1", -58},
		{client{"68:54:fd:00:00:05", "192.168.1.105", "echo-dot"}, "wifi0", -67},
		{client{"f4:f5:d8:00:00:06", "192.168.1.106", "chromecast"}, "wifi1", -55},
		{client{"8c:79:f5:00:00:07", "192.168.1.107", "galaxy-tab"}, "wifi1", -63},
		{client{"74:c2:46:00:00:08", "192.168.1.108", "kindle"}, "wifi0", -71},
		{client{"54:e1:ad:00:00:09", "192.168.1.109", "thinkpad"}, "wifi1", -50},
		{client{"aa:bb:cc:00:00:0a", "192.168.1.110", ""}, "wifi0", -81}, // weak signal, no hostname
	}
	// Clients of the Mercusys in AP mode: not integrated, only seen behind switch port 5.
	mercClients := []client{
		{"2c:3a:e8:70:00:01", "192.168.1.120", "bedroom-tv"},
		{"ea:91:0b:70:00:02", "192.168.1.121", "guest-phone"},
		{"30:05:5c:70:00:03", "192.168.1.122", "printer"},
		{"50:ec:50:70:00:04", "192.168.1.123", "robot-vacuum"},
	}

	t := float64(now.Unix())
	// counter returns a monotonically growing byte counter averaging rate (bytes/s)
	// with a slow wave, so the map shows changing traffic.
	counter := func(rate float64, phase float64) *uint64 {
		v := rate*t + rate*120*math.Sin(t/90+phase)
		return model.Ptr(uint64(math.Max(v, 0)))
	}
	iface := func(name string, typ model.InterfaceType, mac model.MACAddress, up bool, speed uint64, rx, tx float64) model.Interface {
		i := model.Interface{Name: name, Type: &typ, Up: model.Ptr(up), RxErrors: model.Ptr(uint64(0)), TxErrors: model.Ptr(uint64(0))}
		if mac != "" {
			i.MAC = &mac
		}
		if up {
			i.SpeedMbps = model.Ptr(speed)
			i.RxBytes, i.TxBytes = counter(rx, float64(len(name))), counter(tx, float64(len(name))+1)
		}
		return i
	}
	uptime := func(days float64) *uint64 { return model.Ptr(uint64(days * 86400)) }
	eth, wlan := model.InterfaceTypeEthernet, model.InterfaceTypeWireless

	// --- Firewall: routes and serves DHCP, sees every client in ARP. ---
	fw := model.Device{
		Key: string(fwLAN), Name: "opnsense", Host: model.Ptr("192.168.1.1"),
		Role: model.Ptr(model.DeviceRoleFirewall), Vendor: model.Ptr("Deciso"), Model: model.Ptr("OPNsense 25.7"),
		UptimeS: uptime(41.3), CPUPct: model.Ptr(7.5), MemPct: model.Ptr(38.0),
		MACs: []model.MACAddress{fwLAN, fwWAN}, IPs: []string{"192.168.1.1"},
		Interfaces: []model.Interface{
			iface("igc0", eth, fwWAN, true, 1000, 4.2e6, 0.9e6),
			iface("igc1", eth, fwLAN, true, 2500, 0.9e6, 4.2e6),
		},
	}
	arp := func(c client) {
		fw.Arp = append(fw.Arp, model.ArpEntry{IP: c.ip, MAC: c.mac, Interface: model.Ptr("igc1")})
		if c.hostname != "" {
			fw.DhcpLeases = append(fw.DhcpLeases, model.DhcpLease{IP: c.ip, MAC: c.mac, Hostname: model.Ptr(c.hostname)})
		}
	}
	for _, c := range []client{
		{swMAC, "192.168.1.2", ""},
		{apMAC, "192.168.1.3", ""},
		{mercMAC, "192.168.1.4", "mercusys-ap"},
		nas, pve, tv, desktop,
	} {
		arp(c)
	}
	for _, c := range vms {
		arp(c)
	}
	for _, c := range apClients {
		arp(c.client)
	}
	for _, c := range mercClients {
		arp(c)
	}
	// A lease for a device that already left: must not appear as present.
	fw.DhcpLeases = append(fw.DhcpLeases, model.DhcpLease{IP: "192.168.1.199", MAC: "00:24:e4:99:00:01", Hostname: model.Ptr("old-laptop")})

	// --- Core switch (Horaco-like, SNMP): MAC table on every port. ---
	sw := model.Device{
		Key: string(swMAC), Name: "sw-core", Host: model.Ptr("192.168.1.2"),
		Role: model.Ptr(model.DeviceRoleSwitch), Vendor: model.Ptr("Horaco"), Model: model.Ptr("ZX-SWTGW218AS 8x2.5G + 1x10G SFP+"),
		UptimeS: uptime(12.8), MACs: []model.MACAddress{swMAC}, IPs: []string{"192.168.1.2"},
		Interfaces: []model.Interface{
			iface("1", eth, "", true, 2500, 4.2e6, 0.9e6),
			iface("2", eth, "", true, 2500, 1.1e6, 2.6e6),
			iface("3", eth, "", true, 1000, 0.5e6, 1.8e6),
			iface("4", eth, "", true, 2500, 0.8e6, 0.4e6),
			iface("5", eth, "", true, 100, 0.2e6, 0.6e6), // AP negotiated at 100M: an insight later
			iface("6", eth, "", true, 1000, 0.1e6, 1.2e6),
			iface("7", eth, "", true, 1000, 0.3e6, 0.5e6),
			iface("8", eth, "", false, 0, 0, 0),
			iface("sfp1", eth, "", false, 0, 0, 0),
		},
		Neighbors: []model.Neighbor{{
			LocalPort: "3", Protocol: model.Ptr(model.NeighborProtocolLldp),
			RemoteName: model.Ptr("ap-office"), RemotePort: model.Ptr("eth0"), RemoteMAC: &apMAC,
			RemoteIP: model.Ptr("192.168.1.3"), RemotePlatform: model.Ptr("U6-Lite"),
		}},
	}
	fdb := func(port string, macs ...model.MACAddress) {
		for _, m := range macs {
			sw.Fdb = append(sw.Fdb, model.FdbEntry{MAC: m, Port: port, Vlan: model.Ptr(uint16(1))})
		}
	}
	fdb("1", fwLAN)
	fdb("2", nas.mac)
	fdb("3", apMAC)
	for _, c := range apClients {
		fdb("3", c.mac)
	}
	fdb("4", pve.mac)
	for _, c := range vms {
		fdb("4", c.mac)
	}
	fdb("5", mercMAC)
	for _, c := range mercClients {
		fdb("5", c.mac)
	}
	fdb("6", tv.mac)
	fdb("7", desktop.mac)

	// --- Managed access point: LLDP towards the switch and a Wi-Fi client table. ---
	ap := model.Device{
		Key: string(apMAC), Name: "ap-office", Host: model.Ptr("192.168.1.3"),
		Role: model.Ptr(model.DeviceRoleAp), Vendor: model.Ptr("Ubiquiti"), Model: model.Ptr("U6-Lite"),
		UptimeS: uptime(12.8), CPUPct: model.Ptr(18.0), MemPct: model.Ptr(61.0),
		MACs: []model.MACAddress{apMAC}, IPs: []string{"192.168.1.3"},
		Interfaces: []model.Interface{
			iface("eth0", eth, apMAC, true, 1000, 1.8e6, 0.5e6),
			iface("wifi0", wlan, "", true, 144, 0.1e6, 0.3e6),
			iface("wifi1", wlan, "", true, 1201, 0.4e6, 1.5e6),
		},
		Neighbors: []model.Neighbor{{
			LocalPort: "eth0", Protocol: model.Ptr(model.NeighborProtocolLldp),
			RemoteName: model.Ptr("sw-core"), RemotePort: model.Ptr("3"), RemoteMAC: &swMAC, RemoteIP: model.Ptr("192.168.1.2"),
		}},
	}
	for _, c := range apClients {
		w := model.WirelessClient{MAC: c.mac, Interface: model.Ptr(c.radio), SSID: model.Ptr("homelab"), SignalDBM: model.Ptr(c.signal)}
		if c.radio == "wifi0" {
			w.Band = model.Ptr(model.WifiBandA24Ghz)
		} else {
			w.Band = model.Ptr(model.WifiBandA5Ghz)
		}
		ap.WirelessClients = append(ap.WirelessClients, w)
	}

	return []model.Device{fw, sw, ap}
}

// String implements fmt.Stringer for debugging.
func (c client) String() string { return fmt.Sprintf("%s (%s)", c.hostname, c.mac) }
