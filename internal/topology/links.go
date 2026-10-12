package topology

import (
	"slices"

	"github.com/riccardoalv/omini/internal/model"
)

// markLinks shows what the ports at a link's ends say about it: a port of a
// LAG makes the link the LAG (its number of ports and their total speed; the
// port keeps its name, its counters are the link's traffic), a port spanning
// tree blocks makes it a standby link.
func markLinks(t *Topology) {
	devs := map[string]*model.Device{}
	for _, n := range t.Nodes {
		if n.Device != nil {
			devs[n.ID] = n.Device
		}
	}
	for i := range t.Edges {
		e := &t.Edges[i]
		for _, end := range []struct {
			node string
			port *string
		}{{e.Source, &e.SourcePort}, {e.Target, &e.TargetPort}} {
			d := devs[end.node]
			if d == nil || *end.port == "" {
				continue
			}
			j := slices.IndexFunc(d.Interfaces, func(x model.Interface) bool { return x.Name == *end.port })
			if j < 0 {
				continue
			}
			port := d.Interfaces[j]
			if s := port.Stp; s != nil && s.State != nil && *s.State == model.PortStpStateBlocking {
				e.Standby = true
			}
			lag := model.Deref(port.Lag)
			if lag == "" && slices.ContainsFunc(d.Interfaces, func(x model.Interface) bool { return model.Deref(x.Lag) == port.Name }) {
				lag = port.Name // the link ends on the LAG itself
			}
			if lag == "" {
				continue
			}
			members, speed := 0, uint64(0)
			for _, x := range d.Interfaces {
				if model.Deref(x.Lag) == lag {
					members++
					if model.Deref(x.Up) {
						speed += model.Deref(x.SpeedMbps)
					}
				}
			}
			if members > e.Lag {
				e.Lag = members
			}
			if speed > e.SpeedMbps {
				e.SpeedMbps = speed
			}
		}
	}
}

// Via is the node as the integration that makes change k reads it: the same
// node with that report's device (a router read by SNMP and its plugin
// changes its networks through the plugin's report).
func (n Node) Via(k model.ChangeKind) Node {
	if r, ok := n.ChangeVia[k]; ok && r.Device != nil {
		n.Device, n.IntegrationID = r.Device, r.IntegrationID
	}
	return n
}
