// Package web holds the front ends' files, embedded into the programs.
// Each page also gets the shared map under map/ (web/map).
package web

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed aprs/index.html aprs/style.css aprs/*.js
var aprsFiles embed.FS

//go:embed sat/index.html sat/style.css sat/*.js
var satFiles embed.FS

//go:embed map/*.js
var mapFiles embed.FS

// APRS is d75aprs's page: index.html, style.css, its scripts and map/.
func APRS() fs.FS { return page(aprsFiles, "aprs") }

// Sat is d75sat's page: index.html, style.css, its scripts and map/.
func Sat() fs.FS { return page(satFiles, "sat") }

func page(files embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(files, dir)
	if err != nil {
		panic(err)
	}
	shared, err := fs.Sub(mapFiles, "map")
	if err != nil {
		panic(err)
	}
	return withMap{sub, shared}
}

// withMap serves the shared map's files under map/ and the page's own
// files everywhere else. Tests (*.test.mjs) are not embedded.
type withMap struct{ page, shared fs.FS }

func (m withMap) Open(name string) (fs.File, error) {
	if rest, ok := strings.CutPrefix(name, "map/"); ok {
		return m.shared.Open(rest)
	}
	return m.page.Open(name)
}
