//go:build !windows

// Command csmount mounts a ClassicStack share as a Windows filesystem via WinFsp. It is
// Windows-only; on other platforms this stub prints a message and exits non-zero, keeping
// `go build ./...` green everywhere (mirrors cmd/classicstackd's per-OS stub pattern).
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "csmount is only supported on Windows (WinFsp)")
	os.Exit(1)
}
