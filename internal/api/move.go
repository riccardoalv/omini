package api

import (
	"errors"
	"maps"
	"net/http"
	"net/netip"
	"time"

	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/integration"
)

// moveIntegration points an integration at the address its device answers
// at now (DHCP gave it another one): the host part of its address field is
// replaced (scheme, port and path kept), the settings are saved through the
// same validation as an edit (secrets untouched), and the integration is
// collected again.
func (s *Server) moveIntegration(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		To string `json:"to"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	to, err := netip.ParseAddr(in.To)
	if err != nil || to.Zone() != "" {
		writeError(w, http.StatusBadRequest, "enter an IP address like 192.168.1.10")
		return
	}
	current, err := s.Store.GetIntegration(r.Context(), id)
	if isNotFound(err) {
		writeError(w, http.StatusNotFound, "integration not found")
		return
	} else if err != nil {
		internalError(w, err)
		return
	}
	impl, err := s.Registry.Get(current.Type)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	f, ok := integration.AddressField(impl.Info().Fields)
	if !ok || current.Config.String(f.Key) == "" {
		writeError(w, http.StatusBadRequest, "this integration has no address to change")
		return
	}
	value, err := integration.WithHost(current.Config.String(f.Key), to.Unmap().String())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := maps.Clone(current.Config)
	cfg[f.Key] = value
	sealed, err := s.prepare(current.Type, cfg, current.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current.Config = sealed
	updated, err := s.Store.UpdateIntegration(r.Context(), current)
	if err != nil {
		internalError(w, err)
		return
	}
	out := struct {
		Integration integrationView   `json:"integration"`
		Status      *collector.Status `json:"status,omitempty"`
	}{}
	if updated.Enabled {
		ctx, cancel := contextWithTimeout(r, 2*time.Minute)
		defer cancel()
		st, err := s.Collector.CollectIntegration(ctx, id)
		switch {
		case errors.Is(err, collector.ErrDisabled):
		case err != nil:
			internalError(w, err)
			return
		default:
			out.Status = &st
		}
	}
	out.Integration = s.view(updated, s.statuses())
	writeJSON(w, http.StatusOK, out)
}
