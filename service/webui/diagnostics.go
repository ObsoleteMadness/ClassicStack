//go:build webui || all

package webui

import (
	"net/http"
	"strconv"
)

// registerDiagnosticRoutes wires the read-only network-probe endpoints.
// Each delegates to the control plane's Diagnostics facade, which reports
// ErrDiagUnavailable for probes not compiled into this build.
func (s *Server) registerDiagnosticRoutes() {
	s.mux.HandleFunc("/api/diag/zones", s.handleDiagZones)
	s.mux.HandleFunc("/api/diag/zip", s.handleDiagZIP)
	s.mux.HandleFunc("/api/diag/ddp", s.handleDiagDDP)
	s.mux.HandleFunc("/api/diag/aep-echo", s.handleDiagAEPEcho)
	s.mux.HandleFunc("/api/diag/smb-browse", s.handleDiagSMBBrowse)
	s.mux.HandleFunc("/api/diag/macip-leases", s.handleDiagMacIPLeases)
}

func (s *Server) handleDiagZones(w http.ResponseWriter, r *http.Request) {
	if s.opts.Plane == nil {
		writeError(w, http.StatusServiceUnavailable, errNoPlane)
		return
	}
	zones, err := s.opts.Plane.Diagnostics().ListZones(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, zones)
}

func (s *Server) handleDiagZIP(w http.ResponseWriter, r *http.Request) {
	if s.opts.Plane == nil {
		writeError(w, http.StatusServiceUnavailable, errNoPlane)
		return
	}
	zones, err := s.opts.Plane.Diagnostics().ZIPEnumerate(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, zones)
}

func (s *Server) handleDiagDDP(w http.ResponseWriter, r *http.Request) {
	if s.opts.Plane == nil {
		writeError(w, http.StatusServiceUnavailable, errNoPlane)
		return
	}
	nets, err := s.opts.Plane.Diagnostics().DDPEnumerate(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, nets)
}

func (s *Server) handleDiagAEPEcho(w http.ResponseWriter, r *http.Request) {
	if s.opts.Plane == nil {
		writeError(w, http.StatusServiceUnavailable, errNoPlane)
		return
	}
	net64, err := strconv.ParseUint(r.URL.Query().Get("network"), 10, 16)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	node64, err := strconv.ParseUint(r.URL.Query().Get("node"), 10, 8)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.opts.Plane.Diagnostics().AEPEcho(r.Context(), uint16(net64), uint8(node64))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleDiagSMBBrowse(w http.ResponseWriter, r *http.Request) {
	if s.opts.Plane == nil {
		writeError(w, http.StatusServiceUnavailable, errNoPlane)
		return
	}
	servers, err := s.opts.Plane.Diagnostics().SMBBrowse(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, servers)
}

func (s *Server) handleDiagMacIPLeases(w http.ResponseWriter, r *http.Request) {
	if s.opts.Plane == nil {
		writeError(w, http.StatusServiceUnavailable, errNoPlane)
		return
	}
	leases, err := s.opts.Plane.Diagnostics().MacIPLeases(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, leases)
}
