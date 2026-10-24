package integration

import (
	"testing"

	"github.com/riccardoalv/omini/internal/model"
)

func TestAddressField(t *testing.T) {
	f, ok := AddressField([]model.FormField{
		{Key: "user", Type: model.FormFieldTypeString},
		{Key: "host", Type: model.FormFieldTypeHost},
		{Key: "url", Type: model.FormFieldTypeURL},
	})
	if !ok || f.Key != "host" {
		t.Fatalf("got %+v, %v", f, ok)
	}
	if _, ok := AddressField([]model.FormField{{Key: "community", Type: model.FormFieldTypeSecret}}); ok {
		t.Fatal("a form without an address has no address field")
	}
}

func TestHostOf(t *testing.T) {
	for in, want := range map[string]string{
		"192.168.1.96":                 "192.168.1.96",
		"192.168.1.96:8080":            "192.168.1.96",
		"http://192.168.1.96":          "192.168.1.96",
		"https://192.168.1.1:8443/api": "192.168.1.1",
		"https://[fd00::1]:8443":       "fd00::1",
		"[fd00::1]:22":                 "fd00::1",
		"fd00::1":                      "fd00::1",
		"switch.lan":                   "switch.lan",
		"":                             "",
	} {
		if got := HostOf(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	if _, ok := AddressIP("https://switch.lan"); ok {
		t.Error("a hostname is no IP")
	}
	if ip, ok := AddressIP("http://192.168.1.96"); !ok || ip.String() != "192.168.1.96" {
		t.Errorf("got %v, %v", ip, ok)
	}
}

func TestWithHost(t *testing.T) {
	for _, c := range []struct{ in, host, want string }{
		{"192.168.1.96", "192.168.1.110", "192.168.1.110"},
		{"192.168.1.96:8080", "192.168.1.110", "192.168.1.110:8080"},
		{"http://192.168.1.96", "192.168.1.110", "http://192.168.1.110"},
		{"https://192.168.1.1:8443/api/v2", "192.168.1.2", "https://192.168.1.2:8443/api/v2"},
		{"https://192.168.1.1", "fd00::2", "https://[fd00::2]"},
		{"https://[fd00::1]:8443", "fd00::2", "https://[fd00::2]:8443"},
		{"https://192.168.1.1", "[fd00::2]", "https://[fd00::2]"},
		{"[fd00::1]:22", "192.168.1.5", "192.168.1.5:22"},
		{"fd00::1", "fd00::2", "fd00::2"},
	} {
		got, err := WithHost(c.in, c.host)
		if err != nil || got != c.want {
			t.Errorf("%q → %q: got %q, %v; want %q", c.in, c.host, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "a/b", "x@y", "1.2.3.4?x"} {
		if _, err := WithHost("http://192.168.1.1", bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
