package collector

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

func TestFlowNamesFromTheDNSServers(t *testing.T) {
	topo := topology.Topology{Nodes: []topology.Node{
		{ID: "adguard", IP: "192.168.1.53", Device: &model.Device{DnsLookups: []model.DnsLookup{
			{Client: "192.168.1.42", Domain: "YouTube.com.", Answers: []string{"142.250.78.14"}},
		}}},
		{ID: "pihole", IP: "192.168.1.54", Device: &model.Device{DnsLookups: []model.DnsLookup{
			{Client: "192.168.1.43", Domain: "netflix.com"}, // Pi-hole logs no answers: Omini asks it
		}}},
	}}
	asked := 0
	resolve := func(_ context.Context, server netip.Addr, name string) ([]netip.Addr, error) {
		asked++
		if server.String() != "192.168.1.54" || name != "netflix.com" {
			t.Errorf("asked %s for %s", server, name)
		}
		return []netip.Addr{netip.MustParseAddr("54.155.178.5")}, nil
	}
	var f flowNames
	now := time.Unix(1_790_000_000, 0)
	f.update(context.Background(), topo, now, resolve)
	f.update(context.Background(), topo, now.Add(time.Minute), resolve)
	if asked != 1 {
		t.Errorf("asked %d times: Omini's lookups are reused", asked)
	}
	ip := netip.MustParseAddr
	if got := f.dir.Name(ip("192.168.1.42"), ip("142.250.78.14")); got != "youtube.com" {
		t.Errorf("name = %q", got)
	}
	if got := f.dir.Name(ip("192.168.1.99"), ip("54.155.178.5")); got != "netflix.com" {
		t.Errorf("anyone's name = %q", got)
	}
	// A day later, without new lookups, the names are forgotten.
	f.update(context.Background(), topology.Topology{}, now.Add(25*time.Hour), resolve)
	if got := f.dir.Name(ip("192.168.1.42"), ip("142.250.78.14")); got != "" {
		t.Errorf("kept a day-old name: %q", got)
	}
}
