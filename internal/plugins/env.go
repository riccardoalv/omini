package plugins

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const envMarker = ".omini-env" // hash of what the environment was built from

func (p *Plugin) venv() string   { return filepath.Join(p.home, ".venv") }
func (p *Plugin) python() string { return filepath.Join(p.venv(), "bin", "python") }

func (m *Manager) buildEnv(ctx context.Context, p *Plugin) error {
	if m.Interpreter != "" {
		return nil
	}
	return m.ensureEnv(ctx, p)
}

// ensureEnv builds the plugin's virtual environment when it is missing or
// outdated (new requirements or a new SDK).
func (m *Manager) ensureEnv(ctx context.Context, p *Plugin) error {
	lock, _ := m.envMu.LoadOrStore(p.Manifest.ID, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()

	reqs, _ := os.ReadFile(filepath.Join(p.root, "requirements.txt"))
	want, err := m.envHash(reqs)
	if err != nil {
		return err
	}
	if have, err := os.ReadFile(filepath.Join(p.venv(), envMarker)); err == nil && string(have) == want {
		if _, err := os.Stat(p.python()); err == nil {
			return nil
		}
	}
	if _, err := exec.LookPath(m.UV); err != nil {
		return fmt.Errorf("plugins need uv to create their Python environment: install it (https://docs.astral.sh/uv/) or set OMINI_UV")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	sdkDir, err := os.MkdirTemp("", "omini-sdk-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(sdkDir)
	if err := copyFS(m.SDK, "python", sdkDir); err != nil {
		return fmt.Errorf("unpack the SDK: %w", err)
	}

	_ = os.RemoveAll(p.venv())
	if err := os.MkdirAll(p.home, 0o750); err != nil {
		return err
	}
	if out, err := m.uv(ctx, "venv", "--quiet", "--python", ">=3.10", p.venv()); err != nil {
		return fmt.Errorf("create the Python environment: %w: %s", err, out)
	}
	args := []string{"pip", "install", "--quiet", "--python", p.python(), sdkDir}
	if len(bytes.TrimSpace(reqs)) > 0 {
		args = append(args, "-r", filepath.Join(p.root, "requirements.txt"))
	}
	if out, err := m.uv(ctx, args...); err != nil {
		return fmt.Errorf("install the plugin's dependencies: %w: %s", err, out)
	}
	return os.WriteFile(filepath.Join(p.venv(), envMarker), []byte(want), 0o640)
}

func (m *Manager) uv(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, m.UV, args...)
	cmd.Env = append(childEnv(), "UV_NO_PROGRESS=1")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return strings.TrimSpace(tail(out.String(), 2000)), err
}

// envHash identifies an environment: the requirements and the SDK version.
func (m *Manager) envHash(reqs []byte) (string, error) {
	h := sha256.New()
	h.Write(reqs)
	err := fs.WalkDir(m.SDK, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		f, err := m.SDK.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, _ = io.WriteString(h, path)
		_, err = io.Copy(h, f)
		return err
	})
	return hex.EncodeToString(h.Sum(nil)), err
}

func copyFS(src fs.FS, root, dest string) error {
	return fs.WalkDir(src, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		b, err := fs.ReadFile(src, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o640)
	})
}

// childEnv is the environment of plugin processes: enough to run Python and
// reach the network, never Omini's own settings (secret key...).
func childEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case k == "PATH", k == "HOME", k == "TZ", k == "LANG", k == "TMPDIR",
			strings.HasPrefix(k, "LC_"), strings.HasSuffix(k, "_PROXY"), strings.HasSuffix(k, "_proxy"),
			k == "SSL_CERT_FILE", k == "SSL_CERT_DIR", k == "NIX_SSL_CERT_FILE",
			strings.HasPrefix(k, "UV_"), k == "XDG_CACHE_HOME", k == "XDG_DATA_HOME":
			env = append(env, kv)
		}
	}
	return env
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

var errNoOutput = errors.New("the plugin wrote no answer")
