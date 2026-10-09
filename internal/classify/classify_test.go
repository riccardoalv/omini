package classify

import (
	"reflect"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		in   Input
		want Result // Reasons are checked separately
	}{
		// DHCP fingerprints: what the device asks for when it joins.
		{
			"Android phone by its DHCP vendor class",
			Input{Kind: "client", Hostname: "moto-edge-70", DHCPVendor: "android-dhcp-14", DHCPParams: "1,3,6,15,26,28,51,58,59,43,114,108"},
			Result{Type: Phone, OS: "android"},
		},
		{
			"Windows by its DHCP parameter list",
			Input{Kind: "client", Hostname: "sala", DHCPParams: "1,3,6,15,31,33,43,44,46,47,119,121,249,252"},
			Result{Type: Computer, OS: "windows"},
		},
		{
			"Windows by its DHCP vendor class",
			Input{Kind: "client", DHCPVendor: "MSFT 5.0"},
			Result{Type: Computer, OS: "windows"},
		},
		{
			"Apple by its DHCP parameter list, an iPhone by its port",
			Input{Kind: "client", DHCPParams: "1,121,3,6,15,108,114,119,252,95,44,46", OpenPorts: []int{62078}},
			Result{Type: Phone, OS: "ios", Brand: "apple"},
		},
		{
			"Linux with dhcpcd",
			Input{Kind: "client", DHCPVendor: "dhcpcd-10.0.6:Linux-6.6.31+rpt-rpi-v8:aarch64:BCM2835"},
			Result{OS: "linux", Type: Unknown},
		},
		{
			"A desk phone's DHCP vendor class names it",
			Input{Kind: "client", DHCPVendor: "Cisco Systems, Inc. IP Phone CP-8845"},
			Result{Type: IPPhone},
		},
		// Building gear made with a 100 Mbps port.
		{
			"Badge reader (HID, page title)",
			Input{Kind: "client", Hostname: "badge-f1", Vendor: "HID Global", Titles: []string{"HID Reader"}},
			Result{Type: SmartHome},
		},
		{
			"Daikin air conditioning controller",
			Input{Kind: "client", Hostname: "hvac-f1", Titles: []string{"intelligent Touch Manager"}},
			Result{Type: AirConditioner},
		},
		{
			"Access control by MAC vendor only",
			Input{Kind: "client", Vendor: "ZKTeco Co., Ltd."},
			Result{Type: SmartHome},
		},
		// Devices seen on a real homelab network.
		{
			"Huawei solar inverter (DHCP name)",
			Input{Kind: "client", Hostname: "SUN2000", Vendor: "HUAWEI TECHNOLOGIES CO.,LTD", TTL: 255},
			Result{Type: SolarInverter, Brand: "huawei"},
		},
		{
			"Fronius inverter (MAC vendor only)",
			Input{Kind: "client", Vendor: "Fronius International GmbH"},
			Result{Type: SolarInverter},
		},
		{
			"OPNsense gateway (DNS name + FreeBSD MAC)",
			Input{Kind: "device", Role: "router", Hostname: "OPNsense", Vendor: "FreeBSD Foundation", OpenPorts: []int{22, 53, 80, 443}},
			Result{Type: Firewall, OS: "freebsd", Brand: "freebsd", Product: "opnsense"},
		},
		{
			"TrueNAS VM (web title)",
			Input{Kind: "client", Vendor: "Proxmox Server Solutions", Hostname: "TRUENAS", Titles: []string{"TrueNAS - 192.168.1.51"}, OpenPorts: []int{22, 80, 139, 443, 445}},
			Result{Type: NAS, Brand: "proxmox", Product: "truenas"},
		},
		{
			"Proxmox host (port 8006 only)",
			Input{Kind: "client", OpenPorts: []int{22, 8006}},
			Result{Type: Hypervisor, OS: "debian", Product: "proxmox"},
		},
		{
			"Home Assistant VM (mDNS)",
			Input{Kind: "client", Vendor: "Proxmox Server Solutions", Services: []string{"_home-assistant._tcp"}, OpenPorts: []int{1883, 8123}},
			Result{Type: Server, Brand: "proxmox", Product: "homeassistant"},
		},
		{
			"Jellyfin title wins over other ports",
			Input{Kind: "client", Vendor: "Proxmox Server Solutions", Titles: []string{"Jellyfin"}, OpenPorts: []int{22, 8006, 8096}},
			Result{Type: Server, Brand: "proxmox", Product: "jellyfin"},
		},
		{
			"Mercusys in AP mode",
			Input{Kind: "client", Vendor: "MERCUSYS", OpenPorts: []int{80, 443}},
			Result{Type: AccessPoint, Brand: "mercusys"},
		},
		{
			"ESP32 smart plug",
			Input{Kind: "client", Vendor: "Espressif"},
			Result{Type: SmartHome, Brand: "espressif"},
		},
		{
			"Midea air conditioner",
			Input{Kind: "client", Vendor: "GD Midea Air-Conditioning Equipment"},
			Result{Type: AirConditioner, Brand: "midea"},
		},
		{
			"Midea appliance that is not an air conditioner",
			Input{Kind: "client", Vendor: "Midea Group"},
			Result{Type: Appliance, Brand: "midea"},
		},
		{
			"Huawei phone",
			Input{Kind: "client", Vendor: "HUAWEI TECHNOLOGIES"},
			Result{Type: Phone, OS: "android", Brand: "huawei"},
		},
		{
			"Android phone with private MAC (nearby share)",
			Input{Kind: "client", RandomMAC: true, Services: []string{"_nearbypresence._tcp"}},
			Result{Type: Phone, OS: "android"},
		},
		{
			"this NixOS server",
			Input{Kind: "client", Self: true, OS: "nixos", Vendor: "Intel Corporate", Hostname: "NixOS-Laptop"},
			Result{Type: Computer, OS: "nixos", Brand: "intel"},
		},

		// Other common devices.
		{
			"iPhone (lockdown port)",
			Input{Kind: "client", RandomMAC: true, OpenPorts: []int{62078}},
			Result{Type: Phone, OS: "ios", Brand: "apple"},
		},
		{
			"iPad by hostname",
			Input{Kind: "client", Hostname: "iPad-de-Ana", Vendor: "Apple"},
			Result{Type: Tablet, OS: "ios", Brand: "apple"},
		},
		{
			"MacBook",
			Input{Kind: "client", Hostname: "MacBook-Pro.local", Vendor: "Apple", Services: []string{"_companion-link._tcp"}},
			Result{Type: Computer, OS: "macos", Brand: "apple"},
		},
		{
			"Apple TV (device-info model)",
			Input{Kind: "client", Model: "AppleTV11,1", Vendor: "Apple", Services: []string{"_airplay._tcp"}},
			Result{Type: MediaPlayer, OS: "tvos", Brand: "apple"},
		},
		{
			"Windows PC (hostname)",
			Input{Kind: "client", Hostname: "DESKTOP-4F2KQ1B", Vendor: "Micro-Star INTL", OpenPorts: []int{139, 445}},
			Result{Type: Computer, OS: "windows", Brand: "msi"},
		},
		{
			"Windows by TTL",
			Input{Kind: "client", TTL: 128, OpenPorts: []int{3389}},
			Result{Type: Computer, OS: "windows"},
		},
		{
			"Debian server (SSH banner)",
			Input{Kind: "client", Banners: []string{"SSH-2.0-OpenSSH_9.2p1 Debian-2+deb12u3"}, OpenPorts: []int{22}},
			Result{Type: Server, OS: "debian"},
		},
		{
			"Raspberry Pi",
			Input{Kind: "client", Vendor: "Raspberry Pi Foundation", OpenPorts: []int{22}},
			Result{Type: Server, Brand: "raspberrypi"},
		},
		{
			"Network printer",
			Input{Kind: "client", Vendor: "Brother Industries", OpenPorts: []int{80, 631, 9100}},
			Result{Type: Printer, Brand: "brother"},
		},
		{
			"Chromecast",
			Input{Kind: "client", Services: []string{"_googlecast._tcp"}},
			Result{Type: MediaPlayer, Brand: "google"},
		},
		{
			"Samsung TV (UPnP)",
			Input{Kind: "client", Vendor: "Samsung Electronics", Services: []string{"upnp:MediaRenderer"}, OpenPorts: []int{8001}},
			Result{Type: TV, Brand: "samsung"},
		},
		{
			"IP camera (RTSP)",
			Input{Kind: "client", OpenPorts: []int{80, 554}},
			Result{Type: Camera},
		},
		{
			"managed switch from SNMP",
			Input{Kind: "device", Role: "switch", Vendor: "Horaco"},
			Result{Type: Switch},
		},

		// Devices of the development network (testdata/devnet), as it reports them.
		{
			"Samsung TV by its model code (DHCP name)",
			Input{Kind: "client", Hostname: "Samsung-QN85B", Vendor: "Samsung Electronics Co.,Ltd"},
			Result{Type: TV, Brand: "samsung"},
		},
		{
			"Huawei LTE modem on the WAN port",
			Input{Kind: "client", Vendor: "HUAWEI TECHNOLOGIES CO.,LTD", Upstream: true},
			Result{Type: Router, Brand: "huawei"},
		},
		{
			"Huawei LTE router (web title)",
			Input{Kind: "client", Vendor: "HUAWEI TECHNOLOGIES CO.,LTD", Titles: []string{"HUAWEI B535-232"}, OpenPorts: []int{80, 443}},
			Result{Type: Router, Brand: "huawei"},
		},
		{
			"Huawei 4G router (web title)",
			Input{Kind: "client", Vendor: "HUAWEI TECHNOLOGIES CO.,LTD", Titles: []string{"Huawei 4G Router"}},
			Result{Type: Router, Brand: "huawei"},
		},
		{
			"Huawei solar inverter is no Android device (TTL 64)",
			Input{Kind: "client", Hostname: "SUN2000-10KTL-M1", Vendor: "HUAWEI TECHNOLOGIES CO.,LTD", TTL: 64},
			Result{Type: SolarInverter, Brand: "huawei"},
		},
		{
			"LG webOS TV (DHCP name)",
			Input{Kind: "client", Hostname: "LG-webOS-TV", Vendor: "LG"},
			Result{Type: TV, Brand: "lg"},
		},
		{
			"Amazon Echo Dot",
			Input{Kind: "client", Hostname: "Echo-Dot", Vendor: "Amazon Technologies Inc."},
			Result{Type: Speaker, Brand: "amazon"},
		},
		{
			"Google Nest Hub (a smart display that also casts)",
			Input{Kind: "client", Hostname: "Google-Nest-Hub", Vendor: "Google", Services: []string{"_googlecast._tcp"}},
			Result{Type: SmartHome, Brand: "google"},
		},
		{
			"Yealink desk phone (name)",
			Input{Kind: "client", Hostname: "SIP-T54W-1", Vendor: "YEALINK(XIAMEN) NETWORK TECHNOLOGY CO.,LTD"},
			Result{Type: IPPhone, Brand: "yealink"},
		},
		{
			"Yealink conference phone (vendor only)",
			Input{Kind: "client", Hostname: "SIP-CP965", Vendor: "XIAMEN YEALINK NETWORK TECHNOLOGY CO.,LTD"},
			Result{Type: IPPhone, Brand: "yealink"},
		},
		{
			"Cisco IP phone (SEP + MAC)",
			Input{Kind: "unmanaged", Hostname: "sepf87b20a1b2c3.acme.lan", Model: "Cisco IP Phone 8845"},
			Result{Type: IPPhone},
		},
		{
			"Chromecast by its name",
			Input{Kind: "client", Hostname: "Chromecast", Vendor: "Google"},
			Result{Type: MediaPlayer, Brand: "google"},
		},
		{
			"APC UPS with its network card",
			Input{Kind: "device", Hostname: "ups-01", Vendor: "APC", Model: "Smart-UPS SRT 3000", Titles: []string{"APC | Network Management Card"}, OpenPorts: []int{80, 443}},
			Result{Type: UPS, Brand: "apc"},
		},
		{
			"UPS by MAC vendor only",
			Input{Kind: "client", Vendor: "American Power Conversion Corp"},
			Result{Type: UPS, Brand: "apc"},
		},
		{
			"brand fragments match whole words only",
			Input{Kind: "client", Vendor: "Algo Communication Products"},
			Result{Type: Unknown},
		},

		{"unmanaged segment", Input{Kind: "segment"}, Result{Type: Segment}},
		{"nothing known", Input{Kind: "client"}, Result{Type: Unknown}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.in)
			reasons := got.Reasons
			got.Reasons = nil
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Classify() = %+v, want %+v (reasons %v)", got, tc.want, reasons)
			}
			if got.Type != Unknown && got.Type != Segment && len(reasons) == 0 {
				t.Errorf("no evidence recorded for %+v", got)
			}
		})
	}
}

func TestReasonsNameTheEvidence(t *testing.T) {
	r := Classify(Input{Kind: "client", Titles: []string{"TrueNAS - nas"}, Vendor: "Proxmox Server Solutions"})
	want := []string{"title:TrueNAS - nas", "vendor:Proxmox Server Solutions"}
	if len(r.Reasons) != 2 || r.Reasons[0] != want[0] || r.Reasons[1] != want[1] {
		t.Fatalf("reasons = %v, want %v", r.Reasons, want)
	}
}
