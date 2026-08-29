//go:build !webui && !all

package http

import "net/http"

// This is the API-only build (the webui tag is absent): no SPA is embedded, so the
// HTTP control adapter serves only the JSON/SSE API. mountSPA is a no-op and no
// static path is exempted from the auth gate — every route is gated. The §8 build-
// tag gate, mirroring how the legacy service/webui embed was tag-guarded.

func (s *Server) mountSPA(*http.ServeMux) {}

func spaStaticPath(string) bool { return false }
