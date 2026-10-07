package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/riccardoalv/omini/internal/model"
)

// Plugin is an installed (or development) plugin.
type Plugin struct {
	Manifest model.PluginManifest `json:"manifest"`
	Source   *Source              `json:"source,omitempty"` // nil for development plugins
	Dev      bool                 `json:"dev"`              // loaded in place from OMINI_PLUGIN_DIRS

	root string // plugin code
	home string // <data>/plugins/<id>: venv, state
}

// Source records where an installed plugin came from.
type Source struct {
	URL         string    `json:"url"`
	Version     string    `json:"version"` // tag, or branch@commit when the repository has no release
	Commit      string    `json:"commit"`
	InstalledAt time.Time `json:"installed_at"`
}

// Manager installs, loads and removes plugins.
type Manager struct {
	Dir      string   // <data>/plugins
	DevDirs  []string // plugins loaded in place (development)
	UV       string   // uv binary; default "uv" from PATH
	SDK      fs.FS    // the embedded omini-sdk (sdk.Python)
	HTTP     *http.Client
	GitHub   string // API base; default https://api.github.com (tests use a fake)
	Codeload string // tarball base; default https://codeload.github.com
	// Interpreter runs entrypoints directly with this program, without
	// building Python environments (tests use /bin/sh plugins).
	Interpreter string

	mu      sync.Mutex
	plugins map[string]*Plugin
	envMu   sync.Map // per-plugin lock while its environment is (re)built
}

func (m *Manager) init() {
	if m.plugins == nil {
		m.plugins = map[string]*Plugin{}
	}
	if m.UV == "" {
		m.UV = "uv"
	}
	if m.HTTP == nil {
		m.HTTP = &http.Client{Timeout: 2 * time.Minute}
	}
	if m.GitHub == "" {
		m.GitHub = "https://api.github.com"
	}
	if m.Codeload == "" {
		m.Codeload = "https://codeload.github.com"
	}
}

const sourceFile = "install.json"

// Load finds the installed plugins and the development ones. A broken plugin
// is reported and skipped; the others still load.
func (m *Manager) Load() ([]*Plugin, []error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	var errs []error
	entries, err := os.ReadDir(m.Dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		home := filepath.Join(m.Dir, e.Name())
		if _, err := os.Stat(filepath.Join(home, "src")); err != nil {
			continue // only the environment of a development plugin
		}
		p, err := m.open(filepath.Join(home, "src"), home)
		if err != nil {
			errs = append(errs, fmt.Errorf("plugin %s: %w", e.Name(), err))
			continue
		}
		var src Source
		if b, err := os.ReadFile(filepath.Join(home, sourceFile)); err == nil && json.Unmarshal(b, &src) == nil {
			p.Source = &src
		}
		m.plugins[p.Manifest.ID] = p
	}
	for _, dir := range m.DevDirs {
		probe, err := readManifest(dir)
		if err != nil {
			errs = append(errs, fmt.Errorf("plugin in %s: %w", dir, err))
			continue
		}
		p, err := m.open(dir, filepath.Join(m.Dir, probe.ID))
		if err != nil {
			errs = append(errs, fmt.Errorf("plugin in %s: %w", dir, err))
			continue
		}
		p.Dev = true
		m.plugins[p.Manifest.ID] = p // a development copy wins over an installed one
	}
	return m.listLocked(), errs
}

func (m *Manager) open(root, home string) (*Plugin, error) {
	man, err := readManifest(root)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(root, man.Entrypoint)); err != nil {
		return nil, fmt.Errorf("entrypoint %s not found", man.Entrypoint)
	}
	return &Plugin{Manifest: man, root: root, home: home}, nil
}

// List returns the loaded plugins sorted by name.
func (m *Manager) List() []*Plugin {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	return m.listLocked()
}

func (m *Manager) listLocked() []*Plugin {
	out := make([]*Plugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.Name < out[j].Manifest.Name })
	return out
}

// Get returns a loaded plugin.
func (m *Manager) Get(id string) (*Plugin, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	p, ok := m.plugins[id]
	return p, ok
}

// ErrDevPlugin is returned when removing a plugin loaded from OMINI_PLUGIN_DIRS.
var ErrDevPlugin = errors.New("development plugins are loaded from OMINI_PLUGIN_DIRS and cannot be removed here")

// Remove deletes an installed plugin with its environment and state.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	p, ok := m.plugins[id]
	if !ok {
		return fs.ErrNotExist
	}
	if p.Dev {
		return ErrDevPlugin
	}
	delete(m.plugins, id)
	slog.Info("plugin removed", "plugin", id)
	return os.RemoveAll(p.home)
}

// Install downloads a plugin from GitHub (see resolve for the version),
// checks it and builds its environment. Installing a
// plugin that is already installed updates it.
func (m *Manager) Install(ctx context.Context, url, version string) (*Plugin, error) {
	m.mu.Lock()
	m.init()
	m.mu.Unlock()
	owner, repo, err := parseGitHubURL(url)
	if err != nil {
		return nil, err
	}
	sha, version, err := m.resolve(ctx, owner, repo, strings.TrimSpace(version))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(m.Dir, 0o750); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(m.Dir, ".install-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := m.download(ctx, owner, repo, sha, filepath.Join(tmp, "src")); err != nil {
		return nil, err
	}
	man, err := readManifest(filepath.Join(tmp, "src"))
	if err != nil {
		return nil, err
	}
	home := filepath.Join(m.Dir, man.ID)
	if existing, ok := m.Get(man.ID); ok && existing.Dev {
		return nil, fmt.Errorf("plugin %s is loaded from OMINI_PLUGIN_DIRS; remove it from there to install it", man.ID)
	}
	p, err := m.open(filepath.Join(tmp, "src"), home)
	if err != nil {
		return nil, err
	}
	// Build the environment before replacing a working version.
	if err := os.MkdirAll(home, 0o750); err != nil {
		return nil, err
	}
	src := filepath.Join(home, "src")
	old := filepath.Join(home, ".src-old")
	_ = os.RemoveAll(old)
	if _, err := os.Stat(src); err == nil {
		if err := os.Rename(src, old); err != nil {
			return nil, err
		}
	}
	if err := os.Rename(filepath.Join(tmp, "src"), src); err != nil {
		_ = os.Rename(old, src)
		return nil, err
	}
	p.root = src
	if err := m.buildEnv(ctx, p); err != nil {
		_ = os.RemoveAll(src)
		_ = os.Rename(old, src)
		return nil, err
	}
	_ = os.RemoveAll(old)
	p.Source = &Source{URL: url, Version: version, Commit: sha, InstalledAt: time.Now().UTC().Truncate(time.Second)}
	b, _ := json.MarshalIndent(p.Source, "", "  ")
	if err := os.WriteFile(filepath.Join(home, sourceFile), b, 0o640); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.plugins[man.ID] = p
	m.mu.Unlock()
	slog.Info("plugin installed", "plugin", man.ID, "version", version, "url", url)
	return p, nil
}
