package api_test

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// fakeSwitch is a development plugin (sh) for a switch that applies
// port.enabled: a phone alone on Port 2, the firewall on Port 1.
func fakeSwitch(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	const man = "id: fakesw\nname: Fake switch\nversion: 1.0.0\nprotocol: 1\nentrypoint: main.sh\n" +
		"changes: [port.enabled]\nfields:\n  - {key: host, type: host, required: true}\n"
	const script = `req=$(cat)
p2=true
case "$req" in
  *'"action":"apply"'*'"port":"Port 2"'*) p2=false ;;
  *'"action":"apply"'*) echo '{"error":"the switch did not answer"}'; exit 1 ;;
esac
echo '{"devices":[{"key":"58:9c:fc:00:00:01","name":"fw","role":"firewall","macs":["58:9c:fc:00:00:01"]},
 {"key":"1c:2a:a3:00:00:01","name":"sw","role":"switch","macs":["1c:2a:a3:00:00:01"],
 "interfaces":[{"name":"Port 1","enabled":true},{"name":"Port 2","enabled":'$p2'}],
 "fdb":[{"mac":"92:34:c8:00:00:01","port":"Port 2"},{"mac":"58:9c:fc:00:00:01","port":"Port 1"}]}]}'
`
	_ = os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(man), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "main.sh"), []byte(script), 0o644)
	return dir
}

func TestConfigurationChanges(t *testing.T) {
	h := newHarness(t, nil, fakeSwitch(t))
	h.login()
	if code := h.do("POST", "/api/integrations", map[string]any{"type": "fakesw", "config": map[string]any{"host": "10.0.0.2"}}, nil); code != http.StatusCreated {
		t.Fatalf("add: %d", code)
	}
	if err := h.coll.CollectNow(t.Context()); err != nil {
		t.Fatal(err)
	}
	sw := "/api/nodes/" + url.PathEscape("dev:1c:2a:a3:00:00:01")

	var topo struct {
		Topology struct {
			Nodes []struct {
				ID        string                       `json:"id"`
				Changes   []string                     `json:"changes"`
				Protected map[string]map[string]string `json:"protected"`
			} `json:"nodes"`
		} `json:"topology"`
	}
	h.do("GET", "/api/topology", nil, &topo)
	for _, n := range topo.Topology.Nodes {
		if n.ID == "dev:1c:2a:a3:00:00:01" && (len(n.Changes) != 1 || n.Protected["Port 1"]["reason"] != "internet") {
			t.Fatalf("switch: %+v", n)
		}
	}

	var out map[string]any
	if code := h.do("POST", sw+"/changes", map[string]any{"kind": "port.enabled", "port": "Port 1", "value": false}, &out); code != http.StatusConflict ||
		out["reason"] != "internet" || out["allowed"] != false {
		t.Fatalf("destructive: %d %v", code, out)
	}
	if code := h.do("POST", sw+"/changes", map[string]any{"kind": "port.enabled", "port": "Port 2", "value": false}, &out); code != http.StatusOK ||
		out["after"] != false || out["before"] != true {
		t.Fatalf("disable: %d %v", code, out)
	}
	if code := h.do("POST", sw+"/changes", map[string]any{"kind": "port.enabled", "port": "Port 1", "value": true}, &out); code != http.StatusBadGateway ||
		out["error"] != "the switch did not answer" {
		t.Fatalf("refused: %d %v", code, out)
	}
	if code := h.do("POST", sw+"/changes", map[string]any{"kind": "port.enabled", "port": "Port 9", "value": true}, nil); code != http.StatusBadRequest {
		t.Fatalf("unknown port: %d", code)
	}
	var history []map[string]any
	if h.do("GET", sw+"/changes", nil, &history); len(history) != 2 || history[0]["error"] == nil {
		t.Fatalf("history: %v", history)
	}

	var settings map[string]any
	if h.do("PUT", "/api/change-settings", map[string]bool{"allow_destructive": true}, &settings); settings["allow_destructive"] != true {
		t.Fatalf("settings: %v", settings)
	}
	if h.do("PUT", "/api/change-settings", map[string]string{"dns_domain": " Home.Arpa. "}, &settings); settings["dns_domain"] != "home.arpa" ||
		settings["allow_destructive"] != true {
		t.Fatalf("domain: %v", settings)
	}
	if code := h.do("PUT", "/api/change-settings", map[string]string{"dns_domain": "not a domain"}, nil); code != http.StatusBadRequest {
		t.Fatalf("bad domain: %d", code)
	}
	if code := h.do("POST", sw+"/changes", map[string]any{"kind": "port.enabled", "port": "Port 1", "value": false}, &out); code != http.StatusConflict ||
		out["allowed"] != true {
		t.Fatalf("allowed, asked again: %d %v", code, out)
	}
}
