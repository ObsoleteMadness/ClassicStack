//go:build windows

package main

import (
	"fmt"
	"os"
)

// The Unix daemon model (fork/setsid + PID file) does not apply on Windows,
// which has its own Service Control Manager. The stub keeps the package
// buildable in a cross-platform `go build ./...`; use classicstack-svc on
// Windows.
func main() {
	fmt.Fprintln(os.Stderr, "classicstackd is a Unix daemon; use classicstack-svc on Windows")
	os.Exit(1)
}
