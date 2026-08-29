//go:build (afp || smb) && !macgarden && !all

// Package macgarden's disabled stub: in a build that has a file service (afp/smb) but
// was NOT built with the `macgarden` tag, register the "macgarden" fs_type so a config
// naming it fails with an actionable "rebuild with -tags macgarden" message rather than
// the generic "no backend registered" error. The real backend (the HTTP scraper + the
// x/net/html parser) is only linked under the macgarden/all tag, so a minimal build
// stays free of that dependency. Mirrors the legacy service/afp/macgarden_fs_stub.go.
package macgarden

import (
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	corefs "github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// ErrMacGardenDisabled is returned when a volume/share is configured with
// fs_type = "macgarden" in a binary built without the "macgarden" build tag.
var ErrMacGardenDisabled = errors.New("macgarden backend not built; rebuild with -tags macgarden")

func init() {
	corefs.RegisterFS("macgarden", func(corefs.ShareSpec, bus.Bus, metastore.Store) (corefs.FileSystem, error) {
		return nil, ErrMacGardenDisabled
	})
}
