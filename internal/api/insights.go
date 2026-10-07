package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/riccardoalv/omini/internal/store"
)

// alerts lists the open alerts, plus those resolved in the last
// `resolved_hours` hours (up to 90 days).
func (s *Server) alerts(w http.ResponseWriter, r *http.Request) {
	var since time.Time
	if v := r.URL.Query().Get("resolved_hours"); v != "" {
		h, err := strconv.Atoi(v)
		if err != nil || h < 1 || h > 90*24 {
			writeError(w, http.StatusBadRequest, "resolved_hours must be between 1 and 2160")
			return
		}
		since = time.Now().Add(-time.Duration(h) * time.Hour)
	}
	list, err := s.Store.ListAlerts(r.Context(), since)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// dismissAlert hides an alert (or shows it again) until it is resolved.
func (s *Server) dismissAlert(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Dismissed bool `json:"dismissed"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Store.DismissAlert(r.Context(), id, body.Dismissed); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "alert not found")
			return
		}
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// presence lists the presence timeline, newest first: `node` for one device,
// `before` (an event id) for the next page, `first=1` for new devices only.
func (s *Server) presence(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pq := store.PresenceQuery{NodeID: q.Get("node"), First: q.Get("first") == "1"}
	for key, dst := range map[string]*int64{"before": &pq.BeforeID} {
		if v := q.Get(key); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				writeError(w, http.StatusBadRequest, "invalid "+key)
				return
			}
			*dst = n
		}
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 500")
			return
		}
		pq.Limit = n
	}
	list, err := s.Store.ListPresence(r.Context(), pq)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// trafficHistory returns one interface's traffic over the last `hours`
// (default 24; per minute up to a day, per hour up to a year). `iface` empty
// is the node's own traffic (a Wi-Fi client).
func (s *Server) trafficHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	nodeID := q.Get("node")
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, "node is required")
		return
	}
	hours := 24
	if v := q.Get("hours"); v != "" {
		h, err := strconv.Atoi(v)
		if err != nil || h < 1 || h > 365*24 {
			writeError(w, http.StatusBadRequest, "hours must be between 1 and 8760")
			return
		}
		hours = h
	}
	now := time.Now()
	points, err := s.Store.TrafficHistory(r.Context(), nodeID, q.Get("iface"), now.Add(-time.Duration(hours)*time.Hour), now)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, points)
}
