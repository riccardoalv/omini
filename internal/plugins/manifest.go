// Package plugins installs and runs Python plugins: integrations that live in
// their own repositories (e.g. omini-plugin-opnsense). The core runs a plugin
// once per collection, writes a request as JSON to its stdin and reads the
// response from its stdout (see CLAUDE.md, "Plugin runtime").
package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/riccardoalv/omini/internal/model"
)

// Protocols are the plugin protocol versions this core can run.
var Protocols = []uint64{1}

const (
	defaultTimeout = 60 * time.Second
	manifestFile   = "plugin.yaml"
)

// readManifest reads and validates plugin.yaml in dir.
func readManifest(dir string) (model.PluginManifest, error) {
	var m model.PluginManifest
	raw, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if err != nil {
		return m, fmt.Errorf("not a plugin: %s not found", manifestFile)
	}
	return parseManifest(raw)
}

// parseManifest decodes YAML through JSON, so the schema's validation
// (required fields, id pattern) applies.
func parseManifest(raw []byte) (model.PluginManifest, error) {
	var m model.PluginManifest
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return m, fmt.Errorf("%s: %w", manifestFile, err)
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return m, fmt.Errorf("%s: %w", manifestFile, err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("%s: %w", manifestFile, err)
	}
	if !slices.Contains(Protocols, m.Protocol) {
		return m, fmt.Errorf("plugin %s uses protocol %d, this Omini supports %v: update Omini or the plugin", m.ID, m.Protocol, Protocols)
	}
	if !filepath.IsLocal(m.Entrypoint) {
		return m, fmt.Errorf("%s: entrypoint must be a file inside the plugin", manifestFile)
	}
	return m, nil
}

func timeout(m model.PluginManifest) time.Duration {
	if m.TimeoutS != nil && *m.TimeoutS > 0 {
		return time.Duration(*m.TimeoutS) * time.Second
	}
	return defaultTimeout
}
