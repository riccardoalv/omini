package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/riccardoalv/omini/internal/diagnostics"
)

// diagnosticsBundle is the anonymized file a tester attaches to a bug
// report (Settings → Help us improve Omini).
func (s *Server) diagnosticsBundle(w http.ResponseWriter, r *http.Request) {
	all, err := s.Store.ListIntegrations(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	state := s.Collector.State()
	statuses := s.statuses()
	versions := map[string]string{}
	if s.Plugins != nil {
		for _, p := range s.Plugins.List() {
			versions[p.Manifest.ID] = p.Manifest.Version
		}
	}
	in := diagnostics.Input{Version: s.Version, GeneratedAt: time.Now().UTC().Format(time.RFC3339), Topology: state.Topology}
	if state.Round != nil {
		in.Round = state.Round
	}
	for _, i := range all {
		d := diagnostics.Integration{ID: i.ID, Type: i.Type, Enabled: i.Enabled, Plugin: versions[i.Type]}
		if st, ok := statuses[i.ID]; ok {
			d.Status = st
		}
		in.Integrations = append(in.Integrations, d)
	}
	for _, a := range state.Alerts {
		in.Alerts = append(in.Alerts, diagnostics.Alert{Rule: a.Rule, Severity: a.Severity, NodeID: a.NodeID, Params: a.Params})
	}
	out, err := diagnostics.Bundle(in)
	if err != nil {
		internalError(w, err)
		return
	}
	name := fmt.Sprintf("omini-diagnostics-%s.json", time.Now().UTC().Format("2006-01-02"))
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	writeJSON(w, http.StatusOK, out)
}
