// Package wifichan finds and settles conflicts between the 2.4 GHz channels of
// the network's access points. Every access point is taken to hear the others
// (a homelab is one house). Channels 1, 6 and 11 do not overlap at 20 MHz: the
// plan keeps radios already alone on one of them, moves as few as it can (the
// one with fewest clients first) and shares the three evenly among more than
// three. It never picks a DFS channel nor another width, and leaves alone a
// radio on automatic channel, a mesh unit and one its integration cannot move.
// It is a pure function of the topology.
package wifichan

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// Channels are the 2.4 GHz channels that do not overlap at 20 MHz.
var Channels = []int{1, 6, 11}

// Why a radio is left alone.
const (
	// SkipAuto: its channel is automatic; its controller would move it back.
	SkipAuto = "auto"
	// SkipMesh: a mesh unit (wireless uplink): a new channel can drop its
	// backhaul and the unit with it. Never moved, not even with destructive
	// changes allowed.
	SkipMesh = "mesh"
	// SkipUnsupported: its integration does not declare wifi.channel.
	SkipUnsupported = "unsupported"
	// SkipWidth: it uses a width other than 20 MHz (change it on the device).
	SkipWidth = "width"
)

// Radio is an access point's 2.4 GHz radio as the plan sees it.
type Radio struct {
	NodeID  string `json:"node_id"`
	Label   string `json:"label"`
	Radio   string `json:"radio"`
	Channel int    `json:"channel"`
	// Clients on this radio (the access point's Wi-Fi clients on 2.4 GHz).
	Clients int `json:"clients"`
	// Skip is why it cannot be moved ("" when it can).
	Skip string `json:"skip,omitempty"`
}

// Step moves one radio to another channel.
type Step struct {
	NodeID  string `json:"node_id"`
	Label   string `json:"label"`
	Radio   string `json:"radio"`
	From    int    `json:"from"`
	To      int    `json:"to"`
	Clients int    `json:"clients"`
}

// Radios are the 2.4 GHz radios of the network's online access points that
// are on and report their channel, sorted by access point and radio.
func Radios(topo topology.Topology) []Radio {
	var out []Radio
	for _, n := range topo.Nodes {
		if n.Kind != topology.KindDevice || !n.Online || n.Device == nil {
			continue
		}
		movable := slices.Contains(n.Changes, model.ChangeKindWifiChannel)
		d := n.Via(model.ChangeKindWifiChannel).Device
		for _, r := range d.Radios {
			if r.Band != model.WifiBandA24Ghz || r.Channel == nil || (r.Enabled != nil && !*r.Enabled) {
				continue
			}
			x := Radio{NodeID: n.ID, Label: n.Label, Radio: r.Name, Channel: int(*r.Channel), Clients: Clients(d)}
			switch {
			case model.Deref(d.WirelessUplink):
				x.Skip = SkipMesh
			case model.Deref(r.Auto):
				x.Skip = SkipAuto
			case !movable:
				x.Skip = SkipUnsupported
			case r.WidthMhz != nil && *r.WidthMhz != 20:
				x.Skip = SkipWidth
			}
			out = append(out, x)
		}
	}
	slices.SortFunc(out, func(a, b Radio) int {
		return cmp.Or(strings.Compare(a.Label, b.Label), strings.Compare(a.NodeID, b.NodeID), strings.Compare(a.Radio, b.Radio))
	})
	return out
}

// Clients counts an access point's Wi-Fi clients on its 2.4 GHz radio: those
// it reports on 2.4 GHz, and those without a band when 2.4 GHz is its only band.
func Clients(d *model.Device) int {
	only := !slices.ContainsFunc(d.Radios, func(r model.Radio) bool { return r.Band != model.WifiBandA24Ghz })
	n := 0
	for _, c := range d.WirelessClients {
		if (c.Band != nil && *c.Band == model.WifiBandA24Ghz) || (c.Band == nil && only) {
			n++
		}
	}
	return n
}

// MayBeOn reports whether a Wi-Fi client may be on the access point's 2.4 GHz
// radio: it is on 2.4 GHz, or its band is unknown (when a change could cut it
// off, unknown counts as on it).
func MayBeOn(c model.WirelessClient) bool {
	return c.Band == nil || *c.Band == model.WifiBandA24Ghz
}

// capacity is how many radios each of the three channels takes when they are
// shared evenly.
func capacity(n int) int { return max(1, (n+len(Channels)-1)/len(Channels)) }

func standard(ch int) bool { return slices.Contains(Channels, ch) }

func overlaps(a, b int) bool { return a-b < 5 && b-a < 5 }

// Conflict reports whether the radios need a plan: one is on a channel other
// than 1, 6 or 11, or two share (or overlap on) a channel. More than three
// share the three channels: then a channel only conflicts when it holds more
// than its even share.
func Conflict(rs []Radio) bool {
	count := map[int]int{}
	for _, r := range rs {
		if !standard(r.Channel) {
			return true
		}
		count[r.Channel]++
	}
	limit := capacity(len(rs))
	for _, n := range count {
		if n > limit {
			return true
		}
	}
	return false
}

// Plan is what settles the radios' channels: the steps (fewest clients first)
// and the radios left alone with why. Radios it cannot move stay where they
// are and count on their channel (and on those they overlap).
func Plan(rs []Radio) ([]Step, []Radio) {
	steps := []Step{}
	skipped := []Radio{}
	if !Conflict(rs) {
		return steps, skipped
	}
	limit := capacity(len(rs))
	load := map[int]int{}
	occupy := func(ch int) {
		for _, c := range Channels {
			if overlaps(c, ch) {
				load[c]++
			}
		}
	}
	var move []Radio
	for _, r := range rs {
		if r.Skip != "" {
			skipped = append(skipped, r)
			occupy(r.Channel)
		}
	}
	// Those already on 1, 6 or 11 stay while the channel has room: the ones
	// with most clients first, so the fewest clients move.
	var candidates []Radio
	for _, r := range rs {
		switch {
		case r.Skip != "":
		case standard(r.Channel):
			candidates = append(candidates, r)
		default:
			move = append(move, r)
		}
	}
	slices.SortStableFunc(candidates, func(a, b Radio) int { return b.Clients - a.Clients })
	for _, r := range candidates {
		if load[r.Channel] < limit {
			occupy(r.Channel)
		} else {
			move = append(move, r)
		}
	}
	slices.SortStableFunc(move, func(a, b Radio) int {
		return cmp.Or(a.Clients-b.Clients, strings.Compare(a.Label, b.Label), strings.Compare(a.Radio, b.Radio))
	})
	for _, r := range move {
		to := Channels[0]
		for _, c := range Channels[1:] {
			if load[c] < load[to] || (load[c] == load[to] && distance(c, r.Channel) < distance(to, r.Channel)) {
				to = c
			}
		}
		occupy(to)
		if to == r.Channel {
			continue
		}
		steps = append(steps, Step{NodeID: r.NodeID, Label: r.Label, Radio: r.Radio, From: r.Channel, To: to, Clients: r.Clients})
	}
	return steps, skipped
}

func distance(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// Describe lists radios as "AP (6), Other AP (3)".
func Describe(rs []Radio) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = fmt.Sprintf("%s (%d)", r.Label, r.Channel)
	}
	return strings.Join(parts, ", ")
}
