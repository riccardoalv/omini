package api

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"time"

	"github.com/riccardoalv/omini/internal/flows"
	"github.com/riccardoalv/omini/internal/geoip"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

// internetRanges are the periods of the Internet screen.
var internetRanges = map[string]func(now time.Time) time.Time{
	"15m": func(now time.Time) time.Time { return now.Add(-15 * time.Minute) },
	"1h":  func(now time.Time) time.Time { return now.Add(-time.Hour) },
	"today": func(now time.Time) time.Time {
		y, m, d := now.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	},
	"7d":  func(now time.Time) time.Time { return now.Add(-7 * 24 * time.Hour) },
	"30d": func(now time.Time) time.Time { return now.Add(-30 * 24 * time.Hour) },
}

type usageView struct {
	store.Usage
	Label string `json:"label"`
	OnMap bool   `json:"on_map"`
}

// internetReport is the Internet screen's data over a period.
type internetReport struct {
	Devices   []usageView        `json:"devices"`
	Total     store.Usage        `json:"total"`
	Since     time.Time          `json:"since"`
	Seconds   int                `json:"seconds"`
	Series    []store.UsagePoint `json:"series"`
	StepS     int                `json:"step_s"`
	Now       map[string]float64 `json:"now"`
	Countries []countryUsage     `json:"countries"`
	Orgs      []orgUsage         `json:"orgs"`
	Home      string             `json:"home"`
	Listening bool               `json:"listening"`
	Exporters []flows.Exporter   `json:"exporters"`
	// Attribution credits DB-IP when the places come from it.
	Attribution string `json:"attribution,omitempty"`
}

var errInternetRange = errors.New("range must be 15m, 1h, today, 7d or 30d")

// internetUsage is who used the internet over a period, busiest first
// (`range`: 15m, 1h, today, 7d or 30d).
func (s *Server) internetUsage(w http.ResponseWriter, r *http.Request) {
	rep, err := s.internet(r.Context(), r.URL.Query().Get("range"))
	if errors.Is(err, errInternetRange) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// internet builds the report of a range ("" is 15m).
func (s *Server) internet(ctx context.Context, rng string) (internetReport, error) {
	if rng == "" {
		rng = "15m"
	}
	from, ok := internetRanges[rng]
	if !ok {
		return internetReport{}, errInternetRange
	}
	now := time.Now()
	since := from(now)
	list, err := s.Store.InternetUsage(ctx, since, now)
	if err != nil {
		return internetReport{}, err
	}
	labels := nodeLabels(s.Collector.State().Topology)
	// A device no longer on the map keeps its name from the inventory.
	known := map[string]string{}
	if inv, err := s.Store.ListInventory(ctx); err == nil {
		for _, e := range inv {
			known[e.ID] = cmp.Or(e.Alias, e.Label)
		}
	}
	out := make([]usageView, 0, len(list))
	var total store.Usage
	for _, u := range list {
		v := usageView{Usage: u, Label: u.Node}
		if l, ok := labels[u.Node]; ok {
			v.Label, v.OnMap = l, true
		} else if l := known[u.Node]; l != "" {
			v.Label = l
		} else if len(u.Node) > 3 && u.Node[:3] == "ip:" {
			v.Label = u.Node[3:]
		}
		total.RX += u.RX
		total.TX += u.TX
		out = append(out, v)
	}
	series, step, err := s.Store.InternetSeries(ctx, since, now)
	if err != nil {
		return internetReport{}, err
	}
	places, err := s.Store.Places(ctx, since)
	if err != nil {
		return internetReport{}, err
	}
	rep := internetReport{
		Devices: out, Total: total, Since: since, Seconds: int(now.Sub(since).Seconds()),
		Series: series, StepS: int(step.Seconds()), Now: s.rateNow(ctx, now),
		Countries: countries(places), Orgs: orgs(places), Home: s.home(),
		Listening: s.flowsListening(), Exporters: s.flowExporters(),
	}
	if s.Geo.Enabled() {
		rep.Attribution = geoip.Attribution
	}
	return rep, nil
}

// rateNow is the network's internet rate over the last five minutes (bits/s).
func (s *Server) rateNow(ctx context.Context, now time.Time) map[string]float64 {
	list, err := s.Store.InternetUsage(ctx, now.Add(-5*time.Minute), now)
	out := map[string]float64{"rx_bps": 0, "tx_bps": 0}
	if err != nil {
		return out
	}
	for _, u := range list {
		out["rx_bps"] += float64(u.RX) * 8 / 300
		out["tx_bps"] += float64(u.TX) * 8 / 300
	}
	return out
}

// home is where the network is: the country of a router's internet address.
func (s *Server) home() string {
	if s.Geo == nil {
		return ""
	}
	for _, a := range s.Collector.FlowDirectory().WANAddrs() {
		if c := s.Geo.Lookup(a).Country; c != "" {
			return c
		}
	}
	return ""
}

type countryUsage struct {
	Country string `json:"country"`
	RX      uint64 `json:"rx"`
	TX      uint64 `json:"tx"`
}

// countries sums the places per country, busiest first (unknown left out).
func countries(places []store.PlaceUsage) []countryUsage {
	by := map[string]*countryUsage{}
	for _, p := range places {
		if p.Country == "" {
			continue
		}
		c := by[p.Country]
		if c == nil {
			c = &countryUsage{Country: p.Country}
			by[p.Country] = c
		}
		c.RX += p.RX
		c.TX += p.TX
	}
	out := make([]countryUsage, 0, len(by))
	for _, c := range by {
		out = append(out, *c)
	}
	slices.SortFunc(out, func(a, b countryUsage) int {
		if c := cmp.Compare(b.RX+b.TX, a.RX+a.TX); c != 0 {
			return c
		}
		return cmp.Compare(a.Country, b.Country)
	})
	return out
}

type orgUsage struct {
	Org       string   `json:"org"`
	Countries []string `json:"countries"` // busiest first
	RX        uint64   `json:"rx"`
	TX        uint64   `json:"tx"`
}

// orgs sums the places per owner, busiest first (at most 20).
func orgs(places []store.PlaceUsage) []orgUsage {
	by := map[string]*orgUsage{}
	var order []string
	for _, p := range places { // busiest first already
		o := by[p.Org]
		if o == nil {
			o = &orgUsage{Org: p.Org, Countries: []string{}}
			by[p.Org] = o
			order = append(order, p.Org)
		}
		if p.Country != "" && !slices.Contains(o.Countries, p.Country) {
			o.Countries = append(o.Countries, p.Country)
		}
		o.RX += p.RX
		o.TX += p.TX
	}
	out := make([]orgUsage, 0, len(order))
	for _, k := range order {
		out = append(out, *by[k])
	}
	slices.SortStableFunc(out, func(a, b orgUsage) int { return cmp.Compare(b.RX+b.TX, a.RX+a.TX) })
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

// nodeDestinations is where a device went on the internet (by name when a
// DNS server Omini reads saw the lookup) over the last `hours` (1 to 24).
func (s *Server) nodeDestinations(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if v := r.URL.Query().Get("hours"); v != "" {
		h, err := strconv.Atoi(v)
		if err != nil || h < 1 || h > 24 {
			writeError(w, http.StatusBadRequest, "hours must be between 1 and 24")
			return
		}
		hours = h
	}
	list, err := s.Store.Destinations(r.Context(), time.Now().Add(-time.Duration(hours)*time.Hour), r.PathValue("id"))
	if err != nil {
		internalError(w, err)
		return
	}
	type dest struct {
		Name    string           `json:"name"`
		Country string           `json:"country,omitempty"`
		Org     string           `json:"org,omitempty"`
		RX      uint64           `json:"rx"`
		TX      uint64           `json:"tx"`
		Ports   []store.FlowPort `json:"ports"`
	}
	byName := map[string]*dest{}
	var order []string
	for _, d := range list {
		x := byName[d.Dest]
		if x == nil {
			x = &dest{Name: d.Dest}
			if a, err := netip.ParseAddr(d.Addr); err == nil {
				p := s.Geo.Lookup(a)
				x.Country, x.Org = p.Country, p.Org
			}
			byName[d.Dest] = x
			order = append(order, d.Dest)
		}
		x.RX += d.RX
		x.TX += d.TX
		if len(x.Ports) < 5 {
			x.Ports = append(x.Ports, store.FlowPort{Proto: d.Proto, Port: d.Port, Bytes: d.RX + d.TX})
		}
	}
	out := make([]dest, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name])
	}
	slices.SortStableFunc(out, func(a, b dest) int { return cmp.Compare(b.RX+b.TX, a.RX+a.TX) })
	if len(out) > 50 {
		out = out[:50]
	}
	writeJSON(w, http.StatusOK, map[string]any{"destinations": out, "hours": hours})
}

// nodeLabels are the map's nodes by id.
func nodeLabels(topo topology.Topology) map[string]string {
	out := make(map[string]string, len(topo.Nodes))
	for _, n := range topo.Nodes {
		out[n.ID] = n.Label
	}
	return out
}
