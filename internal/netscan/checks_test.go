package netscan

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
)

// The scan checks DHCP on each local network and DNS on the servers DHCP hands
// out, and puts the results on that network's device.
func TestDHCPAndDNSChecks(t *testing.T) {
	var (
		mu      sync.Mutex
		probed  []string
		queried = map[string][]string{}
	)
	s := &Integration{
		ARPPath: "testdata/arp", RoutePath: "testdata/route", Ports: []int{},
		SettleTime: time.Millisecond, NoMulticast: true, NoPing: true, NoWebTitles: true,
		Locals: func() []localNet {
			return []localNet{{
				Prefix: netip.MustParsePrefix("127.0.0.0/30"), Self: netip.MustParseAddr("127.0.0.2"),
				MAC: "00:e0:4c:68:08:a3", Iface: "eth0",
			}}
		},
		ProbeDHCP: func(_ context.Context, iface string, mac net.HardwareAddr) ([]model.DhcpOffer, error) {
			mu.Lock()
			probed = append(probed, iface+" "+mac.String())
			mu.Unlock()
			return []model.DhcpOffer{
				{ServerIP: "192.168.1.1", Dns: []string{"192.168.1.1", "192.168.1.53"}},
				{ServerIP: "192.168.1.250"}, // another server: the rule tells rogue ones
			}, nil
		},
		QueryDNS: func(_ context.Context, server, name string) model.DnsQuery {
			mu.Lock()
			queried[server] = append(queried[server], name)
			mu.Unlock()
			return model.DnsQuery{Name: name, Ok: server == "192.168.1.1"}
		},
	}
	devices, err := s.Collect(context.Background(), integration.Config{
		"subnets": "127.0.0.0/30", "snmp": false, "dns_test_name": "omini.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(probed, []string{"eth0 00:e0:4c:68:08:a3"}) {
		t.Fatalf("probed: %v", probed)
	}
	net0 := devices[0]
	if len(net0.DhcpChecks) != 1 || len(net0.DhcpChecks[0].Offers) != 2 || net0.DhcpChecks[0].Interface != "eth0" {
		t.Fatalf("dhcp checks: %+v", net0.DhcpChecks)
	}
	if len(net0.DnsChecks) != 2 || net0.DnsChecks[0].Server != "192.168.1.1" || !net0.DnsChecks[0].External.Ok ||
		net0.DnsChecks[1].External.Ok || net0.DnsChecks[0].External.Name != "omini.test" {
		t.Fatalf("dns checks: %+v", net0.DnsChecks)
	}

	// Turned off: no checks.
	probed, queried = nil, map[string][]string{}
	devices, _ = s.Collect(context.Background(), integration.Config{
		"subnets": "127.0.0.0/30", "snmp": false, "dhcp_check": false, "dns_check": false,
	})
	if len(probed) != 0 || len(queried) != 0 || devices[0].DhcpChecks != nil || devices[0].DnsChecks != nil {
		t.Fatalf("off: %v %v %+v", probed, queried, devices[0])
	}
}

func TestLocalName(t *testing.T) {
	for names, want := range map[string]string{
		"nas.home.lan":              "nas.home.lan",
		"Living-Room.local":         "",
		"10.1.168.192.in-addr.arpa": "",
		"router":                    "",
	} {
		if got := localName([]string{names}); got != want {
			t.Errorf("%q: %q", names, got)
		}
	}
}

// A host answering DNS that the clients are not given (the operator's router
// in front of the firewall) is an upstream forwarder: asked the internet name
// only, never a local name it cannot know. The gateway and DHCP's servers get
// both.
func TestUpstreamDNSIsNotAskedLocalNames(t *testing.T) {
	var mu sync.Mutex
	queried := map[string][]string{}
	s := &Integration{
		deep: map[string]deepInfo{
			"10:00:00:00:00:01": {ports: []int{53}, names: []string{"fw.home.lan"}},
			"10:00:00:00:00:02": {ports: []int{53, 80}},
		},
		QueryDNS: func(_ context.Context, server, name string) model.DnsQuery {
			mu.Lock()
			queried[server] = append(queried[server], name)
			mu.Unlock()
			return model.DnsQuery{Name: name, Ok: true}
		},
	}
	gateway := netip.MustParseAddr("192.168.1.1")
	hosts := []*hostAcc{
		{ip: gateway, mac: "10:00:00:00:00:01"},
		{ip: netip.MustParseAddr("192.168.100.1"), mac: "10:00:00:00:00:02"},
	}
	dhcp := map[string]model.DhcpCheck{"eth0": {Offers: []model.DhcpOffer{{ServerIP: "192.168.1.1", Dns: []string{"192.168.1.53"}}}}}
	checks := s.checkDNS(context.Background(), "example.com", dhcp, gateway, hosts)
	by := map[string]model.DnsCheck{}
	for _, c := range checks {
		by[c.Server] = c
	}
	if len(by) != 3 {
		t.Fatalf("servers: %+v", checks)
	}
	for _, srv := range []string{"192.168.1.1", "192.168.1.53"} {
		if c := by[srv]; model.Deref(c.Upstream) || c.Local == nil || c.Local.Name != "fw.home.lan" {
			t.Fatalf("%s: %+v", srv, c)
		}
	}
	up := by["192.168.100.1"]
	if !model.Deref(up.Upstream) || up.Local != nil || !slices.Equal(queried["192.168.100.1"], []string{"example.com"}) {
		t.Fatalf("upstream: %+v asked %v", up, queried["192.168.100.1"])
	}
}
