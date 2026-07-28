//go:build !windows

package winfsp

import (
	"errors"
	"io"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// ErrUnsupported is returned by Mount on non-Windows platforms, where WinFsp does not
// exist. It keeps `go build ./...` green everywhere while confining the real binding to
// the //go:build windows files.
var ErrUnsupported = errors.New("winfsp: mounting is only supported on Windows")

// DefaultFileInfoTimeoutMs mirrors the Windows default (unused off Windows).
const DefaultFileInfoTimeoutMs = 1000

// Options mirrors the Windows Options so callers compile cross-platform.
type Options struct {
	// VolumeLabel is the label shown for the mounted volume (empty → derived from the URI).
	VolumeLabel string
	// ReadOnly forces a read-only mount even if the ForkFS itself is writable.
	ReadOnly bool
	// FileInfoTimeoutMs is WinFsp FileInfoTimeout in milliseconds (Windows-only).
	FileInfoTimeoutMs  int
	FileInfoTimeoutSet bool
}

// Mount is the non-Windows stub: it always fails with ErrUnsupported.
type Mount struct{}

// Unmount is a no-op on non-Windows.
func (*Mount) Unmount() {}

// Wait is a no-op on non-Windows.
func (*Mount) Wait() {}

// New is unavailable off Windows.
func New(_ fs.ForkFS, _ Options) (*Mount, error) { return nil, ErrUnsupported }

// MountAt is unavailable off Windows.
func MountAt(_ fs.ForkFS, _ string, _ Options) (*Mount, error) { return nil, ErrUnsupported }

// TraceTo is a no-op off Windows (delegate tracing is Windows-only).
func TraceTo(_ io.Writer) {}
