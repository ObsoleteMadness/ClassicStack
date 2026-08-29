package http

import "net/http"

// Lifecycle wires process-level shutdown and restart hooks from the web admin.
// The compose edge (cmd/internal/cli) installs these; nil leaves the routes at 501.
type Lifecycle struct {
	// Shutdown requests a graceful stop of the ClassicStack process.
	Shutdown func()
	// Restart requests a graceful stop followed by relaunch (when supported).
	Restart func()
}

// SetLifecycle installs process-level shutdown/restart hooks. Safe before Start.
func (s *Server) SetLifecycle(lc Lifecycle) { s.lifecycle = lc }

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.lifecycle.Shutdown == nil {
		writeJSONError(w, http.StatusNotImplemented, "shutdown unavailable")
		return
	}
	w.WriteHeader(http.StatusOK)
	go s.lifecycle.Shutdown()
}

func (s *Server) handleStackRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.lifecycle.Restart == nil {
		writeJSONError(w, http.StatusNotImplemented, "restart unavailable")
		return
	}
	w.WriteHeader(http.StatusOK)
	go s.lifecycle.Restart()
}
