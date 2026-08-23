// Package buildinfo is the shared -version output for every cmd/ binary. Each command
// keeps its own link-time BuildVersion/BuildCommit/BuildDate vars (so `-ldflags -X
// main.BuildVersion=...` can target them per-package, as scripts/build-local.sh and the
// Makefile already do for the whole cmd/ tree) and calls Print so every tool reports the
// same shape classicstack's -version already uses.
package buildinfo

import (
	"fmt"
	"io"
	"runtime"
)

// Print writes tool's version/commit/date/go-runtime line to w, matching the format
// cmd/internal/cli.Run has printed for classicstack/classicstackd/classicstack-svc since
// -version was added there.
func Print(w io.Writer, tool, version, commit, date string) {
	_, _ = fmt.Fprintf(w, "%s %s\ncommit: %s\nbuilt: %s\ngo: %s\n", tool, version, commit, date, runtime.Version())
}
