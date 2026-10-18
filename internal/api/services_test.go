package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// fakeRouter is a development plugin (sh): a firewall that serves DHCP (a pool
// and a lease) and a network with the scan's DHCP and DNS checks.
func fakeRouter(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	const man = "id: fakert\nname: Fake router\nversion: 1.0.0\nprotocol: 1\nentrypoint: main.sh\nfields:\n  - {key: host, type: host, required: true}\n"
	const script = `cat >/dev/null
echo '{"devices":[
 {"key":"58:9c:fc:00:00:01","name":"fw","role":"firewall","macs":["58:9c:fc:00:00:01"],"ips":["192.168.1.1","2001:db8:1::1"],
  "dhcp_pools":[{"network":"192.168.1.0/24","used":20,"total":200}],
  "dhcp_leases":[{"ip":"192.168.1.20","mac":"00:11:22:00:00:20","hostname":"tv"},{"ip":"192.168.1.3","mac":"00:11:22:00:00:03"},
    {"ip":"2001:db8:1::20","mac":"00:11:22:00:00:20","hostname":"tv"}]},
 {"key":"net","name":"Network","role":"unknown",
  "dhcp_checks":[{"interface":"eth0","offers":[{"server_ip":"192.168.1.1","dns":["192.168.1.1"]},{"server_ip":"192.168.1.250"}]}],
  "dns_checks":[{"server":"192.168.1.1","external":{"name":"example.com","ok":true,"rtt_ms":8}},
   {"server":"2001:db8:1::1","external":{"name":"example.com","ok":true,"rtt_ms":9}}]}
]}'
`
	_ = os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(man), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "main.sh"), []byte(script), 0o644)
	return dir
}

func TestDNSAndDHCPView(t *testing.T) {
	h := newHarness(t, nil, fakeRouter(t))
	h.login()
	if code := h.do("POST", "/api/integrations", map[string]any{"type": "fakert", "config": map[string]any{"host": "192.168.1.1"}}, nil); code != http.StatusCreated {
		t.Fatalf("add: %d", code)
	}
	if err := h.coll.CollectNow(t.Context()); err != nil {
		t.Fatal(err)
	}
	var v struct {
		DHCP struct {
			Checks []struct {
				Interface string `json:"interface"`
				Offers    []struct {
					ServerIP string `json:"server_ip"`
					Known    bool   `json:"known"`
					NodeID   string `json:"node_id"`
				} `json:"offers"`
			} `json:"checks"`
			Pools  []map[string]any `json:"pools"`
			Leases []map[string]any `json:"leases"`
		} `json:"dhcp"`
		DNS struct {
			Servers []map[string]any `json:"servers"`
		} `json:"dns"`
	}
	if code := h.do("GET", "/api/dns-dhcp", nil, &v); code != http.StatusOK {
		t.Fatalf("view: %d", code)
	}
	if len(v.DHCP.Checks) != 1 || len(v.DHCP.Checks[0].Offers) != 2 {
		t.Fatalf("checks: %+v", v.DHCP.Checks)
	}
	fw, rogue := v.DHCP.Checks[0].Offers[0], v.DHCP.Checks[0].Offers[1]
	if !fw.Known || fw.NodeID == "" || rogue.Known {
		t.Errorf("offers: %+v", v.DHCP.Checks[0].Offers)
	}
	if len(v.DHCP.Pools) != 1 || v.DHCP.Pools[0]["network"] != "192.168.1.0/24" {
		t.Errorf("pools: %+v", v.DHCP.Pools)
	}
	if len(v.DHCP.Leases) != 3 || v.DHCP.Leases[0]["ip"] != "192.168.1.3" || v.DHCP.Leases[2]["ip"] != "2001:db8:1::20" {
		t.Errorf("leases, by address: %+v", v.DHCP.Leases)
	}
	if len(v.DNS.Servers) != 2 || v.DNS.Servers[0]["server"] != "192.168.1.1" || v.DNS.Servers[0]["node_id"] == "" {
		t.Errorf("dns: %+v", v.DNS.Servers)
	}
	// The same server over IPv6 is the same node.
	if len(v.DNS.Servers) == 2 && v.DNS.Servers[1]["node_id"] != v.DNS.Servers[0]["node_id"] {
		t.Errorf("dns over IPv6: %+v", v.DNS.Servers)
	}
}
