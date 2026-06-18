package http

import (
	"crypto/rand"
	"encoding/json"
	"net/http"

	"github.com/ObsoleteMadness/ClassicStack/core/auth"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// basicRealm is the WWW-Authenticate realm the browser shows in its login dialog.
const basicRealm = "ClassicStack"

// authGate wraps the mux with the web-management-interface access control (§4-ter).
// It has two modes, keyed off whether an admin credential is configured:
//
//   - First-run (no admin set): only POST /setup is allowed through — it creates the
//     admin. Every other request returns 409 with {"setup_required":true} so the SPA
//     can show the setup screen. 409 (not 401) is deliberate: with no admin to
//     authenticate against, a 401 would pop a useless Basic-auth dialog.
//   - Configured: /setup is refused (409 already-configured — no re-bootstrap without
//     auth) and every other request must carry valid HTTP Basic credentials, verified
//     in constant time against the stored salted hash. A miss returns 401 with a
//     WWW-Authenticate header so the browser prompts.
//
// SECURITY NOTE: HTTP Basic sends credentials base64-encoded, NOT encrypted. This
// adapter has no TLS of its own, so it must run over loopback or behind TLS
// termination. This matches the honest-security posture (charter §55-56): simple,
// real protection, with its limits documented rather than hidden.
func (s *Server) authGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The SPA's static assets (index/app.js/app.css) are served unauthenticated so
		// the page can LOAD and then drive setup (409) or prompt for Basic auth (401)
		// from the browser. They carry no secrets; every data route stays gated below.
		if spaStaticPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		configured := s.plane.AdminConfigured()

		if r.URL.Path == "/setup" {
			// /setup is reachable only during first-run; once an admin exists it is
			// sealed (changing the admin then needs an authenticated path, a follow-on).
			if configured {
				writeJSONError(w, http.StatusConflict, "admin already configured")
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		if !configured {
			// First-run: everything except /setup reports "set me up first".
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]bool{"setup_required": true})
			return
		}

		// Configured: enforce Basic auth on every other route.
		user, pass, ok := r.BasicAuth()
		if !ok || !s.adminAuth().Verify(user, pass) {
			w.Header().Set("WWW-Authenticate", `Basic realm="`+basicRealm+`"`)
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// adminAuth fetches the live admin credential from the plane. Config() returns a
// (masked) clone, but AdminAuth is deliberately never masked (its value is a hash, not
// a reversible secret), so Verify works on the round-tripped model.
func (s *Server) adminAuth() config.AdminAuth {
	m, err := s.plane.Config()
	if err != nil || m == nil {
		return config.AdminAuth{}
	}
	return m.AdminAuth
}

// handleSetup creates the first-run admin credential. It accepts {user, password},
// generates a salt (crypto/rand — the adapter ring owns randomness, keeping core/auth
// reflection-free), derives the PBKDF2-SHA256 hash, and hands the hash-only DTO to the
// plane, which stamps it into the model and auto-saves server.toml. Refused once an
// admin exists (the gate already blocks this, but re-check for defence in depth).
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.plane.AdminConfigured() {
		writeJSONError(w, http.StatusConflict, "admin already configured")
		return
	}

	var body struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.User == "" {
		writeJSONError(w, http.StatusBadRequest, auth.ErrEmptyUsername.Error())
		return
	}
	if body.Password == "" {
		writeJSONError(w, http.StatusBadRequest, auth.ErrEmptyPassword.Error())
		return
	}

	salt := make([]byte, auth.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cred := auth.DeriveCredential(body.Password, salt)
	a := config.AdminAuth{User: body.User, SaltHex: cred.SaltHex(), HashHex: cred.HashHex()}

	rev, err := s.plane.SetAdmin(r.Context(), a)
	if err != nil {
		http.Error(w, err.Error(), statusForErr(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"revision": rev})
}

// writeJSONError writes a {"error":msg} body with the given status.
func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
