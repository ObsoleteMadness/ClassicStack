//go:build webui || all

package webui

import (
	"encoding/json"
	"net/http"
)

// extMapResponse is the GET /api/extmap payload: the resolved file path and
// its current text contents.
type extMapResponse struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// extMapSaveRequest is the PUT /api/extmap body.
type extMapSaveRequest struct {
	Content string `json:"content"`
}

// handleExtMap serves the AFP extension-map editor: GET returns the current
// file, PUT validates and saves edited contents (returning the backup path).
// Save does not restart AFP; the new map loads on the next config Apply.
func (s *Server) handleExtMap(w http.ResponseWriter, r *http.Request) {
	if s.opts.Plane == nil {
		writeError(w, http.StatusServiceUnavailable, errNoPlane)
		return
	}
	switch r.Method {
	case http.MethodGet:
		path, data, err := s.opts.Plane.ExtMap()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, extMapResponse{Path: path, Content: string(data)})
	case http.MethodPut:
		var req extMapSaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		backup, err := s.opts.Plane.SaveExtMap([]byte(req.Content))
		if err != nil {
			// A parse failure is the operator's mistake, not a server fault.
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"saved": true, "backup": backup})
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeError(w, http.StatusMethodNotAllowed, errMethod)
	}
}
