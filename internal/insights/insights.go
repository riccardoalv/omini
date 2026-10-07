// Package insights finds what is wrong (or worth knowing) on the network: each
// rule is a pure function of the topology. The collector stores the result as
// alerts, opened when an insight appears and resolved when it is gone.
package insights

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// Severities, most urgent first.
const (
	Critical = "critical"
	Warning  = "warning"
	Info     = "info"
)

// Thresholds of the rules.
const (
	DiskWarnPct     = 80.0
	DiskCritPct     = 90.0
	CPUTempWarnC    = 70.0
	CPUTempCritC    = 85.0
	CPUWarnPct      = 80.0
	CPUCritPct      = 95.0
	MemWarnPct      = 90.0
	WeakSignalDBM   = -75
	SaturatedRatio  = 0.8
	SlowUplinkMbps  = 1000
	NewDeviceWindow = 24 * time.Hour
)

// Insight is one finding. Key identifies it across runs (rule + subject), so
// it is opened once and resolved when the rule stops finding it. Params carry
// the values the UI needs to describe it (texts are translated there).
type Insight struct {
	Key      string         `json:"key"`
	Rule     string         `json:"rule"`
	Severity string         `json:"severity"`
	NodeID   string         `json:"node_id,omitempty"`
	Params   map[string]any `json:"params"`
}

// Integration is the outcome of an integration's last collection.
type Integration struct {
	ID    int64
	Name  string
	OK    bool
	Error string
}

// NewDevice is a device seen for the first time recently.
type NewDevice struct {
	NodeID    string
	FirstSeen time.Time
}

// Input is everything the rules look at.
type Input struct {
	Topology     topology.Topology
	Integrations []Integration
	NewDevices   []NewDevice
	Now          time.Time
}

// Rule is one check.
type Rule func(in Input, idx index) []Insight

// Rules are every check, in no particular order.
var Rules = map[string]Rule{
	"device_offline":     deviceOffline,
	"integration_failed": integrationFailed,
	"wan_status":         wanStatus,
	"duplicate_ip":       duplicateIP,
	"update_pending":     updatePending,
	"disk_full":          diskFull,
	"hot_cpu":            hotCPU,
	"high_cpu":           highCPU,
	"high_memory":        highMemory,
	"slow_uplink":        slowUplink,
	"interface_errors":   interfaceErrors,
	"weak_wifi":          weakWifi,
	"saturated_link":     saturatedLink,
	"unmanaged_switch":   unmanagedSwitch,
	"unknown_neighbor":   unknownNeighbor,
	"new_device":         newDevice,
}

type index struct {
	byID map[string]*topology.Node
}

func (x index) label(id string) string {
	if n := x.byID[id]; n != nil {
		return n.Label
	}
	return id
}

// Evaluate runs every rule. The result is sorted by severity, then key.
func Evaluate(in Input) []Insight {
	idx := index{byID: map[string]*topology.Node{}}
	for i := range in.Topology.Nodes {
		idx.byID[in.Topology.Nodes[i].ID] = &in.Topology.Nodes[i]
	}
	out := []Insight{}
	for _, r := range Rules {
		out = append(out, r(in, idx)...)
	}
	rank := map[string]int{Critical: 0, Warning: 1, Info: 2}
	slices.SortFunc(out, func(a, b Insight) int {
		return cmp.Or(cmp.Compare(rank[a.Severity], rank[b.Severity]), strings.Compare(a.Key, b.Key))
	})
	return out
}

func node(rule, severity string, n *topology.Node, subject string, params map[string]any) Insight {
	key := rule + "|" + n.ID
	if subject != "" {
		key += "|" + subject
	}
	if params == nil {
		params = map[string]any{}
	}
	params["node"] = n.Label
	return Insight{Key: key, Rule: rule, Severity: severity, NodeID: n.ID, Params: params}
}

// devices are the managed devices (with collected data) of the map.
func devices(in Input) []*topology.Node {
	var out []*topology.Node
	for i := range in.Topology.Nodes {
		if n := &in.Topology.Nodes[i]; n.Kind == topology.KindDevice && n.Device != nil {
			out = append(out, n)
		}
	}
	return out
}

func deviceOffline(in Input, _ index) []Insight {
	errs := map[int64]string{}
	for _, it := range in.Integrations {
		if !it.OK {
			errs[it.ID] = it.Error
		}
	}
	var out []Insight
	for _, n := range devices(in) {
		if n.Online {
			continue
		}
		p := map[string]any{}
		if e := errs[n.IntegrationID]; e != "" {
			p["error"] = e
		}
		out = append(out, node("device_offline", Critical, n, "", p))
	}
	return out
}

// integrationFailed reports integrations that fail and have no device on the
// map (else the device is reported offline, with the error).
func integrationFailed(in Input, _ index) []Insight {
	shown := map[int64]bool{}
	for _, n := range devices(in) {
		shown[n.IntegrationID] = true
	}
	var out []Insight
	for _, it := range in.Integrations {
		if it.OK || shown[it.ID] {
			continue
		}
		out = append(out, Insight{
			Key: fmt.Sprintf("integration_failed|%d", it.ID), Rule: "integration_failed", Severity: Critical,
			Params: map[string]any{"integration": it.Name, "integration_id": it.ID, "error": it.Error},
		})
	}
	return out
}

// wanStatus reports internet uplinks whose gateway is down (critical) or
// degraded (warning).
func wanStatus(in Input, _ index) []Insight {
	var out []Insight
	for i := range in.Topology.Nodes {
		n := &in.Topology.Nodes[i]
		if n.Kind != topology.KindWAN || n.WAN == nil {
			continue
		}
		for _, g := range n.WAN.Gateways {
			p := map[string]any{"gateway": g.Name}
			if g.LossPct != nil {
				p["loss_pct"] = *g.LossPct
			}
			if g.RttMs != nil {
				p["rtt_ms"] = *g.RttMs
			}
			switch g.Status {
			case model.GatewayStatusDown:
				out = append(out, node("wan_down", Critical, n, g.Name, p))
			case model.GatewayStatusDegraded:
				out = append(out, node("wan_degraded", Warning, n, g.Name, p))
			}
		}
	}
	return out
}

func duplicateIP(in Input, idx index) []Insight {
	byIP := map[string][]*topology.Node{}
	for i := range in.Topology.Nodes {
		n := &in.Topology.Nodes[i]
		if !n.Online || n.IP == "" || n.MAC == "" ||
			(n.Kind != topology.KindClient && n.Kind != topology.KindDevice) {
			continue
		}
		byIP[n.IP] = append(byIP[n.IP], n)
	}
	var out []Insight
	for ip, nodes := range byIP {
		macs := map[string]bool{}
		for _, n := range nodes {
			macs[strings.ToLower(n.MAC)] = true
		}
		if len(macs) < 2 {
			continue
		}
		slices.SortFunc(nodes, func(a, b *topology.Node) int { return strings.Compare(a.ID, b.ID) })
		names := make([]string, 0, len(nodes))
		for _, n := range nodes {
			names = append(names, idx.label(n.ID)+" ("+n.MAC+")")
		}
		out = append(out, Insight{
			Key: "duplicate_ip|" + ip, Rule: "duplicate_ip", Severity: Warning, NodeID: nodes[0].ID,
			Params: map[string]any{"ip": ip, "devices": strings.Join(names, ", ")},
		})
	}
	return out
}

func updatePending(in Input, _ index) []Insight {
	var out []Insight
	for _, n := range devices(in) {
		f := n.Device.Firmware
		if f == nil || f.UpdateAvailable == nil || !*f.UpdateAvailable {
			continue
		}
		p := map[string]any{}
		if f.Current != nil {
			p["current"] = *f.Current
		}
		if f.Latest != nil {
			p["latest"] = *f.Latest
		}
		if f.Updates != nil {
			p["updates"] = *f.Updates
		}
		if f.NeedsReboot != nil && *f.NeedsReboot {
			p["reboot"] = true
		}
		out = append(out, node("update_pending", Warning, n, "", p))
	}
	return out
}

func diskFull(in Input, _ index) []Insight {
	var out []Insight
	for _, n := range devices(in) {
		for _, s := range n.Device.Storage {
			if s.TotalBytes == nil || s.UsedBytes == nil || *s.TotalBytes == 0 {
				continue
			}
			pct := float64(*s.UsedBytes) * 100 / float64(*s.TotalBytes)
			sev := ""
			switch {
			case pct >= DiskCritPct:
				sev = Critical
			case pct >= DiskWarnPct:
				sev = Warning
			default:
				continue
			}
			out = append(out, node("disk_full", sev, n, s.Mount, map[string]any{"mount": s.Mount, "pct": round1(pct)}))
		}
	}
	return out
}

func hotCPU(in Input, _ index) []Insight {
	var out []Insight
	for _, n := range devices(in) {
		hottest, sensor := -1.0, ""
		for _, t := range n.Device.Temperatures {
			if t.Kind != nil && *t.Kind == model.TemperatureKindCPU && t.Celsius > hottest {
				hottest, sensor = t.Celsius, t.Sensor
			}
		}
		sev := ""
		switch {
		case hottest >= CPUTempCritC:
			sev = Critical
		case hottest >= CPUTempWarnC:
			sev = Warning
		default:
			continue
		}
		out = append(out, node("hot_cpu", sev, n, "", map[string]any{"celsius": round1(hottest), "sensor": sensor}))
	}
	return out
}

func highCPU(in Input, _ index) []Insight {
	var out []Insight
	for _, n := range devices(in) {
		if c := n.Device.CPUPct; n.Online && c != nil && *c > CPUWarnPct {
			sev := Warning
			if *c >= CPUCritPct {
				sev = Critical
			}
			out = append(out, node("high_cpu", sev, n, "", map[string]any{"pct": round1(*c)}))
		}
	}
	return out
}

func highMemory(in Input, _ index) []Insight {
	var out []Insight
	for _, n := range devices(in) {
		if m := n.Device.MemPct; n.Online && m != nil && *m >= MemWarnPct {
			out = append(out, node("high_memory", Warning, n, "", map[string]any{"pct": round1(*m)}))
		}
	}
	return out
}

// infrastructure reports whether a node carries other devices' traffic.
func infrastructure(n *topology.Node) bool {
	switch n.Kind {
	case topology.KindUnmanaged, topology.KindSegment:
		return true
	case topology.KindDevice:
		return n.Role == "switch" || n.Role == "ap" || n.Role == "router" || n.Role == "firewall"
	}
	return false
}

// slowUplink reports links between network devices (switches, APs, routers)
// that run below 1 Gbps.
func slowUplink(in Input, idx index) []Insight {
	var out []Insight
	for _, e := range in.Topology.Edges {
		src, dst := idx.byID[e.Source], idx.byID[e.Target]
		if src == nil || dst == nil || e.SpeedMbps == 0 || e.SpeedMbps >= SlowUplinkMbps || e.Kind == topology.EdgeWifi {
			continue
		}
		if !infrastructure(src) || !infrastructure(dst) || dst.Kind == topology.KindSegment {
			continue
		}
		out = append(out, node("slow_uplink", Warning, dst, e.Source+"|"+e.SourcePort, map[string]any{
			"speed_mbps": e.SpeedMbps, "from": src.Label, "port": cmp.Or(e.SourcePort, e.TargetPort),
		}))
	}
	return out
}

func interfaceErrors(in Input, _ index) []Insight {
	var out []Insight
	for _, n := range devices(in) {
		for name, r := range n.Traffic {
			if r.RxErrors+r.TxErrors == 0 {
				continue
			}
			out = append(out, node("interface_errors", Warning, n, name, map[string]any{
				"iface": name, "rx_errors": r.RxErrors, "tx_errors": r.TxErrors,
			}))
		}
	}
	return out
}

func weakWifi(in Input, idx index) []Insight {
	var out []Insight
	for i := range in.Topology.Nodes {
		n := &in.Topology.Nodes[i]
		if !n.Online || n.SignalDBM == nil || *n.SignalDBM >= WeakSignalDBM {
			continue
		}
		out = append(out, node("weak_wifi", Warning, n, "", map[string]any{
			"signal_dbm": *n.SignalDBM, "ap": idx.label(n.ParentID), "ssid": n.SSID,
		}))
	}
	return out
}

// physical reports whether an interface is a real port (its speed bounds the traffic).
func physical(i model.Interface) bool {
	return i.Type == nil || *i.Type == model.InterfaceTypeEthernet
}

func saturatedLink(in Input, _ index) []Insight {
	var out []Insight
	for _, n := range devices(in) {
		for _, i := range n.Device.Interfaces {
			r, ok := n.Traffic[i.Name]
			if !ok || !physical(i) || i.SpeedMbps == nil || *i.SpeedMbps == 0 || (i.Up != nil && !*i.Up) {
				continue
			}
			speed := float64(*i.SpeedMbps) * 1e6
			used := max(float64(r.RxBps), float64(r.TxBps)) / speed
			if used <= SaturatedRatio {
				continue
			}
			out = append(out, node("saturated_link", Warning, n, i.Name, map[string]any{
				"iface": i.Name, "pct": round1(used * 100), "speed_mbps": *i.SpeedMbps,
				"rx_bps": r.RxBps, "tx_bps": r.TxBps,
			}))
		}
	}
	return out
}

func unmanagedSwitch(in Input, idx index) []Insight {
	var out []Insight
	for i := range in.Topology.Nodes {
		n := &in.Topology.Nodes[i]
		if n.Kind != topology.KindSegment {
			continue
		}
		out = append(out, node("unmanaged_switch", Info, n, "", map[string]any{
			"parent": idx.label(n.ParentID), "port": n.Port, "macs": n.MACCount,
		}))
	}
	return out
}

func unknownNeighbor(in Input, idx index) []Insight {
	var out []Insight
	for i := range in.Topology.Nodes {
		n := &in.Topology.Nodes[i]
		if n.Kind != topology.KindUnmanaged {
			continue
		}
		out = append(out, node("unknown_neighbor", Info, n, "", map[string]any{
			"parent": idx.label(n.ParentID), "port": n.Port, "model": n.Model,
		}))
	}
	return out
}

func newDevice(in Input, idx index) []Insight {
	var out []Insight
	for _, d := range in.NewDevices {
		if in.Now.Sub(d.FirstSeen) > NewDeviceWindow {
			continue
		}
		p := map[string]any{"first_seen": d.FirstSeen.UTC().Format(time.RFC3339)}
		n := idx.byID[d.NodeID]
		if n == nil {
			n = &topology.Node{ID: d.NodeID, Label: d.NodeID}
		}
		if n.MAC != "" {
			p["mac"] = n.MAC
		}
		if n.IP != "" {
			p["ip"] = n.IP
		}
		if n.Vendor != "" {
			p["vendor"] = n.Vendor
		}
		out = append(out, node("new_device", Info, n, "", p))
	}
	return out
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }
