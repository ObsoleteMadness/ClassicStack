package fs

import "path/filepath"

// defaultStorePath derives the default on-disk location for a share's
// metastore-backed MetaEngine store: ".classicstack/meta<ext>" under the
// share's host directory. Two shares independently pointing at the same
// spec.Path (e.g. an AFP volume and an SMB share exporting the same host
// directory, §10d) derive the same file and safely share it — both backends
// (mem's load-on-open, sqlite's CREATE TABLE IF NOT EXISTS) tolerate reopening
// an existing file; concurrent-write locking is a pre-existing sqlite-file
// concern, not new here.
//
// spec.Path is empty for synthetic backends (memfs, macgarden) and some
// archive-backed ones — there is no stable host location to derive from, so
// the store stays fully volatile (the caller passes the empty result straight
// to metastore.Open, which treats "" as "in-memory, no snapshot").
func defaultStorePath(spec ShareSpec, ext string) string {
	if spec.Path == "" {
		return ""
	}
	return filepath.Join(spec.Path, ".classicstack", "meta"+ext)
}
