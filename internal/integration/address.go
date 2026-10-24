package integration

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strings"

	"github.com/riccardoalv/omini/internal/model"
)

// AddressField is the form field holding the address of the device an
// integration reads (the first field of type url or host), if it has one.
func AddressField(fields []model.FormField) (model.FormField, bool) {
	for _, f := range fields {
		if f.Type == model.FormFieldTypeURL || f.Type == model.FormFieldTypeHost {
			return f, true
		}
	}
	return model.FormField{}, false
}

// HostOf is the host part of an address field's value: the hostname of a URL
// ("https://192.168.1.1:8443/api" → "192.168.1.1"), or a host field's host
// without its port and brackets.
func HostOf(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil {
			return ""
		}
		return u.Hostname()
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return value // an IPv6 address without brackets has colons, but no port
	}
	if h, _, err := net.SplitHostPort(value); err == nil {
		return h
	}
	return strings.Trim(strings.SplitN(value, "/", 2)[0], "[]")
}

// AddressIP is the IP address an address field's value points to, if it is
// one (a hostname is not: DNS says where it is, not the integration).
func AddressIP(value string) (netip.Addr, bool) {
	ip, err := netip.ParseAddr(HostOf(value))
	if err != nil {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}

// WithHost replaces the host part of an address field's value with host,
// keeping its scheme, port and path ("http://192.168.1.96:8080/x" →
// "http://192.168.1.110:8080/x"); an IPv6 host goes in brackets where a port
// or a URL needs them.
func WithHost(value, host string) (string, error) {
	value, host = strings.TrimSpace(value), strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" || strings.ContainsAny(host, "/?#@ ") {
		return "", errors.New("enter an address like 192.168.1.10")
	}
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil {
			return "", err
		}
		if port := u.Port(); port != "" {
			u.Host = net.JoinHostPort(host, port)
		} else if strings.Contains(host, ":") {
			u.Host = "[" + host + "]"
		} else {
			u.Host = host
		}
		return u.String(), nil
	}
	if _, err := netip.ParseAddr(value); err != nil {
		if _, port, err := net.SplitHostPort(value); err == nil {
			return net.JoinHostPort(host, port), nil
		}
	}
	return host, nil
}
