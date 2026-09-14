// Package web serves the embedded React dashboard SPA.
//
// go:embed cannot use ".." paths, so Makefile syncs frontend/dist → pkg/web/dist
// before `go build`. A minimal placeholder dist is committed so `go build ./...`
// works without running npm first (use `make build` for a real dashboard).
package web

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves static assets with SPA fallback to index.html for client routes.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic("web: embed dist: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		// Try the exact file; if missing, fall back to index.html (SPA routing).
		f, err := sub.Open(path)
		if err != nil {
			serveIndex(w, r, sub)
			return
		}
		stat, err := f.Stat()
		_ = f.Close()
		if err != nil || stat.IsDir() {
			serveIndex(w, r, sub)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, sub fs.FS) {
	f, err := sub.Open("index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.Copy(w, f)
}
