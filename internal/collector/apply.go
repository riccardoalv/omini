package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

// AllowDestructiveSetting is the settings key of "Allow destructive changes":
// "1" lets a change that cuts Omini or other gear off go through, after a
// second confirmation.
const AllowDestructiveSetting = "allow_destructive_changes"

// ChangeRequest is a configuration change asked for in the UI.
type ChangeRequest struct {
	NodeID string
	Kind   model.ChangeKind
	Port   string
	Value  any
	// Target is the device the change is for when another node makes it (a
	// reservation or a pause on the router, a name on the DNS server): the
	// change shows in its history too.
	Target string
	// Confirmed: the user confirmed a destructive change a second time.
	Confirmed bool
}

var (
	// ErrNotChangeable: the node is not a device whose integration applies this change.
	ErrNotChangeable = errors.New("this device cannot apply that change")
	// ErrInvalidChange: a port that does not exist, a value of the wrong type.
	ErrInvalidChange = errors.New("invalid change")
)

// DestructiveError is returned for a destructive change that is not allowed
// (Allowed false) or not confirmed a second time yet (Allowed true).
type DestructiveError struct {
	topology.Protection
	Allowed bool
}

func (e *DestructiveError) Error() string {
	if e.Allowed {
		return "destructive change: confirm it again"
	}
	return "destructive changes are not allowed (Settings)"
}

// AllowDestructive reports whether destructive changes are allowed.
func (c *Collector) AllowDestructive(ctx context.Context) bool {
	v, ok, err := c.store.GetSetting(ctx, AllowDestructiveSetting)
	return err == nil && ok && v == "1"
}

// SetAllowDestructive turns "Allow destructive changes" on or off.
func (c *Collector) SetAllowDestructive(ctx context.Context, on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	return c.store.SetSetting(ctx, AllowDestructiveSetting, v)
}

// Apply applies a configuration change through the node's integration, which
// applies, verifies and saves it, and puts the device as read after the change
// on the map at once. Every attempt is kept in the history; the returned
// record carries the error of one the device refused.
func (c *Collector) Apply(ctx context.Context, req ChangeRequest) (store.ConfigChange, error) {
	n, ok := c.node(req.NodeID)
	if !ok || n.Device == nil || n.IntegrationID == 0 || !slices.Contains(n.Changes, req.Kind) {
		return store.ConfigChange{}, ErrNotChangeable
	}
	// Through the integration that makes this change (and its report).
	n = n.Via(req.Kind)
	// Waits for this integration only (a collection of it in progress), not
	// for the whole round.
	busy := c.lockIntegration(n.IntegrationID)
	defer busy.Unlock()
	before, value, err := checkChange(n, req)
	if err != nil {
		return store.ConfigChange{}, err
	}
	req.Value = value
	// Disabling a protected port, or changing its VLANs (other than adding a
	// tagged one), is destructive; so is cutting the network Omini is in.
	p, guarded := n.Protected[req.Port]
	guarded = guarded && destructive(req, before)
	if req.Kind == model.ChangeKindNetworkSet || req.Kind == model.ChangeKindNetworkDelete {
		p, guarded = networkGuard(n, req, c.opts.Self)
	}
	if guarded {
		if allowed := c.AllowDestructive(ctx); !allowed || !req.Confirmed {
			return store.ConfigChange{}, &DestructiveError{Protection: p, Allowed: allowed}
		}
	}

	in, err := c.store.GetIntegration(ctx, n.IntegrationID)
	if err != nil {
		return store.ConfigChange{}, err
	}
	change := model.Change{Device: n.Device.Key, Kind: req.Kind, Value: req.Value}
	if req.Port != "" {
		change.Port = &req.Port
	}
	devices, applyErr := c.applyWith(ctx, in, change)

	after, _ := json.Marshal(redacted(req.Value))
	rec := store.ConfigChange{
		NodeID: n.ID, IntegrationID: n.IntegrationID, Kind: string(req.Kind), Port: req.Port,
		Before: before, After: after, At: c.opts.Now(),
	}
	if req.Target != n.ID {
		rec.TargetID = req.Target
	}
	if applyErr != nil {
		rec.Error = applyErr.Error()
		slog.Warn("configuration change failed", "node", n.ID, "kind", req.Kind, "port", req.Port, "err", applyErr)
	} else {
		slog.Info("configuration changed", "node", n.ID, "kind", req.Kind, "port", req.Port, "value", string(after))
	}
	if rec.ID, err = c.store.AddConfigChange(ctx, rec); err != nil {
		return rec, err
	}
	if applyErr != nil {
		return rec, applyErr
	}
	c.replaceDevices(ctx, n.IntegrationID, devices)
	if req.Kind != model.ChangeKindWolSend {
		c.markBackupDue(n.IntegrationID)
	}
	return rec, c.rebuild(ctx)
}

func (c *Collector) node(id string) (topology.Node, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, n := range c.state.Topology.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return topology.Node{}, false
}

// checkChange validates a change against the device, and returns the value it
// replaces as JSON and the value to send (normalized).
func checkChange(n topology.Node, req ChangeRequest) (json.RawMessage, any, error) {
	switch req.Kind {
	case model.ChangeKindVlanSet:
		return vlanSet(n, req.Value)
	case model.ChangeKindVlanDelete:
		return vlanDelete(n, req.Value)
	case model.ChangeKindNetworkSet:
		return networkSet(n, req.Value)
	case model.ChangeKindNetworkDelete:
		return networkDelete(n, req.Value)
	case model.ChangeKindReservationSet:
		return reservationSet(n, req.Value)
	case model.ChangeKindReservationDelete:
		return reservationDelete(n, req.Value)
	case model.ChangeKindWifiSet:
		return wifiSet(n, req.Value)
	case model.ChangeKindWifiDelete:
		return wifiDelete(n, req.Value)
	case model.ChangeKindDnsSet:
		return dnsSet(n, req.Value)
	case model.ChangeKindDnsDelete:
		return dnsDelete(n, req.Value)
	case model.ChangeKindWolSend:
		return wolSend(n, req.Value)
	case model.ChangeKindPauseSet:
		return pauseSet(n, req.Value)
	case model.ChangeKindPauseDelete:
		return pauseDelete(n, req.Value)
	}
	i := slices.IndexFunc(n.Device.Interfaces, func(i model.Interface) bool { return i.Name == req.Port })
	if i < 0 {
		return nil, nil, fmt.Errorf("%w: %s has no port %q", ErrInvalidChange, n.Label, req.Port)
	}
	port := n.Device.Interfaces[i]
	switch req.Kind {
	case model.ChangeKindPortEnabled:
		if _, ok := req.Value.(bool); !ok {
			return nil, nil, fmt.Errorf("%w: %s takes true or false", ErrInvalidChange, req.Kind)
		}
		if port.Enabled == nil {
			return nil, nil, fmt.Errorf("%w: %s does not report whether %s is enabled", ErrInvalidChange, n.Label, req.Port)
		}
		before, err := json.Marshal(*port.Enabled)
		return before, req.Value, err
	case model.ChangeKindPortVlans:
		if port.Vlans == nil {
			return nil, nil, fmt.Errorf("%w: %s does not report the VLANs of %s", ErrInvalidChange, n.Label, req.Port)
		}
		v, err := portVlans(n.Device, req.Value)
		if err != nil {
			return nil, nil, err
		}
		before, err := json.Marshal(port.Vlans)
		return before, v, err
	}
	return nil, nil, fmt.Errorf("%w: unknown change %q", ErrInvalidChange, req.Kind)
}

// portVlans reads a port.vlans value: only VLANs the device has, the
// untagged (native) one not also tagged, at least one VLAN; tagged sorted.
func portVlans(d *model.Device, value any) (model.PortVlans, error) {
	var v model.PortVlans
	raw, err := json.Marshal(value)
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		err = dec.Decode(&v)
	}
	if err != nil {
		return v, fmt.Errorf("%w: port.vlans takes {untagged, tagged}", ErrInvalidChange)
	}
	// A device that keeps no list of VLANs (a VM's virtual NIC) takes any tag.
	has := func(id uint16) bool {
		return len(d.Vlans) == 0 || slices.ContainsFunc(d.Vlans, func(x model.Vlan) bool { return x.ID == id })
	}
	if v.Untagged == nil && len(v.Tagged) == 0 {
		return v, fmt.Errorf("%w: a port needs at least one VLAN", ErrInvalidChange)
	}
	if v.Untagged != nil && !has(*v.Untagged) {
		return v, fmt.Errorf("%w: the device has no VLAN %d", ErrInvalidChange, *v.Untagged)
	}
	slices.Sort(v.Tagged)
	v.Tagged = slices.Compact(v.Tagged)
	for _, id := range v.Tagged {
		if !has(id) {
			return v, fmt.Errorf("%w: the device has no VLAN %d", ErrInvalidChange, id)
		}
		if v.Untagged != nil && id == *v.Untagged {
			return v, fmt.Errorf("%w: VLAN %d cannot be both untagged and tagged", ErrInvalidChange, id)
		}
	}
	return v, nil
}

func (c *Collector) applyWith(ctx context.Context, in store.Integration, change model.Change) (devices []model.Device, err error) {
	impl, err := c.reg.Get(in.Type)
	if err != nil {
		return nil, err
	}
	applier, ok := impl.(integration.Applier)
	if !ok {
		return nil, ErrNotChangeable
	}
	cfg, err := integration.OpenSecrets(c.box, impl.Info().Fields, in.Config)
	if err != nil {
		return nil, err
	}
	limit := c.opts.Timeout
	if t, ok := impl.(interface{ Timeout() time.Duration }); ok && t.Timeout() > limit {
		limit = t.Timeout()
	}
	ctx, cancel := context.WithTimeout(integration.WithInstance(ctx, in.ID), limit)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			devices, err = nil, fmt.Errorf("integration panicked: %v", r)
		}
	}()
	return applier.Apply(ctx, cfg, change)
}

// replaceDevices puts the devices read after a change in the integration's
// snapshot, in place of those with the same key (a controller's other devices
// keep their data until the next round).
func (c *Collector) replaceDevices(ctx context.Context, id int64, devices []model.Device) {
	c.mu.Lock()
	snap := c.snaps[id]
	merged := slices.Clone(snap.Devices)
	for _, d := range devices {
		if i := slices.IndexFunc(merged, func(o model.Device) bool { return o.Key == d.Key }); i >= 0 {
			merged[i] = d
		} else {
			merged = append(merged, d)
		}
	}
	snap.IntegrationID, snap.Devices, snap.OK, snap.Error = id, merged, true, ""
	c.snaps[id] = snap
	c.applied[id] = c.opts.Now()
	c.mu.Unlock()
	if err := c.store.SaveSnapshot(ctx, snap); err != nil {
		slog.Error("save snapshot", "integration", id, "err", err)
	}
}

// markChangeable sets, on each device whose integration applies changes, the
// changes it can apply there and its protected ports. A port change needs the
// device to report its ports' state: a plugin that cannot change a kind of
// device (an access point) leaves it empty, and no button shows.
func (c *Collector) markChangeable(ctx context.Context, topo *topology.Topology) {
	list, err := c.store.ListIntegrations(ctx)
	if err != nil {
		return
	}
	changes := map[int64][]model.ChangeKind{}
	core := map[int64]bool{}
	for _, in := range list {
		if impl, err := c.reg.Get(in.Type); err == nil {
			if _, ok := impl.(integration.Applier); ok {
				changes[in.ID] = impl.Info().Changes
				core[in.ID] = impl.Info().Kind == integration.KindCore
			}
		}
	}
	protected := ProtectedPorts(*topo, c.opts.Self)
	for i := range topo.Nodes {
		n := &topo.Nodes[i]
		if n.Kind != topology.KindDevice || !n.Online || n.Device == nil {
			continue
		}
		// Every report of the device offers what its integration can change;
		// a plugin's goes before the network scan's for the same change.
		reports := slices.Clone(n.Reports)
		if len(reports) == 0 {
			reports = []topology.Report{{IntegrationID: n.IntegrationID, Device: n.Device}}
		}
		slices.SortStableFunc(reports, func(a, b topology.Report) int {
			switch {
			case core[a.IntegrationID] == core[b.IntegrationID]:
				return 0
			case core[a.IntegrationID]:
				return 1
			}
			return -1
		})
		// A device a plugin reads is that plugin's on the map (its data, its
		// integration), even when the network scan's SNMP read more of it.
		if core[n.IntegrationID] && len(reports) > 0 && !core[reports[0].IntegrationID] && reports[0].Device != nil {
			n.Device, n.IntegrationID = reports[0].Device, reports[0].IntegrationID
		}
		for _, r := range reports {
			for _, k := range changes[r.IntegrationID] {
				if r.Device == nil || !offers(r.Device, k) || slices.Contains(n.Changes, k) {
					continue
				}
				if n.ChangeVia == nil {
					n.ChangeVia = map[model.ChangeKind]topology.Report{}
				}
				n.Changes = append(n.Changes, k)
				n.ChangeVia[k] = r
			}
		}
		if len(n.Changes) > 0 {
			n.Protected = protected[n.ID]
		}
	}
}

// ProtectedPorts returns, per device, the ports whose disabling is
// destructive: those that would cut Omini off ("omini": where the device
// learned one of Omini's MACs, or that lead to Omini's node) and those that
// would cut the network off the internet ("internet": the device's uplink,
// where it learned a router's or firewall's MAC, or that lead to one). Taking
// a device off the network (an access point, a server, another switch) is an
// ordinary change. Omini's node is found by its MACs or addresses (self); when
// it is not on the map, only the internet ports are known.
func ProtectedPorts(topo topology.Topology, self Self) map[string]map[string]topology.Protection {
	byID := map[string]*topology.Node{}
	below := map[string][]topology.Edge{} // a node's downstream edges
	for i := range topo.Nodes {
		byID[topo.Nodes[i].ID] = &topo.Nodes[i]
	}
	for _, e := range topo.Edges {
		if e.Kind != topology.EdgeVPN {
			below[e.Source] = append(below[e.Source], e)
		}
	}
	omini := ""
	routers := map[string]bool{}
	var routerMACs []model.MACAddress
	for _, n := range topo.Nodes {
		if omini == "" && n.Kind != topology.KindApp && self.Owns(n) {
			omini = n.ID
		}
		if n.Kind == topology.KindDevice && isRouter(n.Role) {
			routers[n.ID] = true
			routerMACs = append(routerMACs, model.MACAddress(n.MAC))
			if n.Device != nil {
				routerMACs = append(routerMACs, n.Device.MACs...)
			}
		}
	}
	// reaches reports whether the subtree under a node holds one of want.
	var reaches func(id string, want func(string) bool, seen map[string]bool) bool
	reaches = func(id string, want func(string) bool, seen map[string]bool) bool {
		if seen[id] {
			return false
		}
		seen[id] = true
		if want(id) {
			return true
		}
		for _, e := range below[id] {
			if reaches(e.Target, want, seen) {
				return true
			}
		}
		return false
	}
	isOmini := func(id string) bool { return id == omini }
	isRouterNode := func(id string) bool { return routers[id] }

	out := map[string]map[string]topology.Protection{}
	set := func(node, port, reason, via string) {
		if port == "" {
			return
		}
		if out[node] == nil {
			out[node] = map[string]topology.Protection{}
		}
		if _, done := out[node][port]; !done { // the first reason wins: omini, then internet
			out[node][port] = topology.Protection{Reason: reason, Node: via}
		}
	}
	for _, n := range topo.Nodes {
		if n.Kind != topology.KindDevice || n.Device == nil {
			continue
		}
		for _, f := range n.Device.Fdb {
			if slices.Contains(self.MACs, f.MAC) {
				set(n.ID, f.Port, "omini", "")
			}
		}
		for _, e := range below[n.ID] {
			if omini != "" && reaches(e.Target, isOmini, map[string]bool{}) {
				set(n.ID, e.SourcePort, "omini", "")
			}
		}
		for _, e := range topo.Edges {
			if e.Target == n.ID && e.Kind != topology.EdgeVPN {
				if omini != "" && !reaches(n.ID, isOmini, map[string]bool{}) {
					set(n.ID, e.TargetPort, "omini", "") // Omini is upstream
				}
				set(n.ID, e.TargetPort, "internet", e.Source)
			}
		}
		for _, f := range n.Device.Fdb {
			if slices.Contains(routerMACs, f.MAC) && !routers[n.ID] {
				set(n.ID, f.Port, "internet", "")
			}
		}
		for _, e := range below[n.ID] {
			if reaches(e.Target, isRouterNode, map[string]bool{}) {
				set(n.ID, e.SourcePort, "internet", e.Target)
			}
		}
	}
	return out
}

func isRouter(role string) bool {
	return role == string(model.DeviceRoleRouter) || role == string(model.DeviceRoleFirewall)
}

// Self is the machine Omini runs on: its MACs and addresses.
type Self struct {
	MACs []model.MACAddress
	IPs  []string
}

// Owns reports whether a node is the machine Omini runs on.
func (s Self) Owns(n topology.Node) bool {
	if n.MAC != "" && slices.Contains(s.MACs, model.MACAddress(n.MAC)) {
		return true
	}
	return n.IP != "" && slices.Contains(s.IPs, n.IP)
}

// lockIntegration locks the integration's runs (see Collector.busy).
func (c *Collector) lockIntegration(id int64) *sync.Mutex {
	c.busyMu.Lock()
	m := c.busy[id]
	if m == nil {
		m = &sync.Mutex{}
		c.busy[id] = m
	}
	c.busyMu.Unlock()
	m.Lock()
	return m
}

// changedSince reports whether a change replaced the integration's devices
// after this collection of it started: its result is older than the map.
func (c *Collector) changedSince(s store.Snapshot) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.applied[s.IntegrationID]
	return ok && s.CollectedAt.Before(t)
}

// offers reports whether a device can be offered a change: its ports report
// what the change sets (their state, their VLANs), and for VLANs it has more
// than one to choose from.
func offers(d *model.Device, k model.ChangeKind) bool {
	switch k {
	case model.ChangeKindPortEnabled:
		return slices.ContainsFunc(d.Interfaces, func(i model.Interface) bool { return i.Enabled != nil })
	case model.ChangeKindPortVlans:
		return slices.ContainsFunc(d.Interfaces, func(i model.Interface) bool { return i.Vlans != nil })
	case model.ChangeKindVlanSet, model.ChangeKindVlanDelete:
		return len(d.Vlans) > 0
	case model.ChangeKindNetworkSet, model.ChangeKindNetworkDelete,
		model.ChangeKindReservationSet, model.ChangeKindReservationDelete:
		return len(d.Networks) > 0
	case model.ChangeKindWifiSet, model.ChangeKindWifiDelete:
		return len(d.WifiNetworks) > 0
	case model.ChangeKindDnsSet, model.ChangeKindDnsDelete:
		// A DNS server reports its names (an empty list too) or its domain.
		return d.DnsRecords != nil || d.DnsDomain != nil
	case model.ChangeKindWolSend, model.ChangeKindPauseSet, model.ChangeKindPauseDelete:
		return len(d.Networks) > 0
	}
	return false
}

// VlanValue is the value of vlan.set ({id, name}) and vlan.delete ({id}).
type VlanValue struct {
	ID   uint16 `json:"id"`
	Name string `json:"name,omitempty"`
}

func vlanValue(value any) (VlanValue, error) {
	var v VlanValue
	raw, err := json.Marshal(value)
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		err = dec.Decode(&v)
	}
	if err != nil || v.ID < 1 || v.ID > 4094 {
		return v, fmt.Errorf("%w: a VLAN is {id (1-4094), name}", ErrInvalidChange)
	}
	v.Name = strings.TrimSpace(v.Name)
	return v, nil
}

// vlanSet creates a VLAN on the device, or renames it.
func vlanSet(n topology.Node, value any) (json.RawMessage, any, error) {
	v, err := vlanValue(value)
	if err != nil {
		return nil, nil, err
	}
	i := slices.IndexFunc(n.Device.Vlans, func(x model.Vlan) bool { return x.ID == v.ID })
	if i < 0 {
		return nil, v, nil
	}
	before, err := json.Marshal(VlanValue{ID: v.ID, Name: model.Deref(n.Device.Vlans[i].Name)})
	return before, v, err
}

// vlanDelete deletes a VLAN no port of the device uses (never VLAN 1).
func vlanDelete(n topology.Node, value any) (json.RawMessage, any, error) {
	v, err := vlanValue(value)
	if err != nil {
		return nil, nil, err
	}
	if v.ID == 1 {
		return nil, nil, fmt.Errorf("%w: VLAN 1 cannot be deleted", ErrInvalidChange)
	}
	i := slices.IndexFunc(n.Device.Vlans, func(x model.Vlan) bool { return x.ID == v.ID })
	if i < 0 {
		return nil, nil, fmt.Errorf("%w: %s has no VLAN %d", ErrInvalidChange, n.Label, v.ID)
	}
	for _, p := range n.Device.Interfaces {
		if p.Vlans != nil && (model.Deref(p.Vlans.Untagged) == v.ID || slices.Contains(p.Vlans.Tagged, v.ID)) {
			return nil, nil, fmt.Errorf("%w: VLAN %d is in use on %s", ErrInvalidChange, v.ID, p.Name)
		}
	}
	before, err := json.Marshal(VlanValue{ID: v.ID, Name: model.Deref(n.Device.Vlans[i].Name)})
	return before, VlanValue{ID: v.ID}, err
}

// destructive reports whether a change cuts what goes through a protected
// port: disabling it, or changing its VLANs other than adding tagged ones.
func destructive(req ChangeRequest, before json.RawMessage) bool {
	switch req.Kind {
	case model.ChangeKindPortEnabled:
		return req.Value == false
	case model.ChangeKindPortVlans:
		var was model.PortVlans
		after, ok := req.Value.(model.PortVlans)
		if !ok || json.Unmarshal(before, &was) != nil {
			return true
		}
		if model.Deref(was.Untagged) != model.Deref(after.Untagged) {
			return true
		}
		for _, id := range was.Tagged {
			if !slices.Contains(after.Tagged, id) {
				return true
			}
		}
		return false
	}
	return false
}
