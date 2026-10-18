package api_test

import (
	"net/http"
	"testing"
)

func TestWifiChannelsPlan(t *testing.T) {
	h := newHarness(t, nil, fakeRouter(t))
	h.login()
	var plan struct {
		Steps   []any `json:"steps"`
		Skipped []any `json:"skipped"`
	}
	if code := h.do("GET", "/api/wifi/channels", nil, &plan); code != http.StatusOK || plan.Steps == nil || plan.Skipped == nil {
		t.Fatalf("an empty plan: %d %+v", code, plan)
	}
	if code := h.do("POST", "/api/wifi/channels", map[string]any{"confirm": false}, nil); code != http.StatusBadRequest {
		t.Fatalf("nothing to move: %d", code)
	}
}
