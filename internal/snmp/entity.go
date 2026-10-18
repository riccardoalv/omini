package snmp

import (
	"slices"
	"strconv"
	"strings"

	"github.com/riccardoalv/omini/internal/model"
)

// ENTITY-MIB (RFC 6933): the physical parts of a device (chassis, modules,
// ports, sensors), what they are and their serial numbers.
const (
	oidEntPhysical        = "1.3.6.1.2.1.47.1.1.1.1"   // entPhysicalEntry; index: entPhysicalIndex
	oidEntAliasMappingID  = "1.3.6.1.2.1.47.1.3.2.1.2" // entAliasMappingIdentifier; index: physical.logical
	oidIfIndexColumn      = "1.3.6.1.2.1.2.2.1.1"      // ifIndex: what an alias of a port points to
	entClassChassis       = 3
	entClassModule        = 9
	entClassPort          = 10
	entColDescr           = 2
	entColContainedIn     = 4
	entColClass           = 5
	entColName            = 7
	entColFirmwareRev     = 9
	entColSoftwareRev     = 10
	entColSerialNum       = 11
	entColMfgName         = 12
	entColModelName       = 13
	maxEntityAncestorHops = 16 // a containment loop must not hang the read
)

// entity is one row of entPhysicalTable.
type entity struct {
	index       int
	descr, name string
	class       int64
	parent      int // entPhysicalContainedIn, 0 at the top
	firmware    string
	software    string
	serial      string
	mfg, model  string
}

// entities is a device's entPhysicalTable, by index.
type entities map[int]*entity

// readEntities walks entPhysicalTable; nil when the agent has none.
func readEntities(c *client) entities {
	rows, err := c.walk(oidEntPhysical)
	if err != nil || len(rows) == 0 {
		return nil
	}
	t := table(rows)
	out := entities{}
	for idx := range t[entColClass] {
		n, err := strconv.Atoi(idx)
		if err != nil || n <= 0 {
			continue
		}
		e := &entity{
			index:    n,
			descr:    strings.TrimSpace(pduString(t[entColDescr][idx])),
			name:     strings.TrimSpace(pduString(t[entColName][idx])),
			firmware: strings.TrimSpace(pduString(t[entColFirmwareRev][idx])),
			software: strings.TrimSpace(pduString(t[entColSoftwareRev][idx])),
			serial:   strings.TrimSpace(pduString(t[entColSerialNum][idx])),
			mfg:      strings.TrimSpace(pduString(t[entColMfgName][idx])),
			model:    strings.TrimSpace(pduString(t[entColModelName][idx])),
		}
		e.class, _ = pduInt(t[entColClass][idx])
		if p, ok := pduInt(t[entColContainedIn][idx]); ok && p > 0 {
			e.parent = int(p)
		}
		out[n] = e
	}
	return out
}

// sorted is the entities by index.
func (es entities) sorted() []*entity {
	list := make([]*entity, 0, len(es))
	for _, e := range es {
		list = append(list, e)
	}
	slices.SortFunc(list, func(a, b *entity) int { return a.index - b.index })
	return list
}

// chassis is the chassis with the lowest index (the first of a stack).
func (es entities) chassis() *entity {
	for _, e := range es.sorted() {
		if e.class == entClassChassis {
			return e
		}
	}
	return nil
}

// collectEntity reads the chassis' model, serial number and software and
// firmware versions, and the transceivers plugged into the ports. A value the
// device already has is kept, except the model, which here is only the
// first line of sysDescr: the chassis' model name is what the device is.
// Profiles run afterwards and win. It returns the entities and each port's
// transceiver, which the sensors are read with.
func collectEntity(c *client, d *model.Device, ifNames map[string]string) (entities, map[string]*entity) {
	es := readEntities(c)
	if es == nil {
		return nil, nil
	}
	if ch := es.chassis(); ch != nil {
		if ch.model != "" {
			d.Model = model.Ptr(ch.model)
		}
		if d.Serial == nil && ch.serial != "" {
			d.Serial = model.Ptr(ch.serial)
		}
		if d.OSVersion == nil && ch.software != "" {
			d.OSVersion = model.Ptr(ch.software)
		}
		if ch.firmware != "" && (d.Firmware == nil || d.Firmware.Current == nil) {
			if d.Firmware == nil {
				d.Firmware = &model.Firmware{}
			}
			d.Firmware.Current = model.Ptr(ch.firmware)
		}
	}
	byPort := portTransceivers(c, es, ifNames)
	collectTransceivers(byPort, d)
	return es, byPort
}

// portEntities maps entities to the port they are, from ENTITY-MIB's
// entAliasMappingTable (a port's alias is "ifIndex.N"). Without that table
// nothing is mapped: guessing by name would be unreliable.
func portEntities(c *client, ifNames map[string]string) map[int]string {
	rows, err := c.walk(oidEntAliasMappingID)
	if err != nil || len(rows) == 0 {
		return nil
	}
	out := map[int]string{}
	for idx, p := range rows {
		n := splitOID(idx)
		ifIdx, ok := strings.CutPrefix(strings.TrimPrefix(pduString(p), "."), oidIfIndexColumn+".")
		if len(n) != 2 || !ok {
			continue
		}
		if name := ifNames[ifIdx]; name != "" {
			out[n[0]] = name
		}
	}
	return out
}

// transceiverWords are what a pluggable module's description says it is.
var transceiverWords = []string{"sfp", "xfp", "qsfp", "gbic", "cfp", "transceiver", "xcvr"}

func isTransceiver(e *entity) bool {
	s := strings.ToLower(e.descr + " " + e.name)
	return slices.ContainsFunc(transceiverWords, func(w string) bool { return strings.Contains(s, w) })
}

// transceiverOf is the pluggable module of a port entity: the port itself,
// the module it is in (Cisco: the port inside its SFP) or a module inside it
// (Arista, Juniper: the optic in the port), when its description says it is
// a transceiver.
func (es entities) transceiverOf(port *entity) *entity {
	if isTransceiver(port) && port.serial+port.model+port.mfg != "" {
		return port
	}
	if p := es[port.parent]; p != nil && p.class == entClassModule && isTransceiver(p) {
		return p
	}
	for _, e := range es.sorted() {
		if e.parent == port.index && e.class == entClassModule && isTransceiver(e) {
			return e
		}
	}
	return nil
}

// portTransceivers are the transceiver entity of each port that has one.
func portTransceivers(c *client, es entities, ifNames map[string]string) map[string]*entity {
	out := map[string]*entity{}
	for idx, name := range portEntities(c, ifNames) {
		e := es[idx]
		if e == nil || e.class != entClassPort {
			continue
		}
		if x := es.transceiverOf(e); x != nil {
			out[name] = x
		}
	}
	return out
}

// collectTransceivers fills each port's transceiver vendor, part and serial.
func collectTransceivers(byPort map[string]*entity, d *model.Device) {
	for i := range d.Interfaces {
		x := byPort[d.Interfaces[i].Name]
		if x == nil || x.mfg+x.model+x.serial == "" {
			continue
		}
		t := d.Interfaces[i].Transceiver
		if t == nil {
			t = &model.Transceiver{}
		}
		if t.Vendor == nil {
			t.Vendor = nonEmpty(x.mfg)
		}
		if t.Part == nil {
			t.Part = nonEmpty(x.model)
		}
		if t.Serial == nil {
			t.Serial = nonEmpty(x.serial)
		}
		d.Interfaces[i].Transceiver = t
	}
}
