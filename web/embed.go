//go:build !dev

// Package web embeds the WebApp build (web/dist). Run `bun run build` in
// web/ first; with only .gitkeep the Go binary still builds.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

var Dist = func() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}()
