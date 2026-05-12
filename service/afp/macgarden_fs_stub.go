//go:build (afp || all) && !macgarden && !all

package afp

import (
	"errors"
)

// ErrMacGardenDisabled is returned when a volume is configured with
// fs_type = "macgarden" in a binary built without the "macgarden" build tag.
var ErrMacGardenDisabled = errors.New("macgarden backend not built; rebuild with -tags macgarden")

func init() {
	RegisterFS(FSTypeMacGarden, func(_ VolumeConfig, _ Options) (FileSystem, error) {
		return nil, ErrMacGardenDisabled
	})
}
