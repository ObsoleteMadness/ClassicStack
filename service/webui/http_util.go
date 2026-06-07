//go:build webui || all

package webui

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

var (
	errNoPlane  = errors.New("management plane unavailable")
	errMethod   = errors.New("method not allowed")
	errNotFound = errors.New("not found")
	errNoFlush  = errors.New("streaming unsupported")
)

// writeJSON encodes v as the response body with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// writeError encodes a JSON error envelope.
func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// parseServicePath extracts {name} and {action} from
// /api/services/{name}/{action}.
func parseServicePath(path string) (name, action string) {
	rest := strings.TrimPrefix(path, "/api/services/")
	if rest == path { // prefix not present
		return "", ""
	}
	parts := strings.SplitN(strings.Trim(rest, "/"), "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
