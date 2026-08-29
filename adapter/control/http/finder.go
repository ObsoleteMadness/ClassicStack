package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func (s *Server) handleFinderState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	writeJSON(w, f.State())
}

func (s *Server) handleFinderDiscover(w http.ResponseWriter, r *http.Request) {
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, f.LastSeen(r.URL.Query().Get("scheme")))
	case http.MethodPost:
		var req finder.DiscoverRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		out, err := f.Discover(req)
		if err != nil {
			if errors.Is(err, finder.ErrClientDisabled) || errors.Is(err, finder.ErrServiceDisabled) {
				writeFinderErr(w, err)
				return
			}
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, out)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
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
	case http.MethodGet:
		writeJSON(w, f.MountedVolumes())
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleFinderMounted(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	writeJSON(w, f.MountedVolumes())
}

func (s *Server) handleFinderOpen(w http.ResponseWriter, r *http.Request) {
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	switch r.Method {
	case http.MethodPost:
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
			writeFinderErrSession(w, f, req.SessionID, err)
			return
		}
		writeJSON(w, info)
	case http.MethodDelete:
		sessionID := r.URL.Query().Get("session")
		volume := r.URL.Query().Get("volume")
		if sessionID == "" {
			writeJSONError(w, http.StatusBadRequest, "session required")
			return
		}
		if err := f.CloseVolume(sessionID, volume); err != nil {
			writeFinderErr(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
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
		writeFinderErrSession(w, f, sess, err)
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
		writeFinderErrSession(w, f, sess, err)
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
	sess, parent, ok := parentRefQuery(w, r)
	if !ok {
		return
	}
	n, err := f.Lookup(sess, parent, r.URL.Query().Get("name"))
	if err != nil {
		writeFinderErrSession(w, f, sess, err)
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
		SessionID  string          `json:"sessionId"`
		Parent     json.RawMessage `json:"parentId"`
		ParentPath *string         `json:"parentPath"`
		Name       string          `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	parent, err := parseBodyRef(req.Parent, req.ParentPath)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "parentId or parentPath required (not both)")
		return
	}
	n, err := f.Mkdir(req.SessionID, parent, req.Name)
	if err != nil {
		writeFinderErrSession(w, f, req.SessionID, err)
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
		SessionID  string          `json:"sessionId"`
		Parent     json.RawMessage `json:"parentId"`
		ParentPath *string         `json:"parentPath"`
		Name       string          `json:"name"`
		Data       []byte          `json:"data"`
		Resource   []byte          `json:"resource"`
		FinderInfo []byte          `json:"finderInfo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	parent, err := parseBodyRef(req.Parent, req.ParentPath)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "parentId or parentPath required (not both)")
		return
	}
	n, err := f.CreateFile(req.SessionID, parent, req.Name, req.Data, req.Resource, req.FinderInfo)
	if err != nil {
		writeFinderErrSession(w, f, req.SessionID, err)
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
		SessionID string          `json:"sessionId"`
		ID        json.RawMessage `json:"id"`
		Path      *string         `json:"path"`
		Name      string          `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	ref, err := parseBodyRef(req.ID, req.Path)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "id or path required (not both)")
		return
	}
	if err := f.Rename(req.SessionID, ref, req.Name); err != nil {
		writeFinderErrSession(w, f, req.SessionID, err)
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
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	var xferReq finder.TransferRequest
	if json.Unmarshal(body, &xferReq) == nil && xferReq.SrcSession != "" && xferReq.DestSession != "" {
		streamFinderTransfer(w, r, body, func(ctx context.Context, emit func(finder.OpProgress)) error {
			return f.MoveAcross(ctx, xferReq, emit)
		})
		return
	}
	var req struct {
		SessionID  string          `json:"sessionId"`
		ID         json.RawMessage `json:"id"`
		Path       *string         `json:"path"`
		Parent     json.RawMessage `json:"parentId"`
		ParentPath *string         `json:"parentPath"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	ref, err := parseBodyRef(req.ID, req.Path)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "id or path required (not both)")
		return
	}
	parent, err := parseBodyRef(req.Parent, req.ParentPath)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "parentId or parentPath required (not both)")
		return
	}
	if err := f.Move(req.SessionID, ref, parent); err != nil {
		writeFinderErrSession(w, f, req.SessionID, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleFinderCopy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req finder.TransferRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	streamFinderTransfer(w, r, body, func(ctx context.Context, emit func(finder.OpProgress)) error {
		return f.Copy(ctx, req, emit)
	})
}

func (s *Server) handleFinderExpand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req finder.ExpandRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	streamFinderTransfer(w, r, body, func(ctx context.Context, emit func(finder.OpProgress)) error {
		err := f.Expand(ctx, req, emit)
		if err != nil {
			f.InvalidateOnError(req.SessionID, err)
		}
		return err
	})
}

type finderTransferFn func(ctx context.Context, emit func(finder.OpProgress)) error

func streamFinderTransfer(w http.ResponseWriter, r *http.Request, _ []byte, run finderTransferFn) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	type evt struct {
		op  finder.OpProgress
		err error
	}
	ch := make(chan evt, 8)
	go func() {
		err := run(ctx, func(p finder.OpProgress) {
			select {
			case ch <- evt{op: p}:
			case <-ctx.Done():
			}
		})
		if err != nil {
			ch <- evt{err: err}
			return
		}
		ch <- evt{op: finder.OpProgress{Done: true}}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case e := <-ch:
			if e.err != nil {
				_ = writeFinderSSE(w, flusher, finder.OpProgress{Error: e.err.Error()})
				return
			}
			if err := writeFinderSSE(w, flusher, e.op); err != nil {
				return
			}
			if e.op.Done || e.op.Error != "" {
				return
			}
		}
	}
}

func writeFinderSSE(w http.ResponseWriter, flusher http.Flusher, p finder.OpProgress) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: progress\ndata: %s\n\n", data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
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
		SessionID string          `json:"sessionId"`
		ID        json.RawMessage `json:"id"`
		Path      *string         `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	ref, err := parseBodyRef(req.ID, req.Path)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "id or path required (not both)")
		return
	}
	if err := f.Remove(req.SessionID, ref); err != nil {
		writeFinderErrSession(w, f, req.SessionID, err)
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
	sess, ref, ok := sessionRefQuery(w, r)
	if !ok {
		return
	}
	resource := q.Get("fork") == "resource"
	switch r.Method {
	case http.MethodGet:
		off, _ := strconv.ParseInt(q.Get("off"), 10, 64)
		length, _ := strconv.ParseInt(q.Get("len"), 10, 64)
		if rng := r.Header.Get("Range"); rng != "" {
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
		data, err := f.ReadFork(sess, ref, resource, off, length)
		if err != nil {
			writeFinderErrSession(w, f, sess, err)
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
		if err := f.WriteFork(sess, ref, resource, off, body); err != nil {
			writeFinderErrSession(w, f, sess, err)
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
		SessionID  string          `json:"sessionId"`
		ID         json.RawMessage `json:"id"`
		Path       *string         `json:"path"`
		FinderInfo []byte          `json:"finderInfo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	ref, err := parseBodyRef(req.ID, req.Path)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "id or path required (not both)")
		return
	}
	if err := f.WriteFinderInfo(req.SessionID, ref, req.FinderInfo); err != nil {
		writeFinderErrSession(w, f, req.SessionID, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleFinderAttrs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	var req struct {
		SessionID string          `json:"sessionId"`
		ID        json.RawMessage `json:"id"`
		Path      *string         `json:"path"`
		Attrs     map[string]bool `json:"attrs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	ref, err := parseBodyRef(req.ID, req.Path)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "id or path required (not both)")
		return
	}
	if err := f.WriteAttrs(req.SessionID, ref, req.Attrs); err != nil {
		writeFinderErrSession(w, f, req.SessionID, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleFinderResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	sess := r.URL.Query().Get("session")
	path := r.URL.Query().Get("path")
	if sess == "" {
		writeJSONError(w, http.StatusBadRequest, "session required")
		return
	}
	n, err := f.ResolvePath(sess, path)
	if err != nil {
		writeFinderErrSession(w, f, sess, err)
		return
	}
	writeJSON(w, n)
}

func (s *Server) handleFinderPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	sess, ref, ok := sessionRefQuery(w, r)
	if !ok {
		return
	}
	path, err := f.PathOf(sess, ref)
	if err != nil {
		writeFinderErrSession(w, f, sess, err)
		return
	}
	writeJSON(w, map[string]string{"path": path})
}

func (s *Server) handleFinderMount(w http.ResponseWriter, r *http.Request) {
	f := s.requireFinder(w)
	if f == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, f.MountStatus())
	case http.MethodPost:
		var req finder.MountRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		info, err := f.Mount(r.Context(), req)
		if err != nil {
			writeFinderErr(w, err)
			return
		}
		writeJSON(w, info)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			id = r.URL.Query().Get("mountpoint")
		}
		if err := f.Unmount(id); err != nil {
			writeFinderErr(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func sessionRefQuery(w http.ResponseWriter, r *http.Request) (string, finder.NodeRef, bool) {
	sess := r.URL.Query().Get("session")
	if sess == "" {
		writeJSONError(w, http.StatusBadRequest, "session required")
		return "", finder.NodeRef{}, false
	}
	q := r.URL.Query()
	_, hasID := q["id"]
	_, hasPath := q["path"]
	if hasID == hasPath {
		writeJSONError(w, http.StatusBadRequest, "id or path required (not both)")
		return "", finder.NodeRef{}, false
	}
	if hasPath {
		return sess, finder.PathRef(q.Get("path")), true
	}
	id, err := strconv.ParseUint(q.Get("id"), 10, 32)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid id")
		return "", finder.NodeRef{}, false
	}
	return sess, finder.CNIDRef(uint32(id)), true
}

func sessionNodeQuery(w http.ResponseWriter, r *http.Request) (string, finder.NodeRef, bool) {
	return sessionRefQuery(w, r)
}

func parentRefQuery(w http.ResponseWriter, r *http.Request) (string, finder.NodeRef, bool) {
	sess := r.URL.Query().Get("session")
	if sess == "" {
		writeJSONError(w, http.StatusBadRequest, "session required")
		return "", finder.NodeRef{}, false
	}
	q := r.URL.Query()
	_, hasID := q["parent"]
	_, hasPath := q["parentPath"]
	if hasID == hasPath {
		writeJSONError(w, http.StatusBadRequest, "parent or parentPath required (not both)")
		return "", finder.NodeRef{}, false
	}
	if hasPath {
		return sess, finder.PathRef(q.Get("parentPath")), true
	}
	id, err := strconv.ParseUint(q.Get("parent"), 10, 32)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid parent")
		return "", finder.NodeRef{}, false
	}
	return sess, finder.CNIDRef(uint32(id)), true
}

func parseBodyRef(idRaw json.RawMessage, path *string) (finder.NodeRef, error) {
	hasPath := path != nil
	hasID := len(idRaw) > 0 && string(idRaw) != "null"
	if hasPath == hasID {
		return finder.NodeRef{}, finder.ErrBadRef
	}
	if hasPath {
		return finder.PathRef(*path), nil
	}
	var id uint32
	if err := json.Unmarshal(idRaw, &id); err != nil {
		return finder.NodeRef{}, err
	}
	return finder.CNIDRef(id), nil
}

// writeFinderErrSession is writeFinderErr plus connection-loss cleanup: when err
// shows the session's underlying client transport died, the session (and any host
// mount riding it) is dropped before the response is written, so it stops
// appearing in GET /finder/mounted for every web client, not just this request.
func writeFinderErrSession(w http.ResponseWriter, f *finder.Service, sessionID string, err error) {
	f.InvalidateOnError(sessionID, err)
	writeFinderErr(w, err)
}

func writeFinderErr(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	if errors.Is(err, finder.ErrNotFound) {
		code = http.StatusNotFound
	} else if errors.Is(err, finder.ErrBadRef) {
		code = http.StatusBadRequest
	} else if errors.Is(err, finder.ErrReadOnly) {
		code = http.StatusForbidden
	} else if errors.Is(err, finder.ErrMountUnavailable) {
		code = http.StatusNotImplemented
	} else if errors.Is(err, finder.ErrLocalMount) {
		code = http.StatusBadRequest
	} else if errors.Is(err, finder.ErrClientDisabled) || errors.Is(err, finder.ErrServiceDisabled) ||
		errors.Is(err, finder.ErrMountDisabled) || errors.Is(err, finder.ErrLocalServiceDisabled) {
		code = http.StatusForbidden
	}
	writeJSONError(w, code, err.Error())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
