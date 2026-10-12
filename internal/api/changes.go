package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/model"
)

// maxChangeTime bounds a change: waiting for a round in progress, then the
// plugin (at most 300 s).
const maxChangeTime = 10 * time.Minute

// applyChange applies a configuration change to a node's device. A
// destructive one answers 409 with why (reason, node) and whether it may be
// confirmed a second time (allowed); the device refusing it answers 502 with
// the change as kept in the history.
func (s *Server) applyChange(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind    model.ChangeKind `json:"kind"`
		Port    string           `json:"port"`
		Value   any              `json:"value"`
		Confirm bool             `json:"confirm"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	// Not cancelled with the request: a closed tab must never stop a change
	// between applying and saving it (the plugin has its own time limit).
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), maxChangeTime)
	defer cancel()
	rec, err := s.Collector.Apply(ctx, collector.ChangeRequest{
		NodeID: r.PathValue("id"), Kind: in.Kind, Port: in.Port, Value: in.Value, Confirmed: in.Confirm,
	})
	var de *collector.DestructiveError
	switch {
	case errors.As(err, &de):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "destructive", "reason": de.Reason, "node": de.Node, "allowed": de.Allowed,
		})
	case errors.Is(err, collector.ErrNotChangeable):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, collector.ErrInvalidChange):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil && rec.ID != 0:
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "change": rec})
	case err != nil:
		internalError(w, err)
	default:
		writeJSON(w, http.StatusOK, rec)
	}
}

// configChanges lists the latest changes made to a node.
func (s *Server) configChanges(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ConfigChanges(r.Context(), r.PathValue("id"), 50)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) changeSettings(w http.ResponseWriter, r *http.Request) {
	domain, _, err := s.Store.GetSetting(r.Context(), collector.DNSDomainSetting)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"allow_destructive": s.Collector.AllowDestructive(r.Context()),
		"dns_domain":        domain,
	})
}

func (s *Server) setChangeSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AllowDestructive *bool   `json:"allow_destructive"`
		DNSDomain        *string `json:"dns_domain"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.AllowDestructive != nil {
		if err := s.Collector.SetAllowDestructive(r.Context(), *in.AllowDestructive); err != nil {
			internalError(w, err)
			return
		}
	}
	if in.DNSDomain != nil {
		d := strings.Trim(strings.ToLower(strings.TrimSpace(*in.DNSDomain)), ".")
		if d != "" && !collector.ValidDomain(d) {
			writeError(w, http.StatusBadRequest, "a domain is labels of letters, digits and dashes (home.arpa, lan)")
			return
		}
		if err := s.Store.SetSetting(r.Context(), collector.DNSDomainSetting, d); err != nil {
			internalError(w, err)
			return
		}
		s.Collector.Refresh() // the names Omini set follow after the round
	}
	s.changeSettings(w, r)
}

// dnsName is a device's local DNS name, as its panel shows it.
func (s *Server) dnsName(w http.ResponseWriter, r *http.Request) {
	name, err := s.Collector.DNSNameOf(r.Context(), r.PathValue("id"))
	if err != nil {
		s.changeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, name)
}

// setDNSName changes a device's name: {name} ("" its own, "-" none).
func (s *Server) setDNSName(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx, cancel := changeContext(r)
	defer cancel()
	name, err := s.Collector.SetDNSName(ctx, r.PathValue("id"), in.Name)
	if err != nil {
		s.changeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, name)
}

// hostActions says what the device's panel can offer: waking it (and how)
// and pausing its internet (on which router).
func (s *Server) hostActions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	router, canPause := s.Collector.CanPause(id)
	out := map[string]any{"wake": s.Collector.CanWake(id), "can_pause": canPause}
	if canPause {
		out["pause_router"] = router
	}
	writeJSON(w, http.StatusOK, out)
}

// wake sends the device a Wake-on-LAN packet.
func (s *Server) wake(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := changeContext(r)
	defer cancel()
	rec, err := s.Collector.Wake(ctx, r.PathValue("id"))
	if err != nil {
		s.changeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// pause cuts the device's internet on its router.
func (s *Server) pause(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := changeContext(r)
	defer cancel()
	rec, err := s.Collector.Pause(ctx, r.PathValue("id"))
	if err != nil {
		s.changeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// resume gives it back.
func (s *Server) resume(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := changeContext(r)
	defer cancel()
	rec, err := s.Collector.Resume(ctx, r.PathValue("id"))
	if err != nil {
		s.changeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}
