//go:build !windows && !darwin && !linux

// Command csmount mounts a ClassicStack share as a host filesystem. It is
// supported on Windows (WinFsp), macOS (macFUSE), and Linux (libfuse); on
// other platforms this stub prints a message and exits non-zero, keeping
// `go build ./...` green everywhere.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "csmount is only supported on Windows (WinFsp), macOS (macFUSE), and Linux (libfuse)")
	os.Exit(1)
}
