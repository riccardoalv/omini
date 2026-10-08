package collector

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/insights"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// devnetAt is a fixed instant for the devnet tests: no scheduled event (WAN
// down, branch tunnel down, ...) is on, the roaming phone is on ap-office1.
const devnetAt = 1791460800 // 2026-10-08T12:00:00Z

// devnet runs testdata/devnet (the simulated development network, see its
// README) at a time and returns each plugin's devices, or its error.
func devnet(t *testing.T, at int64) (map[string][]model.Device, map[string]string) {
	t.Helper()
	if testing.Short() {
		t.Skip("devnet: skipped in -short mode")
	}
	uv, err := exec.LookPath("uv")
	if err != nil {
		t.Skip("devnet: uv is not installed (it runs the simulated network's Python)")
	}
	root := filepath.Join("..", "..")
	cmd := exec.Command(uv, "run", "--quiet", "--project", filepath.Join(root, "sdk", "python"), "python",
		filepath.Join(root, "testdata", "devnet", "_shared", "devnet.py"), "dump", "--at", strconv.FormatInt(at, 10))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("devnet dump: %v: %s", err, stderr.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatal(err)
	}
	devices, failed := map[string][]model.Device{}, map[string]string{}
	for plugin, msg := range raw {
		var e string
		if json.Unmarshal(msg, &e) == nil {
			failed[plugin] = e
			continue
		}
		var ds []model.Device
		if err := json.Unmarshal(msg, &ds); err != nil {
			t.Fatalf("%s: %v", plugin, err)
		}
		devices[plugin] = ds
	}
	return devices, failed
}

// devnetMap builds the map the way the collector does (without the inventory).
func devnetMap(devices map[string][]model.Device, failed map[string]string) topology.Topology {
	plugins := make([]string, 0, len(devices)+len(failed))
	for p := range devices {
		plugins = append(plugins, p)
	}
	for p := range failed {
		plugins = append(plugins, p)
	}
	sort.Strings(plugins)
	var sources []topology.Source
	for i, p := range plugins {
		_, bad := failed[p]
		sources = append(sources, topology.Source{IntegrationID: int64(i + 1), Online: !bad, Devices: devices[p]})
	}
	topo := topology.Build(sources)
	classifyNodes(&topo)
	expandApps(&topo)
	attachVMs(&topo)
	return topo
}

func nodeByLabel(topo topology.Topology, label string) *topology.Node {
	for i := range topo.Nodes {
		if topo.Nodes[i].Label == label {
			return &topo.Nodes[i]
		}
	}
	return nil
}

// parentsOf lists the upstream ends of the links reaching a node.
func parentsOf(topo topology.Topology, id string) []string {
	var out []string
	for _, e := range topo.Edges {
		if e.Target == id {
			out = append(out, e.Source)
		}
	}
	sort.Strings(out)
	return out
}

func label(topo topology.Topology, id string) string {
	for _, n := range topo.Nodes {
		if n.ID == id {
			return n.Label
		}
	}
	return id
}

// unreached lists the nodes no link path joins to the given root.
func unreached(topo topology.Topology, root string) []string {
	adj := map[string][]string{}
	for _, e := range topo.Edges {
		adj[e.Source] = append(adj[e.Source], e.Target)
		adj[e.Target] = append(adj[e.Target], e.Source)
	}
	seen := map[string]bool{root: true}
	queue := []string{root}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, n := range adj[cur] {
			if !seen[n] {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	var out []string
	for _, n := range topo.Nodes {
		if !seen[n.ID] {
			out = append(out, n.Label)
		}
	}
	sort.Strings(out)
	return out
}

// TestDevnet builds the map of the simulated development network
// (testdata/devnet) and checks what it is meant to exercise.
func TestDevnet(t *testing.T) {
	devices, failed := devnet(t, devnetAt)
	if len(failed) > 0 {
		t.Fatalf("no plugin should fail at %d: %v", devnetAt, failed)
	}
	topo := devnetMap(devices, failed)
	fw := nodeByLabel(topo, "fw-edge")
	if fw == nil || fw.Role != "firewall" {
		t.Fatalf("fw-edge: %+v", fw)
	}

	// Everything at the headquarters joins the firewall. The branch office is
	// only reachable through the IPsec tunnel: no MAC table or ARP links it.
	var floating []string
	for _, l := range unreached(topo, fw.ID) {
		if !branchNode(topo, l) {
			floating = append(floating, l)
		}
	}
	if len(floating) > 0 {
		t.Errorf("not joined to fw-edge: %v", floating)
	}

	// One node per device: no MAC is shared by two nodes.
	byMAC := map[string]string{}
	for _, n := range topo.Nodes {
		if n.MAC == "" || n.Kind == topology.KindApp {
			continue
		}
		if other, ok := byMAC[n.MAC]; ok {
			t.Errorf("%s and %s share %s", other, n.Label, n.MAC)
		}
		byMAC[n.MAC] = n.Label
	}

	// Every non-random MAC has a vendor (the OUIs are real).
	for _, n := range topo.Nodes {
		if n.Kind == topology.KindClient && !n.RandomMAC && n.MAC != "" && n.Vendor == "" {
			t.Errorf("%s (%s) has no vendor", n.Label, n.MAC)
		}
	}

	want := map[string][]string{ // node → its parents
		"fw-edge":           {"Fiber (PPPoE)", "LTE backup"},
		"192.168.100.1":     {"Fiber (PPPoE)"}, // the ONT, seen on the WAN port
		"core-sw1":          {"core-sw2", "fw-edge"},
		"acc-floor1":        {"core-sw1", "core-sw2"},
		"acc-lab":           {"acc-floor2"},
		"ap-warehouse":      {"ap-office2"}, // mesh satellite
		"lab-gs108e":        {"acc-lab"},    // LLDP neighbor nobody manages
		"SEPF87B20A1B2C3":   {"acc-floor2"},
		"docker-01":         {"pve1"},
		"win11-vdi":         {"pve2"},
		"lab-web-01":        {"pve3"},
		"iPhone-de-Ricardo": {"ap-office1"},
		"DESKTOP-ERR7C4B":   {"acc-floor1"},
		"ups-01":            {"acc-floor2"},
		"mkt-router":        {"acc-floor2"}, // a double-NAT router: its "WAN" is a link into the LAN
		"lab-router":        {"pve3"},       // a router VM that is not the center hangs from its host
		"br-sw":             {"br-gw"},
		"br-cap":            {"br-sw"},
	}
	for node, parents := range want {
		n := nodeByLabel(topo, node)
		if n == nil {
			t.Errorf("%s is not on the map", node)
			continue
		}
		var got []string
		for _, p := range parentsOf(topo, n.ID) {
			got = append(got, label(topo, p))
		}
		sort.Strings(got)
		if !slices.Equal(got, parents) {
			t.Errorf("%s hangs from %v, want %v", node, got, parents)
		}
	}

	// The meeting room's desk switch on acc-floor1 port 12 becomes a segment.
	seg := false
	for _, n := range topo.Nodes {
		if n.Kind == topology.KindSegment && label(topo, n.ParentID) == "acc-floor1" && n.Port == "12" && n.MACCount == 5 {
			seg = true
		}
	}
	if !seg {
		t.Error("no unmanaged segment with 5 MACs on acc-floor1 port 12")
	}

	// The web apps of docker-01 become app nodes.
	apps := 0
	for _, n := range topo.Nodes {
		if n.Kind == topology.KindApp && label(topo, n.ParentID) == "docker-01" {
			apps++
		}
	}
	if apps != 6 {
		t.Errorf("docker-01 has %d apps, want 6", apps)
	}

	// The scenarios raise their alerts.
	rules := map[string]int{}
	for _, f := range insights.Evaluate(insights.Input{Topology: topo, Now: time.Unix(devnetAt, 0)}) {
		rules[f.Rule]++
		if f.Rule == "duplicate_ip" && f.Params["ip"] != "10.10.20.90" {
			t.Errorf("unexpected duplicate IP: %v", f.Params)
		}
	}
	for _, rule := range []string{"duplicate_ip", "weak_wifi", "slow_uplink", "update_pending", "disk_full", "high_memory", "unknown_neighbor", "unmanaged_switch"} {
		if rules[rule] == 0 {
			t.Errorf("no %s alert (got %v)", rule, rules)
		}
	}
}

// TestDevnetSchedule checks the scenarios that change with time.
func TestDevnetSchedule(t *testing.T) {
	// The phone roams to ap-office2 four minutes later.
	devices, failed := devnet(t, devnetAt+240)
	topo := devnetMap(devices, failed)
	if p := parentsOf(topo, nodeByLabel(topo, "iPhone-de-Ricardo").ID); len(p) != 1 || label(topo, p[0]) != "ap-office2" {
		t.Errorf("roaming phone hangs from %v", p)
	}
	// 25 minutes past the half hour the branch tunnel is down: its plugin fails
	// and its devices are offline (last known state).
	_, failed = devnet(t, devnetAt+1500)
	if _, ok := failed["devnet-branch"]; !ok || len(failed) != 1 {
		t.Fatalf("failed plugins: %v", failed)
	}
	// The fiber is down for three minutes every four hours (at 1:40 into the cycle).
	at := int64(devnetAt - devnetAt%14400 + 6000)
	devices, failed = devnet(t, at)
	topo = devnetMap(devices, failed)
	if n := nodeByLabel(topo, "Fiber (PPPoE)"); n == nil || n.Online {
		t.Errorf("fiber WAN while down: %+v", n)
	}
	if n := nodeByLabel(topo, "LTE backup"); n == nil || !n.Online {
		t.Errorf("LTE WAN while the fiber is down: %+v", n)
	}
}

func branchNode(topo topology.Topology, l string) bool {
	if strings.HasPrefix(l, "br-") || l == "ISP" || strings.HasPrefix(l, "10.20.0.") {
		return true
	}
	for _, n := range topo.Nodes {
		if n.Label == l && strings.HasPrefix(n.IP, "10.20.0.") {
			return true
		}
	}
	return false
}
