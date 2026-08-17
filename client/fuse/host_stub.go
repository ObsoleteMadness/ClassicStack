//go:build !fuse || !cgo || (!darwin && !linux)

package fuse

import "github.com/ObsoleteMadness/ClassicStack/core/fs"

// MountAt is unavailable unless the binary is built with `-tags fuse` and cgo
// on Darwin or Linux.
func MountAt(_ fs.ForkFS, _ string, _ Options) (*Mount, error) {
	return nil, ErrUnsupported
}
