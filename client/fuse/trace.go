package fuse

import (
	"fmt"
	"io"
	"os"
	"sync"
)

var (
	traceOnce   sync.Once
	traceWriter io.Writer
	traceMu     sync.Mutex
)

// TraceTo enables FUSE-op tracing to w (typically os.Stderr from csmount -v).
func TraceTo(w io.Writer) {
	traceMu.Lock()
	defer traceMu.Unlock()
	traceWriter = w
}

func traceInit() {
	if p := os.Getenv("CLASSICSTACK_FUSE_TRACE"); p != "" {
		if p == "-" {
			traceWriter = os.Stderr
			return
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) // #nosec G304 -- operator-supplied debug trace path
		if err == nil {
			traceWriter = f
		}
	}
}

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
