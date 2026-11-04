// Package web embeds the built React UI (web/ → internal/web/dist).
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the UI files rooted at dist/.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

// Built reports whether a real UI build is embedded.
func Built() bool {
	_, err := fs.Stat(FS(), "index.html")
	return err == nil
}
