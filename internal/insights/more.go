package insights

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// Thresholds of the rules below.
const (
	FastEthernetMbps = 100              // a wired link at this speed or less
	FlapsPerHour     = 3                // up/down changes of a port in an hour
	RebootWindow     = time.Hour        // how long a restart stays an alert
	PoolFullPct      = 90.0             // DHCP pool or firewall states in use
	SFPLowRxDBM      = -20.0            // without the module's own threshold
	BurstDevices     = 5                // new devices...
	BurstWindow      = 10 * time.Minute // ...first seen within this time
)

// Detector says which devices an integration knows how to read: by the
// product, brand or OS identification found. Type is the integration type.
type Detector struct {
	Type     string
	Name     string
	Products []string
	Brands   []string
	OS       []string
}

// More inputs of the rules: what the collector remembers between rounds.
type History struct {
	// Flaps counts the up/down changes of each port in the last hour ("node|iface").
	Flaps map[string]int
	// Reboots is when each device restarted (its uptime went back), recently.
	Reboots map[string]time.Time
}

func init() {
	Rules["fast_ethernet"] = fastEthernet
	Rules["integration_available"] = integrationAvailable
	Rules["link_flapping"] = linkFlapping
	Rules["half_duplex"] = halfDuplex
	Rules["device_rebooted"] = deviceRebooted
	Rules["sfp_low_rx"] = sfpLowRx
	Rules["dhcp_pool_full"] = dhcpPoolFull
	Rules["firewall_states_full"] = firewallStatesFull
	Rules["insecure_service"] = insecureService
	Rules["new_devices_burst"] = newDevicesBurst
	Rules["discovery_limited"] = discoveryLimited
}

// fastEthernet: a wired link to a device that runs at 100 Mbps or less (a
// Fast Ethernet port, a cable with a broken pair, a forced speed). Links
// between network devices are slow_uplink's.
func fastEthernet(in Input, idx index) []Insight {
	var out []Insight
	for _, e := range in.Topology.Edges {
		src, dst := idx.byID[e.Source], idx.byID[e.Target]
		if src == nil || dst == nil || !dst.Online || e.Kind == topology.EdgeWifi ||
			e.SpeedMbps == 0 || e.SpeedMbps > FastEthernetMbps {
			continue
		}
		if infrastructure(src) && infrastructure(dst) {
			continue
		}
		out = append(out, node("fast_ethernet", Warning, dst, e.Source+"|"+e.SourcePort, map[string]any{
			"speed_mbps": e.SpeedMbps, "from": src.Label, "port": e.SourcePort,
		}))
	}
	return out
}

// integrationAvailable: a device Omini identified (by product, brand or OS)
// that an integration could read in full, while no integration of that type
// is set up.
func integrationAvailable(in Input, _ index) []Insight {
	var out []Insight
	for i := range in.Topology.Nodes {
		n := &in.Topology.Nodes[i]
		if !n.Online || n.Kind == topology.KindApp {
			continue
		}
		if n.Device != nil && n.IntegrationID != 0 && !in.Core[n.IntegrationID] {
			continue // already read by an integration of its own
		}
		for _, d := range in.Detectors {
			if in.Configured[d.Type] {
				continue
			}
			if slices.Contains(d.Products, n.Product) && n.Product != "" ||
				slices.Contains(d.Brands, n.Brand) && n.Brand != "" ||
				slices.Contains(d.OS, n.OS) && n.OS != "" {
				out = append(out, node("integration_available", Warning, n, d.Type, map[string]any{
					"integration": d.Name, "type": d.Type,
				}))
				break
			}
		}
	}
	return out
}

func linkFlapping(in Input, idx index) []Insight {
	var out []Insight
	for key, n := range in.History.Flaps {
		if n < FlapsPerHour {
			continue
		}
		id, iface, _ := strings.Cut(key, "|")
		if d := idx.byID[id]; d != nil {
			out = append(out, node("link_flapping", Warning, d, iface, map[string]any{"iface": iface, "changes": n}))
		}
	}
	return out
}

func halfDuplex(in Input, _ index) []Insight {
	var out []Insight
	for _, n := range devices(in) {
		for _, i := range n.Device.Interfaces {
			if model.Deref(i.Duplex) == model.InterfaceDuplexHalf && (i.Up == nil || *i.Up) {
				out = append(out, node("half_duplex", Warning, n, i.Name, map[string]any{"iface": i.Name}))
			}
		}
	}
	return out
}

func deviceRebooted(in Input, idx index) []Insight {
	var out []Insight
	for id, at := range in.History.Reboots {
		d := idx.byID[id]
		if d == nil || in.Now.Sub(at) > RebootWindow {
			continue
		}
		out = append(out, node("device_rebooted", Warning, d, "", map[string]any{
			"at": at.UTC().Format(time.RFC3339),
		}))
	}
	return out
}

func sfpLowRx(in Input, _ index) []Insight {
	var out []Insight
	for _, n := range devices(in) {
		for _, i := range n.Device.Interfaces {
			t := i.Transceiver
			if t == nil || t.RxPowerDBM == nil || (i.Up != nil && !*i.Up) {
				continue
			}
			limit := SFPLowRxDBM
			if t.RxPowerLowDBM != nil {
				limit = *t.RxPowerLowDBM
			}
			if *t.RxPowerDBM < limit {
				out = append(out, node("sfp_low_rx", Warning, n, i.Name, map[string]any{
					"iface": i.Name, "rx_dbm": round1(*t.RxPowerDBM), "limit_dbm": round1(limit),
				}))
			}
		}
	}
	return out
}

func dhcpPoolFull(in Input, _ index) []Insight {
	var out []Insight
	for _, n := range devices(in) {
		for _, p := range n.Device.DhcpPools {
			if p.Total == nil || *p.Total == 0 || p.Used == nil {
				continue
			}
			if pct := float64(*p.Used) / float64(*p.Total) * 100; pct >= PoolFullPct {
				out = append(out, node("dhcp_pool_full", Warning, n, p.Network, map[string]any{
					"network": p.Network, "pct": round1(pct), "used": *p.Used, "total": *p.Total,
				}))
			}
		}
	}
	return out
}

func firewallStatesFull(in Input, _ index) []Insight {
	var out []Insight
	for _, n := range devices(in) {
		s := n.Device.FirewallStates
		if s == nil || s.Limit == nil || *s.Limit == 0 || s.Current == nil {
			continue
		}
		if pct := float64(*s.Current) / float64(*s.Limit) * 100; pct >= PoolFullPct {
			out = append(out, node("firewall_states_full", Warning, n, "", map[string]any{
				"pct": round1(pct), "current": *s.Current, "limit": *s.Limit,
			}))
		}
	}
	return out
}

// insecureService: Telnet or FTP open (passwords in clear), or a network
// device's admin page served only over plain HTTP.
func insecureService(in Input, _ index) []Insight {
	var out []Insight
	for i := range in.Topology.Nodes {
		n := &in.Topology.Nodes[i]
		if !n.Online {
			continue
		}
		for port, name := range map[int]string{23: "Telnet", 21: "FTP"} {
			if slices.Contains(n.OpenPorts, port) {
				out = append(out, node("insecure_service", Warning, n, fmt.Sprint(port), map[string]any{
					"service": name, "port": port,
				}))
			}
		}
		if !infrastructure(n) || len(n.Web) == 0 {
			continue
		}
		https := slices.ContainsFunc(n.Web, func(w topology.WebApp) bool { return strings.HasPrefix(w.URL, "https://") }) ||
			slices.Contains(n.OpenPorts, 443) || slices.Contains(n.OpenPorts, 8443) // HTTPS there, even if its page was not read
		if !https {
			out = append(out, node("insecure_service", Warning, n, "http", map[string]any{
				"service": "HTTP", "port": n.Web[0].Port,
			}))
		}
	}
	return out
}

// newDevicesBurst: several devices never seen before in a few minutes (a
// visitor, a new appliance — or someone who should not be there).
func newDevicesBurst(in Input, idx index) []Insight {
	var recent []NewDevice
	for _, d := range in.NewDevices {
		if in.Now.Sub(d.FirstSeen) <= BurstWindow {
			recent = append(recent, d)
		}
	}
	if len(recent) < BurstDevices {
		return nil
	}
	var names []string
	for _, d := range recent {
		names = append(names, idx.label(d.NodeID))
	}
	slices.Sort(names)
	return []Insight{{
		Key: "new_devices_burst", Rule: "new_devices_burst", Severity: Warning,
		Params: map[string]any{"count": len(recent), "minutes": int(BurstWindow / time.Minute), "devices": strings.Join(names, ", ")},
	}}
}

// discoveryLimited: the network scan cannot do everything where Omini runs
// (a Docker bridge network, no multicast, no ping): fewer devices, fewer names.
func discoveryLimited(in Input, _ index) []Insight {
	if len(in.DiscoveryLimited) == 0 {
		return nil
	}
	return []Insight{{
		Key: "discovery_limited", Rule: "discovery_limited", Severity: Warning,
		Params: map[string]any{"limits": strings.Join(in.DiscoveryLimited, ",")},
	}}
}
