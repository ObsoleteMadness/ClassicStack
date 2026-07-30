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
// data-fork namespace has no stream names. When named-stream forks are enabled, the
// caller first peels the stream suffix with splitStream and passes only the base path
// here; a ':' reaching this function is therefore always an error.
func toStorePath(winPath string) (string, error) {
	p := strings.ReplaceAll(winPath, "\\", "/")
	// A ':' anywhere means a named stream was requested (WinFsp never puts a drive-letter
	// colon in a file path). The base data-fork path carries no stream.
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

// splitStream separates a WinFsp path into its base file path and an optional NTFS
// named-stream suffix. WinFsp presents a stream open as "\dir\file:StreamName" (and
// sometimes the fully-qualified "\dir\file:StreamName:$DATA"), so we split on the FIRST
// ':' after the final path separator — a ':' can only appear as a stream separator, never
// inside a legal store name. The returned base is the "\dir\file" part (fed to
// toStorePath); stream is the raw stream name WITHOUT the leading ':' ("" = the unnamed
// data stream). A trailing ":$DATA" type suffix is trimmed.
func splitStream(winPath string) (base, stream string) {
	p := strings.ReplaceAll(winPath, "\\", "/")
	lastSep := strings.LastIndexByte(p, '/')
	colon := strings.IndexByte(p[lastSep+1:], ':')
	if colon < 0 {
		return winPath, ""
	}
	colon += lastSep + 1
	base = winPath[:colon]
	stream = p[colon+1:]
	// Trim the NTFS stream-type suffix (":$DATA"); the stream name is what precedes it.
	if i := strings.IndexByte(stream, ':'); i >= 0 {
		stream = stream[:i]
	}
	return base, stream
}

// joinStore joins a store dir and a leaf into a '/'-separated store path ("" dir → leaf).
func joinStore(dir, leaf string) string {
	if dir == "" {
		return leaf
	}
	return dir + "/" + leaf
}
