package insights

import (
	"strings"

	"github.com/riccardoalv/omini/internal/wifichan"
)

func init() { Rules["wifi_channel_conflict"] = wifiChannelConflict }

// wifiChannelConflict: the access points' 2.4 GHz radios are on the same or
// overlapping channels, or one is off 1, 6 and 11. One insight for the whole
// network (every access point is taken to hear the others), on the first
// access point, listing them all; movable is how many radios Resolve would
// move (none: no button), auto_aps those on automatic channel, left alone.
func wifiChannelConflict(in Input, _ index) []Insight {
	radios := wifichan.Radios(in.Topology)
	if len(radios) == 0 || !wifichan.Conflict(radios) {
		return nil
	}
	steps, _ := wifichan.Plan(radios)
	var nodes, auto []string
	for _, r := range radios {
		if len(nodes) == 0 || nodes[len(nodes)-1] != r.NodeID {
			nodes = append(nodes, r.NodeID)
		}
		if r.Skip == wifichan.SkipAuto {
			auto = append(auto, r.Label)
		}
	}
	params := map[string]any{
		"aps": wifichan.Describe(radios), "count": len(nodes), "movable": len(steps), "nodes": nodes,
	}
	if len(auto) > 0 {
		params["auto_aps"] = strings.Join(auto, ", ")
	}
	// Nothing Omini can move (a mesh that picks its channels, a controller on
	// auto, a plugin that cannot change them): worth knowing, not a warning
	// that pops up with nothing to press.
	severity := Warning
	if len(steps) == 0 {
		severity = Info
	}
	return []Insight{{
		Key: "wifi_channel_conflict", Rule: "wifi_channel_conflict", Severity: severity,
		NodeID: radios[0].NodeID, Params: params,
	}}
}
