package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/riccardoalv/omini/internal/auth"
)

// Read-only API tokens: Settings → API tokens creates them; they open the
// API's GET routes and the MCP server, never a change.

// bearerToken is the token of an "Authorization: Bearer" header.
func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	scheme, token, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	return strings.TrimSpace(token), true
}

// tokenMayRead is false for what a token may not open even with GET. Tokens are
// managed by the admin only, and a device's configuration backup may hold
// its passwords.
func tokenMayRead(path string) bool {
	switch {
	case path == "/api/tokens", strings.HasPrefix(path, "/api/tokens/"),
		strings.HasPrefix(path, "/api/backups/"):
		return false
	}
	return true
}

// authenticate tells whether the request carries a valid admin session or
// API token, writing the error when it does not. A token opens reads only.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) bool {
	if token, ok := bearerToken(r); ok {
		if _, err := s.Auth.AuthenticateAPIToken(r.Context(), token); err != nil {
			if !errors.Is(err, auth.ErrUnauthenticated) {
				internalError(w, err)
				return false
			}
			writeError(w, http.StatusUnauthorized, "invalid or revoked API token")
			return false
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusForbidden, "API tokens are read-only")
			return false
		}
		if !tokenMayRead(r.URL.Path) {
			writeError(w, http.StatusForbidden, "not available to API tokens")
			return false
		}
		return true
	}
	if _, err := s.Auth.Authenticate(r.Context(), sessionToken(r)); err != nil {
		writeError(w, http.StatusUnauthorized, "login required")
		return false
	}
	return true
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	list, err := s.Auth.ListAPITokens(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list) // the hash is never serialized
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	t, token, err := s.Auth.CreateAPIToken(r.Context(), body.Name)
	if errors.Is(err, auth.ErrTokenName) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	// The only time the token is sent: the store keeps its hash.
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": t.ID, "name": t.Name, "token": token, "created_at": t.CreatedAt,
	})
}

func (s *Server) deleteToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.Auth.RevokeAPIToken(r.Context(), id); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "token not found")
			return
		}
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
