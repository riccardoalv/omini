package api_test

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// fakeDHCP is a development plugin (sh) for a router that makes
// reservations, but not inside its DHCP range.
func fakeDHCP(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	const man = "id: fakedhcp\nname: Fake DHCP\nversion: 1.0.0\nprotocol: 1\nentrypoint: main.sh\n" +
		"changes: [reservation.set, reservation.delete, pause.set, pause.delete, wol.send]\nbackup: true\nfields:\n  - {key: host, type: host, required: true}\n"
	const script = `req=$(cat)
res=''
case "$req" in
  *'"action":"backup"'*) echo '{"backups":[{"device":"58:9c:fc:00:00:01","format":"text","content":"hostname fw\nsecret-free\n"}]}'; exit 0 ;;
  *'"action":"apply"'*'"ip":"192.168.1.2"'*) res='{"mac":"00:11:22:00:00:20","ip":"192.168.1.2","name":"tv"}' ;;
  *'"action":"apply"'*'"kind":"pause.set"'*) ;;
  *'"action":"apply"'*'"kind":"wol.send"'*) ;;
  *'"action":"apply"'*) echo '{"error":"inside the range"}'; exit 1 ;;
esac
echo '{"devices":[{"key":"58:9c:fc:00:00:01","name":"fw","role":"firewall","macs":["58:9c:fc:00:00:01"],"ips":["192.168.1.1"],
  "networks":[{"vlan":1,"gateway":"192.168.1.1/24","dhcp":{"enabled":true,"from":"192.168.1.100","to":"192.168.1.199","reserve_in_range":false}}],
  "dhcp_reservations":['"$res"'],
  "arp":[{"ip":"192.168.1.150","mac":"00:11:22:00:00:20"},{"ip":"192.168.1.3","mac":"00:11:22:00:00:03"}],
  "dhcp_leases":[{"ip":"192.168.1.150","mac":"00:11:22:00:00:20","hostname":"tv"},{"ip":"192.168.1.3","mac":"00:11:22:00:00:03"}]}]}'
`
	_ = os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(man), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "main.sh"), []byte(script), 0o644)
	return dir
}

// TestFixingAddresses: the devices on DHCP, with an address proposed outside
// the range, fixed many at once; one failing does not stop the others.
func TestFixingAddresses(t *testing.T) {
	h := newHarness(t, nil, fakeDHCP(t))
	h.login()
	h.do("POST", "/api/integrations", map[string]any{"type": "fakedhcp", "config": map[string]any{"host": "192.168.1.1"}}, nil)
	if err := h.coll.CollectNow(t.Context()); err != nil {
		t.Fatal(err)
	}
	var list []map[string]any
	if code := h.do("GET", "/api/fix-ip", nil, &list); code != http.StatusOK || len(list) != 2 {
		t.Fatalf("candidates: %d %v", code, list)
	}
	tv := ""
	for _, c := range list {
		if c["mac"] == "00:11:22:00:00:20" {
			tv = c["node_id"].(string)
			if c["ip"] != "192.168.1.150" || c["proposed"] != "192.168.1.2" {
				t.Fatalf("tv: %v", c)
			}
		} else if c["proposed"] != nil {
			t.Fatalf("outside the range already: %v", c)
		}
	}
	var res []map[string]any
	code := h.do("POST", "/api/fix-ip", map[string]any{"items": []map[string]any{
		{"node_id": tv, "ip": "192.168.1.2"},
		{"node_id": "no-such-node", "ip": "192.168.1.9"},
	}}, &res)
	if code != http.StatusOK || len(res) != 2 || res[0]["change"] == nil || res[0]["error"] != nil || res[1]["error"] == nil {
		t.Fatalf("fix: %d %v", code, res)
	}
	if code := h.do("POST", "/api/fix-ip", map[string]any{"items": []any{}}, nil); code != http.StatusBadRequest {
		t.Fatalf("nothing to fix: %d", code)
	}
}

// TestHostActions: a client can be woken and paused through its router.
func TestHostActions(t *testing.T) {
	h := newHarness(t, nil, fakeDHCP(t))
	h.login()
	h.do("POST", "/api/integrations", map[string]any{"type": "fakedhcp", "config": map[string]any{"host": "192.168.1.1"}}, nil)
	if err := h.coll.CollectNow(t.Context()); err != nil {
		t.Fatal(err)
	}
	tv := ""
	for _, n := range h.coll.State().Topology.Nodes {
		if n.MAC == "00:11:22:00:00:20" {
			tv = "/api/nodes/" + url.PathEscape(n.ID)
		}
	}
	var got struct {
		Wake        struct{ Via, Router string }
		CanPause    bool   `json:"can_pause"`
		PauseRouter string `json:"pause_router"`
	}
	if code := h.do("GET", tv+"/actions", nil, &got); code != http.StatusOK || got.Wake.Via != "router" || !got.CanPause {
		t.Fatalf("actions: %d %+v", code, got)
	}
	if code := h.do("POST", tv+"/wake", nil, nil); code != http.StatusOK {
		t.Fatalf("wake: %d", code)
	}
	if code := h.do("PUT", tv+"/pause", nil, nil); code != http.StatusOK {
		t.Fatalf("pause: %d", code)
	}
	fw := "/api/nodes/" + url.PathEscape("dev:58:9c:fc:00:00:01")
	if code := h.do("PUT", fw+"/pause", nil, nil); code == http.StatusOK {
		t.Fatalf("the router itself: %d", code)
	}
}

// TestBackups: a backup now, the device's versions and one's content.
func TestBackups(t *testing.T) {
	h := newHarness(t, nil, fakeDHCP(t))
	h.login()
	var in map[string]any
	h.do("POST", "/api/integrations", map[string]any{"type": "fakedhcp", "config": map[string]any{"host": "192.168.1.1"}}, &in)
	if err := h.coll.CollectNow(t.Context()); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprint(in["id"])
	if code := h.do("POST", "/api/integrations/"+id+"/backup", nil, nil); code != http.StatusNoContent {
		t.Fatalf("backup now: %d", code)
	}
	var list []map[string]any
	fw := "/api/nodes/" + url.PathEscape("dev:58:9c:fc:00:00:01")
	if code := h.do("GET", fw+"/backups", nil, &list); code != http.StatusOK || len(list) != 1 || list[0]["content"] != nil {
		t.Fatalf("list: %d %v", code, list)
	}
	var one map[string]any
	if code := h.do("GET", fmt.Sprintf("/api/backups/%v", list[0]["id"]), nil, &one); code != http.StatusOK || one["content"] != "hostname fw\nsecret-free\n" {
		t.Fatalf("one: %d %v", code, one)
	}
	if code := h.do("GET", "/api/backups/999", nil, nil); code != http.StatusNotFound {
		t.Fatalf("missing: %d", code)
	}
}
