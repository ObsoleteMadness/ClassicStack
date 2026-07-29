//go:build windows

package winfsp

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// Delegate tracing logs every Behaviour* call name (and args / result) so a STATUS_
// INTERNAL_ERROR from WinFsp can be matched to the Go delegate that failed. Enable via:
//
//	CLASSICSTACK_WINFSP_TRACE=<path>   — append to that file
//	CLASSICSTACK_WINFSP_TRACE=-        — stderr
//	winfsp.TraceTo(w)                 — csmount -v wires stderr this way
//
// The hot path costs one env/writer-guarded check when tracing is off.
var (
	traceOnce   sync.Once
	traceWriter io.Writer
	traceMu     sync.Mutex
)

// TraceTo enables delegate tracing to w (typically os.Stderr from csmount -v). A nil
// writer disables tracing unless CLASSICSTACK_WINFSP_TRACE is set. Safe to call before
// MountAt; subsequent calls replace the writer.
func TraceTo(w io.Writer) {
	traceMu.Lock()
	defer traceMu.Unlock()
	traceWriter = w
}

func traceInit() {
	if p := os.Getenv("CLASSICSTACK_WINFSP_TRACE"); p != "" {
		if p == "-" {
			traceWriter = os.Stderr
			return
		}
		// The trace path is supplied by the operator via an environment
		// variable to enable opt-in debug logging; it is trusted input, not
		// attacker-controlled, and the file is created private (0600).
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) // #nosec G304,G703 -- operator-supplied debug trace path
		if err == nil {
			traceWriter = f
		}
	}
}

// trace logs one delegate call when tracing is enabled; it is a no-op otherwise.
// format should start with the WinFsp Behaviour name (Open, ReadDirectory, …).
func trace(format string, args ...any) {
	traceOnce.Do(traceInit)
	traceMu.Lock()
	w := traceWriter
	traceMu.Unlock()
	if w == nil {
		return
	}
	traceMu.Lock()
	defer traceMu.Unlock()
	fmt.Fprintf(w, format+"\n", args...)
}
