//go:build darwin

// Package hfs implements the "hfs" fork adapter: real HFS+ resource-fork access via
// macOS facilities — the resource fork is the "<file>/..namedfork/rsrc" stream and the
// Finder info is the com.apple.FinderInfo xattr. It is the macOS arm of the per-OS
// "native" fork alias (core/fs resolves fork_backend="native" to "hfs" on darwin,
// "ads" on Windows, "xattr" on Linux).
//
// It lives in adapter/ (not core/) because it does host-specific syscalls
// (finderinfo_darwin.go uses x/sys/unix), keeping the core ring syscall-free and
// TinyGo-clean. It is darwin-only and needs no build tag: a non-macOS build simply does
// not compile it, and the "native" alias there resolves to the platform's own engine.
//
// The adapter operates on the share's HOST path, so it requires a base FileSystem that
// implements fs.HostPather (local_fs / an hfs-image backend). On a base that cannot
// resolve a host path (memfs, zipfs, a synthetic store) it returns fs.ErrNoHostPath so
// the misconfiguration is loud.
package hfs

import (
	"errors"

	corefs "github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// ErrNoHostPath is returned when "hfs" is configured over a base FileSystem that is not
// a host-backed fs.HostPather (so there is no real file to reach a host fork on).
var ErrNoHostPath = errors.New("fork/hfs: requires a host-backed FileSystem (HostPather)")

func init() {
	corefs.RegisterForkAdapter("hfs", func(spec corefs.ShareSpec, base corefs.FileSystem) (corefs.ForkEngine, error) {
		_ = spec
		hp, ok := base.(corefs.HostPather)
		if !ok {
			return nil, ErrNoHostPath
		}
		return newHFSForkEngine(base, hp), nil
	})
}
