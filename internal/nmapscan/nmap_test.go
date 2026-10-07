package nmapscan

import (
	"context"
	"errors"
	"net/netip"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
)

const output = `<?xml version="1.0"?>
<nmaprun scanner="nmap" args="nmap -oX - ...">
<host><status state="up"/>
 <address addr="192.168.1.1" addrtype="ipv4"/>
 <address addr="58:9C:FC:10:8F:2C" addrtype="mac" vendor="FreeBSD Foundation"/>
 <ports><port protocol="tcp" portid="443"><state state="open"/><service name="http" product="lighttpd"/></port></ports>
</host>
<host><status state="up"/>
 <address addr="192.168.1.52" addrtype="ipv4"/>
 <address addr="BC:24:11:98:FC:70" addrtype="mac" vendor="Proxmox Server Solutions"/>
 <ports>
  <port protocol="tcp" portid="22"><state state="open"/><service name="ssh" product="OpenSSH" version="9.6p1 Ubuntu 3ubuntu13.5" extrainfo="Ubuntu Linux; protocol 2.0" ostype="Linux"/></port>
  <port protocol="tcp" portid="8096"><state state="open"/><service name="http" product="Jellyfin"/></port>
  <port protocol="tcp" portid="3306"><state state="closed"/></port>
 </ports>
</host>
<host><status state="up"/>
 <address addr="192.168.1.30" addrtype="ipv4"/>
 <address addr="00:15:5D:01:02:03" addrtype="mac" vendor="Microsoft"/>
 <ports><port protocol="tcp" portid="445"><state state="open"/><service name="microsoft-ds"/></port></ports>
 <os><osmatch name="Microsoft Windows 11 21H2" accuracy="96"><osclass type="general purpose" vendor="Microsoft" osfamily="Windows"/></osmatch></os>
</host>
<host><status state="down"/><address addr="192.168.1.99" addrtype="ipv4"/></host>
</nmaprun>`

type fakeNmap struct {
	calls [][]string
	err   error
}

func (f *fakeNmap) run(_ context.Context, _ string, args []string) ([]byte, error) {
	f.calls = append(f.calls, args)
	if f.err != nil {
		return nil, f.err
	}
	if slices.Contains(args, "--version") {
		return []byte("Nmap version 7.95 ( https://nmap.org )\nPlatform: x86_64"), nil
	}
	return []byte(output), nil
}

func newTest(root bool) (*Integration, *fakeNmap) {
	f := &fakeNmap{}
	clock := time.Unix(1_790_000_000, 0)
	return &Integration{
		Run: f.run,
		Subnets: func(string) ([]netip.Prefix, error) {
			return []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}, nil
		},
		Gateway:    func() netip.Addr { return netip.MustParseAddr("192.168.1.1") },
		Root:       &root,
		ARP:        func() map[string]model.MACAddress { return nil },
		Privileged: func() *bool { f := false; return &f }(),
		Now:        func() time.Time { return clock },
	}, f
}

func TestScansInTheBackground(t *testing.T) {
	s, f := newTest(true)
	ctx := context.Background()

	devices, err := s.Collect(ctx, integration.Config{})
	if err != nil || len(devices) != 0 {
		t.Fatalf("first collection returns at once, without results: %v %v", devices, err)
	}
	s.wait()
	devices, err = s.Collect(ctx, integration.Config{})
	if err != nil || len(devices) != 1 {
		t.Fatalf("after the scan: %v %v", devices, err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("fresh results must not start another scan: %d scans", len(f.calls))
	}
	args := strings.Join(f.calls[0], " ")
	for _, want := range []string{"-oX -", "-sV", "-O", "192.168.1.0/24"} {
		if !strings.Contains(args, want) {
			t.Errorf("args %q lack %q", args, want)
		}
	}

	// Same router node as the network scan: keyed by the gateway's MAC.
	gw := devices[0]
	if gw.Key != "58:9c:fc:10:8f:2c" || model.Deref(gw.Host) != "192.168.1.1" || len(gw.Hosts) != 3 {
		t.Fatalf("gateway device %+v", gw)
	}
	byIP := map[string]model.Host{}
	for _, h := range gw.Hosts {
		byIP[h.IP] = h
	}
	ubuntu := byIP["192.168.1.52"]
	if *ubuntu.MAC != "bc:24:11:98:fc:70" || model.Deref(ubuntu.OS) != "linux" ||
		!slices.Equal(ubuntu.OpenPorts, []uint16{22, 8096}) || ubuntu.Banners[0] != "OpenSSH 9.6p1 Ubuntu 3ubuntu13.5 Ubuntu Linux; protocol 2.0" {
		t.Fatalf("ubuntu host %+v", ubuntu)
	}
	if model.Deref(byIP["192.168.1.30"].OS) != "windows" || model.Deref(byIP["192.168.1.30"].Manufacturer) != "Microsoft" {
		t.Fatalf("windows host %+v", byIP["192.168.1.30"])
	}
	if _, ok := byIP["192.168.1.99"]; ok {
		t.Fatal("hosts that are down are left out")
	}

	// "Run now" starts a new scan even with fresh results.
	if _, err := s.Collect(integration.WithForce(ctx), integration.Config{}); err != nil {
		t.Fatal(err)
	}
	s.wait()
	if len(f.calls) != 2 {
		t.Fatalf("forced collection must scan again: %d scans", len(f.calls))
	}
}

func TestWithoutRootNoOSDetection(t *testing.T) {
	s, f := newTest(false)
	msg, err := s.Test(context.Background(), integration.Config{})
	if err != nil || !strings.HasPrefix(msg, "Nmap version 7.95") || !strings.Contains(msg, "needs root") {
		t.Fatalf("test: %q %v", msg, err)
	}
	_, _ = s.Collect(context.Background(), integration.Config{})
	s.wait()
	if slices.Contains(f.calls[len(f.calls)-1], "-O") {
		t.Fatal("-O needs root")
	}
}

func TestNmapNotInstalled(t *testing.T) {
	s, f := newTest(true)
	f.err = exec.ErrNotFound
	if _, err := s.Test(context.Background(), integration.Config{}); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("test: %v", err)
	}
	_, _ = s.Collect(context.Background(), integration.Config{})
	s.wait()
	if _, err := s.Collect(context.Background(), integration.Config{}); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("collect: %v", err)
	}
}

func TestRealNmapIfInstalled(t *testing.T) {
	if _, err := exec.LookPath("nmap"); err != nil {
		t.Skip("nmap not installed")
	}
	s := New()
	msg, err := s.Test(context.Background(), integration.Config{})
	if err != nil || !strings.Contains(msg, "Nmap version") {
		t.Fatalf("real nmap: %q %v", msg, err)
	}
}

// Without root nmap shows no MACs: the system's ARP cache names the hosts,
// so the gateway merges with the router node of the other integrations.
func TestMACsFromARPWithoutRoot(t *testing.T) {
	s, _ := newTest(false)
	s.ARP = func() map[string]model.MACAddress {
		return map[string]model.MACAddress{"192.168.1.1": "58:9c:fc:10:8f:2c", "192.168.1.7": "aa:bb:cc:00:00:07"}
	}
	hosts := []model.Host{{IP: "192.168.1.1"}, {IP: "192.168.1.7"}, {IP: "192.168.1.8"}}
	devs := s.devices(hosts)
	gw := devs[0]
	if gw.Key != "58:9c:fc:10:8f:2c" || len(gw.MACs) != 1 {
		t.Fatalf("gateway: %+v", gw)
	}
	byIP := map[string]model.Host{}
	for _, h := range gw.Hosts {
		byIP[h.IP] = h
	}
	if byIP["192.168.1.7"].MAC == nil || *byIP["192.168.1.7"].MAC != "aa:bb:cc:00:00:07" || byIP["192.168.1.8"].MAC != nil {
		t.Fatalf("hosts: %+v", gw.Hosts)
	}
}

func TestScanOneDevice(t *testing.T) {
	s, f := newTest(true)
	ctx := context.Background()
	host, err := s.ScanHost(ctx, integration.Config{}, "192.168.1.1")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(f.calls[0], " ")
	// One device in depth: the 1024 top ports, full versions, default scripts,
	// OS and route (root here) — like -A.
	for _, want := range []string{"--top-ports 1024", "-sV", "-sC", "-O", "--traceroute"} {
		if !strings.Contains(args, want) {
			t.Errorf("args %q lack %q", args, want)
		}
	}
	if !strings.HasSuffix(args, " 192.168.1.1") || strings.Contains(args, "/24") || strings.Contains(args, "--version-light") {
		t.Fatalf("args: %q", args)
	}
	if host.IP != "192.168.1.1" || len(host.OpenPorts) != 1 {
		t.Fatalf("host: %+v", host)
	}
	// Shown before any full scan has finished.
	devices, err := s.Collect(ctx, integration.Config{})
	if err != nil || len(devices) != 1 || len(devices[0].Hosts) == 0 {
		t.Fatalf("collect after a single scan: %+v %v", devices, err)
	}
	s.wait()

	if _, err := s.ScanHost(ctx, integration.Config{}, "not an ip"); err == nil {
		t.Fatal("invalid addresses are refused")
	}
}

func TestScanOneDeviceThatDoesNotAnswer(t *testing.T) {
	s, _ := newTest(true)
	s.Run = func(context.Context, string, []string) ([]byte, error) {
		return []byte(`<nmaprun><host><status state="down"/><address addr="192.168.1.9" addrtype="ipv4"/></host></nmaprun>`), nil
	}
	if _, err := s.ScanHost(context.Background(), integration.Config{}, "192.168.1.9"); !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("err = %v", err)
	}
}

func TestPrivilegedWithoutRoot(t *testing.T) {
	s, f := newTest(false)
	yes := true
	s.Privileged = &yes
	_, _ = s.ScanHost(context.Background(), integration.Config{}, "192.168.1.1")
	args := strings.Join(f.calls[0], " ")
	if !strings.Contains(args, "-O") || !strings.Contains(args, "--privileged") {
		t.Fatalf("with capabilities, OS detection runs with --privileged: %q", args)
	}
	msg, _ := s.Test(context.Background(), integration.Config{})
	if strings.Contains(msg, "needs root") {
		t.Fatalf("no warning when privileged: %q", msg)
	}
}
