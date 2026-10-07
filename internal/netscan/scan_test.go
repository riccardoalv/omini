package netscan

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
)

// TestCollectOnLoopback runs the whole scan on 127.0.0.0/30 with fixture ARP
// and route tables: 127.0.0.1 is the gateway (alive over TCP), 127.0.0.2 is a
// Raspberry Pi known only from ARP.
func TestCollectOnLoopback(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	s := &Integration{
		ARPPath: "testdata/arp", RoutePath: "testdata/route",
		Ports: []int{port}, PortTimeout: 500 * time.Millisecond, SettleTime: 10 * time.Millisecond,
		NoMulticast: true, NoPing: true,
		Locals: func() []localNet { return nil },
	}
	devices, err := s.Collect(context.Background(), integration.Config{"subnets": "127.0.0.0/30"})
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices = %+v", devices)
	}
	gw := devices[0]
	if gw.Key != "gw:127.0.0.1" || *gw.Role != "router" || *gw.Host != "127.0.0.1" {
		t.Fatalf("gateway device = %+v", gw)
	}
	if len(gw.Hosts) != 2 {
		t.Fatalf("hosts = %+v", gw.Hosts)
	}
	self, pi := gw.Hosts[0], gw.Hosts[1]
	if self.IP != "127.0.0.1" || len(self.OpenPorts) != 1 || int(self.OpenPorts[0]) != port {
		t.Errorf("gateway host = %+v (want open port %d)", self, port)
	}
	if pi.IP != "127.0.0.2" || pi.MAC == nil || *pi.MAC != "b8:27:eb:12:34:56" ||
		pi.Vendor == nil || *pi.Vendor != "Raspberry Pi Foundation" || pi.Sources[0] != "arp" {
		t.Errorf("ARP host = %+v", pi)
	}
}

func TestDeepScanIsCached(t *testing.T) {
	clock := time.Unix(1_790_000_000, 0)
	s := &Integration{DeepEvery: time.Hour, Ports: []int{}}
	s.defaults()
	s.now = func() time.Time { return clock }
	h := &hostAcc{ip: netip.MustParseAddr("127.0.0.1")}
	opts := options{ports: true, deepEvery: time.Hour}

	first := s.deepScan(context.Background(), h, netip.Addr{}, opts)
	s.deep[h.ip.String()] = deepInfo{ports: []int{22}, at: first.at}
	if got := s.deepScan(context.Background(), h, netip.Addr{}, opts); len(got.ports) != 1 {
		t.Fatal("fresh results must come from the cache")
	}
	clock = clock.Add(2 * time.Hour)
	if got := s.deepScan(context.Background(), h, netip.Addr{}, opts); len(got.ports) != 0 {
		t.Fatal("stale results must be refreshed")
	}
}

func TestTestReportsSubnets(t *testing.T) {
	s := &Integration{Locals: func() []localNet {
		return []localNet{{Prefix: netip.MustParsePrefix("192.168.1.0/24"), Self: netip.MustParseAddr("192.168.1.5")}}
	}}
	msg, err := s.Test(context.Background(), integration.Config{"subnets": "auto"})
	if err != nil || msg != "Will scan 192.168.1.0/24" {
		t.Fatalf("Test = %q, %v", msg, err)
	}
	s.Locals = func() []localNet { return nil }
	if _, err := s.Test(context.Background(), integration.Config{}); err == nil {
		t.Fatal("no local network and no subnets should fail")
	}
}

func TestOptions(t *testing.T) {
	s := &Integration{}
	o, err := s.options(integration.Config{
		"ping": false, "mdns": false, "ports": "22, 80,8000-8002", "deep_interval": float64(12),
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.ping || o.mdns || !o.arp || !o.ssdp || !o.dns {
		t.Errorf("method flags = %+v", o)
	}
	if want := []int{22, 80, 8000, 8001, 8002}; !slices.Equal(o.portList, want) {
		t.Errorf("ports = %v, want %v", o.portList, want)
	}
	if o.deepEvery != 12*time.Hour {
		t.Errorf("deepEvery = %v", o.deepEvery)
	}
	if o, _ := s.options(integration.Config{}); !slices.Equal(o.portList, CommonPorts) || o.deepEvery != 6*time.Hour {
		t.Errorf("defaults = %+v", o)
	}
	for _, bad := range []integration.Config{
		{"ports": "0"},
		{"ports": "80-70"},
		{"ports": "http"},
		{"ports": "1-2000"},
		{"deep_interval": float64(10000)},
		{"subnets": "192.168.1.0"},
	} {
		if err := s.Validate(bad); err == nil {
			t.Errorf("Validate(%v) should fail", bad)
		}
	}
}

func TestDisabledMethodsAreNotUsed(t *testing.T) {
	s := &Integration{
		ARPPath: "testdata/arp", RoutePath: "testdata/route", Ports: []int{1}, SettleTime: time.Millisecond,
		NoMulticast: true, NoPing: true, Locals: func() []localNet { return nil },
	}
	devices, err := s.Collect(context.Background(), integration.Config{"subnets": "127.0.0.0/30", "arp": false})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range devices[0].Hosts {
		if slices.Contains(h.Sources, "arp") {
			t.Fatalf("ARP disabled but used: %+v", h)
		}
	}
}
