//go:build webui || all

package webui

import (
	"encoding/json"
	"net/http"
)

// handleStatsStream is a Server-Sent Events endpoint that pushes a stats
// Frame to the client every second. It subscribes to the control plane's
// broadcaster and unsubscribes when the client disconnects.
func (s *Server) handleStatsStream(w http.ResponseWriter, r *http.Request) {
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

	frames, cancel := s.opts.Plane.Subscribe()
	defer cancel()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-frames:
			if !ok {
				return
			}
			payload, err := json.Marshal(frame)
			if err != nil {
				continue
			}
			if _, err := w.Write([]byte("data: ")); err != nil {
				return
			}
			if _, err := w.Write(payload); err != nil {
				return
			}
			if _, err := w.Write([]byte("\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
