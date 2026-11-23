package netscan

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"

	"github.com/riccardoalv/omini/internal/dhcpcheck"
	"github.com/riccardoalv/omini/internal/dnscheck"
	"github.com/riccardoalv/omini/internal/model"
)

// DefaultDNSName is the external name each DNS server resolves in its check.
const DefaultDNSName = "example.com"

// checkDHCP broadcasts a DHCPDISCOVER on every local network, at once.
func (s *Integration) checkDHCP(ctx context.Context, locals []localNet) map[string]model.DhcpCheck {
	probe := s.ProbeDHCP
	if probe == nil {
		probe = dhcpcheck.Probe
	}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out = map[string]model.DhcpCheck{}
	)
	for _, l := range locals {
		if l.Iface == "" || l.MAC == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			mac, _ := net.ParseMAC(string(l.MAC))
			offers, err := probe(ctx, l.Iface, mac)
			c := model.DhcpCheck{Interface: l.Iface, Offers: offers}
			if err != nil && len(offers) == 0 {
				c.Error = model.Ptr(err.Error())
			}
			mu.Lock()
			out[l.Iface] = c
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// checkDNS resolves an external name and, when one is known, a local name
// with each DNS server of the network: those DHCP hands out, the gateway, and
// the hosts listening on port 53. Those last ones (only) are upstream: a
// forwarder the clients are not given (the operator's router in front of the
// firewall), asked the external name only, since it knows no local names.
func (s *Integration) checkDNS(ctx context.Context, external string, dhcp map[string]model.DhcpCheck,
	gateway netip.Addr, hosts []*hostAcc,
) []model.DnsCheck {
	query := s.QueryDNS
	if query == nil {
		query = dnscheck.Query
	}
	var servers []string
	upstream := map[string]bool{}
	add := func(ip string) {
		if a, err := netip.ParseAddr(ip); err == nil && a.Is4() && !slices.Contains(servers, ip) {
			servers = append(servers, ip)
		}
	}
	for _, c := range dhcp {
		for _, o := range c.Offers {
			for _, d := range o.Dns {
				add(d)
			}
		}
	}
	local := ""
	for _, h := range hosts {
		d := s.cached(h)
		client := h.ip == gateway && gateway.IsValid()
		if slices.Contains(d.ports, 53) || client {
			ip := h.ip.String()
			if !client && !slices.Contains(servers, ip) {
				upstream[ip] = true
			}
			add(ip)
		}
		if name := localName(d.names); name != "" && (local == "" || h.ip == gateway) {
			local = name // the gateway's own name, else any host's
		}
	}
	slices.SortFunc(servers, func(a, b string) int { return netip.MustParseAddr(a).Compare(netip.MustParseAddr(b)) })

	out := make([]model.DnsCheck, len(servers))
	var wg sync.WaitGroup
	for i, srv := range servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := model.DnsCheck{Server: srv}
			if upstream[srv] {
				c.Upstream = model.Ptr(true)
			}
			ext := query(ctx, srv, external)
			c.External = &ext
			if local != "" && !upstream[srv] {
				loc := query(ctx, srv, local)
				c.Local = &loc
			}
			out[i] = c
		}()
	}
	wg.Wait()
	return out
}

// localName is a name of the local DNS among a host's names (reverse DNS):
// fully qualified, not mDNS, not a reverse-lookup name.
func localName(names []string) string {
	for _, n := range names {
		n = strings.TrimSuffix(n, ".")
		if strings.Contains(n, ".") && !strings.HasSuffix(n, ".local") && !strings.HasSuffix(n, ".arpa") {
			return n
		}
	}
	return ""
}
