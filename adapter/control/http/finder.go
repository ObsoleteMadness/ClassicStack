package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/adapter/control/finder"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
)

func (s *Server) requireFinder(w http.ResponseWriter) *finder.Service {
	if s.finder == nil {
		writeJSONError(w, statusForErr(control.ErrUnavailable), control.ErrUnavailable.Error())
		return nil
	}
	return s.finder
}

func (s *Server) handleFinderLocal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	writeJSON(w, f.LocalVolumes())
}

func (s *Server) handleFinderDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	var req finder.DiscoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := f.Discover(req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleFinderSessions(w http.ResponseWriter, r *http.Request) {
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	switch r.Method {
	case http.MethodPost:
		var req finder.ConnectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		info, err := f.Connect(r.Context(), req)
		if err != nil {
			writeFinderErr(w, err)
			return
		}
		writeJSON(w, info)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "missing id")
			return
		}
		if err := f.CloseSession(id); err != nil {
			writeFinderErr(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleFinderOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	var req struct {
		SessionID string `json:"sessionId"`
		Volume    string `json:"volume"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := f.OpenVolume(req.SessionID, req.Volume)
	if err != nil {
		writeFinderErr(w, err)
		return
	}
	writeJSON(w, info)
}

func (s *Server) handleFinderNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	sess, id, ok := sessionNodeQuery(w, r)
	if !ok {
		return
	}
	n, err := f.GetNode(sess, id)
	if err != nil {
		writeFinderErr(w, err)
		return
	}
	writeJSON(w, n)
}

func (s *Server) handleFinderChildren(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	sess, id, ok := sessionNodeQuery(w, r)
	if !ok {
		return
	}
	n, err := f.Children(sess, id)
	if err != nil {
		writeFinderErr(w, err)
		return
	}
	writeJSON(w, n)
}

func (s *Server) handleFinderLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	sess := r.URL.Query().Get("session")
	parent, err := strconv.ParseUint(r.URL.Query().Get("parent"), 10, 32)
	if err != nil || sess == "" {
		writeJSONError(w, http.StatusBadRequest, "session and parent required")
		return
	}
	n, err := f.Lookup(sess, uint32(parent), r.URL.Query().Get("name"))
	if err != nil {
		writeFinderErr(w, err)
		return
	}
	writeJSON(w, n)
}

func (s *Server) handleFinderMkdir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	var req struct {
		SessionID string `json:"sessionId"`
		Parent    uint32 `json:"parentId"`
		Name      string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := f.Mkdir(req.SessionID, req.Parent, req.Name)
	if err != nil {
		writeFinderErr(w, err)
		return
	}
	writeJSON(w, n)
}

func (s *Server) handleFinderCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	var req struct {
		SessionID  string `json:"sessionId"`
		Parent     uint32 `json:"parentId"`
		Name       string `json:"name"`
		Data       []byte `json:"data"`
		Resource   []byte `json:"resource"`
		FinderInfo []byte `json:"finderInfo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := f.CreateFile(req.SessionID, req.Parent, req.Name, req.Data, req.Resource, req.FinderInfo)
	if err != nil {
		writeFinderErr(w, err)
		return
	}
	writeJSON(w, n)
}

func (s *Server) handleFinderRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	var req struct {
		SessionID string `json:"sessionId"`
		ID        uint32 `json:"id"`
		Name      string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := f.Rename(req.SessionID, req.ID, req.Name); err != nil {
		writeFinderErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleFinderMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	var req struct {
		SessionID string `json:"sessionId"`
		ID        uint32 `json:"id"`
		Parent    uint32 `json:"parentId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := f.Move(req.SessionID, req.ID, req.Parent); err != nil {
		writeFinderErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleFinderRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	var req struct {
		SessionID string `json:"sessionId"`
		ID        uint32 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := f.Remove(req.SessionID, req.ID); err != nil {
		writeFinderErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleFinderFork(w http.ResponseWriter, r *http.Request) {
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	q := r.URL.Query()
	sess := q.Get("session")
	id, err := strconv.ParseUint(q.Get("id"), 10, 32)
	if err != nil || sess == "" {
		writeJSONError(w, http.StatusBadRequest, "session and id required")
		return
	}
	resource := q.Get("fork") == "resource"
	switch r.Method {
	case http.MethodGet:
		off, _ := strconv.ParseInt(q.Get("off"), 10, 64)
		length, _ := strconv.ParseInt(q.Get("len"), 10, 64)
		if rng := r.Header.Get("Range"); rng != "" {
			// bytes=start-end
			if _, rest, ok := strings.Cut(rng, "="); ok {
				start, end, _ := strings.Cut(rest, "-")
				off, _ = strconv.ParseInt(start, 10, 64)
				if end != "" {
					if e, err := strconv.ParseInt(end, 10, 64); err == nil && e >= off {
						length = e - off + 1
					}
				}
			}
		}
		data, err := f.ReadFork(sess, uint32(id), resource, off, length)
		if err != nil {
			writeFinderErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(data)
	case http.MethodPut:
		off, _ := strconv.ParseInt(q.Get("off"), 10, 64)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := f.WriteFork(sess, uint32(id), resource, off, body); err != nil {
			writeFinderErr(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleFinderFinderInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	var req struct {
		SessionID  string `json:"sessionId"`
		ID         uint32 `json:"id"`
		FinderInfo []byte `json:"finderInfo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := f.WriteFinderInfo(req.SessionID, req.ID, req.FinderInfo); err != nil {
		writeFinderErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func sessionNodeQuery(w http.ResponseWriter, r *http.Request) (string, uint32, bool) {
	sess := r.URL.Query().Get("session")
	raw := r.URL.Query().Get("id")
	id, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || sess == "" {
		writeJSONError(w, http.StatusBadRequest, "session and id required")
		return "", 0, false
	}
	return sess, uint32(id), true
}

func writeFinderErr(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	if errors.Is(err, finder.ErrNotFound) {
		code = http.StatusNotFound
	} else if errors.Is(err, finder.ErrReadOnly) {
		code = http.StatusForbidden
	}
	writeJSONError(w, code, err.Error())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
