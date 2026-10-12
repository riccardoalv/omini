package collector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// Changes about one host on a router or DNS server: its local DNS name
// (dns.set, dns.delete), waking it (wol.send) and cutting its internet
// (pause.set, pause.delete).

// strict reads a change's value into v, refusing unknown fields.
func strict(value any, v any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// DnsDelete is the value of dns.delete.
type DnsDelete struct {
	Name string `json:"name"`
}

// MACValue is the value of pause.delete.
type MACValue struct {
	MAC model.MACAddress `json:"mac"`
}

// WolValue is the value of wol.send.
type WolValue struct {
	MAC model.MACAddress `json:"mac"`
	IP  string           `json:"ip,omitempty"`
}

// hostname is a fully qualified local name: labels of letters, digits and
// dashes, at least two of them.
var hostname = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func dnsRecordOf(n topology.Node, name string) *model.DnsRecord {
	for i, r := range n.Device.DnsRecords {
		if strings.EqualFold(r.Name, name) {
			return &n.Device.DnsRecords[i]
		}
	}
	return nil
}

// dnsSet gives an address a local name, or moves the name to it.
func dnsSet(n topology.Node, value any) (json.RawMessage, any, error) {
	var v model.DnsRecord
	if err := strict(value, &v); err != nil {
		return nil, nil, fmt.Errorf("%w: dns.set takes {name, ip}", ErrInvalidChange)
	}
	v.Name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(v.Name), "."))
	ip, err := netip.ParseAddr(v.IP)
	if !hostname.MatchString(v.Name) || len(v.Name) > 253 || err != nil || !ip.Is4() {
		return nil, nil, fmt.Errorf("%w: a DNS name is a full name (nas.home.arpa) and an IPv4 address", ErrInvalidChange)
	}
	cur := dnsRecordOf(n, v.Name)
	if cur == nil {
		return nil, v, nil
	}
	before, err := json.Marshal(cur)
	return before, v, err
}

func dnsDelete(n topology.Node, value any) (json.RawMessage, any, error) {
	var v DnsDelete
	if err := strict(value, &v); err != nil {
		return nil, nil, fmt.Errorf("%w: dns.delete takes {name}", ErrInvalidChange)
	}
	cur := dnsRecordOf(n, v.Name)
	if cur == nil {
		return nil, nil, fmt.Errorf("%w: %s has no name %s", ErrNotChangeable, n.Label, v.Name)
	}
	before, err := json.Marshal(cur)
	return before, DnsDelete{Name: cur.Name}, err
}

// wolSend wakes a device on one of the router's networks.
func wolSend(n topology.Node, value any) (json.RawMessage, any, error) {
	var v WolValue
	if err := strict(value, &v); err != nil {
		return nil, nil, fmt.Errorf("%w: wol.send takes {mac, ip}", ErrInvalidChange)
	}
	v.MAC = model.NormMAC(string(v.MAC))
	if v.MAC == "" {
		return nil, nil, fmt.Errorf("%w: waking a device needs its MAC", ErrInvalidChange)
	}
	if v.IP != "" {
		ip, err := netip.ParseAddr(v.IP)
		if err != nil || networkHolding(n, ip) == nil {
			return nil, nil, fmt.Errorf("%w: %s is in none of %s's networks", ErrInvalidChange, v.IP, n.Label)
		}
	}
	return nil, v, nil
}

func pausedOf(n topology.Node, mac model.MACAddress) *model.PausedDevice {
	for i, p := range n.Device.Paused {
		if model.NormMAC(string(p.MAC)) == mac {
			return &n.Device.Paused[i]
		}
	}
	return nil
}

// pauseSet cuts a device's internet on the router (or updates its addresses).
func pauseSet(n topology.Node, value any) (json.RawMessage, any, error) {
	var v model.PausedDevice
	if err := strict(value, &v); err != nil {
		return nil, nil, fmt.Errorf("%w: pause.set takes {mac, ips, name}", ErrInvalidChange)
	}
	v.MAC = model.NormMAC(string(v.MAC))
	if v.MAC == "" || len(v.IPs) == 0 {
		return nil, nil, fmt.Errorf("%w: pausing a device needs its MAC and address", ErrInvalidChange)
	}
	for _, s := range v.IPs {
		ip, err := netip.ParseAddr(s)
		if err != nil || (ip.Is4() && networkHolding(n, ip) == nil) {
			return nil, nil, fmt.Errorf("%w: %s is in none of %s's networks", ErrInvalidChange, s, n.Label)
		}
	}
	slices.Sort(v.IPs)
	cur := pausedOf(n, v.MAC)
	if cur == nil {
		return nil, v, nil
	}
	before, err := json.Marshal(cur)
	return before, v, err
}

func pauseDelete(n topology.Node, value any) (json.RawMessage, any, error) {
	var v MACValue
	if err := strict(value, &v); err != nil {
		return nil, nil, fmt.Errorf("%w: pause.delete takes {mac}", ErrInvalidChange)
	}
	v.MAC = model.NormMAC(string(v.MAC))
	cur := pausedOf(n, v.MAC)
	if cur == nil {
		return nil, nil, fmt.Errorf("%w: %s does not pause %s", ErrNotChangeable, n.Label, v.MAC)
	}
	before, err := json.Marshal(cur)
	return before, v, err
}
