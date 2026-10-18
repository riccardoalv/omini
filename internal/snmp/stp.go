package snmp

import (
	"bytes"
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
	oidStpRootPort       = "1.3.6.1.2.1.17.2.7.0"            // dot1dStpRootPort: the bridge port towards the root; 0 on the root
	oidStpPortState      = "1.3.6.1.2.1.17.2.15.1.3"         // dot1dStpPortState; index: bridge port
	oidStpPortDesigBr    = "1.3.6.1.2.1.17.2.15.1.8"         // dot1dStpPortDesignatedBridge: 8 bytes, priority + MAC
	oidStpPortDesigPort  = "1.3.6.1.2.1.17.2.15.1.9"         // dot1dStpPortDesignatedPort: 2 bytes, priority + port
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

// collectStp reads each bridge port's state and role, and the root the switch
// sees.
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
	portOf := map[string]int{} // name -> bridge port
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
		found[name], portOf[name] = state, bp
		running = running || state != model.PortStpStateDisabled
	}
	if !running {
		return nil
	}
	roles := stpRoles(c, found, portOf)
	for i := range d.Interfaces {
		name := d.Interfaces[i].Name
		if s, ok := found[name]; ok {
			d.Interfaces[i].Stp = &model.PortStp{State: &s}
			if r, ok := roles[name]; ok {
				d.Interfaces[i].Stp.Role = &r
			}
		}
	}
	if root, err := c.get(oidStpDesignatedRoot); err == nil {
		if id := BridgeID(pduBytes(root[oidStpDesignatedRoot])); id != "" {
			d.StpRoot = &id
		}
	}
	return nil
}

// stpRoles tells each port's role (BRIDGE-MIB has no role column): disabled
// when its state is; root when it is dot1dStpRootPort; designated when the
// designated bridge of its segment is this switch; else, while blocking,
// backup (this switch is designated there through another port) or
// alternate (another bridge is). Bridges are compared by MAC: the priority
// part carries the VLAN (PVST, MSTP's system id extension) and may differ
// from dot1dStpPriority. Without the switch's bridge address only disabled
// and root are told; a port that fits none has no role.
func stpRoles(c *client, states map[string]model.PortStpState, portOf map[string]int) map[string]model.PortStpRole {
	var rootPort int64 = -1
	var self net.HardwareAddr
	if r, err := c.get(oidStpRootPort, oidBaseBridgeAddress); err == nil {
		if v, ok := pduInt(r[oidStpRootPort]); ok {
			rootPort = v
		}
		if b := pduBytes(r[oidBaseBridgeAddress]); len(b) == 6 {
			self = b
		}
	}
	desigBridge, _ := c.walk(oidStpPortDesigBr) // optional
	desigPort, _ := c.walk(oidStpPortDesigPort) // optional
	out := map[string]model.PortStpRole{}
	for name, state := range states {
		bp := portOf[name]
		idx := strconv.Itoa(bp)
		bridge := pduBytes(desigBridge[idx])
		switch {
		case state == model.PortStpStateDisabled:
			out[name] = model.PortStpRoleDisabled
		case rootPort > 0 && int64(bp) == rootPort:
			out[name] = model.PortStpRoleRoot
		case self == nil || len(bridge) != 8:
			// unknown
		case bytes.Equal(bridge[2:], self):
			p := pduBytes(desigPort[idx])
			other := len(p) == 2 && (int(p[0])<<8|int(p[1]))&0x0fff != bp
			switch {
			case state != model.PortStpStateBlocking:
				out[name] = model.PortStpRoleDesignated
			case other:
				out[name] = model.PortStpRoleBackup
			}
		case state == model.PortStpStateBlocking:
			out[name] = model.PortStpRoleAlternate
		}
	}
	return out
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
