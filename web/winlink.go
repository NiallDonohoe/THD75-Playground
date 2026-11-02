package web

import (
	"embed"
	"io/fs"
)

// In its own file, so the Winlink branch and others each add their page
// without editing embed.go.

//go:embed winlink/index.html winlink/style.css winlink/*.js
var winlinkFiles embed.FS

// Winlink is d75winlink's page: index.html, style.css and its scripts.
// Tests (*.test.mjs) are not embedded.
func Winlink() fs.FS {
	sub, err := fs.Sub(winlinkFiles, "winlink")
	if err != nil {
		panic(err)
	}
	return sub
}
