package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

// A VLAN's Wi-Fi (wifi.set, wifi.delete): a network name (SSID) whose
// clients the access points put in the VLAN. The password goes to the
// device and nowhere else: never into the history, the logs or the catalog.

// WifiDelete is the value of wifi.delete.
type WifiDelete struct {
	SSID string `json:"ssid"`
}

func wifiOf(n topology.Node, ssid string) *model.WifiNetwork {
	if n.Device == nil || ssid == "" {
		return nil
	}
	for i := range n.Device.WifiNetworks {
		if n.Device.WifiNetworks[i].SSID == ssid {
			return &n.Device.WifiNetworks[i]
		}
	}
	return nil
}

// wifiSet checks a wifi.set value: a name of 1 to 32 bytes and, for a new
// network, a WPA2 password of 8 to 63 characters.
func wifiSet(n topology.Node, value any) (json.RawMessage, any, error) {
	var v model.WifiChange
	raw, err := json.Marshal(value)
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		err = dec.Decode(&v)
	}
	if err != nil || v.SSID == "" || len(v.SSID) > 32 {
		return nil, nil, fmt.Errorf("%w: a Wi-Fi network has a name of 1 to 32 characters", ErrInvalidChange)
	}
	if p := model.Deref(v.Password); p != "" && (utf8.RuneCountInString(p) < 8 || len(p) > 63) {
		return nil, nil, fmt.Errorf("%w: the Wi-Fi password has 8 to 63 characters", ErrInvalidChange)
	}
	cur := wifiOf(n, model.Deref(v.Previous))
	if cur == nil {
		cur = wifiOf(n, v.SSID)
	}
	if cur == nil {
		if model.Deref(v.Password) == "" {
			return nil, nil, fmt.Errorf("%w: a new Wi-Fi network needs a password", ErrInvalidChange)
		}
		return nil, v, nil
	}
	before, err := json.Marshal(cur)
	return before, v, err
}

func wifiDelete(n topology.Node, value any) (json.RawMessage, any, error) {
	var v WifiDelete
	raw, err := json.Marshal(value)
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		err = dec.Decode(&v)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("%w: wifi.delete takes {ssid}", ErrInvalidChange)
	}
	cur := wifiOf(n, v.SSID)
	if cur == nil {
		return nil, nil, fmt.Errorf("%w: %s has no Wi-Fi network %q", ErrInvalidChange, n.Label, v.SSID)
	}
	before, err := json.Marshal(cur)
	return before, v, err
}

// redacted is a change's value as the history and the logs keep it: a
// Wi-Fi password is never kept.
func redacted(value any) any {
	if v, ok := value.(model.WifiChange); ok && v.Password != nil {
		v.Password = nil
		return v
	}
	return value
}

// wifiTargets is one node per integration that can change Wi-Fi (a
// controller changes its whole site at once), the one that has the network
// first.
func wifiTargets(topo topology.Topology, kind model.ChangeKind, names ...string) []topology.Node {
	byIntegration := map[int64]topology.Node{}
	var order []int64
	for _, n := range topo.Nodes {
		if !slices.Contains(n.Changes, kind) {
			continue
		}
		n := n.Via(kind)
		has := slices.ContainsFunc(names, func(s string) bool { return wifiOf(n, s) != nil })
		cur, seen := byIntegration[n.IntegrationID]
		if !seen {
			order = append(order, n.IntegrationID)
		}
		if !seen || (has && !slices.ContainsFunc(names, func(s string) bool { return wifiOf(cur, s) != nil })) {
			byIntegration[n.IntegrationID] = n
		}
	}
	out := make([]topology.Node, 0, len(order))
	for _, id := range order {
		out = append(out, byIntegration[id])
	}
	return out
}

// SetWifi creates or changes a Wi-Fi network on every integration that can
// (each controller once, each standalone access point). One that refuses
// (an access point with no way to the VLAN) does not stop the others: the
// refusals are returned together, with the changes made.
func (c *Collector) SetWifi(ctx context.Context, w model.WifiChange) ([]store.ConfigChange, error) {
	targets := wifiTargets(c.State().Topology, model.ChangeKindWifiSet, w.SSID, model.Deref(w.Previous))
	if len(targets) == 0 {
		return nil, fmt.Errorf("%w: no access point Omini manages can change Wi-Fi", ErrNotChangeable)
	}
	var done []store.ConfigChange
	var failed []error
	for _, n := range targets {
		rec, err := c.Apply(ctx, ChangeRequest{NodeID: n.ID, Kind: model.ChangeKindWifiSet, Value: w})
		if rec.ID != 0 {
			done = append(done, rec)
		}
		if err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", n.Label, err))
		}
	}
	return done, errors.Join(failed...)
}

// DeleteWifi removes a Wi-Fi network from every integration that has it.
func (c *Collector) DeleteWifi(ctx context.Context, ssid string) ([]store.ConfigChange, error) {
	var done []store.ConfigChange
	for _, n := range wifiTargets(c.State().Topology, model.ChangeKindWifiDelete, ssid) {
		if wifiOf(n, ssid) == nil {
			continue
		}
		rec, err := c.Apply(ctx, ChangeRequest{NodeID: n.ID, Kind: model.ChangeKindWifiDelete, Value: WifiDelete{SSID: ssid}})
		if rec.ID != 0 {
			done = append(done, rec)
		}
		if err != nil {
			return done, err
		}
	}
	return done, nil
}
