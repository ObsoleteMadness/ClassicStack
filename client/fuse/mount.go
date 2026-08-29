package fuse

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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

// ResolveMountpoint expands a leading ~ / ~/, makes the path absolute, and
// cleans it. Spaces stay part of a single path (they are not split). A Windows
// drive letter ("X:") is returned as-is. Call this before mkdir or FUSE so a
// value like "~/Volumes/OpenRetroSCSI 7.5.3" does not become ./~/... plus ./7.5.3.
func ResolveMountpoint(point string) (string, error) {
	point = strings.TrimSpace(point)
	if point == "" {
		return "", errors.New("fuse: empty mountpoint")
	}
	if isWindowsDrive(point) {
		return strings.ToUpper(point[:1]) + ":", nil
	}
	expanded, err := expandHome(point)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("fuse: mountpoint %s: %w", point, err)
	}
	return filepath.Clean(abs), nil
}

func isWindowsDrive(point string) bool {
	if len(point) != 2 || point[1] != ':' {
		return false
	}
	c := point[0]
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func expandHome(point string) (string, error) {
	if point == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("fuse: expand ~: %w", err)
		}
		return home, nil
	}
	rest, ok := strings.CutPrefix(point, "~/")
	if !ok {
		rest, ok = strings.CutPrefix(point, `~\`)
	}
	if !ok {
		return point, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("fuse: expand ~: %w", err)
	}
	if rest == "" {
		return home, nil
	}
	return filepath.Join(home, filepath.FromSlash(rest)), nil
}
