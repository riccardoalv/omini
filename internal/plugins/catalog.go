package plugins

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// CatalogEntry is a plugin of the store's index: the list shipped with Omini,
// refreshed from the remote index.
type CatalogEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Icon        string `json:"icon,omitempty"`
	Publisher   string `json:"publisher"` // official | community
	// Trust is assigned by the maintainers after a review (docs/plugin-review.md):
	// plug-and-play (fully tested), stable (known issues documented),
	// experimental (partially tested). Plugins installed from any other URL
	// are unverified.
	Trust string `json:"trust"`
	// Categories group the store: firewall, router, switch, wifi, hypervisor, nas...
	Categories []string `json:"categories,omitempty"`
	// ReviewedVersion is the release the trust level was given to; newer
	// releases are shown as not reviewed yet.
	ReviewedVersion string `json:"reviewed_version,omitempty"`
	ReviewedAt      string `json:"reviewed_at,omitempty"` // date, YYYY-MM-DD
	// KnownIssues links the documented issues (stable plugins).
	KnownIssues string `json:"known_issues,omitempty"`
}

//go:embed catalog.json
var catalogJSON []byte

var (
	catalogMu sync.RWMutex
	remote    []CatalogEntry // from the remote index, when fetched
)

// Catalog returns the store's plugins: the remote index when it was fetched,
// else the list shipped with Omini. Entries of the shipped list missing from
// the remote index are kept (an older index never hides a plugin).
func Catalog() []CatalogEntry {
	var out []CatalogEntry
	if err := json.Unmarshal(catalogJSON, &out); err != nil {
		panic("plugins: invalid catalog.json: " + err.Error())
	}
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	if len(remote) == 0 {
		return out
	}
	byID := map[string]int{}
	merged := append([]CatalogEntry(nil), remote...)
	for i, e := range merged {
		byID[e.ID] = i
	}
	for _, e := range out {
		if _, ok := byID[e.ID]; !ok {
			merged = append(merged, e)
		}
	}
	return merged
}

// CatalogFor returns the catalog entry of a repository, if it is curated.
func CatalogFor(url string) (CatalogEntry, bool) {
	norm := func(s string) string {
		return strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), "/"), ".git")
	}
	for _, e := range Catalog() {
		if norm(e.URL) == norm(url) {
			return e, true
		}
	}
	return CatalogEntry{}, false
}

// DefaultIndex is where the store's index is published: the catalog of the
// Omini repository's main branch.
const DefaultIndex = "https://raw.githubusercontent.com/riccardoalv/omini/main/internal/plugins/catalog.json"

// IndexRefresh is how often the remote index is fetched again.
const IndexRefresh = 24 * time.Hour

// Index keeps the remote index fresh. It is optional: without it (or offline)
// the store shows the list shipped with Omini.
type Index struct {
	URL    string // empty: no remote index
	Cache  string // file keeping the last index fetched, for offline starts
	Client *http.Client

	mu        sync.Mutex
	fetchedAt time.Time
	lastErr   error
}

// IndexStatus describes the remote index for the UI.
type IndexStatus struct {
	URL       string     `json:"url,omitempty"`
	FetchedAt *time.Time `json:"fetched_at,omitempty"`
	Error     string     `json:"error,omitempty"`
	Plugins   int        `json:"plugins"`
}

// Status reports the index's state.
func (x *Index) Status() IndexStatus {
	x.mu.Lock()
	defer x.mu.Unlock()
	st := IndexStatus{URL: x.URL, Plugins: len(Catalog())}
	if !x.fetchedAt.IsZero() {
		at := x.fetchedAt
		st.FetchedAt = &at
	}
	if x.lastErr != nil {
		st.Error = x.lastErr.Error()
	}
	return st
}

// Load reads the cached index (last fetched) so the store has it at once.
func (x *Index) Load() {
	if x.Cache == "" {
		return
	}
	raw, err := os.ReadFile(x.Cache)
	if err != nil {
		return
	}
	list, err := ParseIndex(raw)
	if err != nil {
		slog.Warn("cached plugin index is invalid", "err", err)
		return
	}
	if fi, err := os.Stat(x.Cache); err == nil {
		x.mu.Lock()
		x.fetchedAt = fi.ModTime()
		x.mu.Unlock()
	}
	setRemote(list)
}

// Run fetches the index now and then every IndexRefresh until ctx is done.
func (x *Index) Run(ctx context.Context) {
	if x.URL == "" {
		return
	}
	for {
		if err := x.Refresh(ctx); err != nil && ctx.Err() == nil {
			slog.Info("plugin index not refreshed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(IndexRefresh):
		}
	}
}

// Refresh fetches the remote index now.
func (x *Index) Refresh(ctx context.Context) error {
	if x.URL == "" {
		return errors.New("no remote index configured")
	}
	err := x.fetch(ctx)
	x.mu.Lock()
	x.lastErr = err
	if err == nil {
		x.fetchedAt = time.Now()
	}
	x.mu.Unlock()
	return err
}

func (x *Index) fetch(ctx context.Context) error {
	client := x.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, x.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Omini")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", x.URL, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	list, err := ParseIndex(raw)
	if err != nil {
		return err
	}
	setRemote(list)
	if x.Cache != "" {
		if err := os.WriteFile(x.Cache, raw, 0o600); err != nil {
			slog.Warn("could not cache the plugin index", "err", err)
		}
	}
	return nil
}

func setRemote(list []CatalogEntry) {
	catalogMu.Lock()
	remote = list
	catalogMu.Unlock()
}

var trusts = map[string]bool{"plug-and-play": true, "stable": true, "experimental": true, "unverified": true}

// ParseIndex reads and checks an index: a JSON list of entries. Invalid
// entries make the whole index invalid (it is reviewed before publishing).
func ParseIndex(raw []byte) ([]CatalogEntry, error) {
	var list []CatalogEntry
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("invalid plugin index: %w", err)
	}
	if len(list) == 0 {
		return nil, errors.New("the plugin index is empty")
	}
	seen := map[string]bool{}
	for _, e := range list {
		switch {
		case e.ID == "" || e.Name == "":
			return nil, errors.New("an index entry has no id or name")
		case seen[e.ID]:
			return nil, fmt.Errorf("plugin %q is listed twice", e.ID)
		case !strings.HasPrefix(e.URL, "https://github.com/"):
			return nil, fmt.Errorf("plugin %q: the URL must be a GitHub repository", e.ID)
		case !trusts[e.Trust]:
			return nil, fmt.Errorf("plugin %q: unknown trust level %q", e.ID, e.Trust)
		case e.Publisher != "official" && e.Publisher != "community":
			return nil, fmt.Errorf("plugin %q: unknown publisher %q", e.ID, e.Publisher)
		}
		seen[e.ID] = true
	}
	return list, nil
}
