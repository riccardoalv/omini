package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/store"
)

func TestNotifiers(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	var got []string
	hook := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, string(b))
	}))
	defer hook.Close()

	var types []struct {
		Type string `json:"type"`
	}
	if h.do("GET", "/api/notifier-types", nil, &types); len(types) != 3 {
		t.Fatalf("types: %+v", types)
	}

	// Invalid settings are refused with a readable message.
	var e map[string]string
	if code := h.do("POST", "/api/notifiers", map[string]any{"type": "webhook", "config": map[string]any{"url": "ftp://x"}}, &e); code != http.StatusBadRequest || !strings.Contains(e["error"], "http") {
		t.Fatalf("bad url: %d %v", code, e)
	}

	var n store.Notifier
	code := h.do("POST", "/api/notifiers", map[string]any{
		"type": "webhook", "config": map[string]any{"url": hook.URL, "secret": "shh"}, "min_severity": "critical",
	}, &n)
	if code != http.StatusCreated || n.Config["secret"] != integration.Masked || n.MinSeverity != "critical" || !n.Enabled {
		t.Fatalf("create: %d %+v", code, n)
	}
	// The secret is stored sealed.
	stored, _ := h.store.GetNotifier(t.Context(), n.ID)
	if s, _ := stored.Config["secret"].(string); s == "shh" || s == "" {
		t.Fatalf("secret stored as %q", s)
	}

	// Update keeps the masked secret; test sends a message.
	if code := h.do("PUT", "/api/notifiers/"+itoa(n.ID), map[string]any{"config": map[string]any{"url": hook.URL, "secret": integration.Masked}, "enabled": false}, &n); code != 200 || n.Enabled {
		t.Fatalf("update: %d %+v", code, n)
	}
	var res map[string]any
	if h.do("POST", "/api/notifiers/test", map[string]any{"id": n.ID}, &res); res["ok"] != true || len(got) != 1 {
		t.Fatalf("test: %v %v", res, got)
	}
	var msg struct{ Subject string }
	_ = json.Unmarshal([]byte(got[0]), &msg)
	if msg.Subject != "Omini test message" {
		t.Fatalf("test message: %s", got[0])
	}
	if h.do("POST", "/api/notifiers/test", map[string]any{"type": "telegram", "config": map[string]any{}}, &res); res["ok"] != false {
		t.Fatalf("test of an empty telegram channel: %v", res)
	}

	var list []store.Notifier
	if h.do("GET", "/api/notifiers", nil, &list); len(list) != 1 || list[0].Config["secret"] != integration.Masked {
		t.Fatalf("list: %+v", list)
	}
	if code := h.do("DELETE", "/api/notifiers/"+itoa(n.ID), nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code := h.do("DELETE", "/api/notifiers/"+itoa(n.ID), nil, nil); code != http.StatusNotFound {
		t.Fatalf("delete again: %d", code)
	}
}
