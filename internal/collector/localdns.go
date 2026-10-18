package collector

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/riccardoalv/omini/internal/dnscheck"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// checkOwnDNSName: once Omini has set a device's DNS name, the DNS checks'
// local name is one of those (the first, sorted, so it stays the same), not
// a name the network scan found on some host: a name Omini knows should
// resolve.
func (c *Collector) checkOwnDNSName(ctx context.Context, topo *topology.Topology) {
	if len(topo.DnsChecks) == 0 {
		return
	}
	applied, err := c.store.SettingsWithPrefix(ctx, dnsAppliedSetting)
	if err != nil {
		slog.Warn("could not read the DNS names Omini set", "err", err)
		return
	}
	var names []string
	for _, v := range applied {
		if _, fqdn, ok := strings.Cut(v, "|"); ok && fqdn != "" {
			names = append(names, fqdn)
		}
	}
	if len(names) == 0 {
		return
	}
	slices.Sort(names)
	query := c.opts.QueryDNS
	if query == nil {
		query = dnscheck.Query
	}
	var wg sync.WaitGroup
	for i := range topo.DnsChecks {
		check := &topo.DnsChecks[i]
		wg.Go(func() {
			q := query(ctx, check.Server, names[0])
			check.Local = &q
		})
	}
	wg.Wait()
}

// QueryDNS resolves a name with one DNS server only (dnscheck.Query).
type QueryDNS func(ctx context.Context, server, name string) model.DnsQuery
