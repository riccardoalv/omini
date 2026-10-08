package appicons

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMatch(t *testing.T) {
	cases := map[string]string{
		"qBittorrent WebUI":                     "qbittorrent",
		"Jellyfin":                              "jellyfin",
		"Home Assistant":                        "home-assistant",
		"Login | OPNsense":                      "opnsense",
		"proxmox - Proxmox Virtual Environment": "proxmox",
		"Sonarr":                                "sonarr",
		"Portainer":                             "portainer",
		"TrueNAS - 192.168.1.51":                "truenas",
		"Media Manager":                         "media-manager", // a name of generic words, distinctive as a whole
		// An app's name beats another app's generic alias ("network").
		"UniFi Network":                 "unifi",
		"APC | Network Management Card": "apc",
	}
	for title, want := range cases {
		if got, ok := Match(title); !ok || got != want {
			t.Errorf("Match(%q) = %q, %v; want %q", title, got, ok, want)
		}
	}
	for _, title := range []string{
		"", "Login", "Dashboard", "Welcome to nginx!", "403 Forbidden", "Router admin",
		// Generic words alone: names and aliases made of them never match.
		"Network", "Network Management", "Network Devices", "Files", "Printer", "UPS status", "Management Card",
	} {
		if got, ok := Match(title); ok {
			t.Errorf("Match(%q) = %q, want no match", title, got)
		}
	}
}

func TestDisplayName(t *testing.T) {
	if got := DisplayName("qbittorrent", "qBittorrent WebUI"); got != "qBittorrent" {
		t.Errorf("got %q", got)
	}
	if got := DisplayName("home-assistant", ""); got != "Home Assistant" {
		t.Errorf("got %q", got)
	}
}

func TestCatalog(t *testing.T) {
	if Count() < 2000 || !Known("mercusys") || Known("definitely-not-an-app") {
		t.Fatalf("catalog: %d icons, mercusys known=%v", Count(), Known("mercusys"))
	}
}

func TestServerDownloadsAndCaches(t *testing.T) {
	hits := 0
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/mercusys.svg" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("<svg/>"))
	}))
	defer cdn.Close()
	dir := t.TempDir()
	s := &Server{CacheDir: dir, CDN: cdn.URL}
	s.bundleOnce.Do(func() { s.bundle = map[string][]byte{} }) // ignore any local bundle

	for range 2 {
		b, err := s.Icon(context.Background(), "mercusys")
		if err != nil || string(b) != "<svg/>" {
			t.Fatalf("Icon = %q, %v", b, err)
		}
	}
	if hits != 1 {
		t.Fatalf("CDN hit %d times, want 1 (cached on disk)", hits)
	}
	if _, err := os.Stat(filepath.Join(dir, "mercusys.svg")); err != nil {
		t.Fatal("icon not cached on disk")
	}
	for _, bad := range []string{"../etc/passwd", "definitely-not-an-app", "UPPER"} {
		if _, err := s.Icon(context.Background(), bad); err == nil {
			t.Errorf("Icon(%q) should fail", bad)
		}
	}
}

func TestServeHTTP(t *testing.T) {
	s := &Server{CacheDir: t.TempDir(), Offline: true}
	s.bundleOnce.Do(func() { s.bundle = map[string][]byte{"jellyfin": []byte("<svg>j</svg>")} })
	mux := http.NewServeMux()
	mux.Handle("GET /api/icons/{name}", s)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/icons/jellyfin.svg", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "<svg>j</svg>" || rec.Header().Get("Content-Type") != "image/svg+xml" ||
		rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("response %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/icons/plex.svg", nil)) // offline, not bundled
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing icon: %d", rec.Code)
	}
}
