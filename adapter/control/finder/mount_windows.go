//go:build windows

package finder

import (
	"github.com/ObsoleteMadness/ClassicStack/client/winfsp"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

func platformMountAvailable() bool { return true }

func platformMount(fsys fs.ForkFS, mountpoint, label string, readOnly bool) (func(), error) {
	m, err := winfsp.MountAt(fsys, mountpoint, winfsp.Options{
		VolumeLabel: label,
		NativeForks: true,
		ReadOnly:    readOnly,
	})
	if err != nil {
		return nil, err
	}
	return m.Unmount, nil
}
