//go:build !windows

package main

import (
	"fmt"
	"os"
)

// On non-Windows platforms this binary does nothing useful: the Windows
// service integration is Windows-only. Operators on Linux/macOS should use
// the classicstackd daemon. The stub keeps the package buildable in a
// cross-platform `go build ./...` so CI matrices do not trip over a missing
// main.
func main() {
	fmt.Fprintln(os.Stderr, "classicstack-svc is a Windows-only service wrapper; use classicstackd on this platform")
	os.Exit(1)
}
