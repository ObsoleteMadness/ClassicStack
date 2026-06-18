//go:build webui || all

package http

import (
	"embed"
	"io/fs"
	"net/http"
)

// assets embeds the SPA static files (index.html, app.js, app.css). The directory
// is a sibling package path, embedded only under the webui||all tag so a headless /
// API-only build (or TinyGo) carries no HTML payload — the §8 build-tag gate, the
// new-ring analogue of the legacy service/webui embed.
//
//go:embed spa/index.html spa/app.js spa/app.css
var assets embed.FS

// mountSPA registers the embedded SPA on the mux: index.html at "/" and the assets
// by name. It is served as plain static files; all dynamic data comes from the JSON
// API the page calls. Under !webui the stub mounts nothing (API-only).
func (s *Server) mountSPA(mux *http.ServeMux) {
	sub, err := fs.Sub(assets, "spa")
	if err != nil {
		return // embed guarantees the subtree; defensive only
	}
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Serve index.html's bytes directly for "/" — NOT by rewriting the path into
		// the file server, which would 301-redirect "/" → "/index.html" (its canonical-
		// index behaviour). Named assets (/app.js, /app.css) go to the file server.
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(index)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

// spaStaticPath reports whether p is one of the SPA's static asset paths, which the
// auth gate lets through unauthenticated so the page can load and present the setup
// or login flow (the assets carry no secrets; all data is behind the gated API).
func spaStaticPath(p string) bool {
	switch p {
	case "/", "/index.html", "/app.js", "/app.css":
		return true
	}
	return false
}
