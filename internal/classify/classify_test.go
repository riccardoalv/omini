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
		// Devices seen on a real homelab network.
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
			Result{Type: Phone, OS: "ios"},
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
