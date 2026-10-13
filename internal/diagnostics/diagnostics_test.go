package diagnostics

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBundleHidesWhatIdentifiesTheNetwork(t *testing.T) {
	topo := map[string]any{
		"nodes": []any{
			map[string]any{
				"id": "dev:00:11:32:aa:bb:cc", "label": "Ricardo's NAS", "hostname": "ricardo-nas",
				"ip": "192.168.1.10", "mac": "00:11:32:aa:bb:cc", "ssid": "Casa Silva",
				"device": map[string]any{
					"key": "SN12345", "serial": "SN12345", "firmware": "1.0.2.15",
					"interfaces": []any{map[string]any{"name": "igc0", "description": "uplink to Ricardo's office", "ipv6": []any{"2804:14c:1234::10", "fe80::211:32ff:feaa:bbcc"}}},
					"vlans":      []any{map[string]any{"id": 20, "name": "IoT"}},
					"password":   "hunter2",
				},
			},
			map[string]any{"id": "dev:3:SN12345", "parent_id": "mac:da:a1:19:00:00:01", "port_labels": map[string]any{"ether2": "Ricardo's desk"}},
			map[string]any{"id": "mac:04:5d:4b:00:00:09", "reasons": []any{"vendor:Sony", "hostname:living-room-tv"}},
			map[string]any{"id": "ip:192.168.1.1", "web": []any{map[string]any{"port": 443, "url": "https://silva-router.lan/", "title": "Silva Router"}, map[string]any{"port": 80, "url": "http://192.168.1.1/"}}},
			map[string]any{"id": "wan", "wan": map[string]any{"ip": "177.45.10.2", "gateway": "177.45.10.1"}},
		},
	}
	out, err := Bundle(Input{
		Version: "1.4.0", Topology: topo,
		Integrations: []Integration{{ID: 3, Type: "opnsense", Enabled: true, Plugin: "0.5.3", Status: map[string]any{"error": "dial tcp 177.45.10.1:443: timeout"}}},
		Alerts:       []Alert{{Rule: "device_offline", Severity: "critical", NodeID: "dev:00:11:32:aa:bb:cc", Params: map[string]any{"name": "Ricardo's NAS"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	s := string(raw)
	for _, secret := range []string{"Ricardo", "ricardo", "Casa Silva", "SN12345", "hunter2", "aa:bb:cc", "177.45.10", "2804:14c", "feaa:bbcc", "da:a1:19", "living-room", "Silva"} {
		if strings.Contains(s, secret) {
			t.Errorf("bundle still holds %q:\n%s", secret, s)
		}
	}
	for _, kept := range []string{
		`"192.168.1.10"`, `"00:11:32:00:00:01"`, `"igc0"`, `"IoT"`, `"1.0.2.15"`, `"opnsense"`, `"0.5.3"`,
		`"device_offline"`, `"203.0.113.`, `"2001:db8::`, `"fe80::`, `"mac:02:00:00:`, `"ether2"`, `"1.4.0"`, `"vendor:Sony"`, `"hostname:name-`, `"https://name-`, `"http://192.168.1.1/"`,
	} {
		if !strings.Contains(s, kept) {
			t.Errorf("bundle lost %s:\n%s", kept, s)
		}
	}
}

func TestPlaceholdersAreStable(t *testing.T) {
	a := NewAnonymizer()
	if a.MAC("00:11:32:AA:BB:CC") != a.MAC("00-11-32-aa-bb-cc") {
		t.Error("one MAC got two placeholders")
	}
	first := a.Name("nas")
	if a.Name("nas") != first || a.Name("tv") == first {
		t.Error("names are not stable or collide")
	}
	if got := a.Text("dev:00:11:32:aa:bb:cc down, gateway 8.8.8.8 and 8.8.8.8"); got != "dev:00:11:32:00:00:01 down, gateway 203.0.113.2 and 203.0.113.2" {
		t.Errorf("text = %q", got)
	}
	if got := a.Text("at 10:30:00 on 2026-10-13"); got != "at 10:30:00 on 2026-10-13" {
		t.Errorf("a time was taken for an address: %q", got)
	}
}
