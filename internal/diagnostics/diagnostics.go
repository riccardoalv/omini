// Package diagnostics builds the file a tester attaches to a bug report: what
// Omini collected and drew, anonymized so it can be shared in public. MACs
// keep their vendor part, private addresses stay (the networks' shape is what
// a bug needs), public addresses and names become stable placeholders, and
// secrets, configuration backups and integration settings are left out.
package diagnostics

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
)

// Anonymizer replaces what identifies a network or a person, the same value
// always by the same placeholder, so the bundle stays consistent.
type Anonymizer struct {
	macs  map[string]string
	ips   map[netip.Addr]netip.Addr
	names map[string]string
	n4    uint32 // public IPv4 addresses given so far
	n6    uint64 // IPv6 addresses given so far
}

func NewAnonymizer() *Anonymizer {
	return &Anonymizer{macs: map[string]string{}, ips: map[netip.Addr]netip.Addr{}, names: map[string]string{}}
}

// dropped keys never reach the bundle.
var dropped = map[string]bool{
	"password": true, "secret": true, "token": true, "community": true, "psk": true,
	"content": true, // configuration backups
	"note":    true, // the user's notes about devices (names, passwords)
	"config":  true, // integration settings (hosts, users)
	"layout":  true,
}

// named keys hold names people choose: a device's, a network's, a port's.
var named = map[string]bool{
	"label": true, "hostname": true, "name": true, "ssid": true, "description": true,
	"serial": true, "remote_name": true, "domain": true, "dns_domain": true, "titles": true,
	"user": true, "username": true, "comment": true, "location": true, "contact": true,
	"sys_name": true, "author": true, "port_labels": true, "fqdn": true, "title": true,
	"mount": true, "sensor": true, // may carry the owner's names (/mnt/ricardo-photos)
}

// safeParams are the alert parameters that never hold a name someone chose:
// ports, services, addresses (anonymized as text), times, versions. Every
// other string parameter (the device, its parent, an access point, a
// gateway, a sensor, a mount point...) becomes a placeholder, so a rule that
// adds a parameter cannot leak a name.
var safeParams = map[string]bool{
	"iface": true, "port": true, "lag": true, "service": true, "type": true, "network": true,
	"ip": true, "mac": true, "error": true, "at": true, "first_seen": true, "limits": true,
	"current": true, "latest": true,
}

// kept are the parents whose "name" is a port or interface name (igc0,
// ether1, Port 3), which a bug needs and which says nothing about the owner.
var kept = map[string]bool{"interfaces": true, "ports": true, "members": true, "lag": true, "vlans": true}

// versions look like IPv4 addresses (1.0.2.15) and stay as they are.
var versions = map[string]bool{"version": true, "os_version": true, "firmware": true, "latest": true, "current": true}

var (
	macRe  = regexp.MustCompile(`(?i)\b[0-9a-f]{2}(?:[:-][0-9a-f]{2}){5}\b`)
	ipv4Re = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	ipv6Re = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{0,4}:){2,7}[0-9a-f]{0,4}\b`)
	devRe  = regexp.MustCompile(`^dev:(\d+):(.+)$`) // nodes keyed by an integration's own key
)

// Value anonymizes any JSON-encodable value.
func (a *Anonymizer) Value(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return a.walk(out, "", ""), nil
}

func (a *Anonymizer) walk(v any, key, parent string) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			if dropped[strings.ToLower(k)] {
				continue
			}
			if key == "port_labels" { // the user's port descriptions: keys are ports, values are names
				out[k] = a.walk(x, "label", key)
				continue
			}
			out[a.Text(k)] = a.walk(x, k, key)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = a.walk(x, key, parent)
		}
		return out
	case string:
		return a.String(t, key, parent)
	default:
		return v
	}
}

// String anonymizes one value found under key (inside parent).
func (a *Anonymizer) String(s, key, parent string) string {
	if s == "" {
		return s
	}
	if named[key] && (key != "name" || !kept[parent]) || key == "rule" && parent == "paused" { // a firewall rule's name
		return a.Name(s)
	}
	if versions[key] {
		return s
	}
	if key == "url" {
		if u, err := url.Parse(s); err == nil && u.Host != "" {
			if _, err := netip.ParseAddr(u.Hostname()); err != nil {
				u.Host = strings.Replace(u.Host, u.Hostname(), a.Name(u.Hostname()), 1)
			}
			u.User = nil
			return a.Text(u.String())
		}
	}
	if key == "reasons" { // the evidence behind a guess: "hostname:living-room-tv"
		if kind, v, ok := strings.Cut(s, ":"); ok && (kind == "hostname" || kind == "title") {
			return kind + ":" + a.Name(v)
		}
	}
	if key == "key" && parent == "device" {
		return a.deviceKey(s)
	}
	if m := devRe.FindStringSubmatch(s); m != nil && !macRe.MatchString(s) {
		return "dev:" + m[1] + ":" + a.deviceKey(m[2])
	}
	return a.Text(s)
}

// Text replaces the MACs and addresses inside free text (errors, alert
// parameters, node IDs).
func (a *Anonymizer) Text(s string) string {
	s = macRe.ReplaceAllStringFunc(s, a.MAC)
	s = ipv4Re.ReplaceAllStringFunc(s, a.ip)
	return ipv6Re.ReplaceAllStringFunc(s, a.ip)
}

// Name is a stable placeholder for a name.
func (a *Anonymizer) Name(s string) string {
	if p, ok := a.names[s]; ok {
		return p
	}
	p := fmt.Sprintf("name-%d", len(a.names)+1)
	a.names[s] = p
	return p
}

// MAC keeps the vendor part (the first three bytes) of a universal MAC and
// replaces the rest; a locally administered (random) MAC is replaced whole.
func (a *Anonymizer) MAC(s string) string {
	norm := strings.ToLower(strings.ReplaceAll(s, "-", ":"))
	if p, ok := a.macs[norm]; ok {
		return p
	}
	n := len(a.macs) + 1
	tail := fmt.Sprintf("%02x:%02x:%02x", n>>16&0xff, n>>8&0xff, n&0xff)
	var b byte
	_, _ = fmt.Sscanf(norm[:2], "%02x", &b)
	p := norm[:8] + ":" + tail
	if b&0x02 != 0 {
		p = "02:00:00:" + tail
	}
	a.macs[norm] = p
	return p
}

// ip keeps private, loopback and special IPv4 addresses (they show how the
// network is laid out); public IPv4 and every IPv6 address (they may carry a
// MAC or the provider's prefix) become documentation addresses.
func (a *Anonymizer) ip(s string) string {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return s
	}
	if addr.Is4() && (addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() ||
		addr.IsMulticast() || addr == netip.AddrFrom4([4]byte{255, 255, 255, 255})) {
		return s
	}
	if addr.Is6() && (addr.IsLoopback() || addr.IsUnspecified()) {
		return s
	}
	if p, ok := a.ips[addr]; ok {
		return p.String()
	}
	var p netip.Addr
	if addr.Is4() {
		a.n4++
		p = netip.AddrFrom4([4]byte{203, 0, byte(113 + a.n4/254%3), byte(1 + a.n4%254)}) // TEST-NET-3 and around
	} else {
		a.n6++
		var b [16]byte
		switch {
		case addr.IsLinkLocalUnicast():
			b[0], b[1] = 0xfe, 0x80
		case addr.IsPrivate():
			b[0] = 0xfd
		default:
			b[0], b[1], b[2], b[3] = 0x20, 0x01, 0x0d, 0xb8
		}
		for i := range 8 {
			b[15-i] = byte(a.n6 >> (8 * i))
		}
		p = netip.AddrFrom16(b)
	}
	a.ips[addr] = p
	return p.String()
}

// deviceKey anonymizes a device's key: its MAC or address, or a name the
// integration knows it by (a serial, a hostname).
func (a *Anonymizer) deviceKey(s string) string {
	if _, err := netip.ParseAddr(s); err == nil || macRe.MatchString(s) {
		return a.Text(s)
	}
	return a.Name(s)
}

// Integration is an integration as the bundle shows it: no name, no settings.
type Integration struct {
	ID      int64  `json:"id"`
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
	// Plugin is the plugin's version, for plugin integrations.
	Plugin string `json:"plugin,omitempty"`
	Status any    `json:"status,omitempty"`
}

// Alert is an open alert without its key (it may hold an integration's own
// names).
type Alert struct {
	Rule     string         `json:"rule"`
	Severity string         `json:"severity"`
	NodeID   string         `json:"node_id,omitempty"`
	Params   map[string]any `json:"params,omitempty"`
}

// Input is what the bundle is made of.
type Input struct {
	Version      string
	GeneratedAt  string
	Integrations []Integration
	Alerts       []Alert
	Round        any
	Topology     any // the map: nodes (with what each integration reported) and links
}

// Bundle is the anonymized file.
func Bundle(in Input) (map[string]any, error) {
	a := NewAnonymizer()
	integrations, err := a.Value(in.Integrations)
	if err != nil {
		return nil, err
	}
	alerts := make([]any, 0, len(in.Alerts))
	for _, al := range in.Alerts {
		x, err := a.alert(al)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, x)
	}
	topo, err := a.Value(in.Topology)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"omini_version": in.Version,
		"generated_at":  in.GeneratedAt,
		"anonymized":    "MACs keep their vendor part; public and IPv6 addresses, names, SSIDs and serials are placeholders; private IPv4 addresses stay. Passwords, integration settings and configuration backups are left out.",
		"integrations":  integrations,
		"alerts":        alerts,
		"round":         in.Round,
		"topology":      topo,
	}, nil
}

// alert anonymizes an alert: its string parameters become placeholders
// unless they are known to be safe (see safeParams).
func (a *Anonymizer) alert(al Alert) (map[string]any, error) {
	out := map[string]any{"rule": al.Rule, "severity": al.Severity}
	if al.NodeID != "" {
		out["node_id"] = a.String(al.NodeID, "node_id", "")
	}
	if len(al.Params) == 0 {
		return out, nil
	}
	params := make(map[string]any, len(al.Params))
	for k, v := range al.Params {
		s, ok := v.(string)
		switch {
		case ok && (s == "" || safeParams[k]):
			params[k] = a.Text(s)
		case ok:
			params[k] = a.Name(s)
		default:
			x, err := a.Value(v)
			if err != nil {
				return nil, err
			}
			params[k] = x
		}
	}
	out["params"] = params
	return out, nil
}
