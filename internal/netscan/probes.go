package netscan

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// CommonPorts are probed on each new host. They identify typical homelab
// devices: SSH, DNS, web, SMB/NetBIOS, AFP (Mac), RTSP (cameras), IPP/LPD/
// JetDirect (printers), MQTT, RDP, Synology, Proxmox, Home Assistant,
// Jellyfin/Plex, UniFi, iOS lockdown (iPhone/iPad) and Chromecast.
var CommonPorts = []int{
	22, 53, 80, 139, 443, 445, 515, 548, 554, 631, 1883, 3389, 5000, 5001,
	8006, 8008, 8009, 8080, 8123, 8443, 8096, 9100, 32400, 62078,
}

// livenessPorts answer quickly on most devices; a refused connection also proves the host is up.
var livenessPorts = []int{443, 80, 22, 445, 62078}

// triggerARP sends one UDP datagram so the kernel resolves the address (ARP)
// and the host appears in the ARP cache, even when it ignores everything else.
func triggerARP(ip netip.Addr) {
	conn, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(netip.AddrPortFrom(ip, 9)))
	if err != nil {
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
	_, _ = conn.Write([]byte{0})
	conn.Close()
}

// pinger sends ICMP echo using unprivileged "ping sockets" (Linux, macOS).
// If the OS does not allow them, ping is skipped and other methods are used.
type pinger struct {
	conn *icmp.PacketConn
	id   int
	mu   sync.Mutex
	got  map[netip.Addr]int // address -> TTL of the reply (0 when unknown)
}

func newPinger() (*pinger, error) {
	conn, err := icmp.ListenPacket("udp4", "0.0.0.0")
	if err != nil {
		return nil, err
	}
	p := &pinger{conn: conn, id: os.Getpid() & 0xffff, got: map[netip.Addr]int{}}
	// The reply TTL hints the OS family; not every OS reports it on ping sockets.
	_ = conn.IPv4PacketConn().SetControlMessage(ipv4.FlagTTL, true)
	go p.receive()
	return p, nil
}

func (p *pinger) send(ip netip.Addr, seq int) {
	msg := icmp.Message{Type: ipv4.ICMPTypeEcho, Body: &icmp.Echo{ID: p.id, Seq: seq, Data: []byte("omini")}}
	b, err := msg.Marshal(nil)
	if err != nil {
		return
	}
	_, _ = p.conn.WriteTo(b, &net.UDPAddr{IP: ip.AsSlice()})
}

func (p *pinger) receive() {
	buf := make([]byte, 1500)
	pc := p.conn.IPv4PacketConn()
	for {
		n, cm, addr, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		msg, err := icmp.ParseMessage(1, buf[:n])
		if err != nil || msg.Type != ipv4.ICMPTypeEchoReply {
			continue
		}
		ttl := 0
		if cm != nil {
			ttl = cm.TTL
		}
		if ua, ok := addr.(*net.UDPAddr); ok {
			if ip, ok := netip.AddrFromSlice(ua.IP.To4()); ok {
				p.mu.Lock()
				p.got[ip] = ttl
				p.mu.Unlock()
			}
		}
	}
}

// replies returns the addresses that answered, with the reply TTL.
func (p *pinger) replies() map[netip.Addr]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[netip.Addr]int, len(p.got))
	for k, v := range p.got {
		out[k] = v
	}
	return out
}

func (p *pinger) close() { p.conn.Close() }

// tcpAlive reports whether ip answers on any liveness port (open or refused).
func tcpAlive(ctx context.Context, ip netip.Addr, ports []int, timeout time.Duration) bool {
	for _, port := range ports {
		state := dialPort(ctx, ip, port, timeout)
		if state != portFiltered {
			return true
		}
	}
	return false
}

type portState int

const (
	portFiltered portState = iota // no answer
	portClosed                    // refused: the host is up
	portOpen
)

func dialPort(ctx context.Context, ip netip.Addr, port int, timeout time.Duration) portState {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp4", netip.AddrPortFrom(ip, uint16(port)).String())
	if err == nil {
		conn.Close()
		return portOpen
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return portClosed
	}
	return portFiltered
}

// scanPorts returns the open ports of ip among ports.
func scanPorts(ctx context.Context, ip netip.Addr, ports []int, timeout time.Duration) []int {
	var (
		mu   sync.Mutex
		open []int
		wg   sync.WaitGroup
	)
	sem := make(chan struct{}, 16)
	for _, port := range ports {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if dialPort(ctx, ip, port, timeout) == portOpen {
				mu.Lock()
				open = append(open, port)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return open
}

// sshBanner reads the version line an SSH server sends first, e.g.
// "SSH-2.0-OpenSSH_9.2p1 Debian-2+deb12u3": it often names the OS.
func sshBanner(ctx context.Context, ip netip.Addr, timeout time.Duration) string {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp4", netip.AddrPortFrom(ip, 22).String())
	if err != nil {
		return ""
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 256)
	n, _ := conn.Read(buf)
	line, _, _ := strings.Cut(string(buf[:n]), "\n")
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "SSH-") {
		return ""
	}
	return line
}

// localOS returns this server's OS id from /etc/os-release (e.g. "nixos", "debian").
func localOS(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "ID="); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

// reverseDNS returns the PTR names of ip, without the trailing dot, asking
// the system resolver and then the gateway's DNS directly (home routers like
// OPNsense/pfSense/dnsmasq know their LAN, local stub resolvers often do not).
func reverseDNS(ctx context.Context, ip, gateway netip.Addr, timeout time.Duration) []string {
	names := lookupPTR(ctx, net.DefaultResolver, ip, timeout)
	if len(names) == 0 && gateway.IsValid() {
		gw := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, netip.AddrPortFrom(gateway, 53).String())
			},
		}
		names = lookupPTR(ctx, gw, ip, timeout)
	}
	return names
}

func lookupPTR(ctx context.Context, r *net.Resolver, ip netip.Addr, timeout time.Duration) []string {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	names, err := r.LookupAddr(ctx, ip.String())
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSuffix(n, ".")
		// "_gateway" is synthesized by systemd-resolved, not a real name.
		if n != "" && n != "_gateway" && !strings.HasPrefix(n, "_gateway.") {
			out = append(out, n)
		}
	}
	return out
}

// NetBIOS node status (NBSTAT) query for "*": Windows and Samba answer with their names.
var nbstatQuery = []byte{
	0x13, 0x37, // transaction id
	0x00, 0x00, // flags
	0x00, 0x01, // questions
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x20, // name length: encoded "*" padded to 16 bytes
	'C', 'K', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A',
	'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A',
	0x00,
	0x00, 0x21, // type NBSTAT
	0x00, 0x01, // class IN
}

// netbiosName queries ip on UDP 137 and returns its workstation name.
func netbiosName(ip netip.Addr, timeout time.Duration) string {
	conn, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(netip.AddrPortFrom(ip, 137)))
	if err != nil {
		return ""
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(nbstatQuery); err != nil {
		return ""
	}
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return ""
	}
	return parseNBSTAT(buf[:n])
}

// parseNBSTAT extracts the first unique, non-group name from a node status response.
func parseNBSTAT(b []byte) string {
	// header(12) + name(34) + type(2) + class(2) + ttl(4) + rdlength(2) = 56
	const off = 56
	if len(b) < off+1 {
		return ""
	}
	count := int(b[off])
	pos := off + 1
	for i := 0; i < count && pos+18 <= len(b); i++ {
		name := strings.TrimSpace(strings.TrimRight(string(b[pos:pos+15]), "\x00"))
		suffix := b[pos+15]
		flags := binary.BigEndian.Uint16(b[pos+16 : pos+18])
		pos += 18
		if suffix == 0x00 && flags&0x8000 == 0 && name != "" { // workstation, unique
			return name
		}
	}
	return ""
}
