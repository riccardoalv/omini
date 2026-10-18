package snmp

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/riccardoalv/omini/internal/model"
)

// ENTITY-MIB (RFC 6933): the physical parts of a device (chassis, modules,
// ports, sensors), what they are and their serial numbers.
const (
	oidEntPhysical       = "1.3.6.1.2.1.47.1.1.1.1"   // entPhysicalEntry; index: entPhysicalIndex
	oidEntAliasMappingID = "1.3.6.1.2.1.47.1.3.2.1.2" // entAliasMappingIdentifier; index: physical.logical
	oidIfIndexColumn     = "1.3.6.1.2.1.2.2.1.1"      // ifIndex: what an alias of a port points to
	entClassChassis      = 3
	entClassModule       = 9
	entClassPort         = 10
	entColDescr          = 2
	entColContainedIn    = 4
	entColClass          = 5
	entColName           = 7
	entColFirmwareRev    = 9
	entColSoftwareRev    = 10
	entColSerialNum      = 11
	entColMfgName        = 12
	entColModelName      = 13
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
// firmware versions, the transceivers plugged into the ports, then the
// temperature sensors. A value the device already has is kept, except the
// model, which here is only the first line of sysDescr: the chassis' model
// name is what the device is. Profiles run afterwards and win.
func collectEntity(c *client, d *model.Device, ifNames map[string]string) {
	es := readEntities(c)
	if es == nil {
		return
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
	collectSensors(c, es, byPort, d)
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

// ENTITY-SENSOR-MIB (RFC 3433): one row per sensor entity.
const (
	oidEntPhySensor     = "1.3.6.1.2.1.99.1.1.1" // entPhySensorEntry; index: entPhysicalIndex
	sensorTypeCelsius   = 8
	sensorStatusOK      = 1
	sensorScaleUnits    = 9 // entPhySensorScale units(9): 10^0; each step is 10^3
	entClassCPU         = 12
	maxContainmentDepth = 16 // a containment loop must not hang the read
)

// within reports whether e is the entity outer or inside it.
func (es entities) within(e *entity, outer int) bool {
	for hop := 0; e != nil && hop < maxContainmentDepth; hop++ {
		if e.index == outer {
			return true
		}
		e = es[e.parent]
	}
	return false
}

// sensorKind is cpu for a sensor of a CPU (its entity, or one it is in, is a
// CPU, or its name says so), else board.
func (es entities) sensorKind(e *entity) model.TemperatureKind {
	for hop := 0; e != nil && hop < maxContainmentDepth; hop++ {
		if e.class == entClassCPU || strings.Contains(strings.ToLower(e.name+" "+e.descr), "cpu") {
			return model.TemperatureKindCPU
		}
		if e.class == entClassChassis || e.class == entClassModule {
			break // a CPU's sensor is not above its board
		}
		e = es[e.parent]
	}
	return model.TemperatureKindBoard
}

// collectSensors reads the temperature sensors (celsius) of ENTITY-SENSOR-MIB:
// one under a port's transceiver is that transceiver's temperature, the
// others are the device's, named by their entity. Profiles run afterwards,
// and theirs replace a sensor of the same name.
func collectSensors(c *client, es entities, byPort map[string]*entity, d *model.Device) {
	rows, err := c.walk(oidEntPhySensor)
	if err != nil || len(rows) == 0 {
		return
	}
	t := table(rows)
	var temps []model.Temperature
	xcvr := map[string]float64{}
	for _, e := range es.sorted() {
		idx := strconv.Itoa(e.index)
		typ, ok1 := pduInt(t[1][idx])
		value, ok2 := pduInt(t[4][idx])
		if !ok1 || !ok2 || typ != sensorTypeCelsius {
			continue
		}
		if st, ok := pduInt(t[5][idx]); ok && st != sensorStatusOK {
			continue
		}
		scale, ok := pduInt(t[2][idx])
		if !ok {
			scale = sensorScaleUnits
		}
		precision, _ := pduInt(t[3][idx])
		celsius := float64(value) * math.Pow10(int(scale-sensorScaleUnits)*3-int(precision))
		if celsius <= -50 || celsius >= 200 { // absent sensors report out of range
			continue
		}
		celsius = math.Round(celsius*10) / 10
		port := ""
		for name, x := range byPort {
			if es.within(e, x.index) {
				port = name
				break
			}
		}
		if port != "" {
			xcvr[port] = celsius
			continue
		}
		name := cmp.Or(e.name, e.descr, "Sensor "+idx)
		if slices.ContainsFunc(temps, func(x model.Temperature) bool { return x.Sensor == name }) {
			name += " " + idx
		}
		temps = append(temps, model.Temperature{Sensor: name, Celsius: celsius, Kind: model.Ptr(es.sensorKind(e))})
	}
	for _, x := range temps {
		d.Temperatures = append(removeSensor(d.Temperatures, x.Sensor), x)
	}
	for i := range d.Interfaces {
		if temp, ok := xcvr[d.Interfaces[i].Name]; ok {
			if d.Interfaces[i].Transceiver == nil {
				d.Interfaces[i].Transceiver = &model.Transceiver{}
			}
			d.Interfaces[i].Transceiver.TemperatureC = model.Ptr(temp)
		}
	}
}
