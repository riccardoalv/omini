package insights

import (
	"time"

	"github.com/riccardoalv/omini/internal/model"
)

// VPNWindow: a VPN peer connected this recently that is not any more is down;
// one that has not connected within it (a road warrior's phone) is not told.
const VPNWindow = 24 * time.Hour

func init() {
	Rules["service_stopped"] = serviceStopped
	Rules["vpn_down"] = vpnDown
	Rules["reboot_pending"] = rebootPending
}

// serviceStopped: a service the device is configured to run is not running.
func serviceStopped(in Input, _ index) []Insight {
	var out []Insight
	for _, n := range devices(in) {
		if !n.Online {
			continue // its last data is old: device_offline tells it
		}
		for _, s := range n.Device.Services {
			if !model.Deref(s.Enabled) || s.Running == nil || *s.Running {
				continue
			}
			p := map[string]any{"service": s.Name}
			if d := model.Deref(s.Description); d != "" {
				p["description"] = d
			}
			out = append(out, node("service_stopped", Warning, n, s.Name, p))
		}
	}
	return out
}

// vpnDown: a VPN peer that was connected in the last VPNWindow is not now.
// When it was last connected comes from the collector's memory of the
// rounds, or the peer's last handshake.
func vpnDown(in Input, _ index) []Insight {
	var out []Insight
	for _, n := range devices(in) {
		if !n.Online {
			continue
		}
		for _, p := range n.Device.VpnPeers {
			if p.Connected == nil || *p.Connected {
				continue
			}
			last := in.History.VPNConnected[n.ID+"|"+p.Name]
			if h := p.LastHandshake; h != nil && h.After(last) {
				last = *h
			}
			if last.IsZero() || in.Now.Sub(last) > VPNWindow {
				continue
			}
			params := map[string]any{"peer": p.Name, "at": last.UTC().Format(time.RFC3339)}
			if e := model.Deref(p.Endpoint); e != "" {
				params["endpoint"] = e
			}
			out = append(out, node("vpn_down", Warning, n, p.Name, params))
		}
	}
	return out
}

// rebootPending: the device asks for a restart (to finish an update it
// installed, or a change), whether or not an update is pending.
func rebootPending(in Input, _ index) []Insight {
	var out []Insight
	for _, n := range devices(in) {
		if f := n.Device.Firmware; n.Online && f != nil && model.Deref(f.NeedsReboot) {
			out = append(out, node("reboot_pending", Info, n, "", nil))
		}
	}
	return out
}
