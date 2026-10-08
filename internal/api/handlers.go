package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/riccardoalv/omini/internal/auth"
	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/plugins"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
	"github.com/riccardoalv/omini/internal/webui"
)

// --- auth ---

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Locale   string `json:"locale"` // setup only: the language chosen before the account existed
}

// locales are the UI languages; an empty locale means the browser default.
var locales = map[string]bool{"": true, "en": true, "pt-BR": true}

type userView struct {
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username"`
	Locale        string `json:"locale"`
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	required, err := s.Auth.SetupRequired(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	resp := map[string]any{"setup_required": required, "authenticated": false}
	if u, err := s.Auth.Authenticate(r.Context(), sessionToken(r)); err == nil {
		resp["authenticated"], resp["username"], resp["locale"] = true, u.Username, u.Locale
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) authSetup(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !readJSON(w, r, &c) {
		return
	}
	token, expires, err := s.Auth.Setup(r.Context(), c.Username, c.Password)
	switch {
	case errors.Is(err, auth.ErrAlreadySetUp):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	setSessionCookie(w, r, token, expires)
	s.signedIn(w, r, c.Username, c.Locale)
}

// signedIn answers a successful setup or login with the user's preferences.
// A locale sent at setup becomes the user's language.
func (s *Server) signedIn(w http.ResponseWriter, r *http.Request, username, locale string) {
	u, err := s.Store.GetUserByName(r.Context(), strings.TrimSpace(username))
	if err != nil {
		internalError(w, err)
		return
	}
	if locale != "" && locales[locale] && u.Locale == "" {
		if err := s.Store.SetUserLocale(r.Context(), u.ID, locale); err != nil {
			internalError(w, err)
			return
		}
		u.Locale = locale
	}
	writeJSON(w, http.StatusOK, userView{Authenticated: true, Username: u.Username, Locale: u.Locale})
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !readJSON(w, r, &c) {
		return
	}
	token, expires, err := s.Auth.Login(r.Context(), c.Username, c.Password)
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	case err != nil:
		internalError(w, err)
		return
	}
	setSessionCookie(w, r, token, expires)
	s.signedIn(w, r, c.Username, "")
}

// updateMe saves the signed-in user's preferences.
func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	u, err := s.Auth.Authenticate(r.Context(), sessionToken(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "login required")
		return
	}
	var in struct {
		Locale *string `json:"locale"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Locale != nil {
		if !locales[*in.Locale] {
			writeError(w, http.StatusBadRequest, "unknown language")
			return
		}
		if err := s.Store.SetUserLocale(r.Context(), u.ID, *in.Locale); err != nil {
			internalError(w, err)
			return
		}
		u.Locale = *in.Locale
	}
	writeJSON(w, http.StatusOK, userView{Authenticated: true, Username: u.Username, Locale: u.Locale})
}

// changePassword sets a new admin password; other sessions are signed out.
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	err := s.Auth.ChangePassword(r.Context(), sessionToken(r), in.Current, in.New)
	switch {
	case errors.Is(err, auth.ErrUnauthenticated):
		writeError(w, http.StatusUnauthorized, "login required")
	case errors.Is(err, auth.ErrWrongPassword):
		// Not 401: the session is fine, only the password typed is wrong.
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, auth.ErrPasswordTooShort):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		internalError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	if token := sessionToken(r); token != "" {
		if err := s.Auth.Logout(r.Context(), token); err != nil {
			internalError(w, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
}

// --- integrations ---

func (s *Server) integrationTypes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Registry.List())
}

type integrationView struct {
	store.Integration
	Status *collector.Status `json:"status,omitempty"`
}

func (s *Server) view(in store.Integration, statuses map[int64]collector.Status) integrationView {
	if impl, err := s.Registry.Get(in.Type); err == nil {
		in.Config = integration.MaskSecrets(impl.Info().Fields, in.Config)
	}
	v := integrationView{Integration: in}
	if st, ok := statuses[in.ID]; ok {
		v.Status = &st
	}
	return v
}

func (s *Server) statuses() map[int64]collector.Status {
	out := map[int64]collector.Status{}
	for _, st := range s.Collector.State().Statuses {
		out[st.IntegrationID] = st
	}
	return out
}

func (s *Server) listIntegrations(w http.ResponseWriter, r *http.Request) {
	all, err := s.Store.ListIntegrations(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	statuses := s.statuses()
	out := make([]integrationView, 0, len(all))
	for _, in := range all {
		out = append(out, s.view(in, statuses))
	}
	writeJSON(w, http.StatusOK, out)
}

// integrationInput is what the UI sends. An integration is named after its
// type (e.g. "Network scan"); names are not editable.
type integrationInput struct {
	Name    string             `json:"name"` // ignored: accepted so older clients keep working
	Type    string             `json:"type"`
	Config  integration.Config `json:"config"`
	Enabled *bool              `json:"enabled"`
}

// prepare validates the input config and seals its secrets. stored is the
// current config on updates (masked secrets keep their stored value).
func (s *Server) prepare(typ string, cfg, stored integration.Config) (integration.Config, error) {
	impl, err := s.Registry.Get(typ)
	if err != nil {
		return nil, err
	}
	fields := impl.Info().Fields
	if stored != nil {
		cfg = integration.KeepMaskedSecrets(fields, cfg, stored)
	}
	// Stored secrets are sealed; open them so validation sees real values.
	opened, err := integration.OpenSecrets(s.Box, fields, cfg)
	if err != nil {
		return nil, err
	}
	normalized, err := integration.Normalize(fields, opened)
	if err != nil {
		return nil, err
	}
	if v, ok := impl.(integration.Validator); ok {
		if err := v.Validate(normalized); err != nil {
			return nil, err
		}
	}
	return integration.SealSecrets(s.Box, fields, normalized)
}

func (s *Server) createIntegration(w http.ResponseWriter, r *http.Request) {
	var in integrationInput
	if !readJSON(w, r, &in) {
		return
	}
	cfg, err := s.prepare(in.Type, in.Config, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	impl, _ := s.Registry.Get(in.Type) // prepare checked the type
	if impl.Info().Single {
		all, err := s.Store.ListIntegrations(r.Context())
		if err != nil {
			internalError(w, err)
			return
		}
		for _, other := range all {
			if other.Type == in.Type {
				writeError(w, http.StatusConflict, impl.Info().Name+" is already added: there can be only one")
				return
			}
		}
	}
	enabled := in.Enabled == nil || *in.Enabled
	created, err := s.Store.CreateIntegration(r.Context(), store.Integration{
		Name: impl.Info().Name, Type: in.Type, Config: cfg, Enabled: enabled,
	})
	if err != nil {
		internalError(w, err)
		return
	}
	s.Collector.Refresh()
	writeJSON(w, http.StatusCreated, s.view(created, nil))
}

func (s *Server) updateIntegration(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
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
	var in integrationInput
	if !readJSON(w, r, &in) {
		return
	}
	if in.Type != "" && in.Type != current.Type {
		writeError(w, http.StatusBadRequest, "the integration type cannot be changed")
		return
	}
	if in.Config != nil {
		cfg, err := s.prepare(current.Type, in.Config, current.Config)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		current.Config = cfg
	}
	if in.Enabled != nil {
		current.Enabled = *in.Enabled
	}
	updated, err := s.Store.UpdateIntegration(r.Context(), current)
	if err != nil {
		internalError(w, err)
		return
	}
	s.Collector.Refresh()
	writeJSON(w, http.StatusOK, s.view(updated, s.statuses()))
}

func (s *Server) deleteIntegration(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.Store.DeleteIntegration(r.Context(), id); isNotFound(err) {
		writeError(w, http.StatusNotFound, "integration not found")
		return
	} else if err != nil {
		internalError(w, err)
		return
	}
	s.Collector.Refresh()
	w.WriteHeader(http.StatusNoContent)
}

// runIntegration collects one integration now and returns its new status.
func (s *Server) runIntegration(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx, cancel := contextWithTimeout(r, 2*time.Minute)
	defer cancel()
	st, err := s.Collector.CollectIntegration(ctx, id)
	switch {
	case isNotFound(err):
		writeError(w, http.StatusNotFound, "integration not found")
	case errors.Is(err, collector.ErrDisabled):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		internalError(w, err)
	default:
		writeJSON(w, http.StatusOK, st)
	}
}

type testInput struct {
	ID     int64              `json:"id"` // optional: test an existing integration with edited values
	Type   string             `json:"type"`
	Config integration.Config `json:"config"`
}

func (s *Server) testIntegration(w http.ResponseWriter, r *http.Request) {
	var in testInput
	if !readJSON(w, r, &in) {
		return
	}
	var stored integration.Config
	if in.ID != 0 {
		current, err := s.Store.GetIntegration(r.Context(), in.ID)
		if err != nil {
			writeError(w, http.StatusNotFound, "integration not found")
			return
		}
		in.Type, stored = current.Type, current.Config
	}
	impl, err := s.Registry.Get(in.Type)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sealed, err := s.prepare(in.Type, in.Config, stored)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg, err := integration.OpenSecrets(s.Box, impl.Info().Fields, sealed)
	if err != nil {
		internalError(w, err)
		return
	}
	ctx, cancel := contextWithTimeout(r, 20*time.Second)
	defer cancel()
	msg, err := impl.Test(ctx, cfg)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

// --- map ---

func (s *Server) topology(w http.ResponseWriter, r *http.Request) {
	layout, err := s.Store.GetLayout(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	areas, err := s.Store.ListAreas(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		collector.State
		Layout map[string]store.Point `json:"layout"`
		Areas  []store.Area           `json:"areas"`
	}{s.Collector.State(), layout, areas})
}

func (s *Server) refresh(w http.ResponseWriter, _ *http.Request) {
	s.Collector.Refresh()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "refresh scheduled"})
}

func (s *Server) saveLayout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Positions map[string]store.Point `json:"positions"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.Store.SaveLayout(r.Context(), in.Positions); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) resetLayout(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.ResetLayout(r.Context()); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- plugins ---

// pluginView is an installed plugin with its trust: curated plugins keep the
// catalog's, any other repository is unverified.
type pluginView struct {
	*plugins.Plugin
	Publisher string `json:"publisher"`
	Trust     string `json:"trust"`
}

func (s *Server) listPlugins(w http.ResponseWriter, _ *http.Request) {
	out := []pluginView{}
	if s.Plugins != nil {
		for _, p := range s.Plugins.List() {
			v := pluginView{Plugin: p, Publisher: "community", Trust: "unverified"}
			if p.Source != nil {
				if e, ok := plugins.CatalogFor(p.Source.URL); ok {
					v.Publisher, v.Trust = e.Publisher, e.Trust
				}
			}
			out = append(out, v)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// pluginCatalog lists the curated plugins and whether each is installed.
func (s *Server) pluginCatalog(w http.ResponseWriter, _ *http.Request) {
	type entry struct {
		plugins.CatalogEntry
		Installed bool   `json:"installed"`
		Version   string `json:"version,omitempty"`
	}
	out := []entry{}
	for _, e := range plugins.Catalog() {
		v := entry{CatalogEntry: e}
		if s.Plugins != nil {
			if p, ok := s.Plugins.Get(e.ID); ok {
				v.Installed, v.Version = true, p.Manifest.Version
			}
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

// pluginIndex tells where the store's index comes from and when it was fetched.
func (s *Server) pluginIndex(w http.ResponseWriter, _ *http.Request) {
	if s.PluginIndex == nil {
		writeJSON(w, http.StatusOK, plugins.IndexStatus{Plugins: len(plugins.Catalog())})
		return
	}
	writeJSON(w, http.StatusOK, s.PluginIndex.Status())
}

// refreshPluginIndex fetches the remote index now.
func (s *Server) refreshPluginIndex(w http.ResponseWriter, r *http.Request) {
	if s.PluginIndex == nil || s.PluginIndex.URL == "" {
		writeError(w, http.StatusConflict, "no remote plugin index is configured (OMINI_PLUGIN_INDEX)")
		return
	}
	ctx, cancel := contextWithTimeout(r, 30*time.Second)
	defer cancel()
	if err := s.PluginIndex.Refresh(ctx); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.PluginIndex.Status())
}

// installPlugin installs (or updates) a plugin from its GitHub repository and
// makes it available as an integration type right away.
func (s *Server) installPlugin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL     string `json:"url"`
		Version string `json:"version"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if s.Plugins == nil {
		writeError(w, http.StatusServiceUnavailable, "plugins are not available")
		return
	}
	ctx, cancel := contextWithTimeout(r, 10*time.Minute)
	defer cancel()
	p, err := s.Plugins.Install(ctx, in.URL, strings.TrimSpace(in.Version))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.Registry.Register(s.Plugins.Integration(p))
	writeJSON(w, http.StatusCreated, p)
}

// removePlugin uninstalls a plugin that no integration uses anymore.
func (s *Server) removePlugin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.Plugins == nil {
		writeError(w, http.StatusNotFound, "plugin not found")
		return
	}
	all, err := s.Store.ListIntegrations(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	for _, in := range all {
		if in.Type == id {
			writeError(w, http.StatusConflict, "delete the integrations that use this plugin first")
			return
		}
	}
	switch err := s.Plugins.Remove(id); {
	case errors.Is(err, fs.ErrNotExist):
		writeError(w, http.StatusNotFound, "plugin not found")
	case errors.Is(err, plugins.ErrDevPlugin):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		internalError(w, err)
	default:
		s.Registry.Unregister(id)
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- map areas ---

func (s *Server) createArea(w http.ResponseWriter, r *http.Request) {
	var in store.Area
	if !readJSON(w, r, &in) {
		return
	}
	a, err := s.Store.CreateArea(r.Context(), in)
	if errors.Is(err, store.ErrInvalidArea) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	} else if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) updateArea(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in store.AreaUpdate
	if !readJSON(w, r, &in) {
		return
	}
	a, err := s.Store.UpdateArea(r.Context(), id, in)
	switch {
	case isNotFound(err):
		writeError(w, http.StatusNotFound, "area not found")
	case errors.Is(err, store.ErrInvalidArea):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		internalError(w, err)
	default:
		writeJSON(w, http.StatusOK, a)
	}
}

func (s *Server) deleteArea(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.Store.DeleteArea(r.Context(), id)
	switch {
	case isNotFound(err):
		writeError(w, http.StatusNotFound, "area not found")
	case err != nil:
		internalError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- inventory ---

func (s *Server) listInventory(w http.ResponseWriter, r *http.Request) {
	inv, err := s.Store.ListInventory(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	if inv == nil {
		inv = []store.InventoryEntry{}
	}
	current := map[string]topology.Node{}
	for _, n := range s.Collector.State().Topology.Nodes {
		current[n.ID] = n
	}
	// Classification comes from the current map, when the device is on it.
	type entry struct {
		store.InventoryEntry
		Online  bool   `json:"online"`
		Role    string `json:"role,omitempty"`
		Type    string `json:"type,omitempty"`
		OS      string `json:"os,omitempty"`
		Brand   string `json:"brand,omitempty"`
		Product string `json:"product,omitempty"`
	}
	out := make([]entry, 0, len(inv))
	for _, e := range inv {
		n := current[e.ID]
		out = append(out, entry{e, n.Online, n.Role, n.Type, n.OS, n.Brand, n.Product})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) updateInventory(w http.ResponseWriter, r *http.Request) {
	var in store.InventoryUpdate
	if !readJSON(w, r, &in) {
		return
	}
	e, err := s.Store.UpdateInventory(r.Context(), r.PathValue("id"), in)
	if isNotFound(err) {
		writeError(w, http.StatusNotFound, "device not found")
		return
	} else if err != nil {
		internalError(w, err)
		return
	}
	s.Collector.Refresh()
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) deleteInventory(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.Store.DeleteInventory(r.Context(), in.IDs); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- ports ---

// setPortLabel saves the user's description of a device port (an empty one
// restores the description reported by the device).
func (s *Server) setPortLabel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Label string `json:"label"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	err := s.Store.SetPortLabel(r.Context(), r.PathValue("id"), r.PathValue("port"), in.Label)
	switch {
	case errors.Is(err, store.ErrInvalidPortLabel):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		internalError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- web interfaces ---

// nodeWeb returns the web interfaces of a node on the map. Only IPs of nodes
// on the map are probed, so this endpoint cannot be used to scan arbitrary hosts.
func (s *Server) nodeWeb(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var ip string
	for _, n := range s.Collector.State().Topology.Nodes {
		if n.ID == id {
			ip = n.IP
			break
		}
	}
	if ip == "" {
		writeJSON(w, http.StatusOK, []webui.Service{})
		return
	}
	ctx, cancel := contextWithTimeout(r, 15*time.Second)
	defer cancel()
	services, err := s.WebUI.Find(ctx, ip)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if services == nil {
		services = []webui.Service{}
	}
	writeJSON(w, http.StatusOK, services)
}

// HostScanner is implemented by integrations that can scan one device on
// demand (the nmap integration).
type HostScanner interface {
	ScanHost(ctx context.Context, cfg integration.Config, ip string) (model.Host, error)
}

// scanNode scans one device on the map with the nmap integration. Only nodes
// on the map can be scanned, so it cannot be used to scan arbitrary hosts.
func (s *Server) scanNode(w http.ResponseWriter, r *http.Request) {
	var ip string
	for _, n := range s.Collector.State().Topology.Nodes {
		if n.ID == r.PathValue("id") {
			ip = n.IP
			break
		}
	}
	if ip == "" {
		writeError(w, http.StatusNotFound, "this device has no address on the map")
		return
	}
	all, err := s.Store.ListIntegrations(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	for _, in := range all {
		impl, err := s.Registry.Get(in.Type)
		if err != nil || !in.Enabled {
			continue
		}
		scanner, ok := impl.(HostScanner)
		if !ok {
			continue
		}
		cfg, err := integration.OpenSecrets(s.Box, impl.Info().Fields, in.Config)
		if err != nil {
			internalError(w, err)
			return
		}
		ctx, cancel := contextWithTimeout(r, 6*time.Minute) // nmap gives up on the host after 5
		defer cancel()
		host, err := scanner.ScanHost(ctx, cfg, ip)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		if err := s.Collector.CollectOne(r.Context(), in.ID); err != nil {
			internalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, host)
		return
	}
	writeError(w, http.StatusConflict, "add the nmap integration to scan devices")
}

// --- UI ---

const placeholderPage = `<!doctype html><html><head><meta charset="utf-8"><title>Omini</title></head>
<body style="font-family:sans-serif;max-width:40rem;margin:4rem auto;line-height:1.5">
<h1>Omini</h1><p>The API is running, but the web UI has not been built.</p>
<p>Run <code>make web</code> (or use the Docker image) and restart.</p></body></html>`

// ui serves the single-page app: real files when they exist, index.html otherwise.
func (s *Server) ui() http.Handler {
	if s.UI == nil {
		return placeholder()
	}
	if _, err := fs.Stat(s.UI, "index.html"); err != nil {
		return placeholder()
	}
	files := http.FileServerFS(s.UI)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(s.UI, path); err == nil {
				if strings.HasPrefix(path, "assets/") { // hashed file names
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, s.UI, "index.html")
	})
}

func placeholder() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(placeholderPage))
	})
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

// --- collection rounds ---

type collectionInfo struct {
	IntervalS int `json:"interval_s"`
	// DefaultS is the interval used when none is set (OMINI_POLL_INTERVAL).
	DefaultS int              `json:"default_s"`
	Round    *collector.Round `json:"round,omitempty"`
}

func (s *Server) collectionInfo(r *http.Request) collectionInfo {
	return collectionInfo{
		IntervalS: int(s.Collector.RoundInterval(r.Context()) / time.Second),
		DefaultS:  int(s.Collector.DefaultInterval() / time.Second),
		Round:     s.Collector.State().Round,
	}
}

func (s *Server) getCollection(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.collectionInfo(r))
}

// setCollection changes the time between collection rounds (0: the default).
func (s *Server) setCollection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IntervalS int `json:"interval_s"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	d := time.Duration(in.IntervalS) * time.Second
	if in.IntervalS != 0 && (d < collector.MinRoundInterval || d > collector.MaxRoundInterval) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("the round interval must be between %d seconds and %d hours",
			int(collector.MinRoundInterval/time.Second), int(collector.MaxRoundInterval/time.Hour)))
		return
	}
	if err := s.Collector.SetRoundInterval(r.Context(), d); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.collectionInfo(r))
}
