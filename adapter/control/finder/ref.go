package finder

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// NodeRef is a scheme-native catalog address: a CNID or a store path.
// JSON is a number (CNID) or a string (path, including "" for volume root).
type NodeRef struct {
	ID     uint32
	Path   string
	ByPath bool
}

// CNIDRef is a CNID-addressed NodeRef.
func CNIDRef(id uint32) NodeRef { return NodeRef{ID: id} }

// PathRef is a path-addressed NodeRef. Empty path is the volume root.
func PathRef(path string) NodeRef { return NodeRef{Path: path, ByPath: true} }

func (r NodeRef) MarshalJSON() ([]byte, error) {
	if r.ByPath {
		return json.Marshal(r.Path)
	}
	return json.Marshal(r.ID)
}

func (r *NodeRef) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		r.ByPath = true
		return json.Unmarshal(b, &r.Path)
	}
	r.ByPath = false
	return json.Unmarshal(b, &r.ID)
}

func (r NodeRef) String() string {
	if r.ByPath {
		return r.Path
	}
	return strconv.FormatUint(uint64(r.ID), 10)
}

func parentPathOf(path string) string {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return ""
	}
	return path[:i]
}

func leafOf(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func (s *Service) storePath(sess *Session, ref NodeRef) (string, error) {
	if sess.addressBy() == AddressPath {
		if !ref.ByPath {
			return "", fmt.Errorf("finder: path volume requires path: %w", ErrBadRef)
		}
		return ref.Path, nil
	}
	if ref.ByPath {
		return "", fmt.Errorf("finder: CNID volume requires id: %w", ErrBadRef)
	}
	return s.pathFor(sess, ref.ID)
}

