package atalk

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// verbose.go is the AppleTalk client stack's wire-trace, now built on the shared
// client/trace facility (which wraps the server's core/log library) rather than an ad-hoc
// printf. When verbose is on it narrates each NBP lookup, ATP transaction (TReq send,
// per-attempt timeout, TResp arrival), and the resolved server address, so a stuck
// connect (e.g. an ATP transaction that times out) is diagnosable without a packet
// capture. It stays a package-level facility (no threaded logger) so the existing
// constructors — NewEndpoint/NewATP/Open — need no signature change; the verbose toggle
// lives in client/trace and governs every transport at once (see trace.SetVerbose).

// atalkLog is the scope-named core/log.Logger the AppleTalk client narrates through. It
// shares client/trace's one verbose-gated stderr sink, so `csfs -v` turns it on with
// every other transport's trace.
var atalkLog = trace.Logger("atalk")

// SetVerbose enables or disables the client's wire-trace output. Retained for
// compatibility with callers that toggled the AppleTalk trace specifically; it delegates
// to the shared client/trace toggle, so it governs every transport's trace, not only
// AppleTalk. New code should call client/trace.SetVerbose directly.
func SetVerbose(on bool) { trace.SetVerbose(on) }

// Verbose reports whether wire-trace output is enabled.
func Verbose() bool { return trace.Verbose() }

// tracef narrates one wire-trace line at log.Trace. The Enabled guard means a disabled
// trace builds no message; the "atalk" scope prefixes the output so it is distinguishable
// from the command's normal stdout.
func tracef(format string, args ...any) {
	if !atalkLog.Enabled(log.Trace) {
		return
	}
	atalkLog.Log0(log.Trace, fmt.Sprintf(format, args...))
}

// String renders an address as net.node:socket for trace output.
func (a Addr) String() string {
	return fmt.Sprintf("%d.%d:%d", a.Network, a.Node, a.Socket)
}
