//go:build webui || all

package webui

import (
	"encoding/json"
	"net/http"

	"github.com/ObsoleteMadness/ClassicStack/config"
)

// routes registers all HTTP handlers. Static assets are served from the
// embedded SPA; everything under /api delegates to the control plane.
func (s *Server) routes() {
	s.mux.Handle("/", s.staticHandler())

	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/api/interfaces", s.handleInterfaces)
	s.mux.HandleFunc("/api/fs-types", s.handleFSTypes)
	s.mux.HandleFunc("/api/serial-ports", s.handleSerialPorts)
	s.mux.HandleFunc("/api/config", s.handleConfig)
	s.mux.HandleFunc("/api/config/apply", s.handleApply)
	s.mux.HandleFunc("/api/config/save", s.handleSave)
	s.mux.HandleFunc("/api/config/download", s.handleDownload)
	s.mux.HandleFunc("/api/extmap", s.handleExtMap)
	s.mux.HandleFunc("/api/services/", s.handleServiceAction)
	s.mux.HandleFunc("/api/restart-all", s.handleRestartAll)
	s.mux.HandleFunc("/api/stats/stream", s.handleStatsStream)
	s.mux.HandleFunc("/api/logs", s.handleLogHistory)
	s.mux.HandleFunc("/api/logs/stream", s.handleLogStream)
	s.mux.HandleFunc("/api/logs/download", s.handleLogDownload)

	s.registerDiagnosticRoutes()
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if s.opts.Plane == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, s.opts.Plane.Status())
}

func (s *Server) handleInterfaces(w http.ResponseWriter, r *http.Request) {
	if s.opts.Plane == nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	names, err := s.opts.Plane.ListInterfaces()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, names)
}

func (s *Server) handleFSTypes(w http.ResponseWriter, r *http.Request) {
	if s.opts.Plane == nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	writeJSON(w, http.StatusOK, s.opts.Plane.ListFSTypes())
}

func (s *Server) handleSerialPorts(w http.ResponseWriter, r *http.Request) {
	if s.opts.Plane == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	ports, err := s.opts.Plane.ListSerialPorts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, ports)
}

// configResponse is the GET /api/config payload.
type configResponse struct {
	Config *config.Model `json:"config"`
	Dirty  bool          `json:"dirty"`
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if s.opts.Plane == nil {
		writeError(w, http.StatusServiceUnavailable, errNoPlane)
		return
	}
	switch r.Method {
	case http.MethodGet:
		cfg, dirty := s.opts.Plane.Config()
		model, _ := cfg.(*config.Model)
		writeJSON(w, http.StatusOK, configResponse{Config: model, Dirty: dirty})
	case http.MethodPut:
		var edit config.Model
		if err := json.NewDecoder(r.Body).Decode(&edit); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.opts.Plane.Stage(&edit)
		writeJSON(w, http.StatusOK, map[string]any{"dirty": true})
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeError(w, http.StatusMethodNotAllowed, errMethod)
	}
}

func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errMethod)
		return
	}
	if s.opts.Plane == nil {
		writeError(w, http.StatusServiceUnavailable, errNoPlane)
		return
	}
	if err := s.opts.Plane.Apply(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"applied": true})
}

func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errMethod)
		return
	}
	if s.opts.Plane == nil {
		writeError(w, http.StatusServiceUnavailable, errNoPlane)
		return
	}
	backup, err := s.opts.Plane.Save()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "backup": backup})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if s.opts.Plane == nil {
		writeError(w, http.StatusServiceUnavailable, errNoPlane)
		return
	}
	data, err := s.opts.Plane.Export()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/toml")
	w.Header().Set("Content-Disposition", `attachment; filename="server.toml"`)
	_, _ = w.Write(data)
}

// handleRestartAll handles POST /api/restart-all: restart the whole stack
// (all ports, the router, and every hook) without a configuration change.
func (s *Server) handleRestartAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errMethod)
		return
	}
	if s.opts.Plane == nil {
		writeError(w, http.StatusServiceUnavailable, errNoPlane)
		return
	}
	if err := s.opts.Plane.RestartAll(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": "restart-all"})
}

// handleServiceAction handles POST /api/services/{name}/restart.
func (s *Server) handleServiceAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errMethod)
		return
	}
	if s.opts.Plane == nil {
		writeError(w, http.StatusServiceUnavailable, errNoPlane)
		return
	}
	name, action := parseServicePath(r.URL.Path)
	if name == "" {
		writeError(w, http.StatusNotFound, errNotFound)
		return
	}
	var err error
	switch action {
	case "start":
		err = s.opts.Plane.StartService(r.Context(), name)
	case "stop":
		err = s.opts.Plane.StopService(name)
	case "restart":
		err = s.opts.Plane.RestartService(r.Context(), name)
	default:
		writeError(w, http.StatusNotFound, errNotFound)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": name, "action": action})
}
