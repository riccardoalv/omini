// Package topology turns the devices reported by all integrations into a
// network map: nodes (devices, unmanaged gear, segments, clients) and edges
// (who is plugged into what, on which port).
//
// Build is a pure function: no I/O, deterministic output, easy to test with
// fixtures. The algorithm is described in the README ("How the topology is built").
package topology

import (
	"fmt"
	"net"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/oui"
)

// Source is the output of one integration instance.
type Source struct {
	IntegrationID int64
	Online        bool // false when the last collection failed (devices are the last known state)
	Devices       []model.Device
}

type NodeKind string

const (
	KindDevice    NodeKind = "device"    // managed: data comes from an integration
	KindUnmanaged NodeKind = "unmanaged" // seen via LLDP but not integrated
	KindSegment   NodeKind = "segment"   // inferred: several MACs behind one port (dumb switch, AP, hypervisor)
	KindClient    NodeKind = "client"    // end device (phone, laptop, TV...)
	KindApp       NodeKind = "app"       // a web application running on a device (Jellyfin, qBittorrent...)
	KindWAN       NodeKind = "wan"       // an internet uplink of a router or firewall
)

type EdgeKind string

const (
	EdgeLLDP     EdgeKind = "lldp"     // confirmed by a discovery protocol
	EdgeFDB      EdgeKind = "fdb"      // from a switch MAC table
	EdgeWifi     EdgeKind = "wifi"     // from an AP registration table
	EdgeInferred EdgeKind = "inferred" // best guess (ARP, MAC seen behind an uplink...)
)

// SegmentMinMACs is the number of client MACs behind a single non-uplink port
// from which we assume there is an unmanaged switch/AP/host there.
const SegmentMinMACs = 3

// Rate is the traffic of an interface, in bits per second, averaged over
// the last polling interval. Rx is what the interface received.
type Rate struct {
	RxBps uint64 `json:"rx_bps"`
	TxBps uint64 `json:"tx_bps"`
	// Errors counted during the same interval.
	RxErrors uint64 `json:"rx_errors,omitempty"`
	TxErrors uint64 `json:"tx_errors,omitempty"`
}

// WANLink describes an internet uplink: the router's WAN interface, the
// physical port carrying it and its gateways.
type WANLink struct {
	Interface string          `json:"interface"`      // e.g. pppoe1
	Port      string          `json:"port,omitempty"` // physical port, e.g. re0
	SpeedMbps uint64          `json:"speed_mbps,omitempty"`
	IPs       []string        `json:"ips,omitempty"`
	Gateways  []model.Gateway `json:"gateways,omitempty"`
}

type Node struct {
	ID            string            `json:"id"`
	Kind          NodeKind          `json:"kind"`
	Label         string            `json:"label"`
	Role          string            `json:"role,omitempty"`
	Online        bool              `json:"online"`
	IntegrationID int64             `json:"integration_id,omitempty"`
	Vendor        string            `json:"vendor,omitempty"`
	Model         string            `json:"model,omitempty"`
	IP            string            `json:"ip,omitempty"`
	MAC           string            `json:"mac,omitempty"`
	Hostname      string            `json:"hostname,omitempty"`
	RandomMAC     bool              `json:"random_mac,omitempty"`
	ParentID      string            `json:"parent_id,omitempty"` // where a client/segment is attached
	Port          string            `json:"port,omitempty"`      // port (or radio) on the parent
	SSID          string            `json:"ssid,omitempty"`
	SignalDBM     *int64            `json:"signal_dbm,omitempty"`
	Band          string            `json:"band,omitempty"`        // Wi-Fi clients: 2.4ghz, 5ghz, 6ghz
	LinkMbps      float64           `json:"link_mbps,omitempty"`   // Wi-Fi clients: link rate, when the AP reports it
	Flow          *Rate             `json:"flow,omitempty"`        // Wi-Fi clients: current traffic (rx = download), as the AP measures it
	MACCount      int               `json:"mac_count,omitempty"`   // segments: MACs seen behind the port
	Device        *model.Device     `json:"device,omitempty"`      // managed devices: full collected data
	WAN           *WANLink          `json:"wan,omitempty"`         // WAN nodes: the uplink
	PortLabels    map[string]string `json:"port_labels,omitempty"` // the user's port descriptions
	Traffic       map[string]Rate   `json:"traffic,omitempty"`     // current rate per interface
	OS            string            `json:"os,omitempty"`
	ReportedOS    string            `json:"-"`                    // OS reported by an integration (input of the classifier)
	OpenPorts     []int             `json:"open_ports,omitempty"` // found by the network scan
	Services      []string          `json:"services,omitempty"`   // mDNS/UPnP services
	Titles        []string          `json:"titles,omitempty"`     // web interface titles
	Web           []WebApp          `json:"web,omitempty"`        // web interfaces, one per port
	Banners       []string          `json:"banners,omitempty"`    // e.g. SSH version
	TTL           int               `json:"ttl,omitempty"`        // ICMP reply TTL

	// Classification (set by the collector, see internal/classify).
	Type    string   `json:"type,omitempty"`
	Brand   string   `json:"brand,omitempty"`
	Product string   `json:"product,omitempty"`
	Reasons []string `json:"reasons,omitempty"`
	Icon    string   `json:"icon,omitempty"` // logo chosen by the user (overrides product/OS/brand)

	Pinned   bool       `json:"pinned,omitempty"`    // pinned by the user: never collapsed
	Hidden   bool       `json:"hidden,omitempty"`    // hidden from the map by the user
	LastSeen *time.Time `json:"last_seen,omitempty"` // offline nodes: when they were last present
}

// WebApp is a web interface on a node; App is the recognized application
// (an icon name of the app catalog), set by the collector.
type WebApp struct {
	Port  int    `json:"port"`
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
	App   string `json:"app,omitempty"`
}

type Edge struct {
	ID         string   `json:"id"`
	Source     string   `json:"source"` // upstream end (closer to the router)
	Target     string   `json:"target"`
	SourcePort string   `json:"source_port,omitempty"`
	TargetPort string   `json:"target_port,omitempty"`
	Kind       EdgeKind `json:"kind"`
	SpeedMbps  uint64   `json:"speed_mbps,omitempty"`
}

type Topology struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

type portKey struct{ node, port string }

type builder struct {
	nodes  map[string]*Node
	edges  map[string]*Edge
	byMAC  map[model.MACAddress]string
	byIP   map[string]string
	byName map[string]string
	// uplinks are ports connected to infrastructure; value is the node on the other end.
	uplinks map[portKey]string
	// lastPort: where MACs missing from the MAC tables right now were last learned.
	lastPort map[model.MACAddress]portKey
	// fdbPorts lists where each MAC was learned; portMACs counts MACs per port.
	fdbPorts map[model.MACAddress][]portKey
	portMACs map[portKey]int
	// wanPorts maps a router port that belongs to a WAN (the WAN interface,
	// its VLAN and physical port) to the WAN node: what is seen there is on
	// the internet side.
	wanPorts map[portKey]string
	// sources keeps every device record merged into a managed node (one per
	// integration that reports it), so no integration's data is lost.
	sources map[string][]*model.Device
	// beside: clients seen on the port towards a device that lists its own
	// clients without them — an unmanaged switch is on that port.
	beside map[portKey][]string
	// hangsBy: links an integration declared from one side only (a neighbor
	// of protocol "other": a VM under its host, a mesh satellite under its
	// unit), by edge key → the node that reported it. Only that node counts
	// as placed by the link: the other end is still placed on its port.
	hangsBy map[string]string
}

// Build computes the topology for the given sources.
// PortRef is a port of a device on the map, e.g. where a MAC was learned.
type PortRef struct {
	Node string `json:"node"`
	Port string `json:"port"`
}

// Options tune a build.
type Options struct {
	// LastSeen holds, per MAC, the switch port where it was last learned.
	// Switches forget idle MACs after a few minutes (a phone asleep): such a
	// MAC keeps its port instead of jumping to wherever ARP sees it.
	LastSeen map[model.MACAddress]PortRef
}

func Build(sources []Source) Topology { return BuildWith(sources, Options{}) }

// BuildWith builds the topology with options.
func BuildWith(sources []Source, opts Options) Topology {
	b := &builder{
		nodes:    map[string]*Node{},
		edges:    map[string]*Edge{},
		byMAC:    map[model.MACAddress]string{},
		byIP:     map[string]string{},
		byName:   map[string]string{},
		uplinks:  map[portKey]string{},
		wanPorts: map[portKey]string{},
		fdbPorts: map[model.MACAddress][]portKey{},
		portMACs: map[portKey]int{},
		sources:  map[string][]*model.Device{},
		beside:   map[portKey][]string{},
		hangsBy:  map[string]string{},
	}
	for _, src := range sources {
		for i := range src.Devices {
			b.addManaged(src, &src.Devices[i])
		}
	}
	managed := b.sortedIDs(KindDevice)
	for _, id := range managed {
		b.addNeighborEdges(id)
	}
	b.indexFDB(managed)
	b.rememberFDB(opts.LastSeen)
	b.placeUnlinkedDevices(managed)
	b.addWANs(managed)
	b.placeClients(managed)
	b.groupSegments()
	b.sharedPorts()
	b.fillDirectLinkSpeeds()
	b.dropEmptyNetworks()
	return b.result()
}

// dropEmptyNetworks removes devices that only stand for a scanned network (no
// address, MAC, port or neighbor of their own: they exist to hold the hosts
// found there) when every one of those hosts was placed somewhere else — e.g.
// a routed subnet whose only host is the modem, already under its WAN. An
// empty placeholder floating on the map says nothing.
func (b *builder) dropEmptyNetworks() {
	linked := map[string]bool{}
	for _, e := range b.edges {
		linked[e.Source], linked[e.Target] = true, true
	}
	for id, n := range b.nodes {
		d := n.Device
		if n.Kind != KindDevice || linked[id] || d == nil || len(b.sources[id]) > 1 ||
			d.Host != nil || len(d.IPs) > 0 || len(deviceMACs(d)) > 0 || len(d.Interfaces) > 0 || len(d.Neighbors) > 0 {
			continue
		}
		delete(b.nodes, id)
	}
}

func (b *builder) addManaged(src Source, d *model.Device) {
	macs := deviceMACs(d)
	// The same physical device may be reported by two integrations (e.g. SNMP
	// and a vendor plugin): merge by any shared MAC, keeping the richer data.
	for _, m := range macs {
		if id, ok := b.byMAC[m]; ok && b.nodes[id].Kind == KindDevice {
			n := b.nodes[id]
			n.Online = n.Online || src.Online
			if richness(d) > richness(n.Device) {
				n.Device = d
			}
			b.sources[id] = append(b.sources[id], d)
			b.index(id, d, macs)
			return
		}
	}
	// Without MACs, a device with the address of one already on the map is
	// that device (e.g. a scan that could not read MACs).
	if len(macs) == 0 && d.Host != nil {
		if id, ok := b.byIP[*d.Host]; ok && b.nodes[id].Kind == KindDevice {
			n := b.nodes[id]
			n.Online = n.Online || src.Online
			b.sources[id] = append(b.sources[id], d)
			b.index(id, d, macs)
			return
		}
	}

	id := managedID(src, d, macs)
	n := &Node{
		ID:            id,
		Kind:          KindDevice,
		Label:         d.Name,
		Role:          string(model.Deref(d.Role)),
		Online:        src.Online,
		IntegrationID: src.IntegrationID,
		Vendor:        model.Deref(d.Vendor),
		Model:         model.Deref(d.Model),
		Device:        d,
	}
	if n.Role == "" {
		n.Role = string(model.DeviceRoleUnknown)
	}
	switch {
	case d.Host != nil && *d.Host != "":
		n.IP = *d.Host
	case len(d.IPs) > 0:
		n.IP = d.IPs[0]
	}
	if len(macs) > 0 {
		n.MAC = string(macs[0])
	}
	b.nodes[id] = n
	b.sources[id] = []*model.Device{d}
	b.index(id, d, macs)
}

func (b *builder) index(id string, d *model.Device, macs []model.MACAddress) {
	for _, m := range macs {
		if _, ok := b.byMAC[m]; !ok {
			b.byMAC[m] = id
		}
	}
	for _, ip := range append(slices.Clone(d.IPs), model.Deref(d.Host)) {
		if ip != "" {
			if _, ok := b.byIP[ip]; !ok {
				b.byIP[ip] = id
			}
		}
	}
	if name := strings.ToLower(d.Name); name != "" {
		if _, ok := b.byName[name]; !ok {
			b.byName[name] = id
		}
	}
}

// managedID is stable across collections: a MAC when available.
func managedID(src Source, d *model.Device, macs []model.MACAddress) string {
	if m := model.NormMAC(d.Key); m != "" {
		return "dev:" + string(m)
	}
	if len(macs) > 0 {
		return "dev:" + string(slices.Min(macs))
	}
	return fmt.Sprintf("dev:%d:%s", src.IntegrationID, d.Key)
}

func deviceMACs(d *model.Device) []model.MACAddress {
	seen := map[model.MACAddress]bool{}
	var out []model.MACAddress
	add := func(m model.MACAddress) {
		if m != "" && !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	add(model.NormMAC(d.Key))
	for _, m := range d.MACs {
		add(m)
	}
	for _, i := range d.Interfaces {
		if i.MAC != nil {
			add(*i.MAC)
		}
	}
	return out
}

func richness(d *model.Device) int {
	if d == nil {
		return -1
	}
	return len(d.Interfaces) + len(d.Neighbors) + len(d.Fdb) + len(d.Arp)
}

func (b *builder) lookup(nb *model.Neighbor) string {
	if nb.RemoteMAC != nil {
		if id, ok := b.byMAC[*nb.RemoteMAC]; ok {
			return id
		}
	}
	if nb.RemoteIP != nil {
		if id, ok := b.byIP[*nb.RemoteIP]; ok {
			return id
		}
	}
	if nb.RemoteName != nil {
		name := strings.ToLower(*nb.RemoteName)
		if id, ok := b.byName[name]; ok {
			return id
		}
		if short, _, found := strings.Cut(name, "."); found { // FQDN vs short name
			if id, ok := b.byName[short]; ok {
				return id
			}
		}
	}
	return ""
}

func (b *builder) addNeighborEdges(id string) {
	for _, d := range b.sources[id] {
		b.addNeighborEdgesOf(id, d)
	}
}

func (b *builder) addNeighborEdgesOf(id string, d *model.Device) {
	for i := range d.Neighbors {
		nb := &d.Neighbors[i]
		target := b.lookup(nb)
		if target == "" {
			target = b.addUnmanaged(nb)
		}
		if target == "" || target == id {
			continue
		}
		remotePort := model.Deref(nb.RemotePort)
		b.addEdge(id, nb.LocalPort, target, remotePort, EdgeLLDP, portSpeed(d, nb.LocalPort))
		if model.Deref(nb.Protocol) == model.NeighborProtocolOther {
			b.hangsBy[edgeKey(id, target)] = id
		}
		b.uplinks[portKey{id, nb.LocalPort}] = target
		if remotePort != "" {
			b.uplinks[portKey{target, remotePort}] = id
		}
	}
}

func (b *builder) addUnmanaged(nb *model.Neighbor) string {
	var id, label string
	switch {
	case nb.RemoteMAC != nil:
		id = "ext:" + string(*nb.RemoteMAC)
	case nb.RemoteIP != nil:
		id = "ext:" + *nb.RemoteIP
	case nb.RemoteName != nil:
		id = "ext:" + strings.ToLower(*nb.RemoteName)
	default:
		return ""
	}
	switch {
	case nb.RemoteName != nil:
		label = *nb.RemoteName
	case nb.RemoteIP != nil:
		label = *nb.RemoteIP
	default:
		label = string(*nb.RemoteMAC)
	}
	if _, ok := b.nodes[id]; !ok {
		n := &Node{
			ID: id, Kind: KindUnmanaged, Label: label, Role: string(model.DeviceRoleUnknown), Online: true,
			Model: model.Deref(nb.RemotePlatform), IP: model.Deref(nb.RemoteIP),
		}
		if nb.RemoteMAC != nil {
			n.MAC = string(*nb.RemoteMAC)
			b.byMAC[*nb.RemoteMAC] = id
		}
		if nb.RemoteIP != nil {
			b.byIP[*nb.RemoteIP] = id
		}
		if nb.RemoteName != nil {
			b.byName[strings.ToLower(*nb.RemoteName)] = id
		}
		b.nodes[id] = n
	}
	return id
}

func (b *builder) addEdge(a, aPort, c, cPort string, kind EdgeKind, speed uint64) {
	x, y, xPort, yPort := a, c, aPort, cPort
	if y < x {
		x, y, xPort, yPort = y, x, yPort, xPort
	}
	key := x + "|" + y
	e, ok := b.edges[key]
	if !ok {
		b.edges[key] = &Edge{ID: "e:" + key, Source: x, Target: y, SourcePort: xPort, TargetPort: yPort, Kind: kind, SpeedMbps: speed}
		return
	}
	// The same link reported from the other side: fill in what is missing.
	if e.SourcePort == "" {
		e.SourcePort = xPort
	}
	if e.TargetPort == "" {
		e.TargetPort = yPort
	}
	if e.SpeedMbps == 0 {
		e.SpeedMbps = speed
	}
	if kind == EdgeLLDP {
		e.Kind = EdgeLLDP
	}
}

func portSpeed(d *model.Device, port string) uint64 {
	if d == nil {
		return 0
	}
	for _, i := range d.Interfaces {
		if i.Name == port {
			return model.Deref(i.SpeedMbps)
		}
	}
	return 0
}

func (b *builder) indexFDB(managed []string) {
	for _, id := range managed {
		seen := map[portKey]map[model.MACAddress]bool{}
		var fdb []model.FdbEntry
		for _, d := range b.sources[id] {
			fdb = append(fdb, d.Fdb...)
		}
		for _, e := range fdb {
			if e.MAC.IsGroup() {
				continue
			}
			k := portKey{id, e.Port}
			if seen[k] == nil {
				seen[k] = map[model.MACAddress]bool{}
			}
			if seen[k][e.MAC] {
				continue // same MAC in several VLANs
			}
			seen[k][e.MAC] = true
			b.fdbPorts[e.MAC] = append(b.fdbPorts[e.MAC], k)
			b.portMACs[k]++
		}
	}
}

// rememberFDB keeps, for MACs that no MAC table lists right now, the port
// where they were last learned (if that device is still on the map). It only
// places a device seen by other means (ARP, Wi-Fi...): it never makes one
// that left look present.
func (b *builder) rememberFDB(last map[model.MACAddress]PortRef) {
	b.lastPort = map[model.MACAddress]portKey{}
	for m, ref := range last {
		if len(b.fdbPorts[m]) > 0 || ref.Port == "" {
			continue
		}
		if n := b.nodes[ref.Node]; n != nil && n.Kind == KindDevice {
			b.lastPort[m] = portKey{ref.Node, ref.Port}
		}
	}
}

// fdbOrLast: the ports where a MAC is learned now, else where it last was.
func (b *builder) fdbOrLast(m model.MACAddress) []portKey {
	if ports := b.fdbPorts[m]; len(ports) > 0 {
		return ports
	}
	if k, ok := b.lastPort[m]; ok {
		return []portKey{k}
	}
	return nil
}

// bestPort picks the edge-most port among candidates: non-uplink first, then
// the one with the fewest learned MACs (closest to the device).
func (b *builder) bestPort(cands []portKey, exclude string) (portKey, bool, bool) {
	var best portKey
	found, bestUplink := false, false
	for _, c := range cands {
		if c.node == exclude {
			continue
		}
		_, up := b.uplinks[c]
		switch {
		case !found:
		case bestUplink && !up:
		case bestUplink == up && b.portMACs[c] < b.portMACs[best]:
		case bestUplink == up && b.portMACs[c] == b.portMACs[best] && (c.node+c.port) < (best.node+best.port):
		default:
			continue
		}
		best, found, bestUplink = c, true, up
	}
	return best, found, bestUplink
}

// placeUnlinkedDevices attaches managed devices without any LLDP link using
// the MAC tables of the other devices (or, failing that, their ARP tables).
func (b *builder) placeUnlinkedDevices(managed []string) {
	linked := map[string]bool{}
	for key, e := range b.edges {
		if child, ok := b.hangsBy[key]; ok {
			linked[child] = true
			continue
		}
		linked[e.Source], linked[e.Target] = true, true
	}
	for _, id := range managed {
		if linked[id] {
			continue
		}
		var cands []portKey
		for _, m := range deviceMACs(b.nodes[id].Device) {
			cands = append(cands, b.fdbPorts[m]...)
		}
		if port, ok, _ := b.bestPort(cands, id); ok {
			// Our own end of the link: the interface where we see the peer in ARP.
			localPort := arpInterface(b.nodes[id].Device, deviceMACs(b.nodes[port.node].Device))
			b.addEdge(port.node, port.port, id, localPort, EdgeInferred, portSpeed(b.nodes[port.node].Device, port.port))
			b.uplinks[port] = id
			linked[id], linked[port.node] = true, true
			continue
		}
		if peer, iface := b.arpPeer(id, managed); peer != "" {
			// Our end: the port of our MAC table where the peer is learned.
			// It is our uplink, so what is seen only there is behind the peer.
			local := b.portOf(id, deviceMACs(b.nodes[peer].Device))
			if local != "" {
				b.uplinks[portKey{id, local}] = peer
			}
			b.addEdge(peer, iface, id, local, EdgeInferred, portSpeed(b.nodes[id].Device, local))
			linked[id], linked[peer] = true, true
		}
	}
}

// portOf is the port of a device's own MAC table where any of macs is learned
// — now, or last time when the table forgot them for a while (else the uplink
// would come and go, and what is behind it would look like a segment).
func (b *builder) portOf(id string, macs []model.MACAddress) string {
	for _, m := range macs {
		for _, k := range b.fdbPorts[m] {
			if k.node == id {
				return k.port
			}
		}
	}
	for _, m := range macs {
		if k, ok := b.lastPort[m]; ok && k.node == id {
			return k.port
		}
	}
	return ""
}

func arpInterface(d *model.Device, macs []model.MACAddress) string {
	for _, a := range d.Arp {
		if slices.Contains(macs, a.MAC) {
			return model.Deref(a.Interface)
		}
	}
	return ""
}

func (b *builder) arpPeer(id string, managed []string) (string, string) {
	macs := map[model.MACAddress]bool{}
	for _, m := range deviceMACs(b.nodes[id].Device) {
		macs[m] = true
	}
	for _, other := range managed {
		if other == id {
			continue
		}
		for _, a := range b.nodes[other].Device.Arp {
			if macs[a.MAC] {
				return other, model.Deref(a.Interface)
			}
		}
	}
	return "", ""
}

type wifiSeen struct {
	node string
	c    model.WirelessClient
}

type arpSeen struct {
	node  string
	iface string
	ip    string
}

// hostSeen is a host reported by an integration (network scan, controller).
type hostSeen struct {
	node string
	h    model.Host
}

func mergeHost(a, b model.Host) model.Host {
	a.Hostnames = appendUniqueStr(a.Hostnames, b.Hostnames...)
	a.Services = appendUniqueStr(a.Services, b.Services...)
	a.Sources = appendUniqueStr(a.Sources, b.Sources...)
	for _, p := range b.OpenPorts {
		if !slices.Contains(a.OpenPorts, p) {
			a.OpenPorts = append(a.OpenPorts, p)
		}
	}
	if a.MAC == nil {
		a.MAC = b.MAC
	}
	if a.Vendor == nil {
		a.Vendor = b.Vendor
	}
	if a.Manufacturer == nil {
		a.Manufacturer = b.Manufacturer
	}
	if a.Model == nil {
		a.Model = b.Model
	}
	if a.OS == nil {
		a.OS = b.OS
	}
	return a
}

func (b *builder) placeClients(managed []string) {
	wifi := map[model.MACAddress]wifiSeen{}
	arp := map[model.MACAddress]arpSeen{}
	hostnames := map[model.MACAddress]string{}
	leaseIPs := map[model.MACAddress]string{}
	hostsByMAC := map[model.MACAddress]hostSeen{}
	hostsByIP := map[string]hostSeen{} // hosts without a known MAC (e.g. other subnets)
	for _, id := range managed {
		for _, d := range b.sources[id] {
			b.collectClientsOf(id, d, wifi, arp, hostnames, leaseIPs, hostsByMAC, hostsByIP)
		}
	}
	b.placeCollected(wifi, arp, hostnames, leaseIPs, hostsByMAC, hostsByIP)
}

func (b *builder) collectClientsOf(id string, d *model.Device,
	wifi map[model.MACAddress]wifiSeen, arp map[model.MACAddress]arpSeen,
	hostnames, leaseIPs map[model.MACAddress]string,
	hostsByMAC map[model.MACAddress]hostSeen, hostsByIP map[string]hostSeen,
) {
	for _, w := range d.WirelessClients {
		prev, ok := wifi[w.MAC]
		if !ok || model.Deref(w.SignalDBM) > model.Deref(prev.c.SignalDBM) {
			wifi[w.MAC] = wifiSeen{node: id, c: w}
		}
	}
	for _, a := range d.Arp {
		if _, ok := arp[a.MAC]; !ok {
			arp[a.MAC] = arpSeen{node: id, iface: model.Deref(a.Interface), ip: a.IP}
		}
	}
	for _, l := range d.DhcpLeases {
		if h := model.Deref(l.Hostname); h != "" {
			hostnames[l.MAC] = h
		}
		leaseIPs[l.MAC] = l.IP
	}
	for _, h := range d.Hosts {
		if h.MAC != nil && !h.MAC.IsGroup() {
			prev, ok := hostsByMAC[*h.MAC]
			if ok {
				h = mergeHost(prev.h, h)
			} else {
				prev.node = id
			}
			hostsByMAC[*h.MAC] = hostSeen{node: prev.node, h: h}
			continue
		}
		prev, ok := hostsByIP[h.IP]
		if ok {
			h = mergeHost(prev.h, h)
		} else {
			prev.node = id
		}
		hostsByIP[h.IP] = hostSeen{node: prev.node, h: h}
	}
}

func (b *builder) placeCollected(
	wifi map[model.MACAddress]wifiSeen, arp map[model.MACAddress]arpSeen,
	hostnames, leaseIPs map[model.MACAddress]string,
	hostsByMAC map[model.MACAddress]hostSeen, hostsByIP map[string]hostSeen,
) {
	// Hosts that are infrastructure we already know (e.g. the gateway) enrich that node.
	for m, hs := range hostsByMAC {
		if id, ok := b.byMAC[m]; ok {
			enrich(b.nodes[id], hs.h)
		}
	}
	for ip, hs := range hostsByIP {
		if id, ok := b.byIP[ip]; ok {
			enrich(b.nodes[id], hs.h)
		}
	}

	// A client must be present now (Wi-Fi, MAC table, ARP or a scan); a DHCP
	// lease alone only enriches, since leases outlive devices leaving the network.
	present := map[model.MACAddress]bool{}
	for m := range wifi {
		present[m] = true
	}
	for m := range b.fdbPorts {
		present[m] = true
	}
	for m := range arp {
		present[m] = true
	}
	for m := range hostsByMAC {
		present[m] = true
	}
	macs := make([]model.MACAddress, 0, len(present))
	for m := range present {
		if _, known := b.byMAC[m]; !known && !m.IsGroup() {
			macs = append(macs, m)
		}
	}
	slices.Sort(macs)

	placed := map[string]bool{} // IPs that already have a node
	for _, m := range macs {
		hs, scanned := hostsByMAC[m]
		n := &Node{
			ID: "mac:" + string(m), Kind: KindClient, Role: "client", Online: true, MAC: string(m),
			RandomMAC: m.IsRandomized(), Hostname: hostnames[m],
		}
		n.IP = firstNonEmpty(arp[m].ip, hs.h.IP, leaseIPs[m])
		if scanned {
			enrich(n, hs.h)
		}
		if n.Vendor == "" {
			n.Vendor = oui.Lookup(string(m))
		}
		n.Label = firstNonEmpty(shortName(n.Hostname), n.IP, n.MAC)

		var kind EdgeKind
		if w, ok := wifi[m]; ok {
			n.ParentID, n.Port, kind = w.node, model.Deref(w.c.Interface), EdgeWifi
			n.SSID, n.SignalDBM = model.Deref(w.c.SSID), w.c.SignalDBM
			if w.c.Band != nil {
				n.Band = string(*w.c.Band)
			}
			n.LinkMbps = max(model.Deref(w.c.TxRateMbps), model.Deref(w.c.RxRateMbps))
			if w.c.RxBps != nil || w.c.TxBps != nil {
				n.Flow = &Rate{RxBps: model.Deref(w.c.RxBps), TxBps: model.Deref(w.c.TxBps)}
			}
		} else if port, ok, uplink := b.bestPort(b.fdbOrLast(m), ""); ok {
			n.ParentID, n.Port, kind = port.node, port.port, EdgeFDB
			if uplink {
				// Only seen towards other infrastructure: the client is behind it —
				// unless that device lists its clients without this one: then both
				// hang from an unmanaged switch on that port.
				if behind := b.nodes[b.uplinks[port]]; behind != nil &&
					(behind.Kind == KindUnmanaged || len(behind.Device.Fdb) == 0) {
					if b.listsClients(behind.ID) && !b.isClientOf(behind.ID, m) {
						// Only that MAC table vouches for it (no ARP, no scan): a
						// stale entry, or something the AP itself does not see.
						if _, inARP := arp[m]; !inARP && !scanned {
							continue
						}
						b.beside[port] = append(b.beside[port], n.ID)
					} else {
						n.ParentID, n.Port, kind = behind.ID, "", EdgeInferred
					}
				}
			}
		} else if a, ok := arp[m]; ok {
			n.ParentID, n.Port, kind = a.node, a.iface, EdgeInferred
			if wan := b.wanPorts[portKey{a.node, a.iface}]; wan != "" {
				n.ParentID, n.Port = wan, "" // e.g. the ISP modem, seen on the WAN port
			}
		} else if scanned {
			n.ParentID, kind = hs.node, EdgeInferred
		}
		b.nodes[n.ID] = n
		placed[n.IP] = true
		if n.ParentID != "" {
			b.addEdge(n.ParentID, n.Port, n.ID, "", kind, 0)
		}
	}

	// Hosts known only by IP (routed subnets, no ARP).
	ips := make([]string, 0, len(hostsByIP))
	for ip := range hostsByIP {
		ips = append(ips, ip)
	}
	slices.Sort(ips)
	for _, ip := range ips {
		if _, known := b.byIP[ip]; known || placed[ip] {
			continue
		}
		hs := hostsByIP[ip]
		n := &Node{ID: "ip:" + ip, Kind: KindClient, Role: "client", Online: true, IP: ip, ParentID: hs.node}
		enrich(n, hs.h)
		n.Label = firstNonEmpty(shortName(n.Hostname), ip)
		b.nodes[n.ID] = n
		b.addEdge(hs.node, "", n.ID, "", EdgeInferred, 0)
	}
}

// enrich copies what a scan learned about a host into its node.
func enrich(n *Node, h model.Host) {
	if n.Hostname == "" {
		n.Hostname = bestHostname(h.Hostnames)
	}
	if n.Vendor == "" {
		n.Vendor = firstNonEmpty(model.Deref(h.Manufacturer), model.Deref(h.Vendor))
	}
	if n.Model == "" {
		n.Model = model.Deref(h.Model)
	}
	if n.ReportedOS == "" {
		n.ReportedOS = model.Deref(h.OS)
		n.OS = n.ReportedOS
	}
	for _, p := range h.OpenPorts {
		if !slices.Contains(n.OpenPorts, int(p)) {
			n.OpenPorts = append(n.OpenPorts, int(p))
		}
	}
	slices.Sort(n.OpenPorts)
	n.Services = appendUniqueStr(n.Services, h.Services...)
	n.Titles = appendUniqueStr(n.Titles, h.Titles...)
	for _, w := range h.Web {
		if !slices.ContainsFunc(n.Web, func(x WebApp) bool { return x.Port == int(w.Port) }) {
			n.Web = append(n.Web, WebApp{Port: int(w.Port), URL: w.URL, Title: model.Deref(w.Title)})
		}
	}
	slices.SortFunc(n.Web, func(a, b WebApp) int { return a.Port - b.Port })
	n.Banners = appendUniqueStr(n.Banners, h.Banners...)
	if n.TTL == 0 && h.TTL != nil {
		n.TTL = int(*h.TTL)
	}
}

// bestHostname prefers real names over reverse-DNS placeholders and machine
// identifiers (e.g. Home Assistant announces itself as "<uuid>.local").
func bestHostname(names []string) string {
	for _, n := range names {
		if !strings.HasSuffix(n, ".arpa") && net.ParseIP(n) == nil && !isIdentifier(shortName(n)) {
			return n
		}
	}
	return ""
}

// isIdentifier reports whether a name is a long hex/UUID string rather than a name.
func isIdentifier(s string) bool {
	s = strings.ReplaceAll(s, "-", "")
	if len(s) < 16 {
		return false
	}
	for _, c := range strings.ToLower(s) {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// shortName turns "nas.home.lan" or "Living-Room.local" into "nas" / "Living-Room".
func shortName(name string) string {
	if name == "" || net.ParseIP(name) != nil {
		return name
	}
	short, _, _ := strings.Cut(name, ".")
	return short
}

func appendUniqueStr(list []string, items ...string) []string {
	for _, it := range items {
		if it != "" && !slices.Contains(list, it) {
			list = append(list, it)
		}
	}
	return list
}

// groupSegments replaces "many clients behind one access port" with a
// segment node: something unmanaged (dumb switch, AP, hypervisor) is there.
func (b *builder) groupSegments() {
	groups := map[portKey][]*Node{}
	for _, n := range b.nodes {
		if n.Kind != KindClient || n.Port == "" || n.SSID != "" {
			continue
		}
		parent := b.nodes[n.ParentID]
		if parent == nil || parent.Kind != KindDevice {
			continue
		}
		k := portKey{n.ParentID, n.Port}
		if _, up := b.uplinks[k]; up {
			continue
		}
		if e := b.edges[edgeKey(n.ParentID, n.ID)]; e == nil || e.Kind != EdgeFDB {
			continue
		}
		groups[k] = append(groups[k], n)
	}
	for k, clients := range groups {
		if len(clients) < SegmentMinMACs {
			continue
		}
		seg := &Node{
			ID: "seg:" + k.node + ":" + k.port, Kind: KindSegment, Label: "Unmanaged segment",
			Role: string(model.DeviceRoleUnknown), Online: true, ParentID: k.node, Port: k.port, MACCount: len(clients),
		}
		b.nodes[seg.ID] = seg
		b.addEdge(k.node, k.port, seg.ID, "", EdgeFDB, portSpeed(b.nodes[k.node].Device, k.port))
		for _, c := range clients {
			delete(b.edges, edgeKey(k.node, c.ID))
			c.ParentID, c.Port = seg.ID, ""
			b.addEdge(seg.ID, "", c.ID, "", EdgeInferred, 0)
		}
	}
}

// listsClients: the device's integration reports its clients (an access
// point's Wi-Fi clients, or the wired ones it sees).
func (b *builder) listsClients(id string) bool {
	for _, d := range b.sources[id] {
		if len(d.WirelessClients) > 0 || len(d.Fdb) > 0 {
			return true
		}
	}
	return false
}

func (b *builder) isClientOf(id string, m model.MACAddress) bool {
	for _, d := range b.sources[id] {
		for _, w := range d.WirelessClients {
			if w.MAC == m {
				return true
			}
		}
		for _, f := range d.Fdb {
			if f.MAC == m {
				return true
			}
		}
	}
	return false
}

// sharedPorts draws the unmanaged switch found on a port towards a device (see
// beside): a segment on that port, with the device and those clients under it.
func (b *builder) sharedPorts() {
	keys := make([]portKey, 0, len(b.beside))
	for k := range b.beside {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(x, y portKey) int { return strings.Compare(x.node+x.port, y.node+y.port) })
	for _, k := range keys {
		dev := b.uplinks[k]
		clients := b.beside[k]
		seg := &Node{
			ID: "seg:" + k.node + ":" + k.port, Kind: KindSegment, Label: "Unmanaged segment",
			Role: string(model.DeviceRoleUnknown), Online: true, ParentID: k.node, Port: k.port,
			MACCount: len(clients) + 1,
		}
		b.nodes[seg.ID] = seg
		up := b.edges[edgeKey(k.node, dev)]
		delete(b.edges, edgeKey(k.node, dev))
		b.addEdge(k.node, k.port, seg.ID, "", EdgeFDB, portSpeed(b.nodes[k.node].Device, k.port))
		devPort := ""
		if up != nil {
			devPort = up.TargetPort
		}
		b.addEdge(seg.ID, "", dev, devPort, EdgeInferred, 0)
		if n := b.nodes[dev]; n != nil {
			n.ParentID, n.Port = seg.ID, ""
		}
		for _, id := range clients {
			delete(b.edges, edgeKey(k.node, id))
			if c := b.nodes[id]; c != nil {
				c.ParentID, c.Port = seg.ID, ""
			}
			b.addEdge(seg.ID, "", id, "", EdgeInferred, 0)
		}
	}
}

// fillDirectLinkSpeeds gives a link the speed of the device port it uses when
// that port has a single link: nothing else is behind it, so the port speed is
// the link speed (e.g. a firewall's WAN port and the modem). A port shared by
// many devices (seen through ARP) has a switch behind it; its speed is not
// the speed of each device's link.
func (b *builder) fillDirectLinkSpeeds() {
	uses := map[portKey]int{}
	for _, e := range b.edges {
		if e.SourcePort != "" {
			uses[portKey{e.Source, e.SourcePort}]++
		}
		if e.TargetPort != "" {
			uses[portKey{e.Target, e.TargetPort}]++
		}
	}
	for _, e := range b.edges {
		if e.SpeedMbps != 0 {
			continue
		}
		for _, end := range []portKey{{e.Source, e.SourcePort}, {e.Target, e.TargetPort}} {
			if end.port == "" || uses[end] != 1 || b.nodes[end.node] == nil {
				continue
			}
			if speed := portSpeed(b.nodes[end.node].Device, end.port); speed > 0 {
				e.SpeedMbps = speed
				break
			}
		}
	}
}

// addWANs adds one node per internet uplink of each router/firewall, as
// its parent: several WANs give several parents.
func (b *builder) addWANs(managed []string) {
	for _, id := range managed {
		var ifaces []model.Interface
		var gateways []model.Gateway
		seen := map[string]bool{}
		for _, d := range b.sources[id] {
			for _, i := range d.Interfaces {
				if !seen[i.Name] {
					seen[i.Name] = true
					ifaces = append(ifaces, i)
				}
			}
			gateways = append(gateways, d.Gateways...)
		}
		byName := map[string]model.Interface{}
		for _, i := range ifaces {
			byName[i.Name] = i
		}
		for _, i := range ifaces {
			if !model.Deref(i.Wan) {
				continue
			}
			wanID := "wan:" + id + ":" + i.Name
			link := &WANLink{Interface: i.Name, SpeedMbps: model.Deref(i.SpeedMbps), IPs: i.IPs}
			// Follow the chain (PPPoE → VLAN → port) to the physical port.
			chain := []string{i.Name}
			for p, n := model.Deref(i.Parent), 0; p != "" && n < 4; n++ {
				chain = append(chain, p)
				link.Port = p
				p = model.Deref(byName[p].Parent)
			}
			for _, g := range gateways {
				if model.Deref(g.Interface) == i.Name {
					link.Gateways = append(link.Gateways, g)
				}
			}
			label := model.Deref(i.Description)
			if label == "" {
				label = "WAN"
			}
			b.nodes[wanID] = &Node{
				ID: wanID, Kind: KindWAN, Label: label, Role: "wan",
				Online: model.Deref(i.Up) && wanUp(link.Gateways), WAN: link,
			}
			for _, p := range chain {
				b.wanPorts[portKey{id, p}] = wanID
			}
			port := link.Port
			if port == "" {
				port = i.Name
			}
			b.addEdge(wanID, "", id, port, EdgeInferred, link.SpeedMbps)
		}
	}
}

// wanUp: an uplink is up unless every gateway on it is down.
func wanUp(gateways []model.Gateway) bool {
	for _, g := range gateways {
		if g.Status != model.GatewayStatusDown {
			return true
		}
	}
	return len(gateways) == 0
}

func edgeKey(a, c string) string {
	if c < a {
		a, c = c, a
	}
	return a + "|" + c
}

// result orients edges away from the root (router/firewall) and returns
// nodes and edges in a deterministic order.
func (b *builder) result() Topology {
	adj := map[string][]string{}
	for _, e := range b.edges {
		adj[e.Source] = append(adj[e.Source], e.Target)
		adj[e.Target] = append(adj[e.Target], e.Source)
	}
	depth := map[string]int{}
	var queue []string
	for _, id := range b.rootIDs(adj) {
		if _, seen := depth[id]; !seen {
			depth[id] = 0
			queue = append(queue, id)
		}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			next := adj[cur]
			sort.Strings(next)
			for _, nb := range next {
				if _, seen := depth[nb]; !seen {
					depth[nb] = depth[cur] + 1
					queue = append(queue, nb)
				}
			}
		}
	}

	t := Topology{Nodes: make([]Node, 0, len(b.nodes)), Edges: make([]Edge, 0, len(b.edges))}
	for _, e := range b.edges {
		// WAN nodes are parents of their router, whatever the BFS found.
		toWAN := b.nodes[e.Target] != nil && b.nodes[e.Target].Kind == KindWAN &&
			(b.nodes[e.Source] == nil || b.nodes[e.Source].Kind != KindWAN)
		if toWAN || (depth[e.Target] < depth[e.Source] && b.nodes[e.Source].Kind != KindWAN) {
			e.Source, e.Target, e.SourcePort, e.TargetPort = e.Target, e.Source, e.TargetPort, e.SourcePort
		}
		t.Edges = append(t.Edges, *e)
	}
	for _, n := range b.nodes {
		t.Nodes = append(t.Nodes, *n)
	}
	sort.Slice(t.Nodes, func(i, j int) bool { return t.Nodes[i].ID < t.Nodes[j].ID })
	sort.Slice(t.Edges, func(i, j int) bool { return t.Edges[i].ID < t.Edges[j].ID })
	return t
}

// rootIDs returns BFS starting points: firewalls and routers first, then the
// best connected node of each remaining component.
func (b *builder) rootIDs(adj map[string][]string) []string {
	rank := func(n *Node) int {
		switch {
		case n.Kind == KindDevice && n.Role == string(model.DeviceRoleFirewall):
			return 0
		case n.Kind == KindDevice && n.Role == string(model.DeviceRoleRouter):
			return 1
		case n.Kind == KindDevice:
			return 2
		case n.Kind == KindUnmanaged:
			return 3
		}
		return 4
	}
	ids := make([]string, 0, len(b.nodes))
	for id := range b.nodes {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, c := b.nodes[ids[i]], b.nodes[ids[j]]
		if ra, rc := rank(a), rank(c); ra != rc {
			return ra < rc
		}
		if da, dc := len(adj[a.ID]), len(adj[c.ID]); da != dc {
			return da > dc
		}
		return a.ID < c.ID
	})
	return ids
}

func (b *builder) sortedIDs(kind NodeKind) []string {
	var ids []string
	for id, n := range b.nodes {
		if n.Kind == kind {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
