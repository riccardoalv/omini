package appicons

import "strings"

// publishers are image namespaces that publish many apps: never the app.
var publishers = map[string]bool{
	"library": true, "linuxserver": true, "lscr": true, "bitnami": true, "hotio": true,
	"ghcr": true, "docker": true, "_": true,
}

// imageSuffixes are edition suffixes of image names ("portainer-ce",
// "grafana-oss"): the app is the name without them.
var imageSuffixes = []string{"-ce", "-ee", "-oss", "-server", "-app", "-web"}

// FromImage recognizes the app a container image runs, e.g.
// "lscr.io/linuxserver/jellyfin:latest" -> "jellyfin" or
// "portainer/portainer-ce:2.21" -> "portainer". The image's name is tried
// first (a catalog name, then the catalog's matching), then its namespace
// unless it is a publisher of many apps ("openspeedtest/latest").
func FromImage(image string) (string, bool) {
	ref := strings.ToLower(strings.TrimSpace(image))
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i] // a digest
	}
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref = ref[:i] // a tag
	}
	parts := strings.Split(ref, "/")
	if len(parts) > 1 && (strings.ContainsAny(parts[0], ".:") || parts[0] == "localhost") {
		parts = parts[1:] // a registry
	}
	for i := len(parts) - 1; i >= 0; i-- {
		seg := parts[i]
		if seg == "" || (i < len(parts)-1 && publishers[seg]) {
			continue
		}
		if app, ok := fromName(seg); ok {
			return app, true
		}
	}
	return "", false
}

func fromName(seg string) (string, bool) {
	if Known(seg) {
		return seg, true
	}
	for _, s := range imageSuffixes {
		if base, ok := strings.CutSuffix(seg, s); ok && Known(base) {
			return base, true
		}
	}
	return Match(strings.NewReplacer("-", " ", "_", " ", ".", " ").Replace(seg))
}
