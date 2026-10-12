package snmp

import (
	"net"
	"slices"
	"strconv"

	"github.com/riccardoalv/omini/internal/model"
)

// Spanning tree (BRIDGE-MIB) and link aggregation (IEEE8023-LAG-MIB), read
// only: each port's STP state, the root bridge the switch sees, and the LAG
// each port belongs to.
const (
	oidStpDesignatedRoot = "1.3.6.1.2.1.17.2.5.0"            // dot1dStpDesignatedRoot: 8 bytes, priority + MAC
	oidStpPortState      = "1.3.6.1.2.1.17.2.15.1.3"         // dot1dStpPortState; index: bridge port
	oidLagAttachedAggID  = "1.2.840.10006.300.43.1.2.1.1.13" // dot3adAggPortAttachedAggID; index: ifIndex
)

// stpStates maps dot1dStpPortState (broken(6) is a port the bridge took out).
var stpStates = map[int64]model.PortStpState{
	1: model.PortStpStateDisabled, 2: model.PortStpStateBlocking, 3: model.PortStpStateListening,
	4: model.PortStpStateLearning, 5: model.PortStpStateForwarding, 6: model.PortStpStateDisabled,
}

// BridgeID is a spanning tree bridge ID as "<priority>.<mac>".
func BridgeID(b []byte) string {
	if len(b) != 8 {
		return ""
	}
	prio := int(b[0])<<8 | int(b[1])
	return strconv.Itoa(prio) + "." + net.HardwareAddr(b[2:]).String()
}

// collectStp reads each bridge port's state and the root the switch sees.
// A switch whose ports all report disabled (STP off) reports nothing.
func collectStp(c *client, d *model.Device, ifNames map[string]string) error {
	states, err := c.walk(oidStpPortState)
	if err != nil || len(states) == 0 {
		return err
	}
	byBridgePort := map[int]string{}
	for ifIdx, bp := range bridgePortOf(c) {
		byBridgePort[bp] = ifNames[ifIdx]
	}
	found := map[string]model.PortStpState{}
	running := false
	for idx, p := range states {
		bp, err := strconv.Atoi(idx)
		v, ok := pduInt(p)
		state, known := stpStates[v]
		if err != nil || !ok || !known {
			continue
		}
		name := byBridgePort[bp]
		if name == "" && len(byBridgePort) == 0 {
			name = ifNames[idx]
		}
		if name == "" {
			continue
		}
		found[name] = state
		running = running || state != model.PortStpStateDisabled
	}
	if !running {
		return nil
	}
	for i := range d.Interfaces {
		if s, ok := found[d.Interfaces[i].Name]; ok {
			d.Interfaces[i].Stp = &model.PortStp{State: &s}
		}
	}
	if root, err := c.get(oidStpDesignatedRoot); err == nil {
		if id := BridgeID(pduBytes(root[oidStpDesignatedRoot])); id != "" {
			d.StpRoot = &id
		}
	}
	return nil
}

// collectLag marks each LAG member with the name of its aggregate.
func collectLag(c *client, d *model.Device, ifNames map[string]string) error {
	rows, err := c.walk(oidLagAttachedAggID)
	if err != nil {
		return err
	}
	for ifIdx, p := range rows {
		agg, ok := pduInt(p)
		if !ok || agg == 0 || strconv.FormatInt(agg, 10) == ifIdx {
			continue // not in an aggregate (or the aggregate itself)
		}
		member, lag := ifNames[ifIdx], ifNames[strconv.FormatInt(agg, 10)]
		if member == "" || lag == "" {
			continue
		}
		i := slices.IndexFunc(d.Interfaces, func(x model.Interface) bool { return x.Name == member })
		if i < 0 {
			continue
		}
		d.Interfaces[i].Lag = &lag
		if j := slices.IndexFunc(d.Interfaces, func(x model.Interface) bool { return x.Name == lag }); j >= 0 {
			d.Interfaces[j].Type = model.Ptr(model.InterfaceTypeLag)
			if !slices.Contains(d.Interfaces[j].Members, member) {
				d.Interfaces[j].Members = append(d.Interfaces[j].Members, member)
				slices.Sort(d.Interfaces[j].Members)
			}
		}
	}
	return nil
}
