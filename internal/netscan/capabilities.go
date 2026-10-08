package netscan

import (
	"context"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"golang.org/x/net/icmp"
)

// Capabilities says which discovery methods can work where Omini runs:
// they need different things from the system (see the README, "What the
// network scan needs").
type Capabilities struct {
	// Container: Omini runs in a container (Docker, Podman, Kubernetes).
	Container bool `json:"container"`
	// HostNetwork: the container shares the host's network (Docker
	// network_mode: host); in a bridge network it only sees Docker's own
	// network: no MAC addresses of the LAN, no multicast. Always true outside
	// a container.
	HostNetwork bool `json:"host_network"`
	// UnprivilegedPing: ICMP echo without root ("ping sockets"); else liveness
	// falls back to TCP, which misses devices with no open port.
	UnprivilegedPing bool `json:"unprivileged_ping"`
	// RawSockets: root or CAP_NET_RAW, for nmap's OS detection and traceroute.
	RawSockets bool `json:"raw_sockets"`
	// Multicast: joining multicast groups (mDNS, SSDP: names and models).
	Multicast bool `json:"multicast"`
}

// Limited lists what does not work here, as stable keys for the UI and alerts.
func (c Capabilities) Limited() []string {
	var out []string
	if !c.HostNetwork {
		out = append(out, "host_network")
	}
	if !c.Multicast {
		out = append(out, "multicast")
	}
	if !c.UnprivilegedPing && !c.RawSockets {
		out = append(out, "ping")
	}
	return out
}

// DetectCapabilities tries each thing the scan needs, once (at start).
func DetectCapabilities() Capabilities {
	c := Capabilities{Container: inContainer("/")}
	c.HostNetwork = !c.Container || !bridgeOnly(interfaceNets())
	if conn, err := icmp.ListenPacket("udp4", "0.0.0.0"); err == nil {
		c.UnprivilegedPing = true
		conn.Close()
	}
	if conn, err := net.ListenPacket("ip4:icmp", "0.0.0.0"); err == nil {
		c.RawSockets = true
		conn.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if conn := joinGroup(ctx, mdnsGroup); conn != nil {
		c.Multicast = true
		conn.Close()
	}
	return c
}

// inContainer looks for the marks Docker, Podman and Kubernetes leave.
func inContainer(root string) bool {
	for _, f := range []string{".dockerenv", "run/.containerenv"} {
		if _, err := os.Stat(root + f); err == nil {
			return true
		}
	}
	b, err := os.ReadFile(root + "proc/1/cgroup")
	if err != nil {
		return false
	}
	s := string(b)
	return strings.Contains(s, "docker") || strings.Contains(s, "kubepods") || strings.Contains(s, "containerd") || strings.Contains(s, "libpod")
}

// interfaceNets are the IPv4 networks of the interfaces that are up, but loopback.
func interfaceNets() map[string][]netip.Prefix {
	out := map[string][]netip.Prefix{}
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if p, err := netip.ParsePrefix(a.String()); err == nil && p.Addr().Is4() {
				out[ifc.Name] = append(out[ifc.Name], p.Masked())
			}
		}
	}
	return out
}

// dockerNets: where Docker puts its bridge networks (172.17.0.0/16 and up).
var dockerNets = netip.MustParsePrefix("172.16.0.0/12")

// bridgeOnly: a container that sees a single interface, on a Docker bridge
// network — not the host's (in host mode it sees every host interface).
func bridgeOnly(nets map[string][]netip.Prefix) bool {
	if len(nets) != 1 {
		return false
	}
	for _, ps := range nets {
		for _, p := range ps {
			if !dockerNets.Contains(p.Addr()) {
				return false
			}
		}
	}
	return true
}
