// Package nmapscan is the nmap integration: a deeper scan (operating system
// and service versions) using the nmap installed on the host. Omini never
// ships nmap (its license is not MIT-compatible); the Docker image installs it
// on start when OMINI_NMAP=install.
//
// A scan of a /24 takes minutes, so it runs in the background: Collect
// returns the last results at once and starts a new scan when they are older
// than the configured interval.
package nmapscan

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/netscan"
)

// Integration runs nmap. Zero values are the production defaults; tests set
// Run, Subnets, Gateway and Root.
type Integration struct {
	Binary  string                                                               // default "nmap"
	Run     func(ctx context.Context, bin string, args []string) ([]byte, error) // default exec
	Subnets func(spec string) ([]netip.Prefix, error)                            // default netscan.Subnets
	Gateway func() netip.Addr                                                    // default netscan.Gateway
	Root    *bool                                                                // default: euid == 0
	ARP     func() map[string]model.MACAddress                                   // default netscan.ARPTable
	Now     func() time.Time

	mu      sync.Mutex
	results map[string]result // by settings
	running map[string]bool
	single  map[string]single // devices scanned on their own (ScanHost), by IP
	wg      sync.WaitGroup
}

// single is the result of scanning one device ("Scan" in its panel).
type single struct {
	host model.Host
	at   time.Time
}

type result struct {
	devices []model.Device
	at      time.Time
	err     error
}

func New() *Integration { return &Integration{} }

// ErrNotInstalled explains how to get nmap.
var ErrNotInstalled = errors.New("nmap is not installed on this server: install it (Debian/Ubuntu: apt install nmap; " +
	"Fedora: dnf install nmap; NixOS: add nmap to environment.systemPackages; Docker: set OMINI_NMAP=install)")

func (*Integration) Info() integration.Info {
	return integration.Info{
		Type: "nmap",
		Name: "nmap",
		Description: "Deep scan with the nmap installed on this server: operating system and service versions of every " +
			"device. Runs in the background; a /24 takes a few minutes.",
		Kind:   integration.KindCore,
		Single: true,
		Fields: []model.FormField{
			{
				Key: "subnets", Type: model.FormFieldTypeString, Label: model.Ptr("Subnets"), Default: "auto",
				Help: model.Ptr(`"auto" scans the networks this server is connected to, or list them: 192.168.1.0/24, 10.0.20.0/24`),
			},
			{
				Key: "os_detection", Type: model.FormFieldTypeBool, Label: model.Ptr("Detect operating systems"), Default: true,
				Help: model.Ptr("nmap -O. Needs Omini to run as root (the Docker image does); otherwise it is skipped."),
			},
			{
				Key: "versions", Type: model.FormFieldTypeBool, Label: model.Ptr("Detect service versions"), Default: true,
				Help: model.Ptr("nmap -sV on the most common ports: names the software behind each open port."),
			},
			{
				Key: "every_hours", Type: model.FormFieldTypeInt, Label: model.Ptr("Scan again every (hours)"), Default: 24,
				Help: model.Ptr("A full scan takes minutes and some load on the network: once a day is plenty."),
			},
		},
	}
}

func (s *Integration) defaults() {
	if s.Binary == "" {
		s.Binary = "nmap"
	}
	if s.Run == nil {
		s.Run = runNmap
	}
	if s.Subnets == nil {
		s.Subnets = netscan.Subnets
	}
	if s.Gateway == nil {
		s.Gateway = netscan.Gateway
	}
	if s.ARP == nil {
		s.ARP = netscan.ARPTable
	}
	if s.Root == nil {
		root := os.Geteuid() == 0
		s.Root = &root
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.results == nil {
		s.results = map[string]result{}
		s.running = map[string]bool{}
		s.single = map[string]single{}
	}
}

// Validate checks the settings before they are saved.
func (s *Integration) Validate(cfg integration.Config) error {
	s.mu.Lock()
	s.defaults()
	s.mu.Unlock()
	if _, err := s.Subnets(cfg.String("subnets")); err != nil {
		return err
	}
	if h := cfg.Int("every_hours", 24); h < 1 || h > 24*30 {
		return fmt.Errorf("scan again every 1 to 720 hours")
	}
	return nil
}

func (s *Integration) Test(ctx context.Context, cfg integration.Config) (string, error) {
	s.mu.Lock()
	s.defaults()
	s.mu.Unlock()
	out, err := s.Run(ctx, s.Binary, []string{"--version"})
	if err != nil {
		return "", ErrNotInstalled
	}
	version, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	msg := strings.TrimSpace(version)
	if cfg.Bool("os_detection", true) && !*s.Root {
		msg += ". Operating system detection needs root: only service versions will be read"
	}
	return msg, nil
}

func (s *Integration) Collect(ctx context.Context, cfg integration.Config) ([]model.Device, error) {
	s.mu.Lock()
	s.defaults()
	s.mu.Unlock()
	prefixes, err := s.Subnets(cfg.String("subnets"))
	if err != nil {
		return nil, err
	}
	args := s.args(cfg, prefixes)
	key := settingsKey(args)
	every := time.Duration(cfg.Int("every_hours", 24)) * time.Hour

	s.mu.Lock()
	last, ok := s.results[key]
	stale := !ok || s.Now().Sub(last.at) >= every || integration.Forced(ctx)
	if stale && !s.running[key] {
		s.running[key] = true
		s.wg.Add(1)
		go s.scan(key, args)
	}
	s.mu.Unlock()

	if ok && last.err != nil {
		return nil, last.err
	}
	return s.withSingles(last), nil // empty until a scan finishes
}

// withSingles adds the devices scanned on their own since the last full scan.
func (s *Integration) withSingles(last result) []model.Device {
	s.mu.Lock()
	var newer []model.Host
	for _, sh := range s.single {
		if sh.at.After(last.at) {
			newer = append(newer, sh.host)
		}
	}
	s.mu.Unlock()
	if len(newer) == 0 {
		return last.devices
	}
	var hosts []model.Host
	for _, d := range last.devices {
		hosts = append(hosts, d.Hosts...)
	}
	for _, n := range newer {
		hosts = slices.DeleteFunc(hosts, func(h model.Host) bool { return h.IP == n.IP })
		hosts = append(hosts, n)
	}
	return s.devices(hosts)
}

// ErrNoAnswer is returned when the scanned device did not answer nmap.
var ErrNoAnswer = errors.New("the device did not answer nmap (offline, or it filters every port)")

// ScanHost scans one device now, with the integration's settings, and keeps
// the result until the next full scan.
func (s *Integration) ScanHost(ctx context.Context, cfg integration.Config, ip string) (model.Host, error) {
	s.mu.Lock()
	s.defaults()
	s.mu.Unlock()
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return model.Host{}, fmt.Errorf("invalid address %q", ip)
	}
	out, err := s.Run(ctx, s.Binary, append(s.baseArgs(cfg), addr.String()))
	if errors.Is(err, exec.ErrNotFound) {
		return model.Host{}, ErrNotInstalled
	}
	if err != nil && len(out) == 0 {
		return model.Host{}, fmt.Errorf("nmap failed: %w", err)
	}
	hosts, err := parse(out)
	if err != nil {
		return model.Host{}, err
	}
	if len(hosts) == 0 {
		return model.Host{}, ErrNoAnswer
	}
	h := hosts[0]
	if h.MAC == nil {
		if m, ok := s.ARP()[h.IP]; ok {
			h.MAC = &m
		}
	}
	s.mu.Lock()
	s.single[h.IP] = single{host: h, at: s.Now()}
	s.mu.Unlock()
	return h, nil
}

// wait blocks until background scans finish (tests).
func (s *Integration) wait() { s.wg.Wait() }

func (s *Integration) scan(key string, args []string) {
	defer s.wg.Done()
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	start := s.Now()
	out, err := s.Run(ctx, s.Binary, args)
	r := result{at: s.Now()}
	switch {
	case errors.Is(err, exec.ErrNotFound):
		r.err = ErrNotInstalled
	case err != nil && len(out) == 0:
		r.err = fmt.Errorf("nmap failed: %w", err)
	default:
		hosts, perr := parse(out)
		if perr != nil {
			r.err = perr
			break
		}
		r.devices = s.devices(hosts)
		slog.Info("nmap scan done", "hosts", len(hosts), "duration", s.Now().Sub(start).Round(time.Second))
	}
	s.mu.Lock()
	s.results[key] = r
	s.running[key] = false
	s.mu.Unlock()
}

func (s *Integration) args(cfg integration.Config, prefixes []netip.Prefix) []string {
	args := s.baseArgs(cfg)
	for _, p := range prefixes {
		args = append(args, p.String())
	}
	return args
}

// baseArgs are the nmap options of the settings, without targets.
func (s *Integration) baseArgs(cfg integration.Config) []string {
	args := []string{"-oX", "-", "-T4", "-n", "--host-timeout", "120s"}
	if cfg.Bool("versions", true) {
		args = append(args, "-sV", "--version-light", "--top-ports", "100")
	} else {
		args = append(args, "--top-ports", "100")
	}
	if cfg.Bool("os_detection", true) && *s.Root {
		args = append(args, "-O", "--osscan-limit")
	}
	return args
}

func settingsKey(args []string) string {
	b, _ := json.Marshal(args)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

func runNmap(ctx context.Context, bin string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && stderr.Len() > 0 {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, err
}

// devices puts the hosts under the gateway (like the network scan), so both
// integrations describe the same router node and their hosts merge by MAC.
// Without root nmap reports no MACs: they come from the system's ARP cache.
func (s *Integration) devices(hosts []model.Host) []model.Device {
	arp := s.ARP()
	for i := range hosts {
		if hosts[i].MAC == nil {
			if m, ok := arp[hosts[i].IP]; ok {
				hosts[i].MAC = &m
			}
		}
	}
	gw := s.Gateway()
	d := model.Device{Key: "nmap", Name: "Gateway", Role: model.Ptr(model.DeviceRoleRouter)}
	var rest []model.Host
	for _, h := range hosts {
		if gw.IsValid() && h.IP == gw.String() {
			d.Host, d.IPs = model.Ptr(h.IP), []string{h.IP}
			if h.MAC != nil {
				d.Key, d.MACs = string(*h.MAC), []model.MACAddress{*h.MAC}
			}
			rest = append([]model.Host{h}, rest...)
			continue
		}
		rest = append(rest, h)
	}
	if d.Key == "nmap" && gw.IsValid() {
		d.Key = "gw:" + gw.String()
	}
	d.Hosts = rest
	return []model.Device{d}
}
