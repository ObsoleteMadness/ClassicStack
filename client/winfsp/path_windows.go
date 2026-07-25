//go:build windows

package winfsp

import (
	"errors"
	"path"
	"strings"
)

// errInvalidName is mapped to STATUS_OBJECT_NAME_INVALID by the delegate wrappers.
var errInvalidName = errors.New("winfsp: invalid object name")

// toStorePath converts a WinFsp path (backslash-separated, leading '\', root "\") to the
// '/'-separated, root-is-"" store path the core/fs.ForkFS uses (the same convention
// client/xfer and memfs follow). It rejects '..' escapes and any ':stream' suffix — the
// mount surfaces exactly the namespace the chosen fork backend produces and does not route
// NTFS stream names to forks (go-winfsp exposes no stream enumeration anyway).
func toStorePath(winPath string) (string, error) {
	p := strings.ReplaceAll(winPath, "\\", "/")
	// A ':' anywhere means a named stream was requested (WinFsp never puts a drive-letter
	// colon in a file path). We do not implement streams.
	if strings.ContainsRune(p, ':') {
		return "", errInvalidName
	}
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "", nil
	}
	clean := path.Clean("/" + p)
	// path.Clean collapses '..'; if the result still tries to escape (shouldn't after the
	// leading '/'), or contains a '..' element, reject it.
	if clean == "/.." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", errInvalidName
	}
	return strings.TrimPrefix(clean, "/"), nil
}

// joinStore joins a store dir and a leaf into a '/'-separated store path ("" dir → leaf).
func joinStore(dir, leaf string) string {
	if dir == "" {
		return leaf
	}
	return dir + "/" + leaf
}
