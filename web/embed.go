// Package web embeds the GUI built with Vite (npm run build in this
// directory) into the tabula binary.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Handler serves the GUI from dir if set, else the embedded build. Unknown
// paths get index.html, so client-side routes work on reload.
func Handler(dir string) http.Handler {
	var files fs.FS
	if dir != "" {
		files = os.DirFS(dir)
	} else {
		files, _ = fs.Sub(dist, "dist")
	}
	if _, err := fs.Stat(files, "index.html"); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(notBuilt))
		})
	}
	server := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" {
			if _, err := fs.Stat(files, name); err != nil {
				r.URL.Path = "/"
			}
		}
		server.ServeHTTP(w, r)
	})
}

const notBuilt = `<!doctype html><meta charset="utf-8"><title>tabula</title>
<body style="font:15px system-ui;margin:2rem;max-width:40rem">
<h1>tabula</h1><p>The GUI was not built into this binary.</p>
<pre>cd web && npm ci && npm run build && cd .. && go build ./cmd/tabula</pre>
<p>The API is available under <a href="/api/status">/api/</a>.</p></body>`
