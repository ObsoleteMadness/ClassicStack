//go:build (darwin || linux) && !(fuse && cgo)

package main

import (
	"fmt"
	"io"
	"runtime"

	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/csconnect"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

type mounter interface {
	Unmount()
	Wait()
}

func traceMount(io.Writer) {}

func mountAt(fs.ForkFS, string, string, csconnect.Config) (mounter, error) {
	runtimeHint := "macFUSE (https://macfuse.github.io/)"
	if runtime.GOOS == "linux" {
		runtimeHint = "libfuse (libfuse-dev)"
	}
	return nil, fmt.Errorf("csmount: FUSE support was not compiled in. Rebuild with:\n  go build -tags fuse -o csmount ./cmd/csmount\nRequires %s and cgo", runtimeHint)
}

func usageText() string {
	return `csmount — mount a ClassicStack share via FUSE (not compiled in this binary)

This build of csmount does not include the FUSE host. Rebuild with:

  go build -tags fuse -o csmount ./cmd/csmount

macOS requires macFUSE (https://macfuse.github.io/). Linux requires libfuse.
Linux FUSE support is experimental and has not been tested.

Usage:
  csmount [flags] <uri> <mountpoint>
`
}
