//go:build darwin

package registry

// Blank-import the HFS+ host fork adapter (adapter/fork/hfs) so its init() registers the
// "hfs" fork_backend into the core/fs fork-adapter registry. On macOS the per-OS "native"
// alias resolves to "hfs" (core/fs/fork_native_darwin.go), so linking this package is what
// makes `fork_backend = "native"` (or "hfs") work on a macOS server build.
//
// It is darwin-only and needs no build tag: the adapter does macOS-specific syscalls, so
// it simply is not compiled on other platforms, where "native" resolves to that platform's
// own always-linked engine (ads on Windows, xattr on Linux — both in core/fs). This
// replaces the former forknative-tagged host adapter + disabled stub.
import _ "github.com/ObsoleteMadness/ClassicStack/adapter/fork/hfs"
