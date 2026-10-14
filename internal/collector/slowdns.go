package collector

import (
	"sync"

	"github.com/riccardoalv/omini/internal/insights"
	"github.com/riccardoalv/omini/internal/topology"
)

// slowDNS counts, for each DNS server, the checks in a row it answered slowly.
// A check is counted once even when the map is rebuilt several times with it
// (its timing tells checks apart).
type slowDNS struct {
	mu   sync.Mutex
	last map[string]float64
	runs map[string]int
}

// update counts the topology's DNS checks and returns the runs of slow ones.
func (s *slowDNS) update(topo topology.Topology) map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runs == nil {
		s.last, s.runs = map[string]float64{}, map[string]int{}
	}
	seen := map[string]bool{}
	for _, c := range topo.DnsChecks {
		q := c.External
		if q == nil || q.RttMs == nil || seen[c.Server] {
			continue
		}
		seen[c.Server] = true
		rtt := *q.RttMs
		if prev, ok := s.last[c.Server]; ok && prev == rtt {
			continue // the same check, seen again
		}
		s.last[c.Server] = rtt
		if q.Ok && rtt > insights.DNSSlowMs {
			s.runs[c.Server]++
		} else {
			delete(s.runs, c.Server)
		}
	}
	for srv := range s.last {
		if !seen[srv] {
			delete(s.last, srv)
			delete(s.runs, srv)
		}
	}
	out := make(map[string]int, len(s.runs))
	for srv, n := range s.runs {
		out[srv] = n
	}
	return out
}
