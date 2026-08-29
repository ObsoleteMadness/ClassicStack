//go:build windows

package fs

// On Windows the host's own fork layout is NTFS alternate data streams (the SFM layout),
// so "native" resolves to the "ads" engine (fork_ads.go), which is always compiled in
// core/fs. No build tag is needed.
const nativeForkTarget = "ads"
