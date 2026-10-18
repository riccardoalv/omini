package snmp

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/gosnmp/gosnmp"

	"github.com/riccardoalv/omini/internal/model"
)

// Q-BRIDGE-MIB (IEEE 802.1Q): the VLANs of a switch and its ports'
// membership, read every round and changed by port.vlans, vlan.set and
// vlan.delete. A port list is a bitmap of bridge ports, the first port in
// the first byte's most significant bit.
const (
	oidVlanStatic   = "1.3.6.1.2.1.17.7.1.4.3.1"   // .1 name, .2 egress ports, .4 untagged ports, .5 row status; index: VLAN id
	oidPvid         = "1.3.6.1.2.1.17.7.1.4.5.1.1" // dot1qPvid; index: bridge port
	rowActive       = 1
	rowCreateAndGo  = 4
	rowCreateAndWai = 5
	rowDestroy      = 6
)

// staticVlan is one row of dot1qVlanStaticTable.
type staticVlan struct {
	id       uint16
	name     string
	egress   []byte
	untagged []byte
}

func hasPort(list []byte, port int) bool {
	i := (port - 1) / 8
	return port > 0 && i < len(list) && list[i]&(0x80>>((port-1)%8)) != 0
}

func withPort(list []byte, port int, on bool) []byte {
	i := (port - 1) / 8
	out := slices.Clone(list)
	for len(out) <= i {
		out = append(out, 0)
	}
	if on {
		out[i] |= 0x80 >> ((port - 1) % 8)
	} else {
		out[i] &^= 0x80 >> ((port - 1) % 8)
	}
	return out
}

func readStatic(c *client) (map[uint16]*staticVlan, error) {
	rows, err := c.walk(oidVlanStatic)
	if err != nil {
		return nil, err
	}
	out := map[uint16]*staticVlan{}
	for col, byIdx := range table(rows) {
		for idx, p := range byIdx {
			id, err := strconv.Atoi(idx)
			if err != nil || id < 1 || id > 4094 {
				continue
			}
			v := out[uint16(id)]
			if v == nil {
				v = &staticVlan{id: uint16(id)}
				out[uint16(id)] = v
			}
			switch col {
			case 1:
				v.name = pduString(p)
			case 2:
				v.egress = pduBytes(p)
			case 4:
				v.untagged = pduBytes(p)
			}
		}
	}
	return out, nil
}

// bridgePortOf maps ifIndex → bridge port (BRIDGE-MIB dot1dBasePortIfIndex;
// a bridge port is its ifIndex when the table is missing).
func bridgePortOf(c *client) map[string]int {
	out := map[string]int{}
	if rows, err := c.walk(oidBasePortIfIndex); err == nil {
		for port, p := range rows {
			bp, err := strconv.Atoi(port)
			if v, ok := pduInt(p); ok && err == nil {
				out[strconv.FormatInt(v, 10)] = bp
			}
		}
	}
	return out
}

// collectVlans reads the switch's VLANs and each port's membership: its
// native VLAN (PVID) and the VLANs it carries tagged.
func collectVlans(c *client, d *model.Device, ifNames map[string]string) error {
	static, err := readStatic(c)
	if err != nil || len(static) == 0 {
		return err
	}
	ids := make([]uint16, 0, len(static))
	for id := range static {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if slices.ContainsFunc(d.Vlans, func(v model.Vlan) bool { return v.ID == id }) {
			continue
		}
		d.Vlans = append(d.Vlans, model.Vlan{ID: id, Name: nonEmpty(static[id].name)})
	}
	pvids, _ := c.walk(oidPvid)
	ports := bridgePortOf(c)
	for ifIdx, name := range ifNames {
		bp, ok := ports[ifIdx]
		if !ok {
			if len(ports) > 0 {
				continue // not a bridge port
			}
			bp, _ = strconv.Atoi(ifIdx)
		}
		i := slices.IndexFunc(d.Interfaces, func(x model.Interface) bool { return x.Name == name })
		if i < 0 || bp < 1 {
			continue
		}
		pv := model.PortVlans{}
		var pvid uint16
		if v, ok := pduInt(pvids[strconv.Itoa(bp)]); ok && v >= 1 && v <= 4094 {
			pvid = uint16(v)
		}
		member := false
		var firstUntagged uint16
		for _, id := range ids {
			v := static[id]
			if !hasPort(v.egress, bp) {
				continue
			}
			member = true
			if hasPort(v.untagged, bp) {
				if firstUntagged == 0 {
					firstUntagged = id
				}
				continue
			}
			pv.Tagged = append(pv.Tagged, id)
		}
		// The native VLAN is the PVID when the port is untagged in it. A port
		// untagged nowhere is a trunk without one: Q-BRIDGE always keeps a
		// PVID, which then is not a native VLAN. A port the static table does
		// not list at all keeps its PVID.
		switch {
		case !member && pvid != 0:
			pv.Untagged = model.Ptr(pvid)
		case pvid != 0 && static[pvid] != nil && hasPort(static[pvid].untagged, bp):
			pv.Untagged = model.Ptr(pvid)
		case firstUntagged != 0:
			pv.Untagged = model.Ptr(firstUntagged)
		}
		if member || pv.Untagged != nil {
			d.Interfaces[i].Vlans = &pv
		}
	}
	return nil
}

// set sends one SET and maps the agent's refusals.
func (c *client) set(pdus ...gosnmp.SnmpPDU) error {
	res, err := c.g.Set(pdus)
	if err != nil {
		return err
	}
	switch res.Error {
	case gosnmp.NoError:
		return nil
	case gosnmp.NoAccess, gosnmp.AuthorizationError, gosnmp.NotWritable, gosnmp.ReadOnly, gosnmp.NoSuchName:
		return ErrNoWrite
	default:
		return fmt.Errorf("the SNMP agent refused the change: %s", res.Error)
	}
}

func portList(oid string, list []byte) gosnmp.SnmpPDU {
	return gosnmp.SnmpPDU{Name: oid, Type: gosnmp.OctetString, Value: list}
}

// SetPortVlans puts a port in VLANs (Q-BRIDGE-MIB): each VLAN's egress and
// untagged lists, and the port's PVID. VLANs the port joins are set first,
// then its PVID, then the ones it leaves, so it is never left with none; the
// tables and the PVID are read back to check. Without a native VLAN
// (want.Untagged nil) the port leaves every untagged membership and its PVID
// is left as it is: Q-BRIDGE has no "no PVID", and the read reports a port
// untagged nowhere as having no native VLAN.
func SetPortVlans(ctx context.Context, t Target, port string, want model.PortVlans) error {
	c, err := t.dial(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	idx, err := portIndex(c, port)
	if err != nil {
		return err
	}
	// Like collectVlans: a port missing from dot1dBasePortIfIndex is not a
	// bridge port; only an agent without the table numbers them as ifIndex.
	ports := bridgePortOf(c)
	bp, ok := ports[idx]
	if !ok {
		if len(ports) > 0 {
			return fmt.Errorf("%s is not a bridge port of the switch: its VLANs cannot be changed", port)
		}
		bp, _ = strconv.Atoi(idx)
	}
	static, err := readStatic(c)
	if err != nil {
		return err
	}
	native := model.Deref(want.Untagged)
	in := func(id uint16) (egress, untagged bool) {
		if id == native {
			return true, true
		}
		return slices.Contains(want.Tagged, id), false
	}
	var joins, leaves []uint16
	for id, v := range static {
		e, u := in(id)
		if hasPort(v.egress, bp) == e && hasPort(v.untagged, bp) == u {
			continue
		}
		if e {
			joins = append(joins, id)
		} else {
			leaves = append(leaves, id)
		}
	}
	for _, id := range append(slices.Clone(want.Tagged), native) {
		if _, ok := static[id]; id != 0 && !ok {
			return fmt.Errorf("the switch has no VLAN %d", id)
		}
	}
	slices.Sort(joins)
	slices.Sort(leaves)
	apply := func(id uint16) error {
		v := static[id]
		e, u := in(id)
		egress, untagged := withPort(v.egress, bp, e), withPort(v.untagged, bp, u)
		// Egress first: an untagged port must already be a member.
		if e {
			if err := c.set(portList(fmt.Sprintf("%s.2.%d", oidVlanStatic, id), egress)); err != nil {
				return err
			}
			return c.set(portList(fmt.Sprintf("%s.4.%d", oidVlanStatic, id), untagged))
		}
		if err := c.set(portList(fmt.Sprintf("%s.4.%d", oidVlanStatic, id), untagged)); err != nil {
			return err
		}
		return c.set(portList(fmt.Sprintf("%s.2.%d", oidVlanStatic, id), egress))
	}
	for _, id := range joins {
		if err := apply(id); err != nil {
			return err
		}
	}
	if native != 0 {
		if err := c.set(gosnmp.SnmpPDU{Name: fmt.Sprintf("%s.%d", oidPvid, bp), Type: gosnmp.Gauge32, Value: uint32(native)}); err != nil {
			return err
		}
	}
	for _, id := range leaves {
		if err := apply(id); err != nil {
			return err
		}
	}
	after, err := readStatic(c)
	if err != nil {
		return err
	}
	for id, v := range after {
		e, u := in(id)
		if hasPort(v.egress, bp) != e || hasPort(v.untagged, bp) != u {
			return fmt.Errorf("the agent did not apply the VLANs of %s (VLAN %d)", port, id)
		}
	}
	if native != 0 {
		oid := fmt.Sprintf("%s.%d", oidPvid, bp)
		got, err := c.get(oid)
		if err != nil {
			return err
		}
		if v, ok := pduInt(got[oid]); !ok || v != int64(native) {
			return fmt.Errorf("the agent did not apply the native VLAN of %s (PVID %d)", port, native)
		}
	}
	return nil
}

// SetVlan creates a VLAN (dot1qVlanStaticRowStatus createAndGo, else
// createAndWait then active) or renames it.
func SetVlan(ctx context.Context, t Target, id uint16, name string) error {
	c, err := t.dial(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	static, err := readStatic(c)
	if err != nil {
		return err
	}
	nameOID := fmt.Sprintf("%s.1.%d", oidVlanStatic, id)
	status := fmt.Sprintf("%s.5.%d", oidVlanStatic, id)
	named := gosnmp.SnmpPDU{Name: nameOID, Type: gosnmp.OctetString, Value: []byte(name)}
	if _, ok := static[id]; ok {
		if err := c.set(named); err != nil {
			return err
		}
	} else {
		err := c.set(gosnmp.SnmpPDU{Name: status, Type: gosnmp.Integer, Value: rowCreateAndGo}, named)
		if err != nil {
			if err := c.set(gosnmp.SnmpPDU{Name: status, Type: gosnmp.Integer, Value: rowCreateAndWai}); err != nil {
				return err
			}
			if err := c.set(named); err != nil {
				return err
			}
			if err := c.set(gosnmp.SnmpPDU{Name: status, Type: gosnmp.Integer, Value: rowActive}); err != nil {
				return err
			}
		}
	}
	after, err := readStatic(c)
	if err != nil {
		return err
	}
	if v, ok := after[id]; !ok || (name != "" && v.name != name) {
		return fmt.Errorf("the agent did not create or rename VLAN %d", id)
	}
	return nil
}

// DeleteVlan destroys a VLAN's row (dot1qVlanStaticRowStatus destroy).
func DeleteVlan(ctx context.Context, t Target, id uint16) error {
	c, err := t.dial(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	if err := c.set(gosnmp.SnmpPDU{Name: fmt.Sprintf("%s.5.%d", oidVlanStatic, id), Type: gosnmp.Integer, Value: rowDestroy}); err != nil {
		return err
	}
	after, err := readStatic(c)
	if err != nil {
		return err
	}
	if _, ok := after[id]; ok {
		return fmt.Errorf("the agent did not delete VLAN %d", id)
	}
	return nil
}
