package collector

import (
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

func TestSlowDNSCountsChecksInARow(t *testing.T) {
	check := func(rtt float64) topology.Topology {
		return topology.Topology{DnsChecks: []model.DnsCheck{
			{Server: "192.168.1.1", External: &model.DnsQuery{Name: "example.com", Ok: true, RttMs: model.Ptr(rtt)}},
		}}
	}
	var s slowDNS
	if got := s.update(check(1484.6)); got["192.168.1.1"] != 1 {
		t.Fatalf("first slow check: %v", got)
	}
	// The map rebuilt with the same check: still one.
	if got := s.update(check(1484.6)); got["192.168.1.1"] != 1 {
		t.Fatalf("same check counted again: %v", got)
	}
	if got := s.update(check(900.2)); got["192.168.1.1"] != 2 {
		t.Fatalf("second slow check: %v", got)
	}
	// A fast answer starts over.
	if got := s.update(check(3.1)); got["192.168.1.1"] != 0 {
		t.Fatalf("after a fast check: %v", got)
	}
	if got := s.update(check(700.5)); got["192.168.1.1"] != 1 {
		t.Fatalf("slow again: %v", got)
	}
	// A server no longer checked is forgotten.
	if got := s.update(topology.Topology{}); len(got) != 0 {
		t.Fatalf("gone: %v", got)
	}
}
