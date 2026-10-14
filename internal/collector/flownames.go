package collector

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/riccardoalv/omini/internal/flows"
	"github.com/riccardoalv/omini/internal/insights"
	"github.com/riccardoalv/omini/internal/topology"
)

// Names for the places devices go: the DNS servers (AdGuard Home, Pi-hole)
// report which client looked up which name and the addresses it got; a flow
// to one of those addresses is shown by that name. They are kept in memory
// for a day and never stored.

const (
	// NamesKeep is how long a name is remembered for an address.
	NamesKeep = 24 * time.Hour
	// resolveKeep is how long Omini's own lookup of a name is reused.
	resolveKeep = time.Hour
	// resolvePerRound bounds Omini's own lookups (for servers that do not log
	// the answers).
	resolvePerRound = 200
)

type nameEntry struct {
	name string
	seen time.Time
}

type resolved struct {
	addrs []netip.Addr
	at    time.Time
}

type flowNames struct {
	mu       sync.Mutex
	byClient map[netip.Addr]map[netip.Addr]nameEntry
	any      map[netip.Addr]nameEntry
	resolved map[string]resolved // server|name → addresses
	dir      *flows.Directory
}

// Resolver looks a name up on a DNS server (server is its address).
type Resolver func(ctx context.Context, server netip.Addr, name string) ([]netip.Addr, error)

func resolveOn(ctx context.Context, server netip.Addr, name string) ([]netip.Addr, error) {
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, netip.AddrPortFrom(server, 53).String())
	}}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return r.LookupNetIP(ctx, "ip", name)
}

// update learns the lookups the DNS servers on the map reported, and
// forgets what is older than a day.
func (f *flowNames) update(ctx context.Context, topo topology.Topology, now time.Time, resolve Resolver) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byClient == nil {
		f.byClient = map[netip.Addr]map[netip.Addr]nameEntry{}
		f.any = map[netip.Addr]nameEntry{}
		f.resolved = map[string]resolved{}
	}
	budget := resolvePerRound
	for _, n := range topo.Nodes {
		if n.Device == nil || len(n.Device.DnsLookups) == 0 {
			continue
		}
		server, _ := netip.ParseAddr(n.IP)
		for _, l := range n.Device.DnsLookups {
			client, err := netip.ParseAddr(l.Client)
			name := strings.TrimSuffix(strings.ToLower(l.Domain), ".")
			if err != nil || name == "" {
				continue
			}
			var addrs []netip.Addr
			for _, a := range l.Answers {
				if ip, err := netip.ParseAddr(a); err == nil {
					addrs = append(addrs, ip.Unmap())
				}
			}
			if len(l.Answers) == 0 && server.IsValid() && resolve != nil {
				key := server.String() + "|" + name
				r, ok := f.resolved[key]
				if !ok || now.Sub(r.at) > resolveKeep {
					if budget == 0 {
						continue
					}
					budget--
					got, err := resolve(ctx, server, name)
					if err != nil {
						slog.Debug("could not look a name up", "name", name, "err", err)
					}
					r = resolved{at: now}
					for _, a := range got {
						r.addrs = append(r.addrs, a.Unmap())
					}
					f.resolved[key] = r
				}
				addrs = r.addrs
			}
			client = client.Unmap()
			for _, a := range addrs {
				m := f.byClient[client]
				if m == nil {
					m = map[netip.Addr]nameEntry{}
					f.byClient[client] = m
				}
				m[a] = nameEntry{name, now}
				f.any[a] = nameEntry{name, now}
			}
		}
	}
	for c, m := range f.byClient {
		for a, e := range m {
			if now.Sub(e.seen) > NamesKeep {
				delete(m, a)
			}
		}
		if len(m) == 0 {
			delete(f.byClient, c)
		}
	}
	for a, e := range f.any {
		if now.Sub(e.seen) > NamesKeep {
			delete(f.any, a)
		}
	}
	for k, r := range f.resolved {
		if now.Sub(r.at) > resolveKeep {
			delete(f.resolved, k)
		}
	}
	names := flows.Names{ByClient: map[netip.Addr]map[netip.Addr]string{}, Any: map[netip.Addr]string{}}
	for c, m := range f.byClient {
		names.ByClient[c] = map[netip.Addr]string{}
		for a, e := range m {
			names.ByClient[c][a] = e.name
		}
	}
	for a, e := range f.any {
		names.Any[a] = e.name
	}
	f.dir = flows.NewDirectory(topo, names)
}

// FlowDirectory tells the flow collector which addresses are local, which
// device each is and their names (rebuilt after every round).
func (c *Collector) FlowDirectory() *flows.Directory {
	c.names.mu.Lock()
	defer c.names.mu.Unlock()
	if c.names.dir == nil {
		return flows.NewDirectory(c.State().Topology, flows.Names{})
	}
	return c.names.dir
}

// syncFlowNames learns the names the DNS servers reported this round.
func (c *Collector) syncFlowNames(ctx context.Context) {
	resolve := c.opts.Resolve
	if resolve == nil {
		resolve = resolveOn
	}
	c.names.update(ctx, c.State().Topology, c.opts.Now(), resolve)
}

// flowFacts is what the last hour of flows says for the alerts.
func (c *Collector) flowFacts(ctx context.Context, topo topology.Topology, now time.Time) *insights.Flows {
	f := &insights.Flows{DNSOutside: map[string][]string{}, Upload: map[string]insights.Upload{}, Scans: map[string]int{}}
	if dests, err := c.store.Destinations(ctx, now.Add(-time.Hour), "", 53, 853); err == nil {
		for _, d := range dests {
			if !slices.Contains(f.DNSOutside[d.Node], d.Dest) {
				f.DNSOutside[d.Node] = append(f.DNSOutside[d.Node], d.Dest)
			}
		}
	}
	last := now.Truncate(time.Hour).Add(-time.Hour) // the last full hour
	if hours, err := c.store.HourlyUpload(ctx, last.Add(-7*24*time.Hour)); err == nil {
		oldest := last
		for _, byHour := range hours {
			for at := range byHour {
				if t := time.Unix(at, 0); t.Before(oldest) {
					oldest = t
				}
			}
		}
		covered := int(last.Sub(oldest) / time.Hour)
		for node, byHour := range hours {
			var sum uint64
			for at, tx := range byHour {
				if at < last.Unix() {
					sum += tx
				}
			}
			u := insights.Upload{LastHour: byHour[last.Unix()], Hours: covered}
			if covered > 0 {
				u.Average = sum / uint64(covered) //nolint:gosec // covered > 0
			}
			f.Upload[node] = u
		}
	}
	if c.opts.Flows != nil {
		for _, s := range c.opts.Flows.Scans(now.Add(-time.Hour)) {
			if s.Node == "" || c.selfNode(topo, s.Node) {
				continue // Omini's own scans are expected
			}
			f.Scans[s.Node] = max(f.Scans[s.Node], s.Targets)
		}
	}
	return f
}

// selfNode reports whether a node is the machine Omini runs on.
func (c *Collector) selfNode(topo topology.Topology, id string) bool {
	for _, n := range topo.Nodes {
		if n.ID == id {
			return c.opts.Self.Owns(n)
		}
	}
	return false
}
