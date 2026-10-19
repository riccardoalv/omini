package plugins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRepo is one repository of fakeReleases.
type fakeRepo struct {
	latest  string            // the latest release; "" for none
	commits map[string]string // tag or branch → commit
	broken  bool              // its tarballs fail to download
}

// fakeReleases serves several someone/<repo> repositories, each plugin <repo>
// at the version of the tag, and counts the questions for the latest release.
type fakeReleases struct {
	t     *testing.T
	srv   *httptest.Server
	mu    sync.Mutex
	repos map[string]*fakeRepo
	asked map[string]int // repo → GET releases/latest
}

func newFakeReleases(t *testing.T, repos map[string]*fakeRepo) *fakeReleases {
	t.Helper()
	f := &fakeReleases{t: t, repos: repos, asked: map[string]int{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeReleases) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case len(parts) >= 3 && parts[0] == "repos" && parts[1] == "someone":
		repo, ok := f.repos[parts[2]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		rest := strings.Join(parts[3:], "/")
		switch {
		case rest == "releases/latest":
			f.asked[parts[2]]++
			if repo.latest == "" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"tag_name":"` + repo.latest + `"}`))
		case rest == "":
			_, _ = w.Write([]byte(`{"default_branch":"main"}`))
		case strings.HasPrefix(rest, "commits/"):
			sha, ok := repo.commits[strings.TrimPrefix(rest, "commits/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"sha":"` + sha + `"}`))
		default:
			http.NotFound(w, r)
		}
	case len(parts) == 4 && parts[0] == "someone" && parts[2] == "tar.gz":
		repo, ok := f.repos[parts[1]]
		if !ok || repo.broken {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		for ref, sha := range repo.commits {
			if sha == parts[3] {
				version := strings.TrimPrefix(ref, "v")
				_, _ = w.Write(tarball(f.t, map[string]string{
					"x/plugin.yaml": strings.NewReplacer("id: fake", "id: "+parts[1], "name: Fake router", "name: "+parts[1], "version: 1.0.0", "version: "+version).Replace(manifest),
					"x/main.sh":     script,
				}))
				return
			}
		}
		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeReleases) set(repo string, fn func(*fakeRepo)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f.repos[repo])
}

func (f *fakeReleases) questions(repo string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked[repo]
}

func sha(c byte) string { return strings.Repeat(string(c), 40) }

func TestIsTagAndNewer(t *testing.T) {
	for v, want := range map[string]bool{
		"v1.2.0": true, "1.2": true, "v2": true,
		"main": false, "main@1a2b3c4": false, "1a2b3c4": false, "abcdef0123456789abcdef0123456789abcdef01": false, "": false,
	} {
		if got := isTag(v); got != want {
			t.Errorf("isTag(%q) = %v", v, got)
		}
	}
	for _, c := range []struct {
		latest, installed string
		want              bool
	}{
		{"v1.1.0", "v1.0.0", true},
		{"v1.10.0", "v1.9.0", true},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0", "v1.0.0", false},
		{"v1.0.0", "v1.1.0", false}, // never a downgrade
		{"v2.0.0-rc1", "v1.0.0", true},
		{"v1.0.0", "v1.0.0-rc1", true},
	} {
		if got := newer(c.latest, c.installed); got != c.want {
			t.Errorf("newer(%q, %q) = %v", c.latest, c.installed, got)
		}
	}
}

func TestUpdatesAndUpdateAll(t *testing.T) {
	gh := newFakeReleases(t, map[string]*fakeRepo{
		"alpha":  {latest: "v1.0.0", commits: map[string]string{"v1.0.0": sha('a'), "v1.1.0": sha('b')}},
		"bravo":  {latest: "v1.0.0", commits: map[string]string{"v1.0.0": sha('c'), "v2.0.0": sha('d')}},
		"pinned": {latest: "v1.0.0", commits: map[string]string{"v1.0.0": sha('e'), "main": sha('f')}},
		"norel":  {commits: map[string]string{"main": sha('1')}},
	})
	// A development plugin too.
	devDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(devDir, "plugin.yaml"), []byte(strings.Replace(manifest, "id: fake", "id: devplug", 1)), 0o644)
	_ = os.WriteFile(filepath.Join(devDir, "main.sh"), []byte(script), 0o644)
	m := &Manager{Dir: t.TempDir(), DevDirs: []string{devDir}, Interpreter: "/bin/sh", GitHub: gh.srv.URL, Codeload: gh.srv.URL, HTTP: gh.srv.Client()}
	if _, errs := m.Load(); len(errs) != 0 {
		t.Fatal(errs)
	}
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	ctx := context.Background()
	for repo, version := range map[string]string{"alpha": "", "bravo": "", "pinned": "main", "norel": ""} {
		if _, err := m.Install(ctx, "https://github.com/someone/"+repo, version); err != nil {
			t.Fatalf("install %s: %v", repo, err)
		}
	}
	byID := func(list []Update) map[string]Update {
		out := map[string]Update{}
		for _, u := range list {
			out[u.ID] = u
		}
		return out
	}

	got := byID(m.Updates(ctx, false))
	if len(got) != 5 {
		t.Fatalf("updates: %+v", got)
	}
	if u := got["alpha"]; u.Update || u.Installed != "v1.0.0" || u.Latest != "v1.0.0" || u.Reason != "" {
		t.Fatalf("alpha is up to date: %+v", u)
	}
	if u := got["pinned"]; u.Update || u.Reason != ReasonPinned || u.Installed != "main" {
		t.Fatalf("pinned: %+v", u)
	}
	if u := got["norel"]; u.Update || u.Reason != ReasonPinned || !strings.HasPrefix(u.Installed, "main@") {
		t.Fatalf("a repository without releases is on a branch: %+v", u)
	}
	if u := got["devplug"]; u.Update || u.Reason != ReasonDev {
		t.Fatalf("dev: %+v", u)
	}
	if gh.questions("pinned") != 0 || gh.questions("norel") != 1 { // norel: once by its install only
		t.Fatalf("pinned plugins are not asked: %v", gh.asked)
	}

	// New releases: within the hour, GitHub is not asked again.
	gh.set("alpha", func(r *fakeRepo) { r.latest = "v1.1.0" })
	gh.set("bravo", func(r *fakeRepo) { r.latest = "v2.0.0"; r.broken = true })
	before := gh.questions("alpha")
	now = now.Add(59 * time.Minute)
	if u := byID(m.Updates(ctx, false))["alpha"]; u.Update || gh.questions("alpha") != before {
		t.Fatalf("cached for an hour: %+v, asked %d", u, gh.questions("alpha")-before)
	}
	// A forced refresh asks now.
	if u := byID(m.Updates(ctx, true))["alpha"]; !u.Update || u.Latest != "v1.1.0" || gh.questions("alpha") != before+1 {
		t.Fatalf("forced: %+v", u)
	}
	now = now.Add(2 * time.Hour)
	got = byID(m.Updates(ctx, false))
	if !got["alpha"].Update || !got["bravo"].Update || gh.questions("alpha") != before+2 {
		t.Fatalf("after an hour: %+v", got)
	}

	// bravo fails to download; alpha still updates.
	var reloaded []string
	results := m.UpdateAll(ctx, func(p *Plugin) { reloaded = append(reloaded, p.Manifest.ID) })
	if len(results) != 2 {
		t.Fatalf("results: %+v", results)
	}
	res := map[string]UpdateResult{}
	for _, r := range results {
		res[r.ID] = r
	}
	if r := res["alpha"]; r.Error != "" || r.From != "v1.0.0" || r.To != "v1.1.0" {
		t.Fatalf("alpha: %+v", r)
	}
	if r := res["bravo"]; !strings.Contains(r.Error, "download failed") || r.From != "v1.0.0" || r.To != "v2.0.0" {
		t.Fatalf("bravo: %+v", r)
	}
	if len(reloaded) != 1 || reloaded[0] != "alpha" {
		t.Fatalf("reloaded: %v", reloaded)
	}
	if p, _ := m.Get("alpha"); p.Manifest.Version != "1.1.0" || p.Source.Version != "v1.1.0" {
		t.Fatalf("alpha installed: %+v", p.Source)
	}
	if p, _ := m.Get("bravo"); p.Manifest.Version != "1.0.0" || p.Source.Version != "v1.0.0" {
		t.Fatalf("bravo kept its working version: %+v", p.Source)
	}
	if p, _ := m.Get("pinned"); p.Source.Version != "main" {
		t.Fatalf("pinned left alone: %+v", p.Source)
	}

	// GitHub unreachable: reported, not remembered.
	gh.srv.Close()
	now = now.Add(2 * time.Hour)
	if u := byID(m.Updates(ctx, false))["alpha"]; u.Update || u.Reason != ReasonGitHub || u.Error == "" {
		t.Fatalf("unreachable: %+v", u)
	}
}
