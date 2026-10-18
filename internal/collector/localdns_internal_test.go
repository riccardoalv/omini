package collector

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

// Once Omini set DNS names, the DNS checks test one of them instead of a
// name the scan found.
func TestDNSChecksTestANameOminiSet(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var mu sync.Mutex
	asked := map[string]string{}
	c := &Collector{store: st, opts: Options{QueryDNS: func(_ context.Context, server, name string) model.DnsQuery {
		mu.Lock()
		defer mu.Unlock()
		asked[server] = name
		return model.DnsQuery{Name: name, Ok: server == "192.168.1.1"}
	}}}
	scanned := func() topology.Topology {
		return topology.Topology{DnsChecks: []model.DnsCheck{
			{Server: "192.168.1.1", Local: &model.DnsQuery{Name: "host.lan", Ok: true}},
			{Server: "192.168.1.53"},
		}}
	}
	// No name set yet: the scan's name stays.
	topo := scanned()
	c.checkOwnDNSName(ctx, &topo)
	if len(asked) != 0 || topo.DnsChecks[0].Local.Name != "host.lan" {
		t.Fatalf("asked %v, checks %+v", asked, topo.DnsChecks)
	}
	for k, v := range map[string]string{
		dnsAppliedSetting + "aa:00:00:00:00:02": "dev:adguard|printer.home.arpa",
		dnsAppliedSetting + "aa:00:00:00:00:01": "dev:adguard|nas.home.arpa",
		dnsAppliedSetting + "aa:00:00:00:00:03": "", // turned off
	} {
		if err := st.SetSetting(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	topo = scanned()
	c.checkOwnDNSName(ctx, &topo)
	if asked["192.168.1.1"] != "nas.home.arpa" || asked["192.168.1.53"] != "nas.home.arpa" {
		t.Fatalf("asked %v", asked)
	}
	if l := topo.DnsChecks[1].Local; l == nil || l.Name != "nas.home.arpa" || l.Ok {
		t.Fatalf("checks %+v", topo.DnsChecks)
	}
}
