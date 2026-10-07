package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"golang.org/x/crypto/bcrypt"

	"github.com/riccardoalv/omini/internal/api"
	"github.com/riccardoalv/omini/internal/auth"
	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/demo"
	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/netscan"
	"github.com/riccardoalv/omini/internal/secret"
	"github.com/riccardoalv/omini/internal/snmp"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/webui"
)

func init() { auth.BcryptCost = bcrypt.MinCost }

// fakeWeb records the probed IPs and reports a web interface on the firewall.
type fakeWeb struct{ probed []string }

func (f *fakeWeb) Find(_ context.Context, ip string) ([]webui.Service, error) {
	f.probed = append(f.probed, ip)
	if ip == "192.168.1.1" {
		return []webui.Service{{URL: "https://192.168.1.1/", Port: 443, Title: "OPNsense"}}, nil
	}
	return nil, nil
}

type harness struct {
	t      *testing.T
	srv    *httptest.Server
	client *http.Client
	store  *store.Store
	coll   *collector.Collector
	box    *secret.Box
	web    *fakeWeb
}

func newHarness(t *testing.T, ui *fstest.MapFS) *harness {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := secret.New(make([]byte, 32))
	reg := integration.NewRegistry()
	reg.Register(demo.New())
	reg.Register(snmp.New())
	reg.Register(netscan.New())
	coll := collector.New(st, reg, box, collector.Options{})

	web := &fakeWeb{}
	icons := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("icon:" + r.PathValue("name")))
	})
	s := &api.Server{
		Icons: icons,
		Store: st, Registry: reg, Box: box, Collector: coll, Auth: auth.New(st, 0), Version: "test", WebUI: web,
		Discover: func(_ context.Context, cidr string, opts snmp.ScanOptions) ([]snmp.Found, error) {
			if cidr != "192.168.1.0/24" || opts.Community != "public" {
				return nil, nil
			}
			return []snmp.Found{{IP: "192.168.1.2", Name: "sw-core"}, {IP: "192.168.1.9", Name: "printer"}}, nil
		},
	}
	if ui != nil {
		s.UI = ui
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &harness{t: t, srv: srv, client: &http.Client{Jar: jar}, store: st, coll: coll, box: box, web: web}
}

// do sends a request (with the CSRF header unless csrf is false) and decodes the JSON response into out.
func (h *harness) do(method, path string, body any, out any, csrf ...bool) int {
	h.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	if len(csrf) == 0 || csrf[0] {
		req.Header.Set(api.CSRFHeader, "1")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			h.t.Fatalf("%s %s: decode: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

func (h *harness) login() {
	h.t.Helper()
	if code := h.do("POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "correct horse"}, nil); code != 200 {
		h.t.Fatalf("setup: %d", code)
	}
}

func TestAuthFlow(t *testing.T) {
	h := newHarness(t, nil)

	var status map[string]any
	h.do("GET", "/api/auth/status", nil, &status)
	if status["setup_required"] != true || status["authenticated"] != false {
		t.Fatalf("first run status: %v", status)
	}
	if code := h.do("GET", "/api/integrations", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("protected endpoint without login: %d", code)
	}

	h.login()
	h.do("GET", "/api/auth/status", nil, &status)
	if status["authenticated"] != true || status["username"] != "admin" {
		t.Fatalf("after setup: %v", status)
	}
	if code := h.do("POST", "/api/auth/setup", map[string]string{"username": "x", "password": "another one"}, nil); code != http.StatusConflict {
		t.Fatalf("second setup: %d", code)
	}

	h.do("POST", "/api/auth/logout", nil, nil)
	if code := h.do("GET", "/api/integrations", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("after logout: %d", code)
	}
	if code := h.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "wrong!!!"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", code)
	}
	if code := h.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse"}, nil); code != 200 {
		t.Fatalf("login: %d", code)
	}
	if code := h.do("GET", "/api/integrations", nil, nil); code != 200 {
		t.Fatalf("after login: %d", code)
	}
}

func TestCSRFHeaderRequired(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	if code := h.do("POST", "/api/refresh", nil, nil, false); code != http.StatusForbidden {
		t.Fatalf("POST without CSRF header: %d, want 403", code)
	}
	if code := h.do("POST", "/api/auth/setup", map[string]string{"username": "a", "password": "bbbbbbbb"}, nil, false); code != http.StatusForbidden {
		t.Fatalf("public POST without CSRF header: %d, want 403", code)
	}
}

func TestIntegrationLifecycleKeepsSecretsSecret(t *testing.T) {
	h := newHarness(t, nil)
	h.login()

	var types []integration.Info
	h.do("GET", "/api/integration-types", nil, &types)
	if len(types) != 3 {
		t.Fatalf("integration types: %+v", types)
	}

	var created map[string]any
	code := h.do("POST", "/api/integrations", map[string]any{
		"name": "core switch", "type": "snmp", "config": map[string]any{"host": "192.168.1.2", "community": "s3cret"},
	}, &created)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, created)
	}
	cfg := created["config"].(map[string]any)
	if cfg["community"] != integration.Masked || cfg["port"] != float64(161) || created["enabled"] != true {
		t.Fatalf("created integration: %v", created)
	}
	id := int64(created["id"].(float64))

	// Stored sealed, never in clear text.
	stored, _ := h.store.GetIntegration(context.Background(), id)
	if c := stored.Config.String("community"); c == "s3cret" || !secret.IsSealed(c) {
		t.Fatalf("community stored as %q", c)
	}

	// Updating with the masked value keeps the stored secret.
	var updated map[string]any
	code = h.do("PUT", "/api/integrations/"+itoa(id), map[string]any{
		"name": "sw-core", "config": map[string]any{"host": "192.168.1.3", "community": integration.Masked},
	}, &updated)
	if code != 200 || updated["name"] != "sw-core" {
		t.Fatalf("update: %d %v", code, updated)
	}
	stored, _ = h.store.GetIntegration(context.Background(), id)
	if plain, _ := h.box.Open(stored.Config.String("community")); plain != "s3cret" || stored.Config.String("host") != "192.168.1.3" {
		t.Fatalf("after update: community=%q host=%q", plain, stored.Config.String("host"))
	}

	var list []map[string]any
	h.do("GET", "/api/integrations", nil, &list)
	if len(list) != 1 || list[0]["config"].(map[string]any)["community"] != integration.Masked {
		t.Fatalf("list leaks secrets or is wrong: %v", list)
	}

	if code := h.do("DELETE", "/api/integrations/"+itoa(id), nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code := h.do("DELETE", "/api/integrations/"+itoa(id), nil, nil); code != http.StatusNotFound {
		t.Fatalf("delete again: %d", code)
	}
}

func TestIntegrationValidation(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	cases := []map[string]any{
		{"name": "x", "type": "nope", "config": map[string]any{}},
		{"name": "x", "type": "snmp", "config": map[string]any{}},                         // host missing
		{"name": "", "type": "demo", "config": map[string]any{}},                          // name missing
		{"name": "x", "type": "snmp", "config": map[string]any{"host": "h", "port": "x"}}, // bad type
		{"name": "x", "type": "network", "config": map[string]any{"ports": "80-70"}},      // integration validation
	}
	for _, c := range cases {
		var resp map[string]string
		if code := h.do("POST", "/api/integrations", c, &resp); code != http.StatusBadRequest || resp["error"] == "" {
			t.Errorf("create %v: %d %v", c, code, resp)
		}
	}
	var created map[string]any
	h.do("POST", "/api/integrations", map[string]any{"name": "demo", "type": "demo", "config": map[string]any{}}, &created)
	if code := h.do("PUT", "/api/integrations/"+itoa(int64(created["id"].(float64))), map[string]any{"type": "snmp"}, nil); code != http.StatusBadRequest {
		t.Errorf("changing the type should fail: %d", code)
	}
}

func TestRunIntegrationNow(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	var created map[string]any
	h.do("POST", "/api/integrations", map[string]any{"name": "demo", "type": "demo", "config": map[string]any{}}, &created)
	id := itoa(int64(created["id"].(float64)))

	var st map[string]any
	if code := h.do("POST", "/api/integrations/"+id+"/run", nil, &st); code != 200 {
		t.Fatalf("run: %d %v", code, st)
	}
	if st["ok"] != true || st["devices"] != float64(3) {
		t.Fatalf("status = %v", st)
	}
	if code := h.do("POST", "/api/integrations/999/run", nil, nil); code != http.StatusNotFound {
		t.Fatalf("unknown integration: %d", code)
	}
	h.do("PUT", "/api/integrations/"+id, map[string]any{"enabled": false}, nil)
	if code := h.do("POST", "/api/integrations/"+id+"/run", nil, nil); code != http.StatusConflict {
		t.Fatalf("disabled integration: %d", code)
	}
}

func TestConnectionTest(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	var resp map[string]any
	h.do("POST", "/api/integrations/test", map[string]any{"type": "demo", "config": map[string]any{}}, &resp)
	if resp["ok"] != true || resp["message"] != "Demo network ready" {
		t.Fatalf("demo test: %v", resp)
	}
	h.do("POST", "/api/integrations/test", map[string]any{"type": "snmp", "config": map[string]any{"host": "127.0.0.1", "port": 9}}, &resp)
	if resp["ok"] != false || resp["error"] == "" {
		t.Fatalf("unreachable SNMP should fail with a message: %v", resp)
	}
}

func TestTopologyInventoryAndLayout(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	h.do("POST", "/api/integrations", map[string]any{"name": "demo", "type": "demo", "config": map[string]any{}}, nil)
	if err := h.coll.CollectNow(context.Background()); err != nil {
		t.Fatal(err)
	}

	var topo struct {
		Topology struct {
			Nodes []map[string]any `json:"nodes"`
			Edges []map[string]any `json:"edges"`
		} `json:"topology"`
		Statuses []map[string]any          `json:"statuses"`
		Layout   map[string]map[string]any `json:"layout"`
	}
	h.do("GET", "/api/topology", nil, &topo)
	if len(topo.Topology.Nodes) != 27 || len(topo.Statuses) != 1 || topo.Layout == nil {
		t.Fatalf("topology: %d nodes, %d statuses, layout=%v", len(topo.Topology.Nodes), len(topo.Statuses), topo.Layout)
	}

	nas := "mac:00:11:32:aa:00:01"
	var entry map[string]any
	if code := h.do("PATCH", "/api/inventory/"+url.PathEscape(nas), map[string]any{"alias": "My NAS", "pinned": true}, &entry); code != 200 {
		t.Fatalf("patch inventory: %d %v", code, entry)
	}
	if entry["alias"] != "My NAS" || entry["pinned"] != true {
		t.Fatalf("inventory entry: %v", entry)
	}
	var inv []map[string]any
	h.do("GET", "/api/inventory", nil, &inv)
	if len(inv) != 27 || inv[0]["online"] != true {
		t.Fatalf("inventory: %d entries, first=%v", len(inv), inv[0])
	}
	roles := map[string]any{}
	for _, e := range inv {
		roles[e["id"].(string)] = e["role"]
	}
	if roles["dev:00:e0:4c:68:00:02"] != "firewall" || roles[nas] != "client" {
		t.Fatalf("inventory roles: firewall=%v nas=%v", roles["dev:00:e0:4c:68:00:02"], roles[nas])
	}
	types := map[string]any{}
	for _, e := range inv {
		types[e["id"].(string)] = e["type"]
	}
	if types["dev:00:e0:4c:68:00:02"] != "firewall" {
		t.Fatalf("inventory roles: firewall=%v nas=%v", roles["dev:00:e0:4c:68:00:02"], roles[nas])
	}
	if code := h.do("POST", "/api/inventory/delete", map[string]any{"ids": []string{nas}}, nil); code != http.StatusNoContent {
		t.Fatalf("delete inventory: %d", code)
	}

	if code := h.do("PUT", "/api/layout", map[string]any{"positions": map[string]any{nas: map[string]float64{"x": 10, "y": 20}}}, nil); code != http.StatusNoContent {
		t.Fatalf("save layout: %d", code)
	}
	h.do("GET", "/api/topology", nil, &topo)
	if topo.Layout[nas]["x"] != float64(10) {
		t.Fatalf("layout not saved: %v", topo.Layout)
	}
	h.do("DELETE", "/api/layout", nil, nil)
	var afterReset struct {
		Layout map[string]any `json:"layout"`
	}
	h.do("GET", "/api/topology", nil, &afterReset)
	if len(afterReset.Layout) != 0 {
		t.Fatalf("layout not reset: %v", afterReset.Layout)
	}
}

func TestDiscoveryFlagsIntegratedHosts(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	h.do("POST", "/api/integrations", map[string]any{"name": "sw", "type": "snmp", "config": map[string]any{"host": "192.168.1.2"}}, nil)
	var found []map[string]any
	if code := h.do("POST", "/api/discovery/scan", map[string]any{"cidr": "192.168.1.0/24", "community": "public"}, &found); code != 200 {
		t.Fatalf("scan: %d", code)
	}
	if len(found) != 2 || found[0]["integrated"] != true || found[1]["integrated"] != false {
		t.Fatalf("scan results: %v", found)
	}
}

func TestNodeWebInterfaces(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	h.do("POST", "/api/integrations", map[string]any{"name": "demo", "type": "demo", "config": map[string]any{}}, nil)
	if err := h.coll.CollectNow(context.Background()); err != nil {
		t.Fatal(err)
	}

	var services []webui.Service
	if code := h.do("GET", "/api/nodes/"+url.PathEscape("dev:00:e0:4c:68:00:02")+"/web", nil, &services); code != 200 {
		t.Fatalf("status %d", code)
	}
	if len(services) != 1 || services[0].Title != "OPNsense" {
		t.Fatalf("services: %+v", services)
	}

	// Unknown nodes are never probed: the endpoint cannot scan arbitrary hosts.
	var none []webui.Service
	h.do("GET", "/api/nodes/"+url.PathEscape("mac:de:ad:be:ef:00:01")+"/web", nil, &none)
	if len(none) != 0 || len(h.web.probed) != 1 || h.web.probed[0] != "192.168.1.1" {
		t.Fatalf("unexpected probes: %v (result %v)", h.web.probed, none)
	}
}

func TestIconsArePublic(t *testing.T) {
	h := newHarness(t, nil)
	resp, err := h.client.Get(h.srv.URL + "/api/icons/jellyfin.svg") // not logged in
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "icon:jellyfin.svg" {
		t.Fatalf("icons: %d %q", resp.StatusCode, body)
	}
	var names []string
	if code := h.do("GET", "/api/icons", nil, &names); code != 200 || len(names) < 2000 {
		t.Fatalf("catalog: %d, %d names", code, len(names))
	}
}

func TestUI(t *testing.T) {
	h := newHarness(t, nil)
	resp, _ := h.client.Get(h.srv.URL + "/")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "web UI has not been built") {
		t.Fatalf("placeholder page not served: %s", body)
	}

	ui := &fstest.MapFS{
		"index.html":    {Data: []byte("<html>app</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	h = newHarness(t, ui)
	for path, want := range map[string]string{"/": "app", "/devices": "app", "/assets/app.js": "console.log(1)"} {
		resp, _ := h.client.Get(h.srv.URL + path)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if !strings.Contains(string(body), want) {
			t.Errorf("GET %s = %q, want %q", path, body, want)
		}
	}
	var health map[string]string
	h.do("GET", "/api/health", nil, &health)
	if health["status"] != "ok" {
		t.Fatalf("health: %v", health)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestMapAreas(t *testing.T) {
	h := newHarness(t, nil)
	h.login()

	var a map[string]any
	in := map[string]any{"name": "Rack", "color": "blue", "direction": "RIGHT", "x": 0, "y": 0, "width": 400, "height": 300}
	if code := h.do("POST", "/api/areas", in, &a); code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, a)
	}
	id := fmt.Sprint(a["id"])

	var topo struct {
		Areas []map[string]any `json:"areas"`
	}
	h.do("GET", "/api/topology", nil, &topo)
	if len(topo.Areas) != 1 || topo.Areas[0]["name"] != "Rack" {
		t.Fatalf("topology areas: %v", topo.Areas)
	}

	var updated map[string]any
	if code := h.do("PATCH", "/api/areas/"+id, map[string]any{"name": "Server rack", "x": 30}, &updated); code != http.StatusOK {
		t.Fatalf("update: %d", code)
	}
	if updated["name"] != "Server rack" || updated["x"] != float64(30) || updated["width"] != float64(400) {
		t.Fatalf("updated: %v", updated)
	}
	if code := h.do("PATCH", "/api/areas/"+id, map[string]any{"color": "pink"}, nil); code != http.StatusBadRequest {
		t.Fatalf("invalid color: %d", code)
	}
	if code := h.do("POST", "/api/areas", map[string]any{"name": "x", "color": "blue", "direction": "UP", "width": 100, "height": 100}, nil); code != http.StatusBadRequest {
		t.Fatalf("invalid direction: %d", code)
	}

	// Resetting the layout keeps the areas: they are the user's own content.
	h.do("DELETE", "/api/layout", nil, nil)
	h.do("GET", "/api/topology", nil, &topo)
	if len(topo.Areas) != 1 {
		t.Fatalf("areas after layout reset: %v", topo.Areas)
	}

	if code := h.do("DELETE", "/api/areas/"+id, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code := h.do("DELETE", "/api/areas/"+id, nil, nil); code != http.StatusNotFound {
		t.Fatalf("delete again: %d", code)
	}
}

func TestUserLanguageIsSaved(t *testing.T) {
	h := newHarness(t, nil)

	// The language picked on the setup screen becomes the user's language.
	var me map[string]any
	if code := h.do("POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "correct horse", "locale": "pt-BR"}, &me); code != 200 {
		t.Fatalf("setup: %d", code)
	}
	if me["locale"] != "pt-BR" {
		t.Fatalf("setup response: %v", me)
	}

	if code := h.do("PATCH", "/api/me", map[string]string{"locale": "en"}, &me); code != 200 || me["locale"] != "en" {
		t.Fatalf("update: %d %v", code, me)
	}
	if code := h.do("PATCH", "/api/me", map[string]string{"locale": "xx"}, nil); code != http.StatusBadRequest {
		t.Fatalf("unknown language: %d", code)
	}

	// Another browser: the login brings the saved language.
	h.do("POST", "/api/auth/logout", nil, nil)
	h.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse"}, &me)
	if me["locale"] != "en" || me["username"] != "admin" {
		t.Fatalf("login response: %v", me)
	}
	var status map[string]any
	h.do("GET", "/api/auth/status", nil, &status)
	if status["locale"] != "en" {
		t.Fatalf("status: %v", status)
	}

	h.do("POST", "/api/auth/logout", nil, nil)
	if code := h.do("PATCH", "/api/me", map[string]string{"locale": "en"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("update without login: %d", code)
	}
}
