package api_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakePluginRepo serves someone/omini-plugin-up on a fake GitHub: tags
// v1.0.0 and v1.1.0, the latest release being whatever latest() says.
func fakePluginRepo(t *testing.T) (*httptest.Server, func(string)) {
	t.Helper()
	var mu sync.Mutex
	latest := "v1.0.0"
	shas := map[string]string{"v1.0.0": strings.Repeat("a", 40), "v1.1.0": strings.Repeat("b", 40)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		const repo = "/repos/someone/omini-plugin-up"
		switch {
		case r.URL.Path == repo+"/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"` + latest + `"}`))
		case strings.HasPrefix(r.URL.Path, repo+"/commits/"):
			sha, ok := shas[strings.TrimPrefix(r.URL.Path, repo+"/commits/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"sha":"` + sha + `"}`))
		case strings.HasPrefix(r.URL.Path, "/someone/omini-plugin-up/tar.gz/"):
			for tag, sha := range shas {
				if strings.HasSuffix(r.URL.Path, sha) {
					_, _ = w.Write(pluginTarball(t, "id: up\nname: Up\nversion: "+strings.TrimPrefix(tag, "v")+"\nprotocol: 1\nentrypoint: main.sh\n"))
					return
				}
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func(tag string) { mu.Lock(); latest = tag; mu.Unlock() }
}

func pluginTarball(t *testing.T, manifest string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, body := range map[string]string{"x/plugin.yaml": manifest, "x/main.sh": `cat >/dev/null; echo '{"devices":[]}'`} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	tw.Close()
	zw.Close()
	return buf.Bytes()
}

func TestUpdatePlugins(t *testing.T) {
	h := newHarness(t, nil)
	gh, release := fakePluginRepo(t)
	h.plugs.GitHub, h.plugs.Codeload, h.plugs.HTTP = gh.URL, gh.URL, gh.Client()

	// The admin only.
	if code := h.do("GET", "/api/plugins/updates", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("updates without login: %d", code)
	}
	if code := h.do("POST", "/api/plugins/update-all", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("update-all without login: %d", code)
	}
	h.login()
	if code := h.do("POST", "/api/plugins", map[string]string{"url": "https://github.com/someone/omini-plugin-up"}, nil); code != http.StatusCreated {
		t.Fatalf("install: %d", code)
	}

	type update struct {
		ID, Name, Installed, Latest, Reason string
		Update                              bool
	}
	updates := func(path string) map[string]update {
		var list []update
		if code := h.do("GET", path, nil, &list); code != 200 {
			t.Fatalf("GET %s: %d", path, code)
		}
		out := map[string]update{}
		for _, u := range list {
			out[u.ID] = u
		}
		return out
	}
	got := updates("/api/plugins/updates")
	if u := got["up"]; u.Update || u.Installed != "v1.0.0" || u.Latest != "v1.0.0" || u.Name != "Up" {
		t.Fatalf("up to date: %+v", got)
	}
	if u := got["fakefw"]; u.Update || u.Reason != "dev" {
		t.Fatalf("development plugins are left alone: %+v", got)
	}

	release("v1.1.0")
	if u := updates("/api/plugins/updates")["up"]; u.Update {
		t.Fatalf("remembered for an hour: %+v", u)
	}
	if u := updates("/api/plugins/updates?refresh=1")["up"]; !u.Update || u.Latest != "v1.1.0" {
		t.Fatalf("refresh: %+v", u)
	}

	// Tokens read the list but never update; the session needs the CSRF header.
	_, token := h.newToken("reader")
	if code, body := h.bearer(token, "GET", "/api/plugins/updates", nil); code != 200 || !strings.Contains(body, `"update":true`) {
		t.Fatalf("token GET: %d %s", code, body)
	}
	if code, _ := h.bearer(token, "POST", "/api/plugins/update-all", nil); code != http.StatusForbidden {
		t.Fatalf("token POST: %d", code)
	}
	if code := h.do("POST", "/api/plugins/update-all", nil, nil, false); code != http.StatusForbidden {
		t.Fatalf("update-all without CSRF: %d", code)
	}

	var results []map[string]string
	if code := h.do("POST", "/api/plugins/update-all", nil, &results); code != 200 || len(results) != 1 ||
		results[0]["id"] != "up" || results[0]["from"] != "v1.0.0" || results[0]["to"] != "v1.1.0" || results[0]["error"] != "" {
		t.Fatalf("update-all: %d %v", code, results)
	}
	// The integration type is reloaded with the new version.
	var types []map[string]any
	h.do("GET", "/api/integration-types", nil, &types)
	found := false
	for _, ty := range types {
		found = found || ty["type"] == "up"
	}
	var list []map[string]any
	h.do("GET", "/api/plugins", nil, &list)
	for _, p := range list {
		if man := p["manifest"].(map[string]any); man["id"] == "up" && man["version"] != "1.1.0" {
			t.Fatalf("installed: %v", man)
		}
	}
	if !found {
		t.Fatalf("integration type up: %v", types)
	}
	if u := updates("/api/plugins/updates")["up"]; u.Update || u.Installed != "v1.1.0" {
		t.Fatalf("after: %+v", u)
	}
	// Nothing left: an empty result.
	if code := h.do("POST", "/api/plugins/update-all", nil, &results); code != 200 || len(results) != 0 {
		t.Fatalf("nothing to update: %d %v", code, results)
	}
}
