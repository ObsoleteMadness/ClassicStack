//go:build webui || all

package webui

import (
	"encoding/json"
	"net/http"
)

// handleLogHistory returns the retained recent log entries (oldest-first) as
// a JSON array, for clients that want a one-shot fetch rather than the stream.
func (s *Server) handleLogHistory(w http.ResponseWriter, r *http.Request) {
	if s.opts.Plane == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, s.opts.Plane.LogHistory())
}

// handleLogStream is a Server-Sent Events endpoint that first replays the
// retained log history, then streams new entries as they are logged. It
// mirrors handleStatsStream: subscribe up front so no entry is missed between
// the snapshot and the live stream, then drain history, then forward.
func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	if s.opts.Plane == nil {
		writeError(w, http.StatusServiceUnavailable, errNoPlane)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errNoFlush)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Subscribe before snapshotting so an entry logged in the gap is captured
	// by the live channel rather than lost. The subscriber's buffer absorbs
	// any overlap; duplicates are harmless for a log view.
	entries, cancel := s.opts.Plane.SubscribeLogs()
	defer cancel()

	for _, e := range s.opts.Plane.LogHistory() {
		if !writeLogEvent(w, e) {
			return
		}
	}
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-entries:
			if !ok {
				return
			}
			if !writeLogEvent(w, e) {
				return
			}
			flusher.Flush()
		}
	}
}

// writeLogEvent marshals one entry as an SSE "data:" frame, returning false
// on write error so the caller can stop.
func writeLogEvent(w http.ResponseWriter, e any) bool {
	payload, err := json.Marshal(e)
	if err != nil {
		return true // skip this entry, keep the stream alive
	}
	if _, err := w.Write([]byte("data: ")); err != nil {
		return false
	}
	if _, err := w.Write(payload); err != nil {
		return false
	}
	_, err = w.Write([]byte("\n\n"))
	return err == nil
}
