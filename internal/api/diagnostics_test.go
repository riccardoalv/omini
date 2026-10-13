package api_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDiagnosticsBundleIsAnonymized(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	h.do("POST", "/api/integrations", map[string]any{"name": "demo", "type": "demo", "config": map[string]any{}}, nil)
	if err := h.coll.CollectNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if code := h.do("GET", "/api/diagnostics", nil, &out); code != 200 {
		t.Fatalf("diagnostics: %d", code)
	}
	raw, _ := json.Marshal(out)
	s := string(raw)
	// The demo NAS: its vendor part stays, the rest of its MAC and the TV.s name go.
	for _, gone := range []string{"00:11:32:aa:00:01", "living-room-tv"} {
		if strings.Contains(s, gone) {
			i := strings.Index(s, gone)
			t.Errorf("bundle holds %s: ...%s...", gone, s[max(0, i-200):min(len(s), i+60)])
		}
	}
	for _, kept := range []string{`"00:11:32:`, `"192.168.1.10"`, `"demo"`, `"topology"`} {
		if !strings.Contains(s, kept) {
			t.Errorf("bundle lost %s", kept)
		}
	}
}
