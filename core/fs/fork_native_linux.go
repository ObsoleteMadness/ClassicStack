//go:build linux

package fs

// On Linux the host's own fork layout is Netatalk-style extended attributes
// (user.org.netatalk.*), so "native" resolves to the "xattr" engine (fork_xattr.go),
// which is always compiled in core/fs. No build tag is needed.
const nativeForkTarget = "xattr"
