package topology

import (
	"cmp"
	"reflect"
	"slices"

	"github.com/riccardoalv/omini/internal/model"
)

// mergeInto makes the node from part of the node to: one machine reported
// twice (a device without a MAC whose address turned out to be another
// device's). Its reports, data and index entries move; from is gone.
func (b *builder) mergeInto(from, to string) {
	f, t := b.nodes[from], b.nodes[to]
	if f == nil || t == nil || from == to {
		return
	}
	t.Online = t.Online || f.Online
	t.Reports = append(t.Reports, f.Reports...)
	b.sources[to] = append(b.sources[to], b.sources[from]...)
	for m, id := range b.byMAC {
		if id == from {
			b.byMAC[m] = to
		}
	}
	for ip, id := range b.byIP {
		if id == from {
			b.byIP[ip] = to
		}
	}
	for name, id := range b.byName {
		if id == from {
			b.byName[name] = to
		}
	}
	delete(b.nodes, from)
	delete(b.sources, from)
}

// GuestView says a report sees the device from outside, as its host does: a
// hypervisor's view of a VM, or Docker's of a container (its link to the
// host, protocol "other"). The device's own report knows its system better.
func GuestView(d *model.Device) bool {
	if d == nil {
		return false
	}
	for _, nb := range d.Neighbors {
		if model.Deref(nb.Protocol) == model.NeighborProtocolOther && nb.RemotePlatform != nil {
			return true
		}
	}
	return false
}

// mergeReports gives a node reported by several integrations one view of all
// of them: the device's own report first (not a hypervisor's view of it), the
// richest first among those, and what it leaves empty filled from the others.
// Ports, neighbors and services are joined by name, so a VM's NIC seen by its
// hypervisor (and the VLAN change that goes with it) stays next to the ports
// its own system reports. The reports themselves stay as they came.
func (b *builder) mergeReports() {
	for _, n := range b.nodes {
		if n.Kind != KindDevice || len(n.Reports) < 2 {
			continue
		}
		reports := slices.Clone(n.Reports)
		slices.SortStableFunc(reports, func(x, y Report) int {
			if gx, gy := GuestView(x.Device), GuestView(y.Device); gx != gy {
				if gx {
					return 1
				}
				return -1
			}
			return cmp.Compare(richness(y.Device), richness(x.Device))
		})
		merged := *reports[0].Device
		for _, r := range reports[1:] {
			fill(&merged, r.Device)
		}
		// A MAC the node adopted from ARP or a lease stays.
		if n.Device != nil {
			merged.MACs = joinMACs(merged.MACs, n.Device.MACs)
		}
		n.Device, n.IntegrationID = &merged, reports[0].IntegrationID
		if r := model.Deref(merged.Role); r != "" {
			n.Role = string(r)
		}
		n.Vendor, n.Model = cmp.Or(model.Deref(merged.Vendor), n.Vendor), cmp.Or(model.Deref(merged.Model), n.Model)
	}
}

// fill copies into dst what it leaves empty from src, and joins the lists
// that describe distinct things by name.
func fill(dst, src *model.Device) {
	dst.Interfaces = joinBy(dst.Interfaces, src.Interfaces, func(i model.Interface) string { return i.Name })
	dst.Services = joinBy(dst.Services, src.Services, func(s model.Service) string { return s.Name })
	dst.Neighbors = joinBy(dst.Neighbors, src.Neighbors, func(nb model.Neighbor) string {
		return nb.LocalPort + "|" + string(model.Deref(nb.RemoteMAC)) + "|" + model.Deref(nb.RemoteIP) + "|" + model.Deref(nb.RemoteName)
	})
	dst.MACs = joinMACs(dst.MACs, src.MACs)
	dst.IPs = joinBy(dst.IPs, src.IPs, func(ip string) string { return ip })
	dv, sv := reflect.ValueOf(dst).Elem(), reflect.ValueOf(src).Elem()
	for i := range dv.NumField() {
		if f := dv.Field(i); f.CanSet() && f.IsZero() && !sv.Field(i).IsZero() {
			f.Set(sv.Field(i))
		}
	}
}

func joinBy[T any](a, b []T, key func(T) string) []T {
	if len(b) == 0 {
		return a
	}
	seen := make(map[string]bool, len(a))
	for _, x := range a {
		seen[key(x)] = true
	}
	out := slices.Clone(a)
	for _, x := range b {
		if k := key(x); !seen[k] {
			seen[k] = true
			out = append(out, x)
		}
	}
	return out
}

func joinMACs(a, b []model.MACAddress) []model.MACAddress {
	return joinBy(a, b, func(m model.MACAddress) string { return string(m) })
}
