package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/riccardoalv/omini/internal/api"
)

// bearer sends a request with an API token instead of the session.
func (h *harness) bearer(token, method, path string, body any) (int, string) {
	h.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(api.CSRFHeader, "1")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req) // no cookie jar: the token alone
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

func (h *harness) newToken(name string) (int64, string) {
	h.t.Helper()
	var created struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Token string `json:"token"`
	}
	if code := h.do("POST", "/api/tokens", map[string]string{"name": name}, &created); code != http.StatusCreated {
		h.t.Fatalf("create token: %d", code)
	}
	if created.Name != name || !strings.HasPrefix(created.Token, "omini_") {
		h.t.Fatalf("created %+v", created)
	}
	return created.ID, created.Token
}

func TestAPITokens(t *testing.T) {
	h := newHarness(t, nil)
	if code := h.do("GET", "/api/tokens", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("tokens need the admin: %d", code)
	}
	h.login()
	if code := h.do("POST", "/api/tokens", map[string]string{"name": ""}, nil); code != http.StatusBadRequest {
		t.Fatalf("a token needs a name: %d", code)
	}
	if code := h.do("POST", "/api/tokens", map[string]string{"name": "x"}, nil, false); code != http.StatusForbidden {
		t.Fatalf("creating a token needs the CSRF header: %d", code)
	}
	id, token := h.newToken("claude")

	var list []map[string]any
	if code := h.do("GET", "/api/tokens", nil, &list); code != 200 || len(list) != 1 {
		t.Fatalf("list: %d %v", code, list)
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), strings.TrimPrefix(token, "omini_")) || strings.Contains(string(raw), "hash") {
		t.Fatalf("the list never carries the secret: %s", raw)
	}
	if list[0]["name"] != "claude" || list[0]["last_used_at"] != nil {
		t.Fatalf("list entry %v", list[0])
	}

	// A token reads...
	if code, body := h.bearer(token, "GET", "/api/topology", nil); code != 200 {
		t.Fatalf("GET with a token: %d %s", code, body)
	}
	h.do("GET", "/api/tokens", nil, &list)
	if list[0]["last_used_at"] == nil {
		t.Fatalf("the use is recorded: %v", list[0])
	}
	// ...and never changes anything.
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/collection/run"},
		{"PATCH", "/api/me"},
		{"PUT", "/api/layout"},
		{"DELETE", "/api/layout"},
		{"POST", "/api/tokens"},
		{"DELETE", "/api/tokens/" + itoa(id)},
	} {
		if code, _ := h.bearer(token, c.method, c.path, map[string]any{}); code != http.StatusForbidden {
			t.Fatalf("%s %s with a token: %d", c.method, c.path, code)
		}
	}
	// Tokens and configuration backups stay with the admin.
	for _, p := range []string{"/api/tokens", "/api/backups/1"} {
		if code, _ := h.bearer(token, "GET", p, nil); code != http.StatusForbidden {
			t.Fatalf("GET %s with a token: %d", p, code)
		}
	}
	if code, _ := h.bearer("omini_wrong", "GET", "/api/topology", nil); code != http.StatusUnauthorized {
		t.Fatalf("a wrong token: %d", code)
	}

	if code := h.do("DELETE", "/api/tokens/"+itoa(id), nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code := h.do("DELETE", "/api/tokens/"+itoa(id), nil, nil); code != http.StatusNotFound {
		t.Fatalf("delete twice: %d", code)
	}
	if code, _ := h.bearer(token, "GET", "/api/topology", nil); code != http.StatusUnauthorized {
		t.Fatalf("a revoked token: %d", code)
	}
}
