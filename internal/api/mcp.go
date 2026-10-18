package api

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/notify"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

// The MCP server (streamable HTTP, stateless, JSON responses) lets an AI
// assistant read the network. Every tool only reads: changes stay confirmed
// by the user in the UI.

const mcpInstructions = `Omini maps a local network: its devices (routers, switches, access points, servers, clients), how they are connected, their health, alerts, VLANs, DNS and DHCP, and internet use. Every tool is read-only. Start with get_map_summary; find devices with search_devices and read one with get_device (ids are stable map node ids). Rates are bits per second, traffic in bytes.`

// mcpHandler serves /mcp: authenticated by an API token (Bearer) or the
// admin session.
func (s *Server) mcpHandler() http.Handler {
	srv := mcp.NewServer(&mcp.Implementation{Name: "omini", Title: "Omini", Version: s.Version},
		&mcp.ServerOptions{Instructions: mcpInstructions})
	s.addMCPTools(srv)
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true,
		// Every request is authenticated, which DNS rebinding cannot do; the
		// localhost check would refuse a reverse proxy on the same machine.
		DisableLocalhostProtection: true,
	})
	// A browser on another site cannot post with the admin's cookie.
	return http.NewCrossOriginProtection().Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authenticate(w, r) {
			return
		}
		h.ServeHTTP(w, r)
	}))
}

// --- tool inputs ---

type noInput struct{}

type searchInput struct {
	Query string `json:"query,omitempty" jsonschema:"text to find in the name, alias, IP, MAC, vendor, hostname or note (case-insensitive); empty lists every device"`
	Limit int    `json:"limit,omitempty" jsonschema:"most results (default 25, at most 200)"`
}

type deviceInput struct {
	ID string `json:"id" jsonschema:"the device id, as search_devices returns it"`
}

type alertsInput struct {
	Severity string `json:"severity,omitempty" jsonschema:"critical, warning or info; empty for all"`
}

type internetInput struct {
	Range string `json:"range,omitempty" jsonschema:"15m, 1h, today, 7d or 30d (default 15m)"`
	Limit int    `json:"limit,omitempty" jsonschema:"most devices (default 20, at most 200)"`
}

func (s *Server) addMCPTools(srv *mcp.Server) {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: model.Ptr(false)}
	tool := func(name, title, desc string) *mcp.Tool {
		return &mcp.Tool{Name: name, Title: title, Description: desc, Annotations: readOnly}
	}
	mcp.AddTool(srv, tool("get_map_summary", "Map summary",
		"Counts of devices by kind, type and role, online and offline; each integration's status; open alerts by severity."),
		func(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, any, error) {
			out, err := s.mapSummary(ctx)
			return nil, out, err
		})
	mcp.AddTool(srv, tool("search_devices", "Search devices",
		"Devices on the map or remembered in the inventory whose name, alias, IP, MAC, vendor, hostname or note contain the query. Online devices first."),
		func(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, any, error) {
			out, err := s.searchDevices(ctx, in.Query, in.Limit)
			return nil, out, err
		})
	mcp.AddTool(srv, tool("get_device", "Get a device",
		"One device: identity, the user's note, type, IPs and MACs, where it is connected, its ports, health and open alerts."),
		func(ctx context.Context, _ *mcp.CallToolRequest, in deviceInput) (*mcp.CallToolResult, any, error) {
			out, err := s.device(ctx, in.ID)
			return nil, out, err
		})
	mcp.AddTool(srv, tool("list_alerts", "List alerts",
		"The open alerts, worst first, with an English title and detail."),
		func(ctx context.Context, _ *mcp.CallToolRequest, in alertsInput) (*mcp.CallToolResult, any, error) {
			out, err := s.openAlerts(ctx, in.Severity)
			return nil, out, err
		})
	mcp.AddTool(srv, tool("list_networks", "List networks",
		"The VLANs and their subnets, with the router serving each network (gateway, DHCP range), the devices that carry each VLAN and its Wi-Fi networks; the DHCP pools."),
		func(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, any, error) {
			out, err := s.networks(ctx)
			return nil, out, err
		})
	mcp.AddTool(srv, tool("get_dns_dhcp", "DNS and DHCP",
		"The DNS servers and their checks, the DHCP checks (who answers), the pools and the number of leases per server."),
		func(_ context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, any, error) {
			return nil, s.dnsDHCP(), nil
		})
	mcp.AddTool(srv, tool("get_internet_usage", "Internet usage",
		"Who used the internet over a period (from NetFlow/IPFIX/sFlow), busiest first: bytes per device, the total, the current rate and the top countries and networks."),
		func(ctx context.Context, _ *mcp.CallToolRequest, in internetInput) (*mcp.CallToolResult, any, error) {
			out, err := s.internetSummary(ctx, in.Range, in.Limit)
			return nil, out, err
		})
}

// --- map summary ---

type integrationSummary struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Type        string     `json:"type"`
	Enabled     bool       `json:"enabled"`
	OK          *bool      `json:"ok,omitempty"` // nil: not collected yet
	Error       string     `json:"error,omitempty"`
	CollectedAt *time.Time `json:"collected_at,omitempty"`
	Devices     int        `json:"devices"`
}

type mapSummary struct {
	GeneratedAt  time.Time            `json:"generated_at"`
	Nodes        int                  `json:"nodes"`
	Online       int                  `json:"online"`
	Offline      int                  `json:"offline"`
	ByKind       map[string]int       `json:"by_kind"`
	ByType       map[string]int       `json:"by_type"`
	ByRole       map[string]int       `json:"by_role"`
	Integrations []integrationSummary `json:"integrations"`
	Alerts       map[string]int       `json:"open_alerts"`
}

// counted: the nodes that are machines (not web apps nor WAN uplinks).
func counted(n topology.Node) bool { return n.Kind != topology.KindApp && n.Kind != topology.KindWAN }

func (s *Server) mapSummary(ctx context.Context) (mapSummary, error) {
	st := s.Collector.State()
	out := mapSummary{
		GeneratedAt: st.GeneratedAt, ByKind: map[string]int{}, ByType: map[string]int{}, ByRole: map[string]int{},
		Integrations: []integrationSummary{}, Alerts: map[string]int{"critical": 0, "warning": 0, "info": 0},
	}
	for _, n := range st.Topology.Nodes {
		if !counted(n) {
			continue
		}
		out.Nodes++
		if n.Online {
			out.Online++
		} else {
			out.Offline++
		}
		out.ByKind[string(n.Kind)]++
		out.ByType[cmp.Or(n.Type, "unknown")]++
		if n.Role != "" {
			out.ByRole[n.Role]++
		}
	}
	all, err := s.Store.ListIntegrations(ctx)
	if err != nil {
		return out, err
	}
	statuses := s.statuses()
	for _, in := range all {
		v := integrationSummary{ID: in.ID, Name: in.Name, Type: in.Type, Enabled: in.Enabled}
		if st, ok := statuses[in.ID]; ok {
			v.OK, v.Error, v.CollectedAt, v.Devices = model.Ptr(st.OK), st.Error, model.Ptr(st.CollectedAt), st.Devices
		}
		out.Integrations = append(out.Integrations, v)
	}
	for _, a := range st.Alerts {
		out.Alerts[a.Severity]++
	}
	return out, nil
}

// --- devices ---

type deviceHit struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Kind     string     `json:"kind"`
	Type     string     `json:"type,omitempty"`
	IP       string     `json:"ip,omitempty"`
	MAC      string     `json:"mac,omitempty"`
	Vendor   string     `json:"vendor,omitempty"`
	Online   bool       `json:"online"`
	OnMap    bool       `json:"on_map"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
	Note     string     `json:"note,omitempty"`
}

type searchResult struct {
	Devices []deviceHit `json:"devices"`
	Total   int         `json:"total"` // matches before the limit
}

func clampLimit(n, def, most int) int {
	if n <= 0 {
		return def
	}
	return min(n, most)
}

func (s *Server) searchDevices(ctx context.Context, query string, limit int) (searchResult, error) {
	limit = clampLimit(limit, 25, 200)
	q := strings.ToLower(strings.TrimSpace(query))
	inv, err := s.Store.ListInventory(ctx)
	if err != nil {
		return searchResult{}, err
	}
	byID := make(map[string]store.InventoryEntry, len(inv))
	for _, e := range inv {
		byID[e.ID] = e
	}
	match := func(fields ...string) bool {
		if q == "" {
			return true
		}
		for _, f := range fields {
			if f != "" && strings.Contains(strings.ToLower(f), q) {
				return true
			}
		}
		return false
	}
	hits := []deviceHit{}
	seen := map[string]bool{}
	for _, n := range s.Collector.State().Topology.Nodes {
		if !counted(n) {
			continue
		}
		e := byID[n.ID]
		seen[n.ID] = true
		fields := []string{n.Label, e.Alias, e.Label, n.IP, n.MAC, n.Vendor, n.Hostname, n.Brand, n.Product, n.Model, n.Note}
		if n.Device != nil {
			fields = append(fields, n.Device.IPs...)
			for _, m := range n.Device.MACs {
				fields = append(fields, string(m))
			}
		}
		if !match(fields...) {
			continue
		}
		hits = append(hits, deviceHit{
			ID: n.ID, Name: n.Label, Kind: string(n.Kind), Type: n.Type, IP: n.IP, MAC: n.MAC,
			Vendor: n.Vendor, Online: n.Online, OnMap: true, LastSeen: n.LastSeen, Note: n.Note,
		})
	}
	// Devices that left the map stay in the inventory.
	for _, e := range inv {
		if seen[e.ID] || e.Kind == string(topology.KindApp) || !match(e.Label, e.Alias, e.IP, e.MAC, e.Vendor, e.Hostname, e.Note) {
			continue
		}
		hits = append(hits, deviceHit{
			ID: e.ID, Name: cmp.Or(e.Alias, e.Label), Kind: e.Kind, Type: e.DeviceType, IP: e.IP, MAC: e.MAC,
			Vendor: e.Vendor, LastSeen: model.Ptr(e.LastSeen), Note: e.Note,
		})
	}
	slices.SortStableFunc(hits, func(a, b deviceHit) int {
		if a.Online != b.Online {
			if a.Online {
				return -1
			}
			return 1
		}
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	out := searchResult{Total: len(hits), Devices: hits}
	if len(hits) > limit {
		out.Devices = hits[:limit]
	}
	return out, nil
}

type link struct {
	NodeID    string  `json:"node_id"`
	Name      string  `json:"name"`
	Port      string  `json:"port,omitempty"`       // on the other device
	LocalPort string  `json:"local_port,omitempty"` // on this one
	Via       string  `json:"via,omitempty"`        // lldp, fdb, wifi, inferred, vpn
	SpeedMbps uint64  `json:"speed_mbps,omitempty"`
	SSID      string  `json:"ssid,omitempty"`
	Band      string  `json:"band,omitempty"`
	SignalDBM *int64  `json:"signal_dbm,omitempty"`
	LinkMbps  float64 `json:"link_mbps,omitempty"`
}

type portSummary struct {
	Name      string `json:"name"`
	Label     string `json:"label,omitempty"` // the user's or the device's description
	Type      string `json:"type,omitempty"`
	Up        *bool  `json:"up,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
	SpeedMbps uint64 `json:"speed_mbps,omitempty"`
	Vlan      any    `json:"vlans,omitempty"`
	RxBps     uint64 `json:"rx_bps,omitempty"`
	TxBps     uint64 `json:"tx_bps,omitempty"`
	Errors    uint64 `json:"errors,omitempty"`
	Neighbor  string `json:"neighbor,omitempty"` // the node on the other end
	Clients   int    `json:"clients,omitempty"`  // nodes attached here
}

type portsView struct {
	Total int           `json:"total"`
	Up    int           `json:"up"`
	Down  int           `json:"down"`
	List  []portSummary `json:"list"`
}

type health struct {
	CPUPct       *float64            `json:"cpu_pct,omitempty"`
	MemPct       *float64            `json:"mem_pct,omitempty"`
	UptimeS      *uint64             `json:"uptime_s,omitempty"`
	LoadAvg      []float64           `json:"load_avg,omitempty"`
	Temperatures []model.Temperature `json:"temperatures,omitempty"`
	Storage      []storageUse        `json:"storage,omitempty"`
	Firmware     *model.Firmware     `json:"firmware,omitempty"`
}

type storageUse struct {
	Mount   string  `json:"mount"`
	UsedPct float64 `json:"used_pct"`
}

type deviceDetail struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Alias    string      `json:"alias,omitempty"`
	Note     string      `json:"note,omitempty"`
	Kind     string      `json:"kind"`
	Role     string      `json:"role,omitempty"`
	Type     string      `json:"type,omitempty"`
	OS       string      `json:"os,omitempty"`
	Brand    string      `json:"brand,omitempty"`
	Product  string      `json:"product,omitempty"`
	Vendor   string      `json:"vendor,omitempty"`
	Model    string      `json:"model,omitempty"`
	Hostname string      `json:"hostname,omitempty"`
	Online   bool        `json:"online"`
	OnMap    bool        `json:"on_map"`
	Managed  bool        `json:"managed"` // an integration reads it
	LastSeen *time.Time  `json:"last_seen,omitempty"`
	IPs      []string    `json:"ips"`
	MACs     []string    `json:"macs"`
	Uplink   *link       `json:"connected_to,omitempty"`
	Links    []link      `json:"links,omitempty"` // other devices linked to it, downstream
	Clients  int         `json:"clients"`         // nodes attached to it
	Ports    *portsView  `json:"ports,omitempty"`
	Services []string    `json:"services,omitempty"`
	Open     []int       `json:"open_ports,omitempty"`
	Web      []string    `json:"web,omitempty"`
	Health   *health     `json:"health,omitempty"`
	Alerts   []alertView `json:"open_alerts"`
}

var errNoDevice = errors.New("no device with that id: find it with search_devices")

func (s *Server) device(ctx context.Context, id string) (deviceDetail, error) {
	st := s.Collector.State()
	topo := st.Topology
	idx := slices.IndexFunc(topo.Nodes, func(n topology.Node) bool { return n.ID == id })
	inv, err := s.Store.ListInventory(ctx)
	if err != nil {
		return deviceDetail{}, err
	}
	var e store.InventoryEntry
	known := false
	for _, x := range inv {
		if x.ID == id {
			e, known = x, true
			break
		}
	}
	if idx < 0 {
		if !known || id == "" {
			return deviceDetail{}, errNoDevice
		}
		d := deviceDetail{
			ID: e.ID, Name: cmp.Or(e.Alias, e.Label), Alias: e.Alias, Note: e.Note, Kind: e.Kind, Type: e.DeviceType,
			Vendor: e.Vendor, Hostname: e.Hostname, LastSeen: model.Ptr(e.LastSeen),
			IPs: nonEmpty(e.IP), MACs: nonEmpty(e.MAC), Alerts: s.alertsOf(st.Alerts, id),
		}
		if e.ParentID != "" {
			d.Uplink = &link{NodeID: e.ParentID, Name: e.ParentID, Port: e.Port}
		}
		return d, nil
	}
	n := topo.Nodes[idx]
	labels := nodeLabels(topo)
	d := deviceDetail{
		ID: n.ID, Name: n.Label, Alias: e.Alias, Note: n.Note, Kind: string(n.Kind), Role: n.Role, Type: n.Type,
		OS: n.OS, Brand: n.Brand, Product: n.Product, Vendor: n.Vendor, Model: n.Model, Hostname: n.Hostname,
		Online: n.Online, OnMap: true, Managed: n.Kind == topology.KindDevice && n.Device != nil, LastSeen: n.LastSeen,
		IPs: nonEmpty(n.IP), MACs: nonEmpty(n.MAC), Services: n.Services, Open: n.OpenPorts,
		Alerts: s.alertsOf(st.Alerts, id),
	}
	for _, w := range n.Web {
		d.Web = append(d.Web, w.URL)
	}
	clientsOn := map[string]int{}
	for _, c := range topo.Nodes {
		if c.ParentID == n.ID && counted(c) {
			d.Clients++
			clientsOn[c.Port]++
		}
	}
	neighbor := map[string]string{}
	for _, ed := range topo.Edges {
		switch {
		case ed.Target == n.ID && d.Uplink == nil && ed.Kind != topology.EdgeVPN:
			d.Uplink = &link{
				NodeID: ed.Source, Name: labels[ed.Source], Port: ed.SourcePort, LocalPort: ed.TargetPort,
				Via: string(ed.Kind), SpeedMbps: ed.SpeedMbps,
			}
			neighbor[ed.TargetPort] = ed.Source
		case ed.Source == n.ID:
			neighbor[ed.SourcePort] = ed.Target
			if t := slices.IndexFunc(topo.Nodes, func(x topology.Node) bool { return x.ID == ed.Target }); t >= 0 && topo.Nodes[t].Kind != topology.KindClient {
				d.Links = append(d.Links, link{
					NodeID: ed.Target, Name: labels[ed.Target], Port: ed.TargetPort, LocalPort: ed.SourcePort,
					Via: string(ed.Kind), SpeedMbps: ed.SpeedMbps,
				})
			}
		case ed.Target == n.ID:
			neighbor[ed.TargetPort] = ed.Source
		}
	}
	if d.Uplink == nil && n.ParentID != "" {
		d.Uplink = &link{NodeID: n.ParentID, Name: labels[n.ParentID], Port: n.Port}
	}
	if d.Uplink != nil && n.SSID != "" {
		d.Uplink.SSID, d.Uplink.Band, d.Uplink.SignalDBM, d.Uplink.LinkMbps = n.SSID, n.Band, n.SignalDBM, n.LinkMbps
		d.Uplink.Via = "wifi"
	}
	if dev := n.Device; dev != nil {
		d.IPs = mergeUnique(d.IPs, dev.IPs)
		for _, m := range dev.MACs {
			d.MACs = mergeUnique(d.MACs, []string{string(m)})
		}
		d.Ports = portsOf(n, neighbor, clientsOn)
		d.Health = healthOf(dev)
	}
	return d, nil
}

func nonEmpty(s string) []string {
	if s == "" {
		return []string{}
	}
	return []string{s}
}

func mergeUnique(a, b []string) []string {
	for _, x := range b {
		if x = strings.TrimSpace(x); x != "" && !slices.Contains(a, x) {
			a = append(a, x)
		}
	}
	return a
}

// portsOf summarizes a device's ports: physical ones, wireless and LAGs.
func portsOf(n topology.Node, neighbor map[string]string, clients map[string]int) *portsView {
	v := &portsView{List: []portSummary{}}
	for _, i := range n.Device.Interfaces {
		typ := model.Deref(i.Type)
		switch typ {
		case model.InterfaceTypeLoopback, model.InterfaceTypeTunnel, model.InterfaceTypeBridge, model.InterfaceTypeVlan:
			continue
		}
		p := portSummary{
			Name: i.Name, Label: cmp.Or(n.PortLabels[i.Name], model.Deref(i.Description)), Type: string(typ),
			Up: i.Up, Enabled: i.Enabled, SpeedMbps: model.Deref(i.SpeedMbps),
			Errors: model.Deref(i.RxErrors) + model.Deref(i.TxErrors), Neighbor: neighbor[i.Name], Clients: clients[i.Name],
		}
		if i.Vlans != nil {
			p.Vlan = i.Vlans
		}
		if r, ok := n.Traffic[i.Name]; ok {
			p.RxBps, p.TxBps = r.RxBps, r.TxBps
		}
		v.Total++
		switch {
		case i.Up != nil && *i.Up:
			v.Up++
		case i.Up != nil:
			v.Down++
		}
		v.List = append(v.List, p)
	}
	return v
}

func healthOf(d *model.Device) *health {
	h := &health{
		CPUPct: d.CPUPct, MemPct: d.MemPct, UptimeS: d.UptimeS, LoadAvg: d.LoadAvg,
		Temperatures: d.Temperatures, Firmware: d.Firmware,
	}
	for _, st := range d.Storage {
		if t, u := model.Deref(st.TotalBytes), model.Deref(st.UsedBytes); t > 0 {
			h.Storage = append(h.Storage, storageUse{Mount: st.Mount, UsedPct: float64(u*1000/t) / 10})
		}
	}
	if h.CPUPct == nil && h.MemPct == nil && h.UptimeS == nil && h.LoadAvg == nil && h.Temperatures == nil && h.Storage == nil && h.Firmware == nil {
		return nil
	}
	return h
}

// --- alerts ---

type alertView struct {
	ID       int64          `json:"id"`
	Rule     string         `json:"rule"`
	Severity string         `json:"severity"`
	Title    string         `json:"title"`
	Detail   string         `json:"detail,omitempty"`
	NodeID   string         `json:"node_id,omitempty"`
	Params   map[string]any `json:"params,omitempty"`
	OpenedAt time.Time      `json:"opened_at"`
	// Dismissed: the user hid it until it resolves.
	Dismissed bool `json:"dismissed,omitempty"`
}

func viewAlert(a store.Alert) alertView {
	title, detail := notify.Describe(a, "en")
	return alertView{
		ID: a.ID, Rule: a.Rule, Severity: a.Severity, Title: title, Detail: detail, NodeID: a.NodeID,
		Params: a.Params, OpenedAt: a.OpenedAt, Dismissed: a.Dismissed,
	}
}

func (s *Server) alertsOf(open []store.Alert, nodeID string) []alertView {
	out := []alertView{}
	for _, a := range open {
		if a.NodeID == nodeID {
			out = append(out, viewAlert(a))
		}
	}
	return out
}

var severityRank = map[string]int{"critical": 0, "warning": 1, "info": 2}

func (s *Server) openAlerts(ctx context.Context, severity string) (map[string]any, error) {
	if severity != "" && !validSeverity(severity) {
		return nil, errors.New("severity must be critical, warning or info")
	}
	list, err := s.Store.ListAlerts(ctx, time.Time{})
	if err != nil {
		return nil, err
	}
	out := []alertView{}
	for _, a := range list {
		if a.ResolvedAt == nil && (severity == "" || a.Severity == severity) {
			out = append(out, viewAlert(a))
		}
	}
	slices.SortStableFunc(out, func(a, b alertView) int {
		return cmp.Or(cmp.Compare(severityRank[a.Severity], severityRank[b.Severity]), b.OpenedAt.Compare(a.OpenedAt))
	})
	return map[string]any{"alerts": out}, nil
}

// --- networks ---

type routedNetwork struct {
	Vlan      uint16             `json:"vlan"`
	Name      string             `json:"name,omitempty"`
	Gateway   string             `json:"gateway,omitempty"`
	Interface string             `json:"interface,omitempty"`
	Dhcp      *model.NetworkDhcp `json:"dhcp,omitempty"`
	RouterID  string             `json:"router_id"`
	Router    string             `json:"router"`
}

type vlanSummary struct {
	ID       uint16          `json:"id"`
	Name     string          `json:"name"`
	Subnet   string          `json:"subnet,omitempty"`
	Networks []routedNetwork `json:"networks"` // routers serving it
	Devices  []vlanOnDevice  `json:"devices"`  // devices with ports in it
	Wifi     []vlanWifi      `json:"wifi"`
}

func (s *Server) networks(ctx context.Context) (map[string]any, error) {
	catalog, err := s.Collector.Vlans(ctx)
	if err != nil {
		return nil, err
	}
	topo := s.Collector.State().Topology
	routed := []routedNetwork{}
	for _, n := range topo.Nodes {
		if n.Kind != topology.KindDevice || n.Device == nil {
			continue
		}
		for _, nw := range n.Device.Networks {
			routed = append(routed, routedNetwork{
				Vlan: nw.Vlan, Name: model.Deref(nw.Name), Gateway: model.Deref(nw.Gateway),
				Interface: model.Deref(nw.Interface), Dhcp: nw.Dhcp, RouterID: n.ID, Router: n.Label,
			})
		}
	}
	vlans := make([]vlanSummary, 0, len(catalog))
	inCatalog := map[uint16]bool{}
	for _, v := range catalog {
		inCatalog[v.ID] = true
		vs := vlanSummary{
			ID: v.ID, Name: v.Name, Subnet: v.Subnet, Networks: []routedNetwork{},
			Devices: vlanDevices(topo, v.ID), Wifi: vlanWifis(topo, v.ID),
		}
		for _, r := range routed {
			if r.Vlan == v.ID {
				vs.Networks = append(vs.Networks, r)
			}
		}
		vlans = append(vlans, vs)
	}
	other := []routedNetwork{} // networks of VLANs the catalog does not have
	for _, r := range routed {
		if !inCatalog[r.Vlan] {
			other = append(other, r)
		}
	}
	return map[string]any{"vlans": vlans, "other_networks": other, "dhcp_pools": dhcpOf(topo).Pools}, nil
}

// --- DNS and DHCP ---

type leaseCount struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
	Leases int    `json:"leases"`
}

func (s *Server) dnsDHCP() map[string]any {
	st := s.Collector.State()
	topo := st.Topology
	dhcp := dhcpOf(topo)
	labels := nodeLabels(topo)
	by := map[string]int{}
	for _, l := range dhcp.Leases {
		by[l.NodeID]++
	}
	counts := []leaseCount{}
	for id, n := range by {
		counts = append(counts, leaseCount{NodeID: id, Name: labels[id], Leases: n})
	}
	slices.SortFunc(counts, func(a, b leaseCount) int { return cmp.Compare(a.NodeID, b.NodeID) })
	return map[string]any{
		"dns_servers": dnsOf(topo), "dhcp_checks": dhcp.Checks, "dhcp_pools": dhcp.Pools,
		"leases": len(dhcp.Leases), "leases_by_server": counts, "generated_at": st.GeneratedAt,
	}
}

// --- internet ---

func (s *Server) internetSummary(ctx context.Context, rng string, limit int) (map[string]any, error) {
	rep, err := s.internet(ctx, rng)
	if err != nil {
		return nil, err
	}
	limit = clampLimit(limit, 20, 200)
	devices := rep.Devices
	if len(devices) > limit {
		devices = devices[:limit]
	}
	out := map[string]any{
		"since": rep.Since, "seconds": rep.Seconds, "total": map[string]uint64{"rx": rep.Total.RX, "tx": rep.Total.TX}, "now": rep.Now,
		"devices": devices, "device_count": len(rep.Devices),
		"countries": rep.Countries[:min(len(rep.Countries), 10)], "orgs": rep.Orgs[:min(len(rep.Orgs), 10)],
		"listening": rep.Listening, "home": rep.Home,
	}
	if !rep.Listening && len(rep.Devices) == 0 {
		out["hint"] = "no flow data: point a router's NetFlow, IPFIX or sFlow export at Omini (see the Internet screen)"
	}
	if rep.Attribution != "" {
		out["attribution"] = rep.Attribution
	}
	return out, nil
}
