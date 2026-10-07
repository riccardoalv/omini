package plugins

import (
	_ "embed"
	"encoding/json"
	"strings"
)

// CatalogEntry is a plugin of the curated list shipped with Omini (the
// store's index; a remote index can replace it later).
type CatalogEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Icon        string `json:"icon,omitempty"`
	Publisher   string `json:"publisher"` // official | community
	// Trust is assigned by the maintainers: plug-and-play (fully tested),
	// stable (known issues documented), experimental (partially tested).
	// Plugins installed from any other URL are unverified.
	Trust string `json:"trust"`
}

//go:embed catalog.json
var catalogJSON []byte

// Catalog returns the curated plugins.
func Catalog() []CatalogEntry {
	var out []CatalogEntry
	if err := json.Unmarshal(catalogJSON, &out); err != nil {
		panic("plugins: invalid catalog.json: " + err.Error())
	}
	return out
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
