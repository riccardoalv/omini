package flows

import (
	"context"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/geoip"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

var ip = netip.MustParseAddr

// home is a router with a LAN (IPv4 and IPv6) and a WAN, a phone known by
// its IPv4 address and, through the router's NDP table, its IPv6 address.
func home() topology.Topology {
	return topology.Topology{Nodes: []topology.Node{
		{ID: "dev:fw", Kind: topology.KindDevice, IP: "192.168.1.1", MAC: "58:9c:fc:00:00:01", Device: &model.Device{
			Interfaces: []model.Interface{
				{Name: "lan", IPs: []string{"192.168.1.1/24", "2804:1128:bd79:3e00::1/64"}},
				{Name: "wan", Wan: model.Ptr(true), IPs: []string{"177.45.10.2/22", "2804:1128:ffff::2/64"}},
			},
			Ndp: []model.ArpEntry{{IP: "2804:1128:bd79:3e00::42", MAC: "aa:bb:cc:00:00:42"}},
		}},
		{ID: "mac:aa:bb:cc:00:00:42", Kind: topology.KindClient, IP: "192.168.1.42", MAC: "aa:bb:cc:00:00:42"},
	}}
}

func TestDirectoryTellsLocalFromInternet(t *testing.T) {
	d := NewDirectory(home(), Names{Any: map[netip.Addr]string{ip("142.250.78.14"): "youtube.com"}})
	for addr, want := range map[string]bool{
		"192.168.1.42": true, "2804:1128:bd79:3e00::42": true, "2804:1128:bd79:3e00::99": true, "fe80::1": true,
		"142.250.78.14": false, "177.45.10.2": false, "2804:1128:ffff::9": false,
	} {
		if got := d.Local(ip(addr)); got != want {
			t.Errorf("Local(%s) = %v", addr, got)
		}
	}
	if d.Node(ip("2804:1128:bd79:3e00::42")) != "mac:aa:bb:cc:00:00:42" {
		t.Error("the phone's IPv6 address is not tied to it through NDP")
	}
	if !d.WAN(ip("177.45.10.2")) || d.WAN(ip("192.168.1.1")) {
		t.Error("the router's internet side is not told apart")
	}
	if d.Name(ip("192.168.1.42"), ip("142.250.78.14")) != "youtube.com" {
		t.Error("name not found")
	}
}

func TestInternetTrafficPerDevice(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Unix(1_790_000_000, 0)
	dir := NewDirectory(home(), Names{ByClient: map[netip.Addr]map[netip.Addr]string{ip("192.168.1.42"): {ip("142.250.78.14"): "youtube.com"}}})
	geo := func(a netip.Addr) geoip.Place {
		if a == ip("142.250.78.14") {
			return geoip.Place{Country: "US", Org: "Google LLC"}
		}
		return geoip.Place{}
	}
	svc := &Service{Store: st, Now: func() time.Time { return now }, Directory: func() *Directory { return dir }, Geo: geo}
	svc.Add(ip("192.168.1.1"), "netflow", "v9", []Record{
		// The phone watches YouTube over IPv4 and talks to another site over IPv6.
		{Src: ip("192.168.1.42"), Dst: ip("142.250.78.14"), Proto: 6, SrcPort: 50000, DstPort: 443, Bytes: 1_000},
		{Src: ip("142.250.78.14"), Dst: ip("192.168.1.42"), Proto: 6, SrcPort: 443, DstPort: 50000, Bytes: 90_000},
		{Src: ip("2a03:2880::1"), Dst: ip("2804:1128:bd79:3e00::42"), Proto: 6, SrcPort: 443, DstPort: 50001, Bytes: 5_000},
		// The same YouTube traffic after NAT, on the WAN: not counted again.
		{Src: ip("142.250.78.14"), Dst: ip("177.45.10.2"), Proto: 6, SrcPort: 443, DstPort: 60000, Bytes: 90_000},
		// A device the map does not know yet.
		{Src: ip("192.168.1.77"), Dst: ip("1.1.1.1"), Proto: 17, SrcPort: 40000, DstPort: 53, Bytes: 100},
		// Local traffic is not internet traffic.
		{Src: ip("192.168.1.42"), Dst: ip("192.168.1.10"), Proto: 6, SrcPort: 50002, DstPort: 445, Bytes: 7_000},
	})
	now = now.Add(time.Minute)
	svc.flush(false)

	usage, err := st.InternetUsage(ctx, now.Add(-time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	want := []store.Usage{{Node: "mac:aa:bb:cc:00:00:42", RX: 95_000, TX: 1_000}, {Node: "ip:192.168.1.77", TX: 100}}
	if len(usage) != 2 || usage[0] != want[0] || usage[1] != want[1] {
		t.Fatalf("usage = %+v", usage)
	}
	dests, err := st.Destinations(ctx, now.Add(-time.Hour), "mac:aa:bb:cc:00:00:42")
	if err != nil {
		t.Fatal(err)
	}
	if len(dests) != 2 || dests[0].Dest != "youtube.com" || dests[0].RX != 90_000 || dests[0].Port != 443 || dests[1].Dest != "2a03:2880::1" {
		t.Fatalf("destinations = %+v", dests)
	}
	places, err := st.Places(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(places) != 3 || places[0] != (store.PlaceUsage{Country: "US", Org: "Google LLC", RX: 90_000, TX: 1_000}) || places[1].Org != "2a03:2880::1" {
		t.Fatalf("places = %+v", places)
	}
	series, step, err := st.InternetSeries(ctx, now.Add(-10*time.Minute), now)
	if err != nil || step != time.Minute || len(series) != 11 {
		t.Fatalf("series: %d points, step %s, %v", len(series), step, err)
	}
	var rx uint64
	for _, p := range series {
		rx += p.RX
	}
	if rx != 95_000 {
		t.Fatalf("series rx = %d", rx)
	}
	if dns, _ := st.Destinations(ctx, now.Add(-time.Hour), "", 53, 853); len(dns) != 1 || dns[0].Node != "ip:192.168.1.77" || dns[0].Dest != "1.1.1.1" {
		t.Fatalf("DNS destinations = %+v", dns)
	}
}

func TestScansAreSeen(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	dir := NewDirectory(home(), Names{})
	svc := &Service{Now: func() time.Time { return now }, Directory: func() *Directory { return dir }}
	var recs []Record
	for host := range 30 {
		for _, port := range []uint16{22, 80} {
			recs = append(recs, Record{Src: ip("192.168.1.42"), Dst: netip.AddrFrom4([4]byte{192, 168, 1, byte(100 + host)}), Proto: 6, SrcPort: 40000, DstPort: port, Bytes: 60})
		}
	}
	// The DNS server answering many clients is no scan: the clients reach it.
	for c := range 60 {
		recs = append(recs, Record{Src: ip("192.168.1.53"), Dst: netip.AddrFrom4([4]byte{192, 168, 1, byte(150 + c)}), Proto: 17, SrcPort: 53, DstPort: 30000, Bytes: 80})
	}
	svc.Add(ip("192.168.1.1"), "netflow", "v9", recs)
	now = now.Add(time.Minute)
	svc.flush(false)
	scans := svc.Scans(now.Add(-time.Hour))
	if len(scans) != 1 || scans[0].Node != "mac:aa:bb:cc:00:00:42" || scans[0].Targets < ScanTargets {
		t.Fatalf("scans = %+v", scans)
	}
}
