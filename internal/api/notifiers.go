package api

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/notify"
	"github.com/riccardoalv/omini/internal/store"
)

func (s *Server) notifierTypes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, notify.Kinds)
}

func maskNotifier(n store.Notifier) store.Notifier {
	if k, err := notify.KindOf(n.Type); err == nil {
		n.Config = integration.MaskSecrets(k.Fields, n.Config)
	}
	return n
}

func (s *Server) listNotifiers(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListNotifiers(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	for i := range list {
		list[i] = maskNotifier(list[i])
	}
	writeJSON(w, http.StatusOK, list)
}

type notifierInput struct {
	Type           string             `json:"type"`
	Config         integration.Config `json:"config"`
	MinSeverity    string             `json:"min_severity"`
	NotifyResolved *bool              `json:"notify_resolved"`
	Enabled        *bool              `json:"enabled"`
}

// prepareNotifier validates a channel's settings (by building its sender) and
// returns them with secrets sealed and with secrets opened.
func (s *Server) prepareNotifier(typ string, cfg, stored integration.Config) (sealed, opened integration.Config, err error) {
	kind, err := notify.KindOf(typ)
	if err != nil {
		return nil, nil, err
	}
	if stored != nil {
		cfg = integration.KeepMaskedSecrets(kind.Fields, cfg, stored)
	}
	if opened, err = integration.OpenSecrets(s.Box, kind.Fields, cfg); err != nil {
		return nil, nil, err
	}
	if opened, err = integration.Normalize(kind.Fields, opened); err != nil {
		return nil, nil, err
	}
	if _, err = notify.New(typ, opened, nil); err != nil {
		return nil, nil, err
	}
	sealed, err = integration.SealSecrets(s.Box, kind.Fields, opened)
	return sealed, opened, err
}

// rememberAddress keeps the address the admin opens Omini with, for the
// links in messages, unless one was set already.
func (s *Server) rememberAddress(r *http.Request) {
	if cur := notify.BaseURL(r.Context(), s.Store); cur != "" {
		return
	}
	if u := originOf(r); u != "" {
		_ = s.Store.SetSetting(r.Context(), notify.URLSetting, u)
	}
}

// originOf is the browser's address of Omini: the Origin header (sent with
// every POST and PUT), else the Referer's scheme and host.
func originOf(r *http.Request) string {
	for _, h := range []string{r.Header.Get("Origin"), r.Header.Get("Referer")} {
		u, err := url.Parse(h)
		if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
	}
	return ""
}

// notifierSettings: Omini's address for the links in messages.
func (s *Server) notifierSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"public_url": notify.BaseURL(r.Context(), s.Store)})
}

func (s *Server) setNotifierSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PublicURL string `json:"public_url"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	v := strings.TrimRight(strings.TrimSpace(in.PublicURL), "/")
	if v != "" {
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			writeError(w, http.StatusBadRequest, "the address must start with http:// or https://")
			return
		}
	}
	if err := s.Store.SetSetting(r.Context(), notify.URLSetting, v); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"public_url": v})
}

func validSeverity(v string) bool { return v == "critical" || v == "warning" || v == "info" }

func (s *Server) createNotifier(w http.ResponseWriter, r *http.Request) {
	var in notifierInput
	if !readJSON(w, r, &in) {
		return
	}
	s.rememberAddress(r)
	if in.MinSeverity == "" {
		in.MinSeverity = "warning"
	}
	if !validSeverity(in.MinSeverity) {
		writeError(w, http.StatusBadRequest, "min_severity must be critical, warning or info")
		return
	}
	sealed, _, err := s.prepareNotifier(in.Type, in.Config, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := s.Store.CreateNotifier(r.Context(), store.Notifier{
		Type: in.Type, Config: sealed, MinSeverity: in.MinSeverity,
		NotifyResolved: in.NotifyResolved == nil || *in.NotifyResolved, Enabled: in.Enabled == nil || *in.Enabled,
	})
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, maskNotifier(n))
}

func (s *Server) updateNotifier(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	cur, err := s.Store.GetNotifier(r.Context(), id)
	if isNotFound(err) {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	} else if err != nil {
		internalError(w, err)
		return
	}
	var in notifierInput
	if !readJSON(w, r, &in) {
		return
	}
	if in.Config != nil {
		sealed, _, err := s.prepareNotifier(cur.Type, in.Config, cur.Config)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cur.Config = sealed
	}
	if in.MinSeverity != "" {
		if !validSeverity(in.MinSeverity) {
			writeError(w, http.StatusBadRequest, "min_severity must be critical, warning or info")
			return
		}
		cur.MinSeverity = in.MinSeverity
	}
	if in.NotifyResolved != nil {
		cur.NotifyResolved = *in.NotifyResolved
	}
	if in.Enabled != nil {
		cur.Enabled = *in.Enabled
	}
	n, err := s.Store.UpdateNotifier(r.Context(), cur)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, maskNotifier(n))
}

func (s *Server) deleteNotifier(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.Store.DeleteNotifier(r.Context(), id); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// testNotifier sends a test message with the given settings (a saved
// channel's when id is set; masked secrets keep their saved value).
func (s *Server) testNotifier(w http.ResponseWriter, r *http.Request) {
	s.rememberAddress(r)
	var in struct {
		ID     int64              `json:"id"`
		Type   string             `json:"type"`
		Config integration.Config `json:"config"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	var stored integration.Config
	if in.ID != 0 {
		cur, err := s.Store.GetNotifier(r.Context(), in.ID)
		if err != nil {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		in.Type, stored = cur.Type, cur.Config
		if in.Config == nil {
			in.Config = cur.Config
		}
	}
	_, opened, err := s.prepareNotifier(in.Type, in.Config, stored)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	msg := notify.TestMessage(s.Store.AdminLocale(r.Context()), time.Now(), notify.BaseURL(r.Context(), s.Store))
	if err := notify.SendWith(r.Context(), in.Type, opened, s.NotifyClient, msg); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
