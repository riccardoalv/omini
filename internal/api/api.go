// Package api exposes Omini's HTTP API and serves the web UI.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/riccardoalv/omini/internal/appicons"
	"github.com/riccardoalv/omini/internal/auth"
	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/secret"
	"github.com/riccardoalv/omini/internal/snmp"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/webui"
)

const sessionCookie = "omini_session"

// CSRFHeader must be sent with every state-changing request. Browsers do not
// let other sites set custom headers without CORS, which Omini never allows.
const CSRFHeader = "X-Omini-Request"

// Discoverer scans a subnet for devices (snmp.Discover in production).
type Discoverer func(ctx context.Context, cidr string, opts snmp.ScanOptions) ([]snmp.Found, error)

// WebFinder detects web interfaces on an IP (*webui.Prober in production).
type WebFinder interface {
	Find(ctx context.Context, ip string) ([]webui.Service, error)
}

type Server struct {
	Store     *store.Store
	Registry  *integration.Registry
	Box       *secret.Box
	Collector *collector.Collector
	Auth      *auth.Service
	Discover  Discoverer
	WebUI     WebFinder
	Icons     http.Handler // app icons (internal/appicons); public, used by <img>
	UI        fs.FS        // built web UI; nil serves a placeholder page
	Version   string
}

// Handler returns the HTTP handler with all routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public endpoints.
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/auth/status", s.authStatus)
	mux.HandleFunc("POST /api/auth/setup", s.authSetup)
	mux.HandleFunc("POST /api/auth/login", s.authLogin)
	mux.HandleFunc("POST /api/auth/logout", s.authLogout)
	if s.Icons != nil {
		mux.Handle("GET /api/icons/{name}", s.Icons)
		mux.HandleFunc("GET /api/icons", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Cache-Control", "public, max-age=86400")
			writeJSON(w, http.StatusOK, appicons.Names())
		})
	}

	// Authenticated endpoints.
	private := http.NewServeMux()
	private.HandleFunc("PATCH /api/me", s.updateMe)
	private.HandleFunc("GET /api/integration-types", s.integrationTypes)
	private.HandleFunc("GET /api/integrations", s.listIntegrations)
	private.HandleFunc("POST /api/integrations", s.createIntegration)
	private.HandleFunc("PUT /api/integrations/{id}", s.updateIntegration)
	private.HandleFunc("DELETE /api/integrations/{id}", s.deleteIntegration)
	private.HandleFunc("POST /api/integrations/test", s.testIntegration)
	private.HandleFunc("POST /api/integrations/{id}/run", s.runIntegration)
	private.HandleFunc("GET /api/topology", s.topology)
	private.HandleFunc("POST /api/refresh", s.refresh)
	private.HandleFunc("GET /api/inventory", s.listInventory)
	private.HandleFunc("PATCH /api/inventory/{id}", s.updateInventory)
	private.HandleFunc("POST /api/inventory/delete", s.deleteInventory)
	private.HandleFunc("PUT /api/layout", s.saveLayout)
	private.HandleFunc("DELETE /api/layout", s.resetLayout)
	private.HandleFunc("POST /api/areas", s.createArea)
	private.HandleFunc("PATCH /api/areas/{id}", s.updateArea)
	private.HandleFunc("DELETE /api/areas/{id}", s.deleteArea)
	private.HandleFunc("POST /api/discovery/scan", s.scan)
	private.HandleFunc("GET /api/nodes/{id}/web", s.nodeWeb)
	mux.Handle("/api/", s.requireAuth(private))

	mux.Handle("/", s.ui())
	return s.csrf(mux)
}

// --- middleware ---

func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get(CSRFHeader) == "" {
				writeError(w, http.StatusForbidden, "missing "+CSRFHeader+" header")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.Auth.Authenticate(r.Context(), sessionToken(r)); err != nil {
			writeError(w, http.StatusUnauthorized, "login required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sessionToken(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: expires,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil,
	})
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// internalError logs the real error and returns a generic message.
func internalError(w http.ResponseWriter, err error) {
	slog.Error("api error", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON body: %v", err))
		return false
	}
	return true
}

func isNotFound(err error) bool { return errors.Is(err, store.ErrNotFound) }

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.Version})
}

func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
