package api

import (
	"net"
	"net/http"
)

// The Wi-Fi channels plan: what settles the access points' 2.4 GHz channel
// conflict, and applying it one access point at a time.

// remoteIP is the address of the browser asking (the proxy's behind one).
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// planChannels is the plan, telling the steps whose access point has the
// browser asking among its clients.
func (s *Server) planChannels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Collector.PlanChannels(remoteIP(r)))
}

// applyChannels applies the plan; a destructive step answers 409 like a
// change, an access point refusing 502 with what became of each step.
func (s *Server) applyChannels(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirm bool `json:"confirm"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx, cancel := changeContext(r)
	defer cancel()
	results, err := s.Collector.ApplyChannels(ctx, in.Confirm)
	switch {
	case err != nil && len(results) > 0:
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "results": results})
	case err != nil:
		s.changeError(w, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
	}
}
