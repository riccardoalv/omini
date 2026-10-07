// Package webui detects whether a device has a web interface (router admin
// page, NAS, hypervisor...) so the UI can offer to open it in a new tab.
package webui

import (
	"context"
	"crypto/tls"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Service is a web interface found on a device.
type Service struct {
	URL   string `json:"url"`
	Port  int    `json:"port"`
	Title string `json:"title,omitempty"` // <title> of the page, when present
}

// Port is a TCP port to probe and the scheme to try first.
type Port struct {
	Number int
	HTTPS  bool
}

// DefaultPorts are common admin interfaces in homelabs.
var DefaultPorts = []Port{
	{443, true},
	{80, false},
	{8443, true},
	{8080, false},
	{8006, true},  // Proxmox VE
	{5001, true},  // Synology DSM (HTTPS)
	{5000, false}, // Synology DSM
	{8123, false}, // Home Assistant
	{8096, false}, // Jellyfin
	{9443, true},  // Portainer
}

// Prober finds web interfaces and caches the results per IP.
type Prober struct {
	Ports   []Port
	Timeout time.Duration // per port
	TTL     time.Duration // cache lifetime

	client *http.Client
	mu     sync.Mutex
	cache  map[string]cached
	now    func() time.Time
}

type cached struct {
	services []Service
	at       time.Time
}

func New() *Prober {
	p := &Prober{Ports: DefaultPorts, Timeout: 2 * time.Second, TTL: 10 * time.Minute}
	p.init()
	return p
}

// HTTPSHint reports whether a port usually speaks HTTPS (only the first scheme tried).
func HTTPSHint(port int) bool {
	for _, p := range DefaultPorts {
		if p.Number == port {
			return p.HTTPS
		}
	}
	return port == 443 || port == 8443 || port == 9443
}

// Titles probes only the given ports of ip (no cache) and returns what answered HTTP.
func Titles(ctx context.Context, ip string, ports []int) []Service {
	list := make([]Port, 0, len(ports))
	for _, p := range ports {
		list = append(list, Port{Number: p, HTTPS: HTTPSHint(p)})
	}
	p := &Prober{Ports: list, Timeout: 2 * time.Second}
	p.init()
	found, _ := p.Find(ctx, ip)
	return found
}

func (p *Prober) init() {
	if p.client != nil {
		return
	}
	p.cache = map[string]cached{}
	if p.now == nil {
		p.now = time.Now
	}
	p.client = &http.Client{
		Transport: &http.Transport{
			// Home devices almost always use self-signed certificates. Only the
			// page title is read; no credentials or data are ever sent.
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // see above
			DisableKeepAlives: true,
		},
		// Redirects (e.g. to /login) are fine: the interface exists.
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

// Find returns the web interfaces of ip, from the cache when fresh.
func (p *Prober) Find(ctx context.Context, ip string) ([]Service, error) {
	if net.ParseIP(ip) == nil {
		return nil, fmt.Errorf("invalid IP %q", ip)
	}
	p.init()
	p.mu.Lock()
	if c, ok := p.cache[ip]; ok && p.now().Sub(c.at) < p.TTL {
		p.mu.Unlock()
		return c.services, nil
	}
	p.mu.Unlock()

	var (
		mu    sync.Mutex
		found []Service
		wg    sync.WaitGroup
	)
	for _, port := range p.Ports {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s, ok := p.probe(ctx, ip, port); ok {
				mu.Lock()
				found = append(found, s)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Keep the order of the configured ports (most typical first).
	order := map[int]int{}
	for i, port := range p.Ports {
		order[port.Number] = i
	}
	sort.Slice(found, func(i, j int) bool { return order[found[i].Port] < order[found[j].Port] })
	found = dedupe(found)

	p.mu.Lock()
	p.cache[ip] = cached{services: found, at: p.now()}
	p.mu.Unlock()
	return found, nil
}

func (p *Prober) probe(ctx context.Context, ip string, port Port) (Service, bool) {
	host := net.JoinHostPort(ip, fmt.Sprint(port.Number))
	// Cheap TCP check first, so closed ports cost nothing more.
	d := net.Dialer{Timeout: p.Timeout}
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return Service{}, false
	}
	conn.Close()

	schemes := []string{"http", "https"}
	if port.HTTPS {
		schemes = []string{"https", "http"}
	}
	var fallback *Service
	for _, scheme := range schemes {
		url := fmt.Sprintf("%s://%s/", scheme, host)
		title, status, ok := p.fetch(ctx, url)
		if !ok {
			continue
		}
		s := Service{URL: url, Port: port.Number, Title: title}
		// A 400 often means "plain HTTP sent to an HTTPS port": try the other scheme first.
		if status == http.StatusBadRequest && fallback == nil {
			fallback = &s
			continue
		}
		return s, true
	}
	if fallback != nil {
		return *fallback, true
	}
	return Service{}, false
}

var (
	titleRe    = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	errorTitle = regexp.MustCompile(`^(\d{3}\b|error\b|bad request|forbidden|not found|unauthorized)`)
)

// fetch reports whether url answers HTTP, with the status and page title.
func (p *Prober) fetch(ctx context.Context, url string) (string, int, bool) {
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, false
	}
	req.Header.Set("User-Agent", "Omini (web interface detection)")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", 0, false // e.g. TLS handshake on a plain HTTP port
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	title := ""
	if m := titleRe.FindSubmatch(body); m != nil {
		title = strings.Join(strings.Fields(html.UnescapeString(string(m[1]))), " ")
		if len(title) > 80 {
			title = title[:80]
		}
	}
	if errorTitle.MatchString(strings.ToLower(title)) {
		title = "" // e.g. "400 The plain HTTP request was sent to HTTPS port"
	}
	return title, resp.StatusCode, true
}

// dedupe drops http://ip:80 when it only redirects to the same interface on
// https://ip:443 (same title), which is the common case for admin pages.
func dedupe(services []Service) []Service {
	seen := map[string]bool{}
	out := services[:0]
	for _, s := range services {
		key := s.Title
		if key == "" {
			key = s.URL
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}
