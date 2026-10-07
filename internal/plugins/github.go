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

// resolve picks what to install and pins it to a commit: the given version
// (a tag, branch or commit), else the latest release, else the newest commit
// of the default branch (repositories without releases work too). It returns
// the commit and a readable version ("v0.2.0" or "main@1a2b3c4").
func (m *Manager) resolve(ctx context.Context, owner, repo, version string) (sha, label string, err error) {
	if version == "" {
		var rel struct {
			TagName string `json:"tag_name"`
		}
		switch code, err := m.githubJSON(ctx, fmt.Sprintf("/repos/%s/%s/releases/latest", owner, repo), &rel); {
		case err != nil:
			return "", "", err
		case code == http.StatusOK && rel.TagName != "":
			version = rel.TagName
		default: // no release: the default branch
			var info struct {
				DefaultBranch string `json:"default_branch"`
			}
			code, err := m.githubJSON(ctx, fmt.Sprintf("/repos/%s/%s", owner, repo), &info)
			if err != nil {
				return "", "", err
			}
			if code == http.StatusNotFound {
				return "", "", fmt.Errorf("repository %s/%s not found (it must be public)", owner, repo)
			}
			if info.DefaultBranch == "" {
				return "", "", fmt.Errorf("unexpected answer from GitHub")
			}
			sha, err := m.commit(ctx, owner, repo, info.DefaultBranch)
			if err != nil {
				return "", "", err
			}
			return sha, info.DefaultBranch + "@" + sha[:7], nil
		}
	}
	sha, err = m.commit(ctx, owner, repo, version)
	return sha, version, err
}

// commit returns the commit a tag, branch or commit id points to.
func (m *Manager) commit(ctx context.Context, owner, repo, ref string) (string, error) {
	var c struct {
		SHA string `json:"sha"`
	}
	code, err := m.githubJSON(ctx, fmt.Sprintf("/repos/%s/%s/commits/%s", owner, repo, url.PathEscape(ref)), &c)
	if err != nil {
		return "", err
	}
	if code != http.StatusOK || len(c.SHA) < 7 {
		return "", fmt.Errorf("version %s not found in %s/%s", ref, owner, repo)
	}
	return c.SHA, nil
}

// githubJSON GETs a GitHub API path; a 404 is returned as a status, not an error.
func (m *Manager) githubJSON(ctx context.Context, path string, out any) (int, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, m.GitHub+path, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "omini")
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("cannot reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
			return 0, fmt.Errorf("unexpected answer from GitHub")
		}
	case http.StatusNotFound, http.StatusUnprocessableEntity:
	case http.StatusForbidden, http.StatusTooManyRequests:
		return 0, fmt.Errorf("GitHub refused the request (rate limit?): try again later")
	default:
		return 0, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	return resp.StatusCode, nil
}

// download extracts the source tarball of a commit into dest.
func (m *Manager) download(ctx context.Context, owner, repo, sha, dest string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/%s/%s/tar.gz/%s", m.Codeload, owner, repo, sha), nil)
	req.Header.Set("User-Agent", "omini")
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
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
