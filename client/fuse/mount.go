package fuse

import "errors"

// ErrUnsupported is returned by MountAt when the binary was not built with
// `-tags fuse` (and cgo) on Darwin/Linux, or on a platform with no FUSE host.
var ErrUnsupported = errors.New("fuse: mounting requires -tags fuse and a FUSE runtime (macFUSE or libfuse)")

// Mount wraps a live FUSE mount so the caller can wait on it and unmount.
type Mount struct {
	unmount func()
	wait    func()
}

// Unmount tears the mount down (idempotent).
func (m *Mount) Unmount() {
	if m == nil || m.unmount == nil {
		return
	}
	m.unmount()
}

// Wait blocks until the mount dispatcher exits.
func (m *Mount) Wait() {
	if m == nil || m.wait == nil {
		return
	}
	m.wait()
}
