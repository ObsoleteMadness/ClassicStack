//go:build webui || all

package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

// assetsFS holds the pre-built single-page app. The committed assets/ tree
// is what ships; service/webui/web/ holds the (optional) source with a
// documented rebuild step.
//
//go:embed assets
var assetsFS embed.FS

// staticHandler serves the embedded SPA, falling back to index.html for
// unknown paths so client-side routing works.
func (s *Server) staticHandler() http.Handler {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		// Embedding guarantees assets/ exists; this is unreachable in a
		// correctly built binary.
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Embedded files carry a zero modtime, so http.FileServer emits no
		// useful Last-Modified/ETag and browsers may cache them indefinitely.
		// After a binary upgrade that leaves a stale app.js running against a
		// fresh index.html (the two fall out of lockstep). Tell the browser to
		// always revalidate the SPA shell so the assets stay consistent.
		w.Header().Set("Cache-Control", "no-cache")
		if _, err := fs.Stat(sub, trimLeadingSlash(r.URL.Path)); err != nil && r.URL.Path != "/" {
			// Unknown path: serve the SPA shell.
			r2 := new(http.Request)
			*r2 = *r
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func trimLeadingSlash(p string) string {
	if p == "/" || p == "" {
		return "index.html"
	}
	if p[0] == '/' {
		return p[1:]
	}
	return p
}
