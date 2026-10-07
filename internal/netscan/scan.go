// Package netscan implements the built-in "Network scan" integration: it
// finds every device on the local networks without any configuration on
// them, combining ARP, ICMP, TCP probes, reverse DNS, NetBIOS, mDNS and SSDP.
package netscan

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/oui"
	"github.com/riccardoalv/omini/internal/webui"
)

// Integration is the network scanner. Zero values of the exported fields mean
// the production defaults; tests override them.
type Integration struct {
	ARPPath     string        // default /proc/net/arp
	RoutePath   string        // default /proc/net/route
	Ports       []int         // default CommonPorts
	PortTimeout time.Duration // default 700ms
	SettleTime  time.Duration // wait after the sweep for ARP/ICMP answers; default 1.5s
	DeepEvery   time.Duration // how often ports and names are re-checked per host; default 6h
	NoMulticast bool          // disable mDNS/SSDP (tests)
	NoPing      bool          // disable ICMP (tests)
	NoWebTitles bool          // disable web title detection (tests)
	OSRelease   string        // default /etc/os-release
	Locals      func() []localNet

	mu   sync.Mutex
	deep map[string]deepInfo // per host (MAC, or IP when unknown)
	now  func() time.Time
}

type deepInfo struct {
	ports   []int
	names   []string
	netbios string
	banner  string   // SSH version string
	titles  []string // web interface titles
	at      time.Time
}

func New() *Integration { return &Integration{} }

func (*Integration) Info() integration.Info {
	return integration.Info{
		Type: "network",
		Name: "Network scan",
		Description: "Finds every device on your network with no setup on them: ARP, ping, open ports, " +
			"reverse DNS, NetBIOS, mDNS/Bonjour and UPnP. Run Omini with host networking for best results.",
		Kind:   integration.KindCore,
		Fields: fields(),
	}
}

func fields() []model.FormField {
	methods := model.Ptr("Methods")
	advanced := model.Ptr("Advanced")
	method := func(key, label, help string) model.FormField {
		return model.FormField{Key: key, Type: model.FormFieldTypeBool, Label: model.Ptr(label), Help: model.Ptr(help), Default: true, Group: methods}
	}
	return []model.FormField{
		{
			Key: "subnets", Type: model.FormFieldTypeString, Label: model.Ptr("Subnets"), Default: "auto",
			Help: model.Ptr(`"auto" scans the networks this server is connected to, or list them: 192.168.1.0/24, 10.0.20.0/24`),
		},
		method("arp", "ARP", "Finds every device on the local network, even ones that ignore everything else."),
		method("ping", "Ping (ICMP)", "Finds devices that answer ping; the reply also hints the operating system."),
		method("port_scan", "Open ports", "Checks common ports to identify services (web, SSH, SMB, printers, cameras...)."),
		method("dns", "Reverse DNS", "Asks this server's DNS and the router's DNS for device names."),
		method("netbios", "NetBIOS", "Asks Windows and Samba machines for their names."),
		method("mdns", "mDNS / Bonjour", "Listens to what devices announce: names, models and services (AirPlay, Chromecast, printers...)."),
		method("ssdp", "SSDP / UPnP", "Reads manufacturer and model from TVs, routers and media players."),
		method("web_titles", "Web page titles", "Reads the title of web interfaces to recognize apps (Proxmox, TrueNAS, Home Assistant...)."),
		method("ssh_banners", "SSH banners", "Reads the SSH version line, which often names the operating system."),
		{
			Key: "ports", Type: model.FormFieldTypeString, Label: model.Ptr("Ports to check"), Group: advanced,
			Help: model.Ptr("Empty uses the common homelab ports. Example: 22,80,443,8000-8100 (at most 1024 ports)."),
		},
		{
			Key: "deep_interval", Type: model.FormFieldTypeInt, Label: model.Ptr("Re-check ports and names every (hours)"),
			Default: 6, Group: advanced,
			Help: model.Ptr("Ports, names and banners are checked once per new device and again after this many hours."),
		},
	}
}

// options are the scan settings of one integration instance.
type options struct {
	arp, ping, ports, dns, netbios, mdns, ssdp, titles, banners bool
	portList                                                    []int
	deepEvery                                                   time.Duration
}

// Validate checks the settings before they are saved.
func (s *Integration) Validate(cfg integration.Config) error {
	if _, err := parseSubnetSpec(cfg.String("subnets")); err != nil {
		return err
	}
	_, err := s.options(cfg)
	return err
}

func (s *Integration) options(cfg integration.Config) (options, error) {
	s.mu.Lock()
	s.defaults()
	s.mu.Unlock()
	o := options{
		arp: cfg.Bool("arp", true), ping: cfg.Bool("ping", true) && !s.NoPing,
		ports: cfg.Bool("port_scan", true), dns: cfg.Bool("dns", true), netbios: cfg.Bool("netbios", true),
		mdns: cfg.Bool("mdns", true) && !s.NoMulticast, ssdp: cfg.Bool("ssdp", true) && !s.NoMulticast,
		titles: cfg.Bool("web_titles", true) && !s.NoWebTitles, banners: cfg.Bool("ssh_banners", true),
		portList: s.Ports, deepEvery: s.DeepEvery,
	}
	if spec := strings.TrimSpace(cfg.String("ports")); spec != "" {
		list, err := parsePorts(spec)
		if err != nil {
			return o, err
		}
		o.portList = list
	}
	if h := cfg.Int("deep_interval", 0); h > 0 {
		if h > 24*30 {
			return o, fmt.Errorf("re-check interval must be at most 720 hours")
		}
		o.deepEvery = time.Duration(h) * time.Hour
	}
	return o, nil
}

// parsePorts parses "22,80,8000-8100".
func parsePorts(spec string) ([]int, error) {
	var out []int
	seen := map[int]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		a, err1 := strconv.Atoi(strings.TrimSpace(lo))
		b := a
		var err2 error
		if isRange {
			b, err2 = strconv.Atoi(strings.TrimSpace(hi))
		}
		if err1 != nil || err2 != nil || a < 1 || b > 65535 || a > b {
			return nil, fmt.Errorf("invalid port or range %q (use e.g. 22,80,8000-8100)", part)
		}
		for p := a; p <= b; p++ {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
		if len(out) > 1024 {
			return nil, fmt.Errorf("too many ports (at most 1024)")
		}
	}
	return out, nil
}

func (s *Integration) Test(ctx context.Context, cfg integration.Config) (string, error) {
	prefixes, err := s.prefixes(cfg)
	if err != nil {
		return "", err
	}
	names := make([]string, len(prefixes))
	for i, p := range prefixes {
		names[i] = p.String()
	}
	return "Will scan " + strings.Join(names, ", "), nil
}

func (s *Integration) defaults() {
	if s.ARPPath == "" {
		s.ARPPath = "/proc/net/arp"
	}
	if s.RoutePath == "" {
		s.RoutePath = "/proc/net/route"
	}
	if s.Ports == nil {
		s.Ports = CommonPorts
	}
	if s.PortTimeout == 0 {
		s.PortTimeout = 700 * time.Millisecond
	}
	if s.SettleTime == 0 {
		s.SettleTime = 1500 * time.Millisecond
	}
	if s.DeepEvery == 0 {
		s.DeepEvery = 6 * time.Hour
	}
	if s.Locals == nil {
		s.Locals = localNetworks
	}
	if s.OSRelease == "" {
		s.OSRelease = "/etc/os-release"
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.deep == nil {
		s.deep = map[string]deepInfo{}
	}
}

func (s *Integration) prefixes(cfg integration.Config) ([]netip.Prefix, error) {
	s.mu.Lock()
	s.defaults()
	s.mu.Unlock()
	spec, err := parseSubnetSpec(cfg.String("subnets"))
	if err != nil {
		return nil, err
	}
	if spec == nil {
		var out []netip.Prefix
		for _, l := range s.Locals() {
			if !containsPrefix(out, l.Prefix) {
				out = append(out, l.Prefix)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("no private IPv4 network found on this server; list the subnets to scan")
		}
		return out, nil
	}
	return spec, nil
}

func containsPrefix(list []netip.Prefix, p netip.Prefix) bool {
	for _, x := range list {
		if x == p {
			return true
		}
	}
	return false
}

// hostAcc accumulates what every method learned about one address.
type hostAcc struct {
	ip  netip.Addr
	mac model.MACAddress
	ann announce
	src []string
	ttl int
	os  string
}

func (s *Integration) Collect(ctx context.Context, cfg integration.Config) ([]model.Device, error) {
	prefixes, err := s.prefixes(cfg)
	if err != nil {
		return nil, err
	}
	opts, err := s.options(cfg)
	if err != nil {
		return nil, err
	}
	locals := s.Locals()
	inScope := func(ip netip.Addr) bool {
		for _, p := range prefixes {
			if p.Contains(ip) {
				return true
			}
		}
		return false
	}
	self := map[netip.Addr]localNet{}
	for _, l := range locals {
		self[l.Self] = l
	}

	var targets []netip.Addr
	for _, p := range prefixes {
		hs, err := hostsOf(p)
		if err != nil {
			return nil, err
		}
		for _, h := range hs {
			if _, mine := self[h]; !mine {
				targets = append(targets, h)
			}
		}
	}

	var (
		mu    sync.Mutex
		hosts = map[netip.Addr]*hostAcc{}
	)
	seen := func(ip netip.Addr, source string) *hostAcc {
		mu.Lock()
		defer mu.Unlock()
		h := hosts[ip]
		if h == nil {
			h = &hostAcc{ip: ip}
			hosts[ip] = h
		}
		h.src = appendUnique(h.src, source)
		return h
	}

	// Multicast discovery runs during the sweep.
	var (
		wg               sync.WaitGroup
		mdnsRes, ssdpRes map[netip.Addr]announce
	)
	if opts.mdns {
		wg.Add(1)
		go func() { defer wg.Done(); mdnsRes = browseMDNS(ctx, 3*time.Second) }()
	}
	if opts.ssdp {
		wg.Add(1)
		go func() { defer wg.Done(); ssdpRes = browseSSDP(ctx, 3*time.Second, inScope) }()
	}

	// Sweep: every address gets one UDP datagram (forces ARP) and an ICMP echo.
	var ping *pinger
	if opts.ping {
		if p, err := newPinger(); err == nil {
			ping = p
			defer p.close()
		} else {
			slog.Debug("icmp unavailable, using ARP and TCP only", "err", err)
		}
	}
	sweep(ctx, targets, 128, func(ip netip.Addr) {
		if opts.arp {
			triggerARP(ip)
		}
		if ping != nil {
			ping.send(ip, 1)
		}
	})
	sleep(ctx, s.SettleTime)

	// ARP cache: every device that answered ARP, even if it ignores everything else.
	arpOK := false
	if entries, err := readARP(s.ARPPath); opts.arp && err == nil {
		arpOK = true
		for _, e := range entries {
			if inScope(e.IP) && !e.MAC.IsGroup() {
				seen(e.IP, "arp").mac = e.MAC
			}
		}
	}
	if ping != nil {
		for ip, ttl := range ping.replies() {
			if inScope(ip) {
				seen(ip, "icmp").ttl = ttl
			}
		}
	}

	// Addresses outside directly connected networks have no ARP: check them over TCP.
	direct := func(ip netip.Addr) bool {
		for _, l := range locals {
			if l.Prefix.Contains(ip) {
				return true
			}
		}
		return false
	}
	var tcpTargets []netip.Addr
	mu.Lock()
	for _, ip := range targets {
		if hosts[ip] == nil && (!arpOK || !direct(ip)) {
			tcpTargets = append(tcpTargets, ip)
		}
	}
	mu.Unlock()
	sweep(ctx, tcpTargets, 64, func(ip netip.Addr) {
		if tcpAlive(ctx, ip, livenessPorts, 400*time.Millisecond) {
			seen(ip, "tcp")
		}
	})

	wg.Wait()
	for src, res := range map[string]map[netip.Addr]announce{"mdns": mdnsRes, "ssdp": ssdpRes} {
		for ip, a := range res {
			if inScope(ip) {
				if _, mine := self[ip]; !mine {
					h := seen(ip, src)
					h.ann.merge(a)
				}
			}
		}
	}

	// Ports and names: once per new host, then every DeepEvery.
	var list []*hostAcc
	for _, h := range hosts {
		list = append(list, h)
	}
	gateway, _, _ := defaultGateway(s.RoutePath)
	sweepHosts(ctx, list, 32, func(h *hostAcc) {
		d := s.deepScan(ctx, h, gateway, opts)
		if len(d.ports) > 0 {
			h.src = appendUnique(h.src, "tcp")
		}
		if len(d.names) > 0 {
			h.src = appendUnique(h.src, "dns")
		}
		if d.netbios != "" {
			h.src = appendUnique(h.src, "netbios")
		}
	})

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return s.devices(prefixes, locals, hosts), nil
}

// deepScan returns cached port/name results, refreshing them when stale.
func (s *Integration) deepScan(ctx context.Context, h *hostAcc, gateway netip.Addr, opts options) deepInfo {
	key := string(h.mac)
	if key == "" {
		key = h.ip.String()
	}
	s.mu.Lock()
	d, ok := s.deep[key]
	s.mu.Unlock()
	if ok && s.now().Sub(d.at) < opts.deepEvery {
		return d
	}
	d = deepInfo{at: s.now()}
	if opts.ports {
		d.ports = scanPorts(ctx, h.ip, opts.portList, s.PortTimeout)
		sort.Ints(d.ports)
	}
	if opts.dns {
		d.names = reverseDNS(ctx, h.ip, gateway, 2*time.Second)
	}
	if opts.netbios {
		d.netbios = netbiosName(h.ip, 800*time.Millisecond)
	}
	if opts.banners && slices.Contains(d.ports, 22) {
		d.banner = sshBanner(ctx, h.ip, 1500*time.Millisecond)
	}
	if opts.titles {
		var web []int
		for _, p := range d.ports {
			if webPorts[p] {
				web = append(web, p)
			}
		}
		if len(web) > 0 {
			for _, svc := range webui.Titles(ctx, h.ip.String(), web) {
				d.titles = appendUnique(d.titles, svc.Title)
			}
		}
	}
	if ctx.Err() == nil {
		s.mu.Lock()
		s.deep[key] = d
		s.mu.Unlock()
	}
	return d
}

func (s *Integration) cached(h *hostAcc) deepInfo {
	key := string(h.mac)
	if key == "" {
		key = h.ip.String()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deep[key]
}

// devices groups hosts under their gateway (the router) or, without one, under
// a device standing for the scanned network.
func (s *Integration) devices(prefixes []netip.Prefix, locals []localNet, hosts map[netip.Addr]*hostAcc) []model.Device {
	gwIP, _, gwErr := defaultGateway(s.RoutePath)

	// This server is on the network too.
	hostname, _ := os.Hostname()
	for _, l := range locals {
		if _, ok := hosts[l.Self]; !ok && containsAny(prefixes, l.Self) {
			hosts[l.Self] = &hostAcc{
				ip: l.Self, mac: l.MAC, src: []string{"self"}, os: localOS(s.OSRelease),
				ann: announce{Hostnames: []string{hostname}, Services: []string{"omini"}},
			}
		}
	}

	byPrefix := map[netip.Prefix][]model.Host{}
	var gateway *model.Host
	for _, h := range hosts {
		mh := s.toHost(h)
		if gwErr == nil && h.ip == gwIP {
			gateway = &mh
			continue
		}
		for _, p := range prefixes {
			if p.Contains(h.ip) {
				byPrefix[p] = append(byPrefix[p], mh)
				break
			}
		}
	}

	var out []model.Device
	for _, p := range prefixes {
		list := byPrefix[p]
		sort.Slice(list, func(i, j int) bool {
			return netip.MustParseAddr(list[i].IP).Less(netip.MustParseAddr(list[j].IP))
		})
		if gateway != nil && p.Contains(gwIP) {
			out = append(out, gatewayDevice(*gateway, list))
			continue
		}
		out = append(out, model.Device{
			Key:   "net:" + p.String(),
			Name:  "Network " + p.String(),
			Role:  model.Ptr(model.DeviceRoleUnknown),
			Hosts: list,
		})
	}
	return out
}

func gatewayDevice(gw model.Host, hosts []model.Host) model.Device {
	d := model.Device{
		Key:   "gw:" + gw.IP,
		Name:  "Gateway",
		Host:  model.Ptr(gw.IP),
		Role:  model.Ptr(model.DeviceRoleRouter),
		IPs:   []string{gw.IP},
		Hosts: hosts,
	}
	if gw.MAC != nil {
		d.Key = string(*gw.MAC)
		d.MACs = []model.MACAddress{*gw.MAC}
	}
	if len(gw.Hostnames) > 0 {
		d.Name = gw.Hostnames[0]
	}
	d.Vendor = gw.Manufacturer
	if d.Vendor == nil {
		d.Vendor = gw.Vendor
	}
	d.Model = gw.Model
	// Keep what was learned about the gateway itself (ports, services) for classification.
	d.Hosts = append([]model.Host{gw}, d.Hosts...)
	return d
}

func (s *Integration) toHost(h *hostAcc) model.Host {
	d := s.cached(h)
	mh := model.Host{IP: h.ip.String(), Sources: h.src}
	if h.mac != "" {
		mac := h.mac
		mh.MAC = &mac
		if v := oui.Lookup(string(mac)); v != "" {
			mh.Vendor = model.Ptr(v)
		}
	}
	names := appendUnique(nil, d.names...)
	names = appendUnique(names, d.netbios)
	names = appendUnique(names, h.ann.Hostnames...)
	if h.ann.Name != "" {
		names = appendUnique(names, h.ann.Name)
	}
	mh.Hostnames = names
	mh.Services = h.ann.Services
	if h.ann.Manufacturer != "" {
		mh.Manufacturer = model.Ptr(h.ann.Manufacturer)
	}
	if h.ann.Model != "" {
		mh.Model = model.Ptr(h.ann.Model)
	}
	for _, p := range d.ports {
		mh.OpenPorts = append(mh.OpenPorts, uint16(p))
	}
	mh.Titles = d.titles
	if d.banner != "" {
		mh.Banners = []string{d.banner}
	}
	if h.ttl > 0 {
		mh.TTL = model.Ptr(uint8(h.ttl))
	}
	if h.os != "" {
		mh.OS = model.Ptr(h.os)
	}
	return mh
}

// webPorts are open ports worth fetching a page title from.
var webPorts = map[int]bool{
	80: true, 443: true, 5000: true, 5001: true, 8006: true, 8008: true, 8080: true,
	8096: true, 8123: true, 8443: true, 9443: true, 32400: true,
}

func containsAny(prefixes []netip.Prefix, ip netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// sweep runs fn for every address with bounded concurrency.
func sweep(ctx context.Context, ips []netip.Addr, workers int, fn func(netip.Addr)) {
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, ip := range ips {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			fn(ip)
		}()
	}
	wg.Wait()
}

func sweepHosts(ctx context.Context, hosts []*hostAcc, workers int, fn func(*hostAcc)) {
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, h := range hosts {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			fn(h)
		}()
	}
	wg.Wait()
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
