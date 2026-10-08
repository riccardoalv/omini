package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/riccardoalv/omini/internal/flows"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

type conversationView struct {
	store.Conversation
	ANode string `json:"a_node,omitempty"` // the map node of each address, when known
	BNode string `json:"b_node,omitempty"`
}

// flowsView lists who talks to whom over the last `minutes` (default 60, up
// to a day); `node` limits it to one device's conversations.
func (s *Server) flowsView(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	minutes := 60
	if v := q.Get("minutes"); v != "" {
		m, err := strconv.Atoi(v)
		if err != nil || m < 1 || m > 24*60 {
			writeError(w, http.StatusBadRequest, "minutes must be between 1 and 1440")
			return
		}
		minutes = m
	}
	limit := 200
	if v := q.Get("limit"); v != "" {
		l, err := strconv.Atoi(v)
		if err != nil || l < 1 || l > 1000 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 1000")
			return
		}
		limit = l
	}
	byIP := nodesByIP(s.Collector.State().Topology)
	ip := q.Get("ip")
	if node := q.Get("node"); node != "" {
		ip = ""
		for addr, id := range byIP {
			if id == node {
				ip = addr
				break
			}
		}
		if ip == "" {
			writeJSON(w, http.StatusOK, map[string]any{"conversations": []conversationView{}, "listening": s.flowsListening(), "exporters": s.flowExporters()})
			return
		}
	}
	list, err := s.Store.Conversations(r.Context(), time.Now().Add(-time.Duration(minutes)*time.Minute), ip, limit)
	if err != nil {
		internalError(w, err)
		return
	}
	out := make([]conversationView, 0, len(list))
	for _, c := range list {
		out = append(out, conversationView{Conversation: c, ANode: byIP[c.A], BNode: byIP[c.B]})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"conversations": out, "listening": s.flowsListening(), "exporters": s.flowExporters(),
	})
}

func (s *Server) flowsListening() bool { return s.Flows != nil && s.Flows.Listening() }

func (s *Server) flowExporters() []flows.Exporter {
	if s.Flows == nil {
		return []flows.Exporter{}
	}
	return s.Flows.Exporters()
}

// nodesByIP maps every address on the map to its node (a device's own
// addresses too, without their prefix length).
func nodesByIP(topo topology.Topology) map[string]string {
	out := map[string]string{}
	for _, n := range topo.Nodes {
		if n.Kind == topology.KindWAN || n.Kind == topology.KindApp {
			continue
		}
		if n.IP != "" {
			out[n.IP] = n.ID
		}
		if n.Device == nil {
			continue
		}
		for _, a := range n.Device.IPs {
			a, _, _ = strings.Cut(a, "/")
			if _, ok := out[a]; !ok {
				out[a] = n.ID
			}
		}
		for _, i := range n.Device.Interfaces {
			for _, a := range i.IPs {
				a, _, _ = strings.Cut(a, "/")
				if _, ok := out[a]; !ok {
					out[a] = n.ID
				}
			}
		}
	}
	return out
}
