package snmp

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"
)

// MaxScanHosts limits a discovery scan (a /22).
const MaxScanHosts = 1024

// Found is a host that answered SNMP during discovery.
type Found struct {
	IP          string `json:"ip"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Vendor      string `json:"vendor,omitempty"`
}

// ScanOptions configures Discover.
type ScanOptions struct {
	Community   string
	Port        int
	Timeout     time.Duration // per host
	Concurrency int
}

// Discover probes every host of a subnet (e.g. "192.168.1.0/24") for SNMP v2c
// and returns those that answered, sorted by IP.
func Discover(ctx context.Context, cidr string, opts ScanOptions) ([]Found, error) {
	hosts, err := expand(cidr)
	if err != nil {
		return nil, err
	}
	if opts.Community == "" {
		opts.Community = "public"
	}
	if opts.Port == 0 {
		opts.Port = 161
	}
	if opts.Timeout == 0 {
		opts.Timeout = time.Second
	}
	if opts.Concurrency == 0 {
		opts.Concurrency = 64
	}

	var (
		mu    sync.Mutex
		found []Found
		wg    sync.WaitGroup
		sem   = make(chan struct{}, opts.Concurrency)
	)
	for _, ip := range hosts {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if f, ok := probe(ctx, ip.String(), opts); ok {
				mu.Lock()
				found = append(found, f)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	sort.Slice(found, func(i, j int) bool {
		return netip.MustParseAddr(found[i].IP).Less(netip.MustParseAddr(found[j].IP))
	})
	return found, ctx.Err()
}

func probe(ctx context.Context, ip string, opts ScanOptions) (Found, bool) {
	c, err := dial(ctx, ip, opts.Port, opts.Community, opts.Timeout)
	if err != nil {
		return Found{}, false
	}
	defer c.close()
	c.g.Retries = 0
	sys, err := c.get(oidSysName, oidSysDescr, oidSysObjectID)
	if err != nil || len(sys) == 0 {
		return Found{}, false
	}
	f := Found{IP: ip, Name: pduString(sys[oidSysName]), Vendor: vendorFromSysObjectID(pduString(sys[oidSysObjectID]))}
	if descr := pduString(sys[oidSysDescr]); descr != "" {
		f.Description, _, _ = strings.Cut(descr, "\n")
	}
	if f.Name == "" {
		f.Name = ip
	}
	return f, true
}

// expand lists the usable host addresses of an IPv4 prefix.
func expand(cidr string) ([]netip.Addr, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
	if err != nil {
		return nil, fmt.Errorf("invalid subnet %q (expected e.g. 192.168.1.0/24)", cidr)
	}
	if !prefix.Addr().Is4() {
		return nil, errors.New("only IPv4 subnets are supported")
	}
	prefix = prefix.Masked()
	size := 1 << (32 - prefix.Bits())
	if size > MaxScanHosts {
		return nil, fmt.Errorf("subnet too large: /%d has %d addresses, the maximum is /22 (%d)", prefix.Bits(), size, MaxScanHosts)
	}
	var out []netip.Addr
	for a := prefix.Addr(); prefix.Contains(a); a = a.Next() {
		out = append(out, a)
	}
	if len(out) > 2 { // drop network and broadcast addresses
		out = out[1 : len(out)-1]
	}
	return out, nil
}
