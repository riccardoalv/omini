package api

import (
	"cmp"
	"net/http"
	"slices"
	"strconv"
	"time"

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

// internetUsage is who used the internet over a period, busiest first
// (`range`: 15m, 1h, today, 7d or 30d).
func (s *Server) internetUsage(w http.ResponseWriter, r *http.Request) {
	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "15m"
	}
	from, ok := internetRanges[rng]
	if !ok {
		writeError(w, http.StatusBadRequest, "range must be 15m, 1h, today, 7d or 30d")
		return
	}
	now := time.Now()
	since := from(now)
	list, err := s.Store.InternetUsage(r.Context(), since, now)
	if err != nil {
		internalError(w, err)
		return
	}
	labels := nodeLabels(s.Collector.State().Topology)
	out := make([]usageView, 0, len(list))
	var total store.Usage
	for _, u := range list {
		v := usageView{Usage: u, Label: u.Node}
		if l, ok := labels[u.Node]; ok {
			v.Label, v.OnMap = l, true
		} else if len(u.Node) > 3 && u.Node[:3] == "ip:" {
			v.Label = u.Node[3:]
		}
		total.RX += u.RX
		total.TX += u.TX
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"devices": out, "total": total, "since": since, "seconds": int(now.Sub(since).Seconds()),
		"listening": s.flowsListening(), "exporters": s.flowExporters(),
	})
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
		Name  string           `json:"name"`
		RX    uint64           `json:"rx"`
		TX    uint64           `json:"tx"`
		Ports []store.FlowPort `json:"ports"`
	}
	byName := map[string]*dest{}
	var order []string
	for _, d := range list {
		x := byName[d.Dest]
		if x == nil {
			x = &dest{Name: d.Dest}
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
