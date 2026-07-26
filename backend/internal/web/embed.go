// Package web embeds the built single-page frontend so the whole application
// ships as one static binary. The Makefile copies the Vite build output into
// the dist/ directory before compiling; a placeholder index.html is kept under
// version control so the package always compiles even before a build.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embeddedFrontendFiles embed.FS

// FrontendFileSystem returns the embedded frontend rooted at the dist directory.
func FrontendFileSystem() (fs.FS, error) {
	return fs.Sub(embeddedFrontendFiles, "dist")
}
