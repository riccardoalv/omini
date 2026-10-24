package api_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
)

// switchPlugin is a development plugin read by host, with a password.
func switchPlugin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte("id: fakesw\nname: Fake switch\nversion: 1.0.0\nprotocol: 1\nentrypoint: main.sh\nfields:\n  - {key: host, type: host, required: true}\n  - {key: password, type: secret, required: true}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "main.sh"), []byte(`cat >/dev/null; echo '{"devices":[{"key":"sw","name":"sw","role":"switch"}]}'`), 0o644)
	return dir
}

func TestMoveIntegration(t *testing.T) {
	h := newHarness(t, nil, switchPlugin(t))
	h.login()
	create := func(typ string, cfg map[string]any) string {
		t.Helper()
		var created map[string]any
		if code := h.do("POST", "/api/integrations", map[string]any{"type": typ, "config": cfg}, &created); code != http.StatusCreated {
			t.Fatalf("create %s: %d %v", typ, code, created)
		}
		return itoa(int64(created["id"].(float64)))
	}
	type result struct {
		Integration struct {
			Config map[string]any `json:"config"`
		} `json:"integration"`
		Status *struct {
			OK bool `json:"ok"`
		} `json:"status"`
	}
	move := func(id, to string) (int, result) {
		t.Helper()
		var out result
		code := h.do("POST", "/api/integrations/"+id+"/move", map[string]string{"to": to}, &out)
		return code, out
	}

	// A plain host, with a password that stays as it was.
	sw := create("fakesw", map[string]any{"host": "192.168.1.96", "password": "s3cret"})
	swID, _ := strconv.ParseInt(sw, 10, 64)
	code, out := move(sw, "192.168.1.110")
	if code != 200 || out.Integration.Config["host"] != "192.168.1.110" || out.Integration.Config["password"] != integration.Masked {
		t.Fatalf("move host: %d %+v", code, out)
	}
	if out.Status == nil || !out.Status.OK {
		t.Fatalf("the integration is collected again: %+v", out.Status)
	}
	stored, _ := h.store.GetIntegration(context.Background(), swID)
	opened, err := integration.OpenSecrets(h.box, []model.FormField{{Key: "password", Type: model.FormFieldTypeSecret}}, stored.Config)
	if err != nil || opened["password"] != "s3cret" {
		t.Fatalf("password: %v %v", opened, err)
	}
	// A URL keeps its scheme, port and path; IPv6 goes in brackets.
	fw := create("fakefw", map[string]any{"url": "http://192.168.1.1:8443/api"})
	if code, out := move(fw, "192.168.1.2"); code != 200 || out.Integration.Config["url"] != "http://192.168.1.2:8443/api" {
		t.Fatalf("move url: %d %+v", code, out)
	}
	if code, out := move(fw, "fd00::2"); code != 200 || out.Integration.Config["url"] != "http://[fd00::2]:8443/api" {
		t.Fatalf("move url to IPv6: %d %+v", code, out)
	}

	for _, bad := range []string{"", "switch.lan", "1.2.3.4/24", "fe80::1%eth0"} {
		if code, _ := move(sw, bad); code != http.StatusBadRequest {
			t.Errorf("%q: %d, want 400", bad, code)
		}
	}
	if code, _ := move("999", "192.168.1.5"); code != http.StatusNotFound {
		t.Errorf("unknown integration: %d", code)
	}
	demo := create("demo", map[string]any{})
	if code, _ := move(demo, "192.168.1.5"); code != http.StatusBadRequest {
		t.Errorf("an integration without an address: %d", code)
	}
	// The CSRF header and the admin's session are needed: tokens only read.
	if code := h.do("POST", "/api/integrations/"+sw+"/move", map[string]string{"to": "192.168.1.5"}, nil, false); code != http.StatusForbidden {
		t.Errorf("without the CSRF header: %d", code)
	}
	_, token := h.newToken("claude")
	if code, body := h.bearer(token, "POST", "/api/integrations/"+sw+"/move", map[string]string{"to": "192.168.1.5"}); code != http.StatusForbidden {
		t.Errorf("with a token: %d %s", code, body)
	}
	h.do("POST", "/api/auth/logout", nil, nil)
	if code := h.do("POST", "/api/integrations/"+sw+"/move", map[string]string{"to": "192.168.1.5"}, nil); code != http.StatusUnauthorized {
		t.Errorf("logged out: %d", code)
	}
}
