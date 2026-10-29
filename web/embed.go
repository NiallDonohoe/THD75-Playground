// Package web holds the front ends' files, embedded into the programs.
package web

import (
	"embed"
	"io/fs"
)

//go:embed aprs/index.html aprs/style.css aprs/*.js
var aprsFiles embed.FS

// APRS is d75aprs's page: index.html, style.css and its scripts.
func APRS() fs.FS {
	sub, err := fs.Sub(aprsFiles, "aprs")
	if err != nil {
		panic(err)
	}
	return sub
}
