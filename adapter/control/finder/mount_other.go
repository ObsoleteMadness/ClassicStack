//go:build !windows && !darwin && !linux

package finder

import "github.com/ObsoleteMadness/ClassicStack/core/fs"

func platformMountAvailable() bool { return false }

func platformMount(_ fs.ForkFS, _, _ string, _ bool) (func(), error) {
	return nil, ErrMountUnavailable
}
