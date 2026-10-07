package plugins

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
)

const manifest = `id: fake
name: Fake router
version: 1.0.0
protocol: 1
entrypoint: main.sh
timeout_s: 5
fields:
  - {key: host, type: host, required: true}
`

// A plugin written in sh: it answers according to the config it receives.
const script = `req=$(cat)
case "$req" in
  *'"action":"test"'*) echo '{"message":"Connected to fake"}' ;;
  *'"host":"broken"'*) echo '{"error":"wrong API key"}'; exit 1 ;;
  *'"host":"slow"'*) sleep 30 ;;
  *'"host":"silent"'*) exit 3 ;;
  *) echo '{"devices":[{"key":"fw","name":"fw","interfaces":[{"name":"igb0","speed_mbps":1000,"media":"1000baseT <full-duplex>"}]}]}' ;;
esac
`

func tarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	tw.Close()
	zw.Close()
	return buf.Bytes()
}

const (
	releaseSHA = "1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	mainSHA    = "2222222bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// fakeGitHub serves the API and tarballs of someone/omini-plugin-fake: the
// release v1.0.0 (unless noRelease) and the main branch.
func fakeGitHub(t *testing.T, files map[string]string, noRelease ...bool) *httptest.Server {
	t.Helper()
	tgz := tarball(t, files)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const repo = "/repos/someone/omini-plugin-fake"
		switch r.URL.Path {
		case repo + "/releases/latest":
			if len(noRelease) > 0 && noRelease[0] {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"tag_name":"v1.0.0"}`))
		case repo:
			_, _ = w.Write([]byte(`{"default_branch":"main"}`))
		case repo + "/commits/v1.0.0":
			_, _ = w.Write([]byte(`{"sha":"` + releaseSHA + `"}`))
		case repo + "/commits/main":
			_, _ = w.Write([]byte(`{"sha":"` + mainSHA + `"}`))
		case "/someone/omini-plugin-fake/tar.gz/" + releaseSHA, "/someone/omini-plugin-fake/tar.gz/" + mainSHA:
			_, _ = w.Write(tgz)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newManager(t *testing.T, gh *httptest.Server) *Manager {
	t.Helper()
	return &Manager{Dir: t.TempDir(), Interpreter: "/bin/sh", GitHub: gh.URL, Codeload: gh.URL, HTTP: gh.Client()}
}

func TestInstallRunAndRemove(t *testing.T) {
	gh := fakeGitHub(t, map[string]string{
		"omini-plugin-fake-1.0.0/plugin.yaml": manifest,
		"omini-plugin-fake-1.0.0/main.sh":     script,
	})
	m := newManager(t, gh)
	ctx := context.Background()

	p, err := m.Install(ctx, "https://github.com/someone/omini-plugin-fake", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Manifest.ID != "fake" || p.Source.Version != "v1.0.0" || p.Source.Commit != releaseSHA || p.Dev {
		t.Fatalf("installed %+v", p)
	}

	in := m.Integration(p)
	if info := in.Info(); info.Type != "fake" || info.Kind != integration.KindPlugin || len(info.Fields) != 1 {
		t.Fatalf("info %+v", info)
	}
	devices, err := in.Collect(integration.WithInstance(ctx, 7), integration.Config{"host": "fw.lan"})
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || *devices[0].Interfaces[0].SpeedMbps != 1000 || *devices[0].Interfaces[0].Media != "1000baseT <full-duplex>" {
		t.Fatalf("devices %+v", devices)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "fake", "state", "7")); err != nil {
		t.Fatalf("state dir per instance: %v", err)
	}
	if msg, err := in.Test(ctx, integration.Config{"host": "fw.lan"}); err != nil || msg != "Connected to fake" {
		t.Fatalf("test: %q %v", msg, err)
	}

	// Loaded again after a restart.
	m2 := newManager(t, gh)
	m2.Dir = m.Dir
	list, errs := m2.Load()
	if len(errs) != 0 || len(list) != 1 || list[0].Source.URL != "https://github.com/someone/omini-plugin-fake" {
		t.Fatalf("load: %+v %v", list, errs)
	}

	if err := m.Remove("fake"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "fake")); !os.IsNotExist(err) {
		t.Fatal("plugin files must be removed")
	}
}

func TestRepositoriesWithoutReleasesInstallTheMainBranch(t *testing.T) {
	gh := fakeGitHub(t, map[string]string{"x/plugin.yaml": manifest, "x/main.sh": script}, true)
	m := newManager(t, gh)
	p, err := m.Install(context.Background(), "https://github.com/someone/omini-plugin-fake", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Source.Version != "main@2222222" || p.Source.Commit != mainSHA {
		t.Fatalf("source %+v", p.Source)
	}
	// A branch or tag can also be asked for explicitly.
	if p, err = m.Install(context.Background(), "https://github.com/someone/omini-plugin-fake", "main"); err != nil || p.Source.Version != "main" {
		t.Fatalf("install main: %+v %v", p, err)
	}
	if _, err := m.Install(context.Background(), "https://github.com/someone/omini-plugin-fake", "v9"); err == nil || !strings.Contains(err.Error(), "version v9 not found") {
		t.Fatalf("unknown version: %v", err)
	}
	if _, err := m.Install(context.Background(), "https://github.com/someone/missing", ""); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing repository: %v", err)
	}
}

func TestPluginErrors(t *testing.T) {
	gh := fakeGitHub(t, map[string]string{"x/plugin.yaml": manifest, "x/main.sh": script})
	m := newManager(t, gh)
	p, err := m.Install(context.Background(), "https://github.com/someone/omini-plugin-fake", "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	in := m.Integration(p)
	for host, want := range map[string]string{
		"broken": "wrong API key",            // the plugin's own message reaches the user
		"silent": "plugin fake failed",       // crashed without answering
		"slow":   "did not answer within 5s", // timeout from the manifest
	} {
		_, err := in.Collect(context.Background(), integration.Config{"host": host})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %v, want %q", host, err, want)
		}
	}
}

func TestInstallRejectsBadPlugins(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"no manifest":     {"x/main.sh": script},
		"newer protocol":  {"x/plugin.yaml": strings.Replace(manifest, "protocol: 1", "protocol: 9", 1), "x/main.sh": script},
		"no entrypoint":   {"x/plugin.yaml": manifest},
		"escaping entry":  {"x/plugin.yaml": strings.Replace(manifest, "main.sh", "../main.sh", 1)},
		"invalid id":      {"x/plugin.yaml": strings.Replace(manifest, "id: fake", "id: Fake Plugin", 1), "x/main.sh": script},
		"unsafe tar path": {"x/../../evil": "x", "x/plugin.yaml": manifest, "x/main.sh": script},
	} {
		t.Run(name, func(t *testing.T) {
			m := newManager(t, fakeGitHub(t, files))
			if _, err := m.Install(context.Background(), "https://github.com/someone/omini-plugin-fake", ""); err == nil {
				t.Fatal("expected an error")
			}
			if list := m.List(); len(list) != 0 {
				t.Fatalf("nothing must be installed: %+v", list)
			}
		})
	}
}

func TestParseGitHubURL(t *testing.T) {
	for _, ok := range []string{"https://github.com/a/b", "https://github.com/a/b.git", "https://github.com/a/b/"} {
		if o, r, err := parseGitHubURL(ok); err != nil || o != "a" || r != "b" {
			t.Errorf("%s: %s %s %v", ok, o, r, err)
		}
	}
	for _, bad := range []string{"http://github.com/a/b", "https://gitlab.com/a/b", "https://github.com/a", "https://github.com/a/b/tree/main", "file:///etc"} {
		if _, _, err := parseGitHubURL(bad); err == nil {
			t.Errorf("%s should be rejected", bad)
		}
	}
}

func TestDevelopmentPluginsLoadInPlace(t *testing.T) {
	dev := t.TempDir()
	_ = os.WriteFile(filepath.Join(dev, "plugin.yaml"), []byte(manifest), 0o644)
	_ = os.WriteFile(filepath.Join(dev, "main.sh"), []byte(script), 0o644)
	m := &Manager{Dir: t.TempDir(), DevDirs: []string{dev}, Interpreter: "/bin/sh"}
	list, errs := m.Load()
	if len(errs) != 0 || len(list) != 1 || !list[0].Dev {
		t.Fatalf("load: %+v %v", list, errs)
	}
	if err := m.Remove("fake"); !errors.Is(err, ErrDevPlugin) {
		t.Fatalf("remove dev plugin: %v", err)
	}
	devices, err := m.Integration(list[0]).Collect(context.Background(), integration.Config{"host": "x"})
	if err != nil || len(devices) != 1 {
		t.Fatalf("collect: %v %v", devices, err)
	}
}

// TestEnvironmentIsBuiltWithUV uses a fake uv that records its arguments.
func TestEnvironmentIsBuiltWithUV(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "uv.log")
	uv := filepath.Join(dir, "uv")
	_ = os.WriteFile(uv, []byte(`#!/bin/sh
echo "$@" >> `+log+`
if [ "$1" = venv ]; then mkdir -p "$5/bin" && touch "$5/bin/python"; fi
`), 0o755)
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "plugin.yaml"), []byte(manifest), 0o644)
	_ = os.WriteFile(filepath.Join(root, "main.sh"), []byte(script), 0o644)
	_ = os.WriteFile(filepath.Join(root, "requirements.txt"), []byte("httpx==0.28.1\n"), 0o644)
	m := &Manager{Dir: t.TempDir(), UV: uv, SDK: fstest.MapFS{
		"python/pyproject.toml":            {Data: []byte("[project]\nname='omini-sdk'\n")},
		"python/src/omini_sdk/__init__.py": {Data: []byte("")},
	}}
	m.init()
	p, err := m.open(root, filepath.Join(m.Dir, "fake"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.ensureEnv(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := m.ensureEnv(ctx, p); err != nil { // up to date: nothing to do
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "venv ") || !strings.Contains(lines[1], "pip install") ||
		!strings.Contains(lines[1], "-r "+filepath.Join(root, "requirements.txt")) || !strings.Contains(lines[1], "omini-sdk-") {
		t.Fatalf("uv calls:\n%s", b)
	}

	// New requirements: rebuilt.
	_ = os.WriteFile(filepath.Join(root, "requirements.txt"), []byte("httpx==0.28.2\n"), 0o644)
	if err := m.ensureEnv(ctx, p); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(log)
	if n := strings.Count(string(b), "\n"); n != 4 {
		t.Fatalf("expected a rebuild, uv calls:\n%s", b)
	}
}

func TestCatalog(t *testing.T) {
	list := Catalog()
	if len(list) == 0 {
		t.Fatal("empty catalog")
	}
	ids := map[string]bool{}
	for _, e := range list {
		if _, _, err := parseGitHubURL(e.URL); err != nil || e.ID == "" || e.Name == "" || ids[e.ID] {
			t.Errorf("invalid entry %+v: %v", e, err)
		}
		if e.Publisher != "official" && e.Publisher != "community" {
			t.Errorf("%s: publisher %q", e.ID, e.Publisher)
		}
		switch e.Trust {
		case "plug-and-play", "stable", "experimental":
		default:
			t.Errorf("%s: trust %q", e.ID, e.Trust)
		}
		ids[e.ID] = true
	}
	if e, ok := CatalogFor("https://github.com/riccardoalv/omini-plugin-opnsense.git/"); !ok || e.ID != "opnsense" {
		t.Fatalf("CatalogFor: %+v %v", e, ok)
	}
	if _, ok := CatalogFor("https://github.com/someone/else"); ok {
		t.Fatal("unknown repositories are not curated")
	}
}
