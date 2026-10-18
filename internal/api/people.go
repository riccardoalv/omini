package api

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

// personView is a person with their devices: home while one of their
// phones, tablets or wearables is online.
type personView struct {
	store.Person
	Devices  []string   `json:"devices"`
	Home     bool       `json:"home"`
	LastSeen *time.Time `json:"last_seen,omitempty"` // the latest of their devices
}

// people are the people with their devices.
func (s *Server) people(r *http.Request) ([]personView, error) {
	list, err := s.Store.ListPeople(r.Context())
	if err != nil {
		return nil, err
	}
	inv, err := s.Store.ListInventory(r.Context())
	if err != nil {
		return nil, err
	}
	home := topology.PeopleHome(s.Collector.State().Topology.Nodes)
	out := make([]personView, 0, len(list))
	for _, p := range list {
		v := personView{Person: p, Devices: []string{}, Home: home[p.ID]}
		for _, e := range inv {
			if e.OwnerID != p.ID {
				continue
			}
			v.Devices = append(v.Devices, e.ID)
			if v.LastSeen == nil || e.LastSeen.After(*v.LastSeen) {
				seen := e.LastSeen
				v.LastSeen = &seen
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) listPeople(w http.ResponseWriter, r *http.Request) {
	out, err := s.people(r)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// personError answers a store error about a person.
func personError(w http.ResponseWriter, err error) {
	switch {
	case isNotFound(err):
		writeError(w, http.StatusNotFound, "person not found")
	case errors.Is(err, store.ErrPersonName), errors.Is(err, store.ErrPersonColor):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrPersonExists):
		writeError(w, http.StatusConflict, err.Error())
	default:
		internalError(w, err)
	}
}

func (s *Server) createPerson(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	p, err := s.Store.CreatePerson(r.Context(), in.Name, in.Color, time.Now())
	if err != nil {
		personError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, personView{Person: p, Devices: []string{}})
}

func personID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func (s *Server) updatePerson(w http.ResponseWriter, r *http.Request) {
	id, ok := personID(w, r)
	if !ok {
		return
	}
	var in struct {
		Name  *string `json:"name"`
		Color *string `json:"color"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	p, err := s.Store.UpdatePerson(r.Context(), id, in.Name, in.Color)
	if err != nil {
		personError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deletePerson(w http.ResponseWriter, r *http.Request) {
	id, ok := personID(w, r)
	if !ok {
		return
	}
	if err := s.Store.DeletePerson(r.Context(), id); err != nil {
		personError(w, err)
		return
	}
	s.Collector.Refresh()
	w.WriteHeader(http.StatusNoContent)
}

// personResult is what pausing or resuming did to one of the person's
// devices: done, skipped (it cannot be paused, or is already as asked) or
// failed (the router refused).
type personResult struct {
	NodeID string              `json:"node_id"`
	Status string              `json:"status"`
	Error  string              `json:"error,omitempty"`
	Change *store.ConfigChange `json:"change,omitempty"`
}

func (s *Server) pausePerson(w http.ResponseWriter, r *http.Request)  { s.pauseOwned(w, r, true) }
func (s *Server) resumePerson(w http.ResponseWriter, r *http.Request) { s.pauseOwned(w, r, false) }

// pauseOwned pauses (or resumes) every device of the person that can be,
// one change per device through the device's own pause: Omini's host,
// routers and network gear are skipped, and one device failing does not
// stop the others.
func (s *Server) pauseOwned(w http.ResponseWriter, r *http.Request, pause bool) {
	id, ok := personID(w, r)
	if !ok {
		return
	}
	if _, err := s.Store.GetPerson(r.Context(), id); err != nil {
		personError(w, err)
		return
	}
	inv, err := s.Store.ListInventory(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	var ids []string
	for _, e := range inv {
		if e.OwnerID == id {
			ids = append(ids, e.ID)
		}
	}
	slices.Sort(ids)
	out := make([]personResult, 0, len(ids))
	for _, node := range ids {
		res := personResult{NodeID: node}
		switch {
		case pause && s.Collector.Paused(node), !pause && !s.Collector.Paused(node):
			res.Status = "skipped"
		case pause:
			if _, can := s.Collector.CanPause(node); !can {
				res.Status = "skipped"
				break
			}
			res.Status, res.Error, res.Change = s.personChange(r, node, s.Collector.Pause)
		default:
			res.Status, res.Error, res.Change = s.personChange(r, node, s.Collector.Resume)
		}
		out = append(out, res)
	}
	writeJSON(w, http.StatusOK, out)
}

// personChange applies one device's pause or resume.
func (s *Server) personChange(r *http.Request, node string, apply func(context.Context, string) (store.ConfigChange, error)) (string, string, *store.ConfigChange) {
	ctx, cancel := changeContext(r)
	defer cancel()
	rec, err := apply(ctx, node)
	if errors.Is(err, collector.ErrNotChangeable) {
		return "skipped", err.Error(), nil
	} else if err != nil {
		return "failed", err.Error(), nil
	}
	return "done", "", &rec
}

// personUsage is a person's internet use: the sum of their devices.
type personUsage struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	RX   uint64 `json:"rx"`
	TX   uint64 `json:"tx"`
}

// peopleUsage sums the devices' use per owner, busiest first (people who
// used nothing are left out).
func peopleUsage(people []store.Person, owners map[string]int64, devices []usageView) []personUsage {
	by := map[int64]*personUsage{}
	for _, p := range people {
		by[p.ID] = &personUsage{ID: p.ID, Name: p.Name}
	}
	for _, d := range devices {
		if p := by[owners[d.Node]]; p != nil {
			p.RX += d.RX
			p.TX += d.TX
		}
	}
	out := []personUsage{}
	for _, p := range by {
		if p.RX+p.TX > 0 {
			out = append(out, *p)
		}
	}
	slices.SortFunc(out, func(a, b personUsage) int {
		if c := cmp.Compare(b.RX+b.TX, a.RX+a.TX); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return out
}
