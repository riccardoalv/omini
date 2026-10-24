package collector

import (
	"net/netip"
	"slices"

	"github.com/riccardoalv/omini/internal/insights"
	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

// readdressed finds failing integrations whose device answers at another
// address now: the integration's address field points to an IP, its device
// was at that IP in its last data, and the map (the router's ARP or a DHCP
// lease, followed by the topology) puts it at another one. A configured
// hostname is left alone: DNS says where it is, not the integration.
func readdressed(topo topology.Topology, all []store.Integration, statuses []Status,
	fields func(typ string) []model.FormField,
) map[int64]insights.Moved {
	failing := map[int64]bool{}
	for _, s := range statuses {
		if !s.OK {
			failing[s.IntegrationID] = true
		}
	}
	out := map[int64]insights.Moved{}
	for _, it := range all {
		if !failing[it.ID] {
			continue
		}
		f, ok := integration.AddressField(fields(it.Type))
		if !ok {
			continue
		}
		from, ok := integration.AddressIP(it.Config.String(f.Key))
		if !ok {
			continue
		}
		for _, n := range topo.Nodes {
			if n.Kind != topology.KindDevice || n.Online || !reportedBy(n, it.ID) {
				continue
			}
			to, err := netip.ParseAddr(n.IP)
			if err != nil || to.Unmap() == from || !wasAt(n, it.ID, from) {
				continue
			}
			out[it.ID] = insights.Moved{NodeID: n.ID, From: from.String(), To: to.Unmap().String()}
			break
		}
	}
	return out
}

// reportedBy reports whether an integration reports a node.
func reportedBy(n topology.Node, id int64) bool {
	return n.IntegrationID == id || slices.ContainsFunc(n.Reports, func(r topology.Report) bool { return r.IntegrationID == id })
}

// wasAt reports whether an integration's last data put a node at an address:
// a controller's integration reports devices at addresses of their own.
func wasAt(n topology.Node, id int64, ip netip.Addr) bool {
	at := func(d *model.Device) bool {
		if d == nil {
			return false
		}
		if d.Host != nil && sameIP(*d.Host, ip) {
			return true
		}
		return slices.ContainsFunc(d.IPs, func(s string) bool { return sameIP(s, ip) })
	}
	for _, r := range n.Reports {
		if r.IntegrationID == id && at(r.Device) {
			return true
		}
	}
	return n.IntegrationID == id && at(n.Device)
}

func sameIP(s string, ip netip.Addr) bool {
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Addr().Unmap() == ip
	}
	a, err := netip.ParseAddr(s)
	return err == nil && a.Unmap() == ip
}

// fields are an integration type's form fields (none for a type no longer
// installed).
func (c *Collector) fields(typ string) []model.FormField {
	impl, err := c.reg.Get(typ)
	if err != nil {
		return nil
	}
	return impl.Info().Fields
}
