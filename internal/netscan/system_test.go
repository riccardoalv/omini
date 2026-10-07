package netscan

import (
	"net/netip"
	"os"
	"testing"
)

func TestParseARP(t *testing.T) {
	f, _ := os.Open("testdata/arp")
	defer f.Close()
	entries, err := parseARP(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 { // the incomplete entry is skipped
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].IP != netip.MustParseAddr("127.0.0.2") || entries[0].MAC != "b8:27:eb:12:34:56" || entries[0].Dev != "eth0" {
		t.Errorf("first entry = %+v", entries[0])
	}
}

func TestParseRoute(t *testing.T) {
	gw, dev, err := defaultGateway("testdata/route")
	if err != nil {
		t.Fatal(err)
	}
	if gw != netip.MustParseAddr("127.0.0.1") || dev != "eth0" {
		t.Fatalf("gateway = %s on %s", gw, dev)
	}
}

func TestSubnets(t *testing.T) {
	hosts, err := hostsOf(netip.MustParsePrefix("192.168.1.0/24"))
	if err != nil || len(hosts) != 254 || hosts[0].String() != "192.168.1.1" {
		t.Fatalf("hostsOf /24 = %d hosts, first %v, err %v", len(hosts), hosts[0], err)
	}
	if _, err := hostsOf(netip.MustParsePrefix("10.0.0.0/16")); err == nil {
		t.Error("/16 must be rejected")
	}
	if got := scanPrefix(netip.MustParsePrefix("10.1.2.3/16")); got.String() != "10.1.0.0/22" {
		t.Errorf("large networks shrink to the /22 around the host, got %s", got)
	}
	list, err := parseSubnets(" 192.168.1.7/24, 10.0.20.0/24 ")
	if err != nil || len(list) != 2 || list[0].String() != "192.168.1.0/24" {
		t.Fatalf("parseSubnets = %v, %v", list, err)
	}
	if _, err := parseSubnets("192.168.1.0"); err == nil {
		t.Error("missing prefix length must be rejected")
	}
}

func TestVirtualInterfacesAreSkipped(t *testing.T) {
	for _, name := range []string{"docker0", "br-1234", "veth9", "tailscale0", "wg0"} {
		if !isVirtual(name) {
			t.Errorf("%s should be skipped", name)
		}
	}
	for _, name := range []string{"eth0", "enp3s0", "wlan0", "bond0"} {
		if isVirtual(name) {
			t.Errorf("%s should be scanned", name)
		}
	}
}
