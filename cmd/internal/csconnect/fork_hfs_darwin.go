//go:build darwin

package csconnect

// Blank-import the HFS+ host fork adapter so the per-OS "native" alias resolves to "hfs"
// on macOS for the client tools that share this connect plumbing (csfs, csmount). The ads
// (Windows) and xattr (Linux) targets live in core/fs and are always linked, so only the
// darwin client needs an explicit import; without it, `-fork native` on macOS would fail
// with "unknown fork backend".
import _ "github.com/ObsoleteMadness/ClassicStack/adapter/fork/hfs"
