package collector

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/insights"
	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/plugins"
	"github.com/riccardoalv/omini/internal/snmp"
	"github.com/riccardoalv/omini/internal/topology"
	"github.com/riccardoalv/omini/sdk"
)

// The devnet (testdata/devnet, see its README and DESIGN.md) emulates the APIs
// of Acme's devices; these tests run Omini's real integrations against it —
// the plugins from their own repositories, built and run by the plugin
// manager like in production, and the SNMP collector — and check the map.

const devnetRoot = "../../testdata/devnet"

type devnetAgent struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Community string `json:"community"`
	V3        *struct {
		User     string `json:"user"`
		Auth     string `json:"auth"`
		AuthPass string `json:"auth_pass"`
		Priv     string `json:"priv"`
		PrivPass string `json:"priv_pass"`
	} `json:"v3"`
	Node     string `json:"node"`
	DesignIP string `json:"design_ip"`
}

type devnetEndpoints struct {
	Control      string                 `json:"control"`
	SNMP         map[string]devnetAgent `json:"snmp"`
	Integrations []struct {
		API      string             `json:"api"`
		Plugin   string             `json:"plugin"`
		Config   integration.Config `json:"config"`
		DesignIP string             `json:"design_ip"`
	} `json:"integrations"`
}

type devnetLab struct {
	ep      devnetEndpoints
	plugins *plugins.Manager
	run     int64
	cmd     *exec.Cmd
	stdin   io.WriteCloser // kept open: the emulator stops when the test binary exits and it closes
}

// devnetPluginsRoot finds the folder with the omini-plugin-* repositories.
func devnetPluginsRoot() string {
	if env := os.Getenv("OMINI_DEVNET_PLUGINS"); env != "" {
		return env
	}
	dir, _ := filepath.Abs("../..")
	for {
		for _, d := range []string{dir, filepath.Dir(dir)} {
			if _, err := os.Stat(filepath.Join(d, "omini-plugin-opnsense", "plugin.yaml")); err == nil {
				return d
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

var devnetOnce struct {
	sync.Mutex
	lab *devnetLab
	err error
}

// startDevnet starts the emulator once per test binary (frozen clock) and
// returns it; tests move its clock.
func startDevnet(t *testing.T) *devnetLab {
	t.Helper()
	if testing.Short() {
		t.Skip("devnet: skipped in -short mode")
	}
	uv, err := exec.LookPath("uv")
	if err != nil {
		t.Skip("devnet: uv is not installed")
	}
	root := devnetPluginsRoot()
	if root == "" {
		t.Skip("devnet: the omini-plugin-* repositories were not found (set OMINI_DEVNET_PLUGINS)")
	}
	devnetOnce.Lock()
	defer devnetOnce.Unlock()
	if devnetOnce.lab != nil || devnetOnce.err != nil {
		if devnetOnce.err != nil {
			t.Fatal(devnetOnce.err)
		}
		return devnetOnce.lab
	}
	lab, err := launchDevnet(uv, root)
	devnetOnce.lab, devnetOnce.err = lab, err
	if err != nil {
		t.Fatal(err)
	}
	return lab
}

func launchDevnet(uv, root string) (*devnetLab, error) {
	dir, err := filepath.Abs(devnetRoot)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "omini-devnet-")
	if err != nil {
		return nil, err
	}
	out := filepath.Join(tmp, "endpoints.json")
	cmd := exec.Command(uv, "run", "--quiet", "--project", dir, "python", "-m", "emulator", "serve",
		"--at", "mon 00:00", "--out", out, "--until-stdin-closes")
	cmd.Dir = dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	ready := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if sc.Text() == "ready" {
				ready <- nil
				_, _ = io.Copy(io.Discard, stdout)
				return
			}
		}
		ready <- fmt.Errorf("devnet emulator exited: %s", stderr.String())
	}()
	select {
	case err := <-ready:
		if err != nil {
			return nil, err
		}
	case <-time.After(3 * time.Minute):
		return nil, fmt.Errorf("devnet emulator did not start: %s", stderr.String())
	}
	b, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	_ = os.RemoveAll(tmp)
	lab := &devnetLab{run: time.Now().UnixNano(), cmd: cmd, stdin: stdin}
	if err := json.Unmarshal(b, &lab.ep); err != nil {
		return nil, err
	}
	var dirs []string
	seen := map[string]bool{}
	for _, in := range lab.ep.Integrations {
		if !seen[in.Plugin] {
			seen[in.Plugin] = true
			dirs = append(dirs, filepath.Join(root, "omini-plugin-"+in.Plugin))
		}
	}
	// Plugin environments are cached between runs (built like Omini builds them).
	lab.plugins = &plugins.Manager{Dir: filepath.Join(dir, ".cache", "omini-plugins"), DevDirs: dirs, SDK: sdk.Python}
	if _, errs := lab.plugins.Load(); len(errs) > 0 {
		return nil, fmt.Errorf("load plugins: %v", errs)
	}
	return lab, nil
}

// at moves the emulator's frozen clock: "tue 10:30" (local time, this devnet week).
func (l *devnetLab) at(t *testing.T, when string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"at": when, "freeze": true})
	req, _ := http.NewRequest(http.MethodPut, l.ep.Control+"/clock", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

type devnetRound struct {
	sources []topology.Source
	failed  map[string]string // API → error
	names   map[int64]string  // integration id → API
	topo    topology.Topology
}

// collect runs every integration once (in parallel, like a collection round)
// and builds the map the way the collector does.
func (l *devnetLab) collect(t *testing.T) devnetRound {
	t.Helper()
	ctx := context.Background()
	type result struct {
		id      int64
		api     string
		devices []model.Device
		err     error
	}
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		res []result
	)
	add := func(r result) { mu.Lock(); res = append(res, r); mu.Unlock() }
	for i, in := range l.ep.Integrations {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, ok := l.plugins.Get(in.Plugin)
			if !ok {
				add(result{id: int64(i + 1), api: in.API, err: fmt.Errorf("plugin %s not loaded", in.Plugin)})
				return
			}
			integ := l.plugins.Integration(p)
			c := integration.WithInstance(ctx, l.run+int64(i))
			devices, err := integ.Collect(c, in.Config)
			for k := range devices {
				atDesignAddress(&devices[k], in.DesignIP)
			}
			add(result{id: int64(i + 1), api: in.API, devices: devices, err: err})
		}()
	}
	// The SNMP agents, read like the network scan reads them (one source: the scan).
	var agents []string
	for name := range l.ep.SNMP {
		agents = append(agents, name)
	}
	sort.Strings(agents)
	scanID := int64(len(l.ep.Integrations) + 1)
	var scanDevices []model.Device
	var smu sync.Mutex
	for _, name := range agents {
		a := l.ep.SNMP[name]
		wg.Add(1)
		go func() {
			defer wg.Done()
			target := snmp.Target{Host: a.Host, Port: a.Port, Community: a.Community, Timeout: 3 * time.Second}
			if a.V3 != nil {
				target.Community = ""
				target.V3 = &snmp.V3{User: a.V3.User, Auth: a.V3.Auth, AuthPass: a.V3.AuthPass, Priv: a.V3.Priv, PrivPass: a.V3.PrivPass}
			}
			d, err := snmp.Collect(ctx, target)
			if err != nil {
				t.Errorf("snmp %s: %v", name, err)
				return
			}
			atDesignAddress(&d, a.DesignIP)
			smu.Lock()
			scanDevices = append(scanDevices, d)
			smu.Unlock()
		}()
	}
	wg.Wait()
	sort.Slice(res, func(i, j int) bool { return res[i].id < res[j].id })
	sort.Slice(scanDevices, func(i, j int) bool { return scanDevices[i].Key < scanDevices[j].Key })
	r := devnetRound{failed: map[string]string{}, names: map[int64]string{scanID: "network scan (SNMP)"}}
	for _, x := range res {
		r.names[x.id] = x.api
		if x.err != nil {
			r.failed[x.api] = x.err.Error()
		}
		r.sources = append(r.sources, topology.Source{IntegrationID: x.id, Online: x.err == nil, Devices: x.devices})
	}
	r.sources = append(r.sources, topology.Source{IntegrationID: scanID, Online: true, Devices: scanDevices})
	r.topo = topology.BuildWith(r.sources, topology.Options{})
	classifyNodes(&r.topo)
	expandApps(&r.topo)
	attachVMs(&r.topo)
	if path := os.Getenv("DEVNET_DUMP"); path != "" {
		b, _ := json.MarshalIndent(r.topo, "", " ")
		_ = os.WriteFile(path, b, 0o600)
	}
	return r
}

// atDesignAddress puts back the address a device has in the lab: tests reach
// the emulated APIs on 127.0.0.1, which integrations report as the device's host.
func atDesignAddress(d *model.Device, ip string) {
	if d.Host != nil && strings.HasPrefix(strings.TrimPrefix(strings.TrimPrefix(*d.Host, "https://"), "http://"), "127.0.0.1") {
		d.Host = model.Ptr(ip)
	}
	ips := d.IPs[:0]
	for _, a := range d.IPs {
		if !strings.HasPrefix(a, "127.") {
			ips = append(ips, a)
		}
	}
	d.IPs = ips
}

func (r devnetRound) label(id string) string {
	for _, n := range r.topo.Nodes {
		if n.ID == id {
			return n.Label
		}
	}
	return id
}

func (r devnetRound) insights(at time.Time) map[string][]insights.Insight {
	var ins []insights.Integration
	for id, name := range r.names {
		e, bad := r.failed[name]
		ins = append(ins, insights.Integration{ID: id, Name: name, OK: !bad, Error: e})
	}
	out := map[string][]insights.Insight{}
	for _, f := range insights.Evaluate(insights.Input{Topology: r.topo, Integrations: ins, Now: at}) {
		out[f.Rule] = append(out[f.Rule], f)
	}
	return out
}

func (r devnetRound) summary() string {
	kinds := map[topology.NodeKind]int{}
	for _, n := range r.topo.Nodes {
		kinds[n.Kind]++
	}
	var parts []string
	for k, v := range kinds {
		parts = append(parts, fmt.Sprintf("%s=%d", k, v))
	}
	sort.Strings(parts)
	return fmt.Sprintf("%d nodes (%s), %d edges", len(r.topo.Nodes), strings.Join(parts, " "), len(r.topo.Edges))
}

// devnetPlace is where the emulator says a node plugs in now.
type devnetPlace struct {
	Kind       string `json:"kind"`
	Site       string `json:"site"`
	MAC        string `json:"mac"`
	IP         string `json:"ip"`
	Present    bool   `json:"present"`
	At         string `json:"at"`
	Port       string `json:"port"`
	PrivateMAC bool   `json:"private_mac"`
	Unmanaged  bool   `json:"unmanaged"`
}

func (l *devnetLab) placements(t *testing.T) map[string]devnetPlace {
	t.Helper()
	resp, err := http.Get(l.ep.Control + "/placements")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]devnetPlace{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// devnetIndex maps the map's nodes to the design's names (by MAC, else by address).
type devnetIndex struct {
	r      devnetRound
	design map[string]devnetPlace
	byNode map[string]string // map node id → design name
	byName map[string]string // design name → map node id
}

func newDevnetIndex(r devnetRound, design map[string]devnetPlace) devnetIndex {
	idx := devnetIndex{r: r, design: design, byNode: map[string]string{}, byName: map[string]string{}}
	byMAC, byIP := map[string]string{}, map[string]string{}
	for name, p := range design {
		if p.MAC != "" {
			byMAC[p.MAC] = name
		}
		if p.IP != "" && p.Present {
			byIP[p.IP] = name
		}
	}
	for _, n := range r.topo.Nodes {
		macs := []string{n.MAC}
		if n.Device != nil {
			for _, m := range n.Device.MACs {
				macs = append(macs, string(m))
			}
		}
		name := ""
		for _, m := range macs {
			if byMAC[m] != "" {
				name = byMAC[m]
				break
			}
		}
		if name == "" && n.Kind != topology.KindWAN {
			name = byIP[n.IP]
		}
		if name == "" {
			continue
		}
		idx.byNode[n.ID] = name
		if _, dup := idx.byName[name]; !dup {
			idx.byName[name] = n.ID
		}
	}
	return idx
}

// parents are the design names (or "wan:<label>" for WAN nodes) of what a design node hangs from.
func (x devnetIndex) parents(name string) ([]string, bool) {
	id, ok := x.byName[name]
	if !ok {
		return nil, false
	}
	var out []string
	for _, e := range x.r.topo.Edges {
		if e.Target != id {
			continue
		}
		if p, ok := x.byNode[e.Source]; ok {
			out = append(out, p)
			continue
		}
		src := x.r.label(e.Source)
		for _, n := range x.r.topo.Nodes {
			if n.ID == e.Source && n.Kind == topology.KindWAN {
				src = "wan:" + n.Label
			}
		}
		out = append(out, src)
	}
	sort.Strings(out)
	return out, true
}

// Where DESIGN.md says the infrastructure hangs ("Expected map"). Any of the
// listed parents will do (a server with two uplinks, a firewall with two WANs).
var devnetPlacements = map[string][]string{
	"core-sw01":    {"fw-hq-01"},
	"fw-hq-02":     {"core-sw01"},
	"sw-f1-01":     {"core-sw01"},
	"sw-f2-01":     {"core-sw01"},
	"sw-f3-01":     {"core-sw01"},
	"sw-stor-01":   {"core-sw01"},
	"sw-wh-01":     {"core-sw01"},
	"sw-dmz-01":    {"fw-hq-01"},
	"pve01":        {"core-sw01", "sw-stor-01"},
	"pve02":        {"core-sw01", "sw-stor-01"},
	"pve03":        {"core-sw01", "sw-stor-01"},
	"dc01":         {"pve01"},
	"dc02":         {"pve02"},
	"lab-fw01":     {"pve02"},
	"omini":        {"pve01"},
	"pbs01":        {"pve03"},
	"nas01":        {"sw-stor-01"},
	"bkp-nas01":    {"sw-stor-01"},
	"ups-rack01":   {"core-sw01"},
	"nvr01":        {"core-sw01"},
	"idrac-pve01":  {"core-sw01"},
	"ap-f1-01":     {"sw-f1-01"},
	"ap-f2-02":     {"sw-f2-01"},
	"ap-mr-01":     {"sw-f3-01"},
	"ap-patio-01":  {"ap-cafe-01"},
	"halo-wh-01":   {"sw-wh-01"},
	"halo-wh-02":   {"sw-wh-01"},
	"halo-wh-03":   {"halo-wh-02"},
	"web01":        {"sw-dmz-01"},
	"cam-f1-01":    {"sw-f1-01"},
	"cam-wh-01":    {"sw-wh-01"},
	"prn-f1-01":    {"sw-f1-01"},
	"prn-wh-zebra": {"sw-wh-01"},
	"br-sw01":      {"br-gw01"},
	"br-ap01":      {"br-sw01"},
	"br-oc200":     {"br-sw01"},
	"br-prn01":     {"br-sw01"},
	"pos-01":       {"store-rt01"},
	"store-modem":  {"wan:"},
	"ont-fiber":    {"wan:"},
	"lte-gw01":     {"wan:"},
}

// devnetKnown are placements Omini gets wrong today (see the report in
// testdata/devnet/README.md): logged, not failed, so the suite stays green
// while they are open. A placement that starts matching is logged too: move
// it out of here.
var devnetKnown = map[string]string{}

func TestDevnetMap(t *testing.T) {
	lab := startDevnet(t)
	lab.at(t, "next tue 10:30")
	r := lab.collect(t)
	design := lab.placements(t)
	t.Log(r.summary())
	for api, e := range r.failed {
		t.Errorf("%s failed: %s", api, e)
	}
	x := newDevnetIndex(r, design)

	names := make([]string, 0, len(devnetPlacements))
	for n := range devnetPlacements {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		want := devnetPlacements[name]
		got, ok := x.parents(name)
		match := false
		for _, g := range got {
			for _, w := range want {
				if g == w || (strings.HasSuffix(w, ":") && strings.HasPrefix(g, w)) {
					match = true
				}
			}
		}
		reason, known := devnetKnown[name]
		switch {
		case !ok && known:
			t.Logf("known issue: %s is not on the map (%s)", name, reason)
		case !ok:
			t.Errorf("%s is not on the map", name)
		case !match && known:
			t.Logf("known issue: %s hangs from %v, want one of %v (%s)", name, got, want, reason)
		case !match:
			t.Errorf("%s hangs from %v, want one of %v", name, got, want)
		case known:
			t.Logf("%s is placed right now: remove it from devnetKnown", name)
		}
	}

	// Every client on the network now is on the map, most of them where they plug in.
	var missing, wrong []string
	right, total := 0, 0
	for name, p := range design {
		if !p.Present || p.At == "" || !devnetEndpoint(p.Kind) {
			continue
		}
		total++
		got, ok := x.parents(name)
		if !ok {
			missing = append(missing, name)
			continue
		}
		accept := map[string]bool{p.At: true}
		if up, ok := design[p.At]; ok && up.Unmanaged {
			accept["Unmanaged segment"] = true // behind the desk switch nobody manages: a segment is right
		}
		if up, ok := design[p.At]; ok && up.Kind == "phone" && up.At != "" {
			accept[up.At] = true // a PC behind a desk phone: on the phone, or on the phone's switch port
		}
		hit := false
		for _, g := range got {
			hit = hit || accept[g]
		}
		if hit {
			right++
		} else {
			wrong = append(wrong, fmt.Sprintf("%s (on %s %s) → %v", name, p.At, p.Port, got))
		}
	}
	sort.Strings(missing)
	sort.Strings(wrong)
	t.Logf("clients: %d on the network, %d placed right, %d elsewhere, %d missing", total, right, len(wrong), len(missing))
	for _, w := range wrong {
		t.Logf("  elsewhere: %s", w)
	}
	if len(missing) > 0 {
		t.Errorf("clients on the network but not on the map: %v", missing)
	}
	if total < 250 || right*100 < total*devnetClientAccuracy {
		t.Errorf("only %d of %d clients placed where they plug in (want ≥ %d%%)", right, total, devnetClientAccuracy)
	}

	// VLANs and subnets reach the map.
	vlans := func(name string) map[uint16]string {
		out := map[uint16]string{}
		if id, ok := x.byName[name]; ok {
			for _, n := range r.topo.Nodes {
				if n.ID == id && n.Device != nil {
					for _, v := range n.Device.Vlans {
						s := ""
						if v.Subnet != nil {
							s = *v.Subnet
						}
						out[v.ID] = s
					}
				}
			}
		}
		return out
	}
	fw := vlans("fw-hq-01")
	for id, subnet := range map[uint16]string{10: "10.10.10.0/24", 32: "10.10.32.0/23", 60: "10.10.60.0/24", 120: "10.10.120.0/24"} {
		if fw[id] != subnet {
			t.Errorf("fw-hq-01 VLAN %d: %q, want %q (all: %v)", id, fw[id], subnet, fw)
		}
	}
	if br := vlans("br-gw01"); len(br) != 5 {
		t.Errorf("br-gw01 VLANs: %v", br)
	}

	// The alerts the scenario raises on a Tuesday morning.
	ins := r.insights(time.Now())
	for _, rule := range []string{"duplicate_ip", "slow_uplink", "weak_wifi", "update_pending", "disk_full", "unmanaged_switch"} {
		if len(ins[rule]) == 0 {
			t.Errorf("no %s alert (got %v)", rule, devnetRules(ins))
		}
	}
	for _, f := range ins["duplicate_ip"] {
		if f.Params["ip"] != "10.10.30.150" {
			t.Logf("known issue: unexpected duplicate IP %v (CARP VIPs reported by both firewalls)", f.Params)
		}
	}
	t.Logf("alerts: %v", devnetRules(ins))
}

// devnetClientAccuracy is the share (%) of clients that must hang where they
// plug in; the rest are known issues (see devnetKnown and the report).
const devnetClientAccuracy = 85

func devnetEndpoint(kind string) bool {
	switch kind {
	case "laptop", "mobile", "desktop", "tablet", "printer", "camera", "tv", "media", "iot", "scanner", "pos", "vm", "lxc":
		return true
	}
	return false
}

func devnetRules(ins map[string][]insights.Insight) []string {
	var out []string
	for k, v := range ins {
		out = append(out, fmt.Sprintf("%s=%d", k, len(v)))
	}
	sort.Strings(out)
	return out
}

// TestDevnetScenario checks the events of the simulated week.
func TestDevnetScenario(t *testing.T) {
	lab := startDevnet(t)

	// Tuesday 14:10: the fiber is down, the 5G link carries the traffic.
	lab.at(t, "next tue 14:10")
	r := lab.collect(t)
	ins := r.insights(time.Now())
	if len(ins["wan_down"]) == 0 {
		// The emulated OPNsense answers a dead PPPoE gateway with status "down" and delay "~"; the plugin
		// reads "~" as "no data" and reports the gateway as unknown, so Omini raises no alert.
		t.Errorf("fiber down raises no wan_down alert (got %v)", devnetRules(ins))
	}
	for _, n := range r.topo.Nodes {
		if n.Kind == topology.KindWAN && n.WAN != nil && n.WAN.Interface == "pppoe0" && n.Online {
			t.Errorf("fiber WAN %s is online while the fiber is down", n.Label)
		}
	}

	// Tuesday 11:15: the 5G backup is degraded.
	lab.at(t, "next tue 11:15")
	r = lab.collect(t)
	if ins := r.insights(time.Now()); len(ins["wan_degraded"]) == 0 {
		t.Errorf("5G degraded: no wan_degraded alert (got %v)", devnetRules(ins))
	}

	// Tuesday 16:05: the WireGuard tunnel to Campinas is down; the branch is unreachable.
	lab.at(t, "next tue 16:05")
	r = lab.collect(t)
	for _, api := range []string{"mikrotik-br-gw01", "omada"} {
		if _, ok := r.failed[api]; !ok {
			t.Errorf("%s should fail while the tunnel is down", api)
		}
	}
	if len(r.failed) != 2 {
		t.Errorf("failed: %v", r.failed)
	}

	// Sunday 02:17: pve02's guests live-migrated to pve03 (pve02 reboots at 02:20).
	lab.at(t, "next sun 02:17")
	r = lab.collect(t)
	x := newDevnetIndex(r, lab.placements(t))
	if p, _ := x.parents("dc02"); len(p) != 1 || p[0] != "pve03" {
		t.Errorf("dc02 during the migration hangs from %v, want pve03", p)
	}

	// Wednesday 03:01: ap-f1-01 reboots for its firmware update.
	lab.at(t, "next wed 03:01")
	r = lab.collect(t)
	x = newDevnetIndex(r, lab.placements(t))
	if id, ok := x.byName["ap-f1-01"]; ok {
		for _, n := range r.topo.Nodes {
			if n.ID == id && n.Online {
				t.Errorf("ap-f1-01 is online while it reboots")
			}
		}
	}
}
