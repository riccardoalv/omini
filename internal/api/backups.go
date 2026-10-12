package api

import (
	"net/http"
	"strconv"

	"github.com/riccardoalv/omini/internal/store"
)

// Configuration backups: each device's versions, one version's content, and
// a backup now. The content holds secrets: only the signed-in admin sees it,
// and it never reaches the logs.

func (s *Server) listBackups(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.Backups(r.Context(), r.PathValue("id"))
	if err != nil {
		internalError(w, err)
		return
	}
	if list == nil {
		list = []store.Backup{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) getBackup(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	b, content, err := s.Collector.BackupContent(r.Context(), id)
	if isNotFound(err) {
		writeError(w, http.StatusNotFound, "backup not found")
		return
	} else if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		store.Backup
		Content string `json:"content"`
	}{b, content})
}

func (s *Server) backupNow(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	ctx, cancel := changeContext(r)
	defer cancel()
	if err := s.Collector.BackupNow(ctx, id); isNotFound(err) {
		writeError(w, http.StatusNotFound, "integration not found")
		return
	} else if err != nil {
		s.changeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
