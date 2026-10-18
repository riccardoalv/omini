package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
	"github.com/riccardoalv/omini/internal/vlans"
)

// A VLAN's network on the router (network.set, network.delete): its
// interface, gateway, DHCP, access and discovery.

// NetworkDelete is the value of network.delete.
type NetworkDelete struct {
	Vlan uint16 `json:"vlan"`
}

// networkOf is the network a router serves on a VLAN.
func networkOf(n topology.Node, id uint16) *model.Network {
	if n.Device == nil {
		return nil
	}
	for i := range n.Device.Networks {
		if n.Device.Networks[i].Vlan == id {
			return &n.Device.Networks[i]
		}
	}
	return nil
}

// networkSet checks a network.set value: a network that holds together.
// "custom" access leaves the router's rules as they are.
func networkSet(n topology.Node, value any) (json.RawMessage, any, error) {
	var v model.Network
	raw, err := json.Marshal(value)
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		err = dec.Decode(&v)
	}
	if err != nil || v.Vlan < 1 || v.Vlan > 4094 {
		return nil, nil, fmt.Errorf("%w: network.set takes a network {vlan, gateway, dhcp, access, mdns}", ErrInvalidChange)
	}
	if _, err := store.CheckNetwork(v); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrInvalidChange, err)
	}
	cur := networkOf(n, v.Vlan)
	if cur == nil {
		return nil, v, nil
	}
	before, err := json.Marshal(cur)
	return before, v, err
}

// networkDelete removes a VLAN's network from the router (never the LAN's).
func networkDelete(n topology.Node, value any) (json.RawMessage, any, error) {
	var v NetworkDelete
	raw, err := json.Marshal(value)
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		err = dec.Decode(&v)
	}
	if err != nil || v.Vlan < 1 || v.Vlan > 4094 {
		return nil, nil, fmt.Errorf("%w: network.delete takes {vlan}", ErrInvalidChange)
	}
	if v.Vlan == 1 {
		return nil, nil, fmt.Errorf("%w: the LAN's network cannot be deleted", ErrInvalidChange)
	}
	cur := networkOf(n, v.Vlan)
	if cur == nil {
		return nil, nil, fmt.Errorf("%w: %s serves no network on VLAN %d", ErrInvalidChange, n.Label, v.Vlan)
	}
	before, err := json.Marshal(cur)
	return before, v, err
}

// networkGuard is the protection a network change crosses: the network Omini
// is in loses it when its gateway or prefix changes, when it gets no internet
// or when it is deleted.
func networkGuard(n topology.Node, req ChangeRequest, self Self) (topology.Protection, bool) {
	var id uint16
	var after *model.Network
	switch v := req.Value.(type) {
	case model.Network:
		id, after = v.Vlan, &v
	case NetworkDelete:
		id = v.Vlan
	default:
		return topology.Protection{}, false
	}
	cur := networkOf(n, id)
	if cur == nil || !holdsOmini(*cur, self) {
		return topology.Protection{}, false
	}
	guard := topology.Protection{Reason: "omini"}
	if after == nil {
		return guard, true
	}
	if model.Deref(after.Gateway) != model.Deref(cur.Gateway) || model.Deref(after.Access) == model.NetworkAccessLocal {
		return guard, true
	}
	return topology.Protection{}, false
}

// holdsOmini reports whether one of Omini's addresses is in the network.
func holdsOmini(nw model.Network, self Self) bool {
	p, err := netip.ParsePrefix(model.Deref(nw.Gateway))
	if err != nil {
		return false
	}
	for _, ip := range self.IPs {
		if a, err := netip.ParseAddr(ip); err == nil && p.Masked().Contains(a) {
			return true
		}
	}
	return false
}

// routerFor is the router that serves (or can serve) a VLAN's network: the
// one that has it, else the first that can create it.
func routerFor(topo topology.Topology, id uint16) (topology.Node, bool) {
	var first *topology.Node
	for i, n := range topo.Nodes {
		if !slices.Contains(n.Changes, model.ChangeKindNetworkSet) {
			continue
		}
		if networkOf(n, id) != nil {
			return n, true
		}
		if first == nil {
			first = &topo.Nodes[i]
		}
	}
	if first == nil {
		return topology.Node{}, false
	}
	return *first, true
}

// routerPort is the router's port toward the switches: the first link down
// to a device.
func routerPort(topo topology.Topology, routerID string) string {
	kinds := map[string]topology.NodeKind{}
	for _, n := range topo.Nodes {
		kinds[n.ID] = n.Kind
	}
	for _, e := range topo.Edges {
		if e.Source == routerID && e.SourcePort != "" && e.Kind != topology.EdgeVPN &&
			(kinds[e.Target] == topology.KindDevice || kinds[e.Target] == topology.KindUnmanaged) {
			return e.SourcePort
		}
	}
	return ""
}

// sameNetwork reports whether the router already serves want.
func sameNetwork(cur, want model.Network) bool {
	dhcp := func(d *model.NetworkDhcp) model.NetworkDhcp {
		if d == nil || !d.Enabled {
			return model.NetworkDhcp{}
		}
		return model.NetworkDhcp{Enabled: true, From: d.From, To: d.To}
	}
	a, b := dhcp(cur.Dhcp), dhcp(want.Dhcp)
	return model.Deref(cur.Gateway) == model.Deref(want.Gateway) &&
		a.Enabled == b.Enabled && model.Deref(a.From) == model.Deref(b.From) && model.Deref(a.To) == model.Deref(b.To) &&
		(want.Access == nil || model.Deref(cur.Access) == model.Deref(want.Access)) &&
		model.Deref(cur.Mdns) == model.Deref(want.Mdns) &&
		(model.Deref(want.Name) == "" || model.Deref(cur.Name) == model.Deref(want.Name))
}

// networkStep is the router change that makes it serve a catalog VLAN's
// network, if any is needed.
func networkStep(topo topology.Topology, v store.Vlan) (ChangeRequest, bool) {
	if v.Network == nil {
		return ChangeRequest{}, false
	}
	r, ok := routerFor(topo, v.ID)
	if !ok {
		return ChangeRequest{}, false
	}
	want := vlans.NetworkFor(vlans.Entry{ID: v.ID, Name: v.Name, Network: v.Network}, routerPort(topo, r.ID))
	if v.ID == 1 {
		want.Parent = nil // the LAN rides on its own port
	}
	if cur := networkOf(r, v.ID); cur != nil {
		if sameNetwork(*cur, want) {
			return ChangeRequest{}, false
		}
		want.Parent = cur.Parent // an existing network stays on its port
		if want.Name == nil {
			want.Name = cur.Name
		}
	}
	return ChangeRequest{NodeID: r.ID, Kind: model.ChangeKindNetworkSet, Value: want}, true
}

// A fixed address on the router's DHCP (reservation.set, reservation.delete).

// ReservationDelete is the value of reservation.delete.
type ReservationDelete struct {
	MAC model.MACAddress `json:"mac"`
}

func reservationOf(n topology.Node, mac model.MACAddress) *model.DhcpReservation {
	if n.Device == nil {
		return nil
	}
	for i := range n.Device.DhcpReservations {
		if n.Device.DhcpReservations[i].MAC == mac {
			return &n.Device.DhcpReservations[i]
		}
	}
	return nil
}

// servesDHCP reports whether a router hands out the network's addresses:
// it says so, or it does not say and holds the device's lease.
func servesDHCP(r topology.Node, nw *model.Network, mac model.MACAddress) bool {
	if nw == nil {
		return false
	}
	if nw.Dhcp != nil {
		return nw.Dhcp.Enabled
	}
	return slices.ContainsFunc(r.Device.DhcpLeases, func(l model.DhcpLease) bool { return model.NormMAC(string(l.MAC)) == mac })
}

// networkHolding is the router's network an address is in (not its gateway,
// nor its first or last address).
func networkHolding(n topology.Node, ip netip.Addr) *model.Network {
	if n.Device == nil {
		return nil
	}
	for i, nw := range n.Device.Networks {
		p, err := netip.ParsePrefix(model.Deref(nw.Gateway))
		if err != nil || !p.Contains(ip) || ip == p.Addr() || ip == p.Masked().Addr() {
			continue
		}
		if ip != store.LastAddr(p) {
			return &n.Device.Networks[i]
		}
	}
	return nil
}

// reservationSet checks a reservation.set value: a MAC and an address of one
// of the router's networks.
func reservationSet(n topology.Node, value any) (json.RawMessage, any, error) {
	var v model.DhcpReservation
	raw, err := json.Marshal(value)
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		err = dec.Decode(&v)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("%w: reservation.set takes {mac, ip, name}", ErrInvalidChange)
	}
	v.MAC = model.NormMAC(string(v.MAC))
	ip, err := netip.ParseAddr(v.IP)
	if v.MAC == "" || err != nil || !ip.Is4() {
		return nil, nil, fmt.Errorf("%w: a reservation is a MAC and an IPv4 address", ErrInvalidChange)
	}
	nw := networkHolding(n, ip)
	if nw == nil {
		return nil, nil, fmt.Errorf("%w: %s is in none of %s's networks", ErrInvalidChange, v.IP, n.Label)
	}
	if !servesDHCP(n, nw, v.MAC) {
		return nil, nil, fmt.Errorf("%w: %s does not serve DHCP on the network of %s", ErrInvalidChange, n.Label, v.IP)
	}
	for _, r := range n.Device.DhcpReservations {
		if r.IP == v.IP && r.MAC != v.MAC {
			return nil, nil, fmt.Errorf("%w: %s is already reserved for %s", ErrInvalidChange, v.IP, r.MAC)
		}
	}
	cur := reservationOf(n, v.MAC)
	if cur == nil {
		return nil, v, nil
	}
	before, err := json.Marshal(cur)
	return before, v, err
}

func reservationDelete(n topology.Node, value any) (json.RawMessage, any, error) {
	var v ReservationDelete
	raw, err := json.Marshal(value)
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		err = dec.Decode(&v)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("%w: reservation.delete takes {mac}", ErrInvalidChange)
	}
	v.MAC = model.NormMAC(string(v.MAC))
	cur := reservationOf(n, v.MAC)
	if cur == nil {
		return nil, nil, fmt.Errorf("%w: %s has no reservation for %s", ErrInvalidChange, n.Label, v.MAC)
	}
	before, err := json.Marshal(cur)
	return before, v, err
}

// Reserve fixes a device's address on the router whose network holds it
// and serves DHCP there (a reservation on a router that does not hand out
// the addresses does nothing).
func (c *Collector) Reserve(ctx context.Context, nodeID, ip string) (store.ConfigChange, error) {
	topo := c.State().Topology
	n, ok := c.node(nodeID)
	addr, err := netip.ParseAddr(ip)
	if !ok || n.MAC == "" || err != nil {
		return store.ConfigChange{}, fmt.Errorf("%w: a reservation needs a device with a MAC and an IPv4 address", ErrInvalidChange)
	}
	// Another address for Omini's own host moves Omini at its next lease
	// renewal (its address in the browser stops working): destructive.
	if c.opts.Self.Owns(n) && addr.String() != n.IP && !c.AllowDestructive(ctx) {
		return store.ConfigChange{}, fmt.Errorf("%w: %s would move Omini's own host at its next lease renewal (allow destructive changes in Settings to do it)", ErrInvalidChange, ip)
	}
	for _, r := range topo.Nodes {
		if slices.Contains(r.Changes, model.ChangeKindReservationSet) && servesDHCP(r, networkHolding(r, addr), model.NormMAC(n.MAC)) {
			name := n.Label
			rec, err := c.Apply(ctx, ChangeRequest{
				NodeID: r.ID, Kind: model.ChangeKindReservationSet, Target: nodeID,
				Value: model.DhcpReservation{MAC: model.NormMAC(n.MAC), IP: ip, Name: &name},
			})
			if err == nil {
				c.syncNameOf(ctx, nodeID)
			}
			return rec, err
		}
	}
	return store.ConfigChange{}, fmt.Errorf("%w: no router Omini manages serves %s", ErrNotChangeable, ip)
}

// Unreserve removes a device's fixed address from the router that has it.
func (c *Collector) Unreserve(ctx context.Context, nodeID string) (store.ConfigChange, error) {
	n, ok := c.node(nodeID)
	if !ok || n.MAC == "" {
		return store.ConfigChange{}, ErrNotChangeable
	}
	mac := model.NormMAC(n.MAC)
	for _, r := range c.State().Topology.Nodes {
		if reservationOf(r, mac) != nil && slices.Contains(r.Changes, model.ChangeKindReservationDelete) {
			rec, err := c.Apply(ctx, ChangeRequest{NodeID: r.ID, Kind: model.ChangeKindReservationDelete, Target: nodeID, Value: ReservationDelete{MAC: mac}})
			if err == nil {
				c.syncNameOf(ctx, nodeID)
			}
			return rec, err
		}
	}
	return store.ConfigChange{}, fmt.Errorf("%w: no router Omini manages has a reservation for it", ErrNotChangeable)
}
