//go:build windows

package winfsp

import (
	"errors"
	"time"

	csfuse "github.com/ObsoleteMadness/ClassicStack/client/fuse"
	winfsp "github.com/winfsp/go-winfsp"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// DefaultFileInfoTimeoutMs is the WinFsp FileInfoTimeout used when Options.FileInfoTimeoutMs
// is left at its zero value with FileInfoTimeoutSet false (csmount default: 1s).
const DefaultFileInfoTimeoutMs = 1000

// Options carries the mount-time knobs.
type Options struct {
	// VolumeLabel is the label shown for the mounted volume (empty → "ClassicStack").
	VolumeLabel string
	// ReadOnly forces a read-only mount even if the ForkFS itself is writable.
	ReadOnly bool
	// NativeForks surfaces a file's resource fork and Apple metadata as NTFS named
	// streams (:AFP_Resource / :AFP_AfpInfo / :Comments), following NT Services-for-
	// Macintosh stream names — see streams_windows.go. csmount sets it for -fork native.
	// When false the mount has no streams and a ':stream' path is rejected as invalid.
	NativeForks bool
	// FileInfoTimeoutMs is WinFsp FSP_FSCTL_VOLUME_PARAMS.FileInfoTimeout in milliseconds.
	// 0 disables FSD metadata caching; -1 means infinite (also enables data caching).
	// When FileInfoTimeoutSet is false, MountAt uses DefaultFileInfoTimeoutMs.
	FileInfoTimeoutMs  int
	FileInfoTimeoutSet bool
}

// storableAttrMask is the subset of Windows FILE_ATTRIBUTE_* bits we persist as DOS
// attributes (read-only/hidden/system/archive); it equals metastore.DOSStorableMask.
const storableAttrMask = metastore.DOSStorableMask

// errDirNotEmpty is mapped to STATUS_DIRECTORY_NOT_EMPTY by the CanDelete delegate.
var errDirNotEmpty = errors.New("winfsp: directory not empty")

// filetimeEpochDelta is the number of 100ns ticks between the Windows FILETIME epoch
// (1601-01-01) and the Unix epoch (1970-01-01).
const filetimeEpochDelta = 116444736000000000

// filetimeToTime converts a Windows FILETIME (100ns ticks since 1601) to a time.Time.
// Zero → the zero time.
func filetimeToTime(ft uint64) time.Time {
	if ft == 0 {
		return time.Time{}
	}
	ns := (int64(ft) - filetimeEpochDelta) * 100
	return time.Unix(0, ns).UTC()
}

// Mount wraps a live go-winfsp mount so the caller can wait on it and unmount cleanly.
type Mount struct {
	fs   *winfsp.FileSystem
	done chan struct{}
}

// New builds an Adapter over an already-connected ForkFS without mounting it (used by
// tests to drive the delegates directly).
func New(fsys fs.ForkFS, opts Options) *Adapter { return newAdapter(fsys, opts) }

// MountAt builds an Adapter over fsys and mounts it at mountpoint (a drive letter like
// "X:" or an empty directory). It is the entry point cmd/csmount drives. Read-only is
// honoured via Options.ReadOnly in the Adapter itself (see newAdapter).
func MountAt(fsys fs.ForkFS, mountpoint string, opts Options) (*Mount, error) {
	var err error
	mountpoint, err = csfuse.ResolveMountpoint(mountpoint)
	if err != nil {
		return nil, err
	}
	a := newAdapter(fsys, opts)
	host, err := winfsp.Mount(a.mountable(), mountpoint, a.mountOptions(opts)...)
	if err != nil {
		return nil, err
	}
	return &Mount{fs: host, done: make(chan struct{})}, nil
}

// Unmount tears the mount down (idempotent).
func (m *Mount) Unmount() {
	if m == nil || m.fs == nil {
		return
	}
	select {
	case <-m.done:
		return // already unmounted
	default:
	}
	m.fs.Unmount()
	close(m.done)
}

// Wait blocks until Unmount is called (go-winfsp's Mount returns immediately and runs the
// dispatcher in the background, so the command waits here on a signal).
func (m *Mount) Wait() {
	if m == nil {
		return
	}
	<-m.done
}
