// Package web embeds the built web UI (web/dist) into the Go binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built UI. It contains only .gitkeep until `make web` runs,
// in which case the server shows a placeholder page.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // the embed directive guarantees "dist" exists
	}
	return sub
}
