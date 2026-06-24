//go:build (afp || smb) && !zipfs && !all

// Package zipfs's disabled stub: in a build that has a file service (afp/smb) but was
// NOT built with the `zipfs` tag, register the "zipfs" fs_type so a config naming it
// fails with an actionable "rebuild with -tags zipfs" message rather than the generic
// "unknown fs type" error. The real backend (archive/zip + compress/flate) is only
// linked under the zipfs/all tag, so a minimal build stays free of that dependency.
// Mirrors the macgarden disabled stub.
package zipfs

import (
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	corefs "github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// ErrZipFSDisabled is returned when a volume/share is configured with
// fs_type = "zipfs" in a binary built without the "zipfs" build tag.
var ErrZipFSDisabled = errors.New("zipfs backend not built; rebuild with -tags zipfs")

func init() {
	corefs.RegisterFS("zipfs", func(corefs.ShareSpec, bus.Bus, metastore.Store) (corefs.FileSystem, error) {
		return nil, ErrZipFSDisabled
	})
}
