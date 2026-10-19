package plugins

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// latestTTL is how long a repository's latest release is remembered: GitHub
// is asked at most once an hour per repository.
const latestTTL = time.Hour

// Why a plugin is left out of "update all".
const (
	ReasonDev       = "dev"        // loaded from OMINI_PLUGIN_DIRS
	ReasonPinned    = "pinned"     // installed from a branch or a commit
	ReasonNoRelease = "no_release" // the repository has no release
	ReasonGitHub    = "github"     // GitHub could not be asked (see Error)
)

// Update tells whether an installed plugin has a newer release.
type Update struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed string `json:"installed"`        // the installed version (Source.Version)
	Latest    string `json:"latest,omitempty"` // the repository's latest release
	Update    bool   `json:"update"`
	Reason    string `json:"reason,omitempty"` // why it is not updated (Reason*), empty when up to date
	Error     string `json:"error,omitempty"`  // GitHub's error, for ReasonGitHub
}

// UpdateResult is what updating one plugin did.
type UpdateResult struct {
	ID    string `json:"id"`
	From  string `json:"from"`
	To    string `json:"to"`
	Error string `json:"error,omitempty"`
}

type latestRelease struct {
	tag string // "" when the repository has no release
	at  time.Time
}

var (
	commitID = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	tagLike  = regexp.MustCompile(`^v?[0-9]`)
)

// isTag tells whether an installed version is a release tag ("v1.2.0",
// "1.2"), not a branch ("main", "main@1a2b3c4") or a commit id.
func isTag(version string) bool {
	return !strings.Contains(version, "@") && !commitID.MatchString(version) && tagLike.MatchString(version)
}

// newer tells whether the release latest is newer than installed: by
// version numbers when both have them, else when they differ.
func newer(latest, installed string) bool {
	a, okA := versionParts(latest)
	b, okB := versionParts(installed)
	if !okA || !okB {
		return latest != installed
	}
	for i := range max(len(a), len(b)) {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// versionParts reads "v1.2.3" as [1 2 3]; a pre-release or build suffix
// ("-rc1", "+meta") makes it unreadable, so the tags are only compared as text.
func versionParts(v string) ([]int, bool) {
	v = strings.TrimPrefix(v, "v")
	var out []int
	for p := range strings.SplitSeq(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, len(out) > 0
}

// latest returns a repository's latest release tag, asking GitHub at most
// once an hour (force asks now). Errors are not remembered.
func (m *Manager) latest(ctx context.Context, owner, repo string, force bool) (string, error) {
	key := owner + "/" + repo
	now := m.clock()
	m.latestMu.Lock()
	c, ok := m.latestCache[key]
	m.latestMu.Unlock()
	if ok && !force && now.Sub(c.at) < latestTTL {
		return c.tag, nil
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	code, err := m.githubJSON(ctx, fmt.Sprintf("/repos/%s/%s/releases/latest", owner, repo), &rel)
	if err != nil {
		return "", err
	}
	tag := ""
	if code == http.StatusOK {
		tag = rel.TagName
	}
	m.latestMu.Lock()
	if m.latestCache == nil {
		m.latestCache = map[string]latestRelease{}
	}
	m.latestCache[key] = latestRelease{tag: tag, at: now}
	m.latestMu.Unlock()
	return tag, nil
}

func (m *Manager) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// Updates tells, for each loaded plugin, whether a newer release exists.
// Development plugins and plugins pinned to a branch or commit are reported
// with update false and the reason.
func (m *Manager) Updates(ctx context.Context, force bool) []Update {
	out := []Update{}
	for _, p := range m.List() {
		u := Update{ID: p.Manifest.ID, Name: p.Manifest.Name, Installed: p.Manifest.Version}
		switch {
		case p.Dev || p.Source == nil:
			u.Reason = ReasonDev
		case !isTag(p.Source.Version):
			u.Installed, u.Reason = p.Source.Version, ReasonPinned
		default:
			u.Installed = p.Source.Version
			owner, repo, err := parseGitHubURL(p.Source.URL)
			if err != nil {
				u.Reason, u.Error = ReasonGitHub, err.Error()
				break
			}
			tag, err := m.latest(ctx, owner, repo, force)
			switch {
			case err != nil:
				u.Reason, u.Error = ReasonGitHub, err.Error()
			case tag == "":
				u.Reason = ReasonNoRelease
			default:
				u.Latest, u.Update = tag, newer(tag, p.Source.Version)
			}
		}
		out = append(out, u)
	}
	return out
}

// UpdateAll updates, one by one, every plugin with a newer release to its
// latest release, through Install (as POST /api/plugins with no version).
// One failure does not stop the others; installed is called for each plugin
// updated.
func (m *Manager) UpdateAll(ctx context.Context, installed func(*Plugin)) []UpdateResult {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	out := []UpdateResult{}
	for _, u := range m.Updates(ctx, false) {
		if !u.Update {
			continue
		}
		p, ok := m.Get(u.ID)
		if !ok || p.Source == nil {
			continue
		}
		r := UpdateResult{ID: u.ID, From: u.Installed, To: u.Latest}
		np, err := m.Install(ctx, p.Source.URL, "")
		if err != nil {
			r.Error = err.Error()
			slog.Warn("plugin update failed", "plugin", u.ID, "err", err)
		} else {
			r.To = np.Source.Version
			if installed != nil {
				installed(np)
			}
		}
		out = append(out, r)
	}
	return out
}
