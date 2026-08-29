//go:build darwin

package fs

// On macOS the host's own fork layout is HFS+/APFS resource forks ("..namedfork/rsrc")
// plus the com.apple.FinderInfo xattr, so "native" resolves to the "hfs" engine
// (adapter/fork/hfs). That engine does host syscalls, so it lives in the adapter ring
// and must be blank-imported to register; if it is not linked, the alias returns
// "unknown fork backend" from forkAdapterByName.
const nativeForkTarget = "hfs"
