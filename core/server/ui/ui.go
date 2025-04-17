package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var StaticFiles embed.FS

// GetHandler returns an http.Handler that serves the UI static files
func GetHandler() http.Handler {
	fsys, err := fs.Sub(StaticFiles, "static")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(fsys))
}