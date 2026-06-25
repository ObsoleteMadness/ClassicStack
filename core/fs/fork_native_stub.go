//go:build !forknative

package fs

import "errors"

// fork_native_stub.go registers the "native" fork adapter as a disabled stub in any
// build WITHOUT the `forknative` tag, so a share configured with fork_backend="native"
// fails with an actionable "rebuild with -tags forknative" message rather than the
// generic "unknown fork backend" error. The real adapter (adapter/fork/native, real
// host resource-fork syscalls — HFS+ "..namedfork/rsrc") links only under that tag and
// registers the same name, replacing this stub. The split is by mutually-exclusive
// build tag (this file is !forknative), so exactly one "native" registration exists.
// Mirrors the macgarden/zipfs disabled-stub pattern, but the host-syscall code lives in
// adapter/ (out of the core ring), so a TinyGo/headless build links only this stub.

// ErrNativeForkDisabled is returned when a share uses fork_backend="native" in a binary
// built without the forknative tag.
var ErrNativeForkDisabled = errors.New("fs: native fork backend not built; rebuild with -tags forknative")

func init() {
	RegisterForkAdapter("native", func(ShareSpec, FileSystem) (ForkEngine, error) {
		return nil, ErrNativeForkDisabled
	})
}
