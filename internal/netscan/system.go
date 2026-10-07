package netscan

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"strings"

	"github.com/riccardoalv/omini/internal/model"
)

// MaxHosts limits the size of one scanned subnet (a /22).
const MaxHosts = 1024

// arpEntry is a resolved neighbor from the OS ARP cache.
type arpEntry struct {
	IP  netip.Addr
	MAC model.MACAddress
	Dev string
}

// readARP parses /proc/net/arp (Linux). Only complete entries are returned.
func readARP(path string) ([]arpEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseARP(f)
}

func parseARP(r io.Reader) ([]arpEntry, error) {
	var out []arpEntry
	s := bufio.NewScanner(r)
	first := true
	for s.Scan() {
		if first { // header
			first = false
			continue
		}
		// IP address  HW type  Flags  HW address  Mask  Device
		f := strings.Fields(s.Text())
		if len(f) < 6 || f[2] == "0x0" { // incomplete
			continue
		}
		ip, err := netip.ParseAddr(f[0])
		mac := model.NormMAC(f[3])
		if err != nil || mac == "" {
			continue
		}
		out = append(out, arpEntry{IP: ip, MAC: mac, Dev: f[5]})
	}
	return out, s.Err()
}

// defaultGateway parses /proc/net/route (Linux) and returns the IPv4 default gateway.
func defaultGateway(path string) (netip.Addr, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return netip.Addr{}, "", err
	}
	defer f.Close()
	return parseRoute(f)
}

func parseRoute(r io.Reader) (netip.Addr, string, error) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		// Iface Destination Gateway Flags ... (hex, little endian)
		f := strings.Fields(s.Text())
		if len(f) < 3 || f[1] != "00000000" || f[2] == "00000000" {
			continue
		}
		b, err := hex.DecodeString(f[2])
		if err != nil || len(b) != 4 {
			continue
		}
		v := binary.LittleEndian.Uint32(b)
		var ip [4]byte
		binary.BigEndian.PutUint32(ip[:], v)
		return netip.AddrFrom4(ip), f[0], nil
	}
	return netip.Addr{}, "", fmt.Errorf("no default route")
}

// localNet is a directly connected IPv4 network of this host.
type localNet struct {
	Prefix netip.Prefix
	Self   netip.Addr
	MAC    model.MACAddress
	Iface  string
}

// localNetworks lists private IPv4 networks on up, non-loopback interfaces.
func localNetworks() []localNet {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []localNet
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isVirtual(ifc.Name) {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil {
				continue
			}
			ip, _ := netip.AddrFromSlice(ipn.IP.To4())
			ones, _ := ipn.Mask.Size()
			if !ip.IsPrivate() {
				continue
			}
			out = append(out, localNet{
				Prefix: scanPrefix(netip.PrefixFrom(ip, ones)),
				Self:   ip,
				MAC:    model.MACFromBytes(ifc.HardwareAddr),
				Iface:  ifc.Name,
			})
		}
	}
	return out
}

// isVirtual skips container and VPN bridges, which are not the home LAN.
func isVirtual(name string) bool {
	for _, p := range []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "tailscale", "wg", "tun", "zt"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// scanPrefix shrinks networks larger than MaxHosts to the /22 around the host address.
func scanPrefix(p netip.Prefix) netip.Prefix {
	if p.Bits() < 22 {
		p = netip.PrefixFrom(p.Addr(), 22)
	}
	return p.Masked()
}

// hostsOf lists the usable host addresses of an IPv4 prefix.
func hostsOf(p netip.Prefix) ([]netip.Addr, error) {
	if !p.Addr().Is4() {
		return nil, fmt.Errorf("only IPv4 subnets are supported: %s", p)
	}
	p = p.Masked()
	if n := 1 << (32 - p.Bits()); n > MaxHosts {
		return nil, fmt.Errorf("subnet %s too large (%d addresses, maximum /22)", p, n)
	}
	var out []netip.Addr
	for a := p.Addr(); p.Contains(a); a = a.Next() {
		out = append(out, a)
	}
	if len(out) > 2 { // network and broadcast addresses
		out = out[1 : len(out)-1]
	}
	return out, nil
}

// parseSubnets parses a comma separated list of CIDRs.
func parseSubnets(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		p, err := netip.ParsePrefix(part)
		if err != nil {
			return nil, fmt.Errorf("invalid subnet %q (expected e.g. 192.168.1.0/24)", part)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

// parseSubnetSpec returns nil for "auto" (or empty), else the parsed subnets.
func parseSubnetSpec(spec string) ([]netip.Prefix, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" || strings.EqualFold(spec, "auto") {
		return nil, nil
	}
	return parseSubnets(spec)
}
