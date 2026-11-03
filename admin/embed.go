// Package admin embeds the compiled admin SPA (built separately from
// admin/src via Vite; see `make build-admin`) into the Go binary.
//
// This file is the only Go source in this directory and is owned by the
// backend foundation. Everything else under admin/ (package.json, vite
// config, src/, the built dist/ output) belongs to the admin frontend.
package admin

import (
	"embed"
	"io/fs"
)

// DistFS holds the embedded build output of the admin SPA.
//
// admin/dist/index.html is checked in as a placeholder so `go build ./...`
// succeeds before the real frontend has been built; the real build replaces
// everything under dist/ (dist/ itself is gitignored).
//
//go:embed all:dist
var DistFS embed.FS

// FS returns the embedded admin SPA filesystem rooted at "dist", so
// callers can serve e.g. "index.html" instead of "dist/index.html".
func FS() (fs.FS, error) {
	return fs.Sub(DistFS, "dist")
}
