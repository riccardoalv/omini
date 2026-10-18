package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
	"github.com/riccardoalv/omini/internal/wifichan"
)

// Wi-Fi channels (wifi.channel): a radio moved to another channel. Only 2.4
// GHz radios at 20 MHz whose channel is not automatic, never a mesh unit's.
// The plan that settles a conflict between access points is in wifichan.

func radioOf(d *model.Device, name string) *model.Radio {
	for i := range d.Radios {
		if d.Radios[i].Name == name {
			return &d.Radios[i]
		}
	}
	return nil
}

// wifiChannel checks a wifi.channel value against the access point.
func wifiChannel(n topology.Node, value any) (json.RawMessage, any, error) {
	var v model.RadioChange
	raw, err := json.Marshal(value)
	if err == nil {
		err = json.Unmarshal(raw, &v)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("%w: wifi.channel takes {radio, channel}", ErrInvalidChange)
	}
	if model.Deref(n.Device.WirelessUplink) {
		return nil, nil, fmt.Errorf("%w: %s reaches the network over Wi-Fi (mesh): a new channel can cut it off", ErrInvalidChange, n.Label)
	}
	r := radioOf(n.Device, v.Radio)
	switch {
	case r == nil:
		return nil, nil, fmt.Errorf("%w: %s has no radio %q", ErrInvalidChange, n.Label, v.Radio)
	case r.Band != model.WifiBandA24Ghz:
		return nil, nil, fmt.Errorf("%w: only 2.4 GHz channels are changed for now", ErrInvalidChange)
	case v.Channel > maxChannel24:
		return nil, nil, fmt.Errorf("%w: 2.4 GHz channels are 1 to 13", ErrInvalidChange)
	case model.Deref(r.Auto):
		return nil, nil, fmt.Errorf("%w: the channel of %s is automatic: its controller would move it back", ErrInvalidChange, v.Radio)
	case r.WidthMhz != nil && *r.WidthMhz != 20:
		return nil, nil, fmt.Errorf("%w: %s uses %d MHz: only 20 MHz radios are moved", ErrInvalidChange, v.Radio, *r.WidthMhz)
	}
	var before json.RawMessage
	if r.Channel != nil {
		before, err = json.Marshal(model.RadioChange{Radio: r.Name, Channel: *r.Channel})
	}
	return before, v, err
}

// radioGuard: moving an access point's 2.4 GHz radio is destructive when
// Omini's own machine is one of its Wi-Fi clients (its band unknown counts).
func radioGuard(n topology.Node, self Self) (topology.Protection, bool) {
	if wirelessClient(n.Device, self.MACs) {
		return topology.Protection{Reason: "omini"}, true
	}
	return topology.Protection{}, false
}

// wirelessClient reports whether one of the MACs may be on the access
// point's 2.4 GHz radio.
func wirelessClient(d *model.Device, macs []model.MACAddress) bool {
	if d == nil {
		return false
	}
	return slices.ContainsFunc(d.WirelessClients, func(c model.WirelessClient) bool {
		return wifichan.MayBeOn(c) && slices.Contains(macs, c.MAC)
	})
}

// ChannelStep is a step of the Wi-Fi channels plan: Protected when Omini's
// machine is on that radio (destructive), Browser when the browser asking is.
type ChannelStep struct {
	wifichan.Step
	Protected *topology.Protection `json:"protected,omitempty"`
	Browser   bool                 `json:"browser,omitempty"`
}

// ChannelPlan is what settles the access points' 2.4 GHz channels, and the
// radios left alone with why (wifichan.Skip*).
type ChannelPlan struct {
	Steps   []ChannelStep    `json:"steps"`
	Skipped []wifichan.Radio `json:"skipped"`
}

// PlanChannels is the plan for the network's Wi-Fi channel conflict. browser
// is the address of the browser asking ("" when unknown).
func (c *Collector) PlanChannels(browser string) ChannelPlan {
	topo := c.State().Topology
	steps, skipped := wifichan.Plan(wifichan.Radios(topo))
	var macs []model.MACAddress
	for _, n := range topo.Nodes {
		if browser != "" && n.IP == browser && n.MAC != "" {
			macs = append(macs, model.MACAddress(n.MAC))
		}
	}
	plan := ChannelPlan{Steps: make([]ChannelStep, len(steps)), Skipped: skipped}
	for i, s := range steps {
		plan.Steps[i] = ChannelStep{Step: s}
		n, ok := c.node(s.NodeID)
		if !ok {
			continue
		}
		n = n.Via(model.ChangeKindWifiChannel)
		if p, guarded := radioGuard(n, c.opts.Self); guarded {
			plan.Steps[i].Protected = &p
		}
		plan.Steps[i].Browser = wirelessClient(n.Device, macs)
	}
	return plan
}

// maxChannel24 is the last 2.4 GHz channel Omini moves a radio to.
const maxChannel24 = 13

// Outcomes of a step.
const (
	StepDone   = "done"
	StepFailed = "failed"
	StepNotRun = "not_run"
)

// ChannelResult is what became of a step.
type ChannelResult struct {
	ChannelStep
	Status string              `json:"status"`
	Error  string              `json:"error,omitempty"`
	Change *store.ConfigChange `json:"change,omitempty"`
}

// ApplyChannels applies the Wi-Fi channels plan one access point at a time,
// through the normal change path (applied, verified and saved by its
// integration, kept in the history), then checks the radio is up on its new
// channel. The first that fails stops it; those already done stay. A
// destructive step (Omini on that radio) is blocked unless destructive
// changes are allowed and the plan is confirmed a second time.
func (c *Collector) ApplyChannels(ctx context.Context, confirmed bool) ([]ChannelResult, error) {
	plan := c.PlanChannels("")
	if len(plan.Steps) == 0 {
		return nil, fmt.Errorf("%w: no access point to move", ErrInvalidChange)
	}
	for _, s := range plan.Steps {
		if s.Protected != nil {
			if allowed := c.AllowDestructive(ctx); !allowed || !confirmed {
				return nil, &DestructiveError{Protection: *s.Protected, Allowed: allowed}
			}
		}
	}
	out := make([]ChannelResult, len(plan.Steps))
	for i, s := range plan.Steps {
		out[i] = ChannelResult{ChannelStep: s, Status: StepNotRun}
	}
	for i, s := range plan.Steps {
		rec, err := c.Apply(ctx, ChangeRequest{
			NodeID: s.NodeID, Kind: model.ChangeKindWifiChannel,
			Value: model.RadioChange{Radio: s.Radio, Channel: uint8(s.To)}, Confirmed: confirmed,
		})
		if rec.ID != 0 {
			out[i].Change = &rec
		}
		if err == nil {
			err = c.radioOn(s)
		}
		if err != nil {
			out[i].Status, out[i].Error = StepFailed, err.Error()
			return out, fmt.Errorf("%s: %w", s.Label, err)
		}
		out[i].Status = StepDone
	}
	return out, nil
}

// radioOn checks the access point, as read after the change, has the radio up
// on its new channel.
func (c *Collector) radioOn(s ChannelStep) error {
	n, ok := c.node(s.NodeID)
	if ok && n.Device != nil {
		n = n.Via(model.ChangeKindWifiChannel)
		r := radioOf(n.Device, s.Radio)
		if r != nil && r.Channel != nil && int(*r.Channel) == s.To && (r.Enabled == nil || *r.Enabled) {
			return nil
		}
	}
	return fmt.Errorf("the radio %s is not up on channel %d after the change", s.Radio, s.To)
}
