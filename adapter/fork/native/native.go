//go:build forknative

// Package native implements the "native" fork adapter: real HOST resource-fork access
// via OS facilities, registered into the core/fs fork-adapter registry under the
// `forknative` build tag. It lives in adapter/ (not core/) because it does host-specific
// file I/O on the platform's native resource-fork stream — core stays syscall-free and
// TinyGo-clean (a build without `forknative` links the core stub in
// core/fs/fork_native_stub.go, which errors with a rebuild hint).
//
// The adapter operates on the share's HOST path, so it requires a base FileSystem that
// implements fs.HostPather (local_fs / an hfs-image backend). On a base that cannot
// resolve a host path (memfs, zipfs, a synthetic store) it returns fs.ErrNoHostPath at
// build time so the misconfiguration is loud. Where the host filesystem has no native
// resource-fork concept (e.g. ext4/NTFS on Linux), the per-OS backend reports forks as
// absent rather than failing — a data-only file is valid.
package native

import (
	"errors"

	corefs "github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// ErrNoHostPath is returned when "native" is configured over a base FileSystem that is
// not a host-backed fs.HostPather (so there is no real file to reach a host fork on).
var ErrNoHostPath = errors.New("fork/native: requires a host-backed FileSystem (HostPather)")

func init() {
	corefs.RegisterForkAdapter("native", func(spec corefs.ShareSpec, base corefs.FileSystem) (corefs.ForkEngine, error) {
		_ = spec
		hp, ok := base.(corefs.HostPather)
		if !ok {
			return nil, ErrNoHostPath
		}
		return newNativeForkEngine(base, hp), nil
	})
}
