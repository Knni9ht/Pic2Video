package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var files embed.FS

// Dist returns the production frontend, built with npm run build.
func Dist() (fs.FS, error) {
	return fs.Sub(files, "dist")
}
