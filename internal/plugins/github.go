package plugins

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxDownload = 50 << 20 // a plugin is source code: 50 MB is plenty

var repoPart = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// parseGitHubURL accepts https://github.com/owner/repo (optionally .git or a trailing slash).
func parseGitHubURL(raw string) (owner, repo string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || (u.Host != "github.com" && u.Host != "www.github.com") {
		return "", "", fmt.Errorf("use the plugin's GitHub address, e.g. https://github.com/riccardoalv/omini-plugin-opnsense")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("use the repository address, e.g. https://github.com/riccardoalv/omini-plugin-opnsense")
	}
	owner, repo = parts[0], strings.TrimSuffix(parts[1], ".git")
	if !repoPart.MatchString(owner) || !repoPart.MatchString(repo) {
		return "", "", fmt.Errorf("invalid repository address")
	}
	return owner, repo, nil
}

// latestRelease returns the tag of the latest GitHub release.
func (m *Manager) latestRelease(ctx context.Context, owner, repo string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/releases/latest", m.GitHub, owner, repo), nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "omini")
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return "", fmt.Errorf("%s/%s has no release yet (or does not exist): give a version to install", owner, repo)
	default:
		return "", fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil || rel.TagName == "" {
		return "", fmt.Errorf("unexpected answer from GitHub")
	}
	return rel.TagName, nil
}

// download extracts the source tarball of a tag into dest.
func (m *Manager) download(ctx context.Context, owner, repo, version, dest string) error {
	ref := url.PathEscape(version)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/%s/%s/tar.gz/refs/tags/%s", m.Codeload, owner, repo, ref), nil)
	req.Header.Set("User-Agent", "omini")
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("version %s not found in %s/%s", version, owner, repo)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	return extract(io.LimitReader(resp.Body, maxDownload), dest)
}

// extract unpacks a GitHub tarball (one top-level folder, stripped) into dest.
// Only regular files and folders are written, never outside dest.
func extract(r io.Reader, dest string) error {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("invalid archive: %w", err)
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("invalid archive: %w", err)
		}
		_, rel, ok := strings.Cut(h.Name, "/")
		if !ok || rel == "" {
			continue
		}
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("invalid archive: unsafe path %q", h.Name)
		}
		target := filepath.Join(dest, rel)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o750); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, io.LimitReader(tr, maxDownload))
			f.Close()
			if err != nil {
				return err
			}
		}
		// Symlinks and devices are skipped.
	}
}
