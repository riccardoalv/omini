package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strings"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// Local DNS names: a device with a fixed address gets <name>.<domain> on the
// DNS server its network uses (AdGuard Home, Pi-hole or the router), kept as
// the device is renamed, its address freed or its name turned off.

const (
	// DNSDomainSetting overrides the domain the DNS servers report.
	DNSDomainSetting = "dns_domain"
	// DefaultDNSDomain is used when no server reports one (RFC 8375).
	DefaultDNSDomain = "home.arpa"
	// dnsNameSetting + MAC: the user's name for a device ("-": no name).
	dnsNameSetting = "dns_name:"
	// dnsAppliedSetting + MAC: "<server node>|<fqdn>" Omini set last.
	dnsAppliedSetting = "dns_applied:"
	dnsOff            = "-"
)

// DNSName is what the device's panel shows: its name, where it is and
// whether Omini keeps it.
type DNSName struct {
	// Name is the full name Omini keeps for the device ("" when none).
	Name string `json:"name,omitempty"`
	// Label is the name's first part, the user's or the device's own.
	Label string `json:"label,omitempty"`
	// Custom: the user chose Label.
	Custom bool `json:"custom,omitempty"`
	// Off: the user turned the name off.
	Off bool `json:"off,omitempty"`
	// Server is the node that answers it.
	Server string `json:"server,omitempty"`
	// Why there is no name: "no_fixed_ip", "no_server" or "no_name" (the
	// device is known by its address or MAC only).
	Missing string `json:"missing,omitempty"`
	// Others are the names any DNS server has for the device's address.
	Others []model.DnsRecord `json:"others,omitempty"`
}

// slug makes a DNS label of a device's name: "Living Room AP" → "living-room-ap".
func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > 63 {
		out = strings.TrimRight(out[:63], "-")
	}
	return out
}

// ownLabel is the name a device gets from its label; none when the label is
// only its address or MAC.
func ownLabel(n topology.Node) string {
	if _, err := netip.ParseAddr(n.Label); err == nil || model.NormMAC(n.Label) != "" {
		return ""
	}
	return slug(n.Label)
}

func nodeIPs(n topology.Node) []string {
	out := []string{n.IP}
	if n.Device != nil {
		out = append(out, n.Device.IPs...)
	}
	return out
}

// dnsTarget is the DNS server that names ip, and the domain: a server Omini
// can change that the network's DHCP hands out, else the router of ip's
// network, else the only server there is.
func dnsTarget(topo topology.Topology, ip netip.Addr) (topology.Node, bool) {
	var servers []topology.Node
	for _, n := range topo.Nodes {
		if n.Device != nil && slices.Contains(n.Changes, model.ChangeKindDnsSet) {
			servers = append(servers, n)
		}
	}
	handed := map[string]bool{}
	for _, n := range topo.Nodes {
		if n.Device == nil {
			continue
		}
		for _, c := range n.Device.DhcpChecks {
			for _, o := range c.Offers {
				for _, d := range o.Dns {
					handed[d] = true
				}
			}
		}
	}
	for _, s := range servers {
		if slices.ContainsFunc(nodeIPs(s), func(a string) bool { return handed[a] }) && networkHolding(s, ip) == nil {
			return s, true // a DNS server of its own (AdGuard Home, Pi-hole)
		}
	}
	for _, s := range servers {
		if networkHolding(s, ip) != nil {
			return s, true
		}
	}
	if len(servers) == 1 {
		return servers[0], true
	}
	return topology.Node{}, false
}

// dnsDomain is the domain names get: the setting, else the server's, else
// the router's, else home.arpa.
func (c *Collector) dnsDomain(ctx context.Context, topo topology.Topology, server topology.Node, ip netip.Addr) string {
	if v, ok, err := c.store.GetSetting(ctx, DNSDomainSetting); err == nil && ok && v != "" {
		return v
	}
	if d := model.Deref(server.Device.DnsDomain); d != "" {
		return strings.Trim(strings.ToLower(d), ".")
	}
	for _, n := range topo.Nodes {
		if n.Device != nil && networkHolding(n, ip) != nil {
			if d := model.Deref(n.Device.DnsDomain); d != "" {
				return strings.Trim(strings.ToLower(d), ".")
			}
		}
	}
	return DefaultDNSDomain
}

// fixedIP is the device's reserved address, on any router.
func fixedIP(topo topology.Topology, mac model.MACAddress) (netip.Addr, bool) {
	for _, r := range topo.Nodes {
		if r.Device == nil {
			continue
		}
		if x := reservationOf(r, mac); x != nil {
			ip, err := netip.ParseAddr(x.IP)
			return ip, err == nil
		}
	}
	return netip.Addr{}, false
}

// DNSNameOf is the device's name as the panel shows it.
func (c *Collector) DNSNameOf(ctx context.Context, nodeID string) (DNSName, error) {
	n, ok := c.node(nodeID)
	mac := model.NormMAC(n.MAC)
	if !ok || mac == "" {
		return DNSName{}, ErrNotChangeable
	}
	topo := c.State().Topology
	user, _, err := c.store.GetSetting(ctx, dnsNameSetting+string(mac))
	if err != nil {
		return DNSName{}, err
	}
	out := DNSName{Label: ownLabel(n), Off: user == dnsOff}
	if user != "" && user != dnsOff {
		out.Label, out.Custom = user, true
	}
	ip, fixed := fixedIP(topo, mac)
	mine := map[string]bool{} // its IPv6 addresses: AAAA records others made name it too
	for _, a := range n.IPs6 {
		mine[a] = true
	}
	for _, s := range topo.Nodes {
		if s.Device == nil {
			continue
		}
		for _, r := range s.Device.DnsRecords {
			if a, err := netip.ParseAddr(r.IP); err == nil && a.Is6() && mine[a.WithZone("").String()] {
				out.Others = append(out.Others, r)
				continue
			}
			if r.IP == ip.String() || (!fixed && r.IP == n.IP) {
				out.Others = append(out.Others, r)
			}
		}
	}
	switch server, found := dnsTarget(topo, ip); {
	case !fixed:
		out.Missing = "no_fixed_ip"
	case !found:
		out.Missing = "no_server"
	case out.Label == "":
		out.Missing = "no_name"
	case !out.Off:
		out.Server = server.ID
		out.Name = out.Label + "." + c.dnsDomain(ctx, topo, server, ip)
	}
	return out, nil
}

// SetDNSName sets the device's name ("" back to its own, "-" off) and
// applies it.
func (c *Collector) SetDNSName(ctx context.Context, nodeID, name string) (DNSName, error) {
	n, ok := c.node(nodeID)
	mac := model.NormMAC(n.MAC)
	if !ok || mac == "" {
		return DNSName{}, ErrNotChangeable
	}
	name = strings.TrimSpace(name)
	if name != "" && name != dnsOff {
		if s := slug(name); s != name {
			return DNSName{}, fmt.Errorf("%w: a name has only lowercase letters, digits and dashes (%s)", ErrInvalidChange, s)
		}
	}
	if err := c.store.SetSetting(ctx, dnsNameSetting+string(mac), name); err != nil {
		return DNSName{}, err
	}
	if err := c.SyncDNSName(ctx, nodeID); err != nil {
		return DNSName{}, err
	}
	return c.DNSNameOf(ctx, nodeID)
}

// SyncDNSName makes the DNS servers hold the device's name as it should be:
// set where it belongs, removed from where Omini set it before.
func (c *Collector) SyncDNSName(ctx context.Context, nodeID string) error {
	want, err := c.DNSNameOf(ctx, nodeID)
	if err != nil {
		return err
	}
	n, _ := c.node(nodeID)
	mac := string(model.NormMAC(n.MAC))
	key := dnsAppliedSetting + mac
	applied, _, err := c.store.GetSetting(ctx, key)
	if err != nil {
		return err
	}
	var errs []error
	if server, fqdn, ok := strings.Cut(applied, "|"); ok && (server != want.Server || fqdn != want.Name) {
		if s, ok := c.node(server); ok && s.Device != nil && aRecordOf(s, fqdn) != nil {
			if _, err := c.Apply(ctx, ChangeRequest{NodeID: server, Kind: model.ChangeKindDnsDelete, Target: nodeID, Value: DnsDelete{Name: fqdn}}); err != nil {
				errs = append(errs, err)
			}
		}
	}
	next := ""
	if want.Name != "" {
		ip, _ := fixedIP(c.State().Topology, model.MACAddress(mac))
		s, _ := c.node(want.Server)
		if r := aRecordOf(s, want.Name); r == nil || r.IP != ip.String() {
			_, err := c.Apply(ctx, ChangeRequest{NodeID: want.Server, Kind: model.ChangeKindDnsSet, Target: nodeID, Value: model.DnsRecord{Name: want.Name, IP: ip.String()}})
			if err != nil {
				errs = append(errs, err)
			}
		}
		next = want.Server + "|" + want.Name
	}
	if err := c.store.SetSetting(ctx, key, next); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// syncNameOf gives a device whose address was just fixed (or freed) its name;
// a DNS server that refuses is logged, the address stays fixed.
func (c *Collector) syncNameOf(ctx context.Context, nodeID string) {
	if err := c.SyncDNSName(ctx, nodeID); err != nil {
		slog.Warn("could not set the device's DNS name", "node", nodeID, "err", err)
	}
}

// ValidDomain reports whether d is a domain names can go under.
func ValidDomain(d string) bool { return hostname.MatchString(d) || slug(d) == d }
