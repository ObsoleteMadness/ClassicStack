//go:build darwin || linux

package finder

import (
	csfuse "github.com/ObsoleteMadness/ClassicStack/client/fuse"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

func platformMountAvailable() bool { return csfuse.Available() }

func platformMount(fsys fs.ForkFS, mountpoint, label string, readOnly bool) (func(), error) {
	m, err := csfuse.MountAt(fsys, mountpoint, csfuse.Options{
		VolumeLabel: label,
		NativeForks: true,
		ReadOnly:    readOnly,
	})
	if err != nil {
		return nil, err
	}
	return m.Unmount, nil
}
