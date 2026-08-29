package atalk

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// verbose.go is the AppleTalk client stack's wire-trace, now built on the shared
// client/trace facility (which wraps the server's core/log library) rather than an ad-hoc
// printf. When verbose is on it narrates each NBP lookup, AEP echo, and (under the
// separate "atp" scope) ATP transactions, so a stuck connect is diagnosable without a
// packet capture. It stays a package-level facility (no threaded logger) so the existing
// constructors — NewEndpoint/NewATP/Open — need no signature change; the verbose toggle
// lives in client/trace and governs every transport at once (see trace.SetVerbose).
// Tools that want NBP/AFP without per-packet ATP noise (csmount) mute the "atp" scope via
// trace.SetScope("atp", false).

// atalkLog narrates NBP/AEP (and other non-ATP AppleTalk) events; atpLog narrates ATP
// TReq/TResp/timeouts. Both share client/trace's verbose-gated stderr sink.
var (
	atalkLog = trace.Logger("atalk")
	atpLog   = trace.Logger("atp")
)

// SetVerbose enables or disables the client's wire-trace output. Retained for
// compatibility with callers that toggled the AppleTalk trace specifically; it delegates
// to the shared client/trace toggle, so it governs every transport's trace, not only
// AppleTalk. New code should call client/trace.SetVerbose directly.
func SetVerbose(on bool) { trace.SetVerbose(on) }

// Verbose reports whether wire-trace output is enabled.
func Verbose() bool { return trace.Verbose() }

// tracef narrates one non-ATP AppleTalk wire-trace line at log.Trace (NBP, AEP, …).
func tracef(format string, args ...any) {
	if !atalkLog.Enabled(log.Trace) {
		return
	}
	atalkLog.Log0(log.Trace, fmt.Sprintf(format, args...))
}

// atpf narrates one ATP wire-trace line at log.Trace under the "atp" scope.
func atpf(format string, args ...any) {
	if !atpLog.Enabled(log.Trace) {
		return
	}
	atpLog.Log0(log.Trace, fmt.Sprintf(format, args...))
}

// String renders an address as net.node:socket for trace output.
func (a Addr) String() string {
	return fmt.Sprintf("%d.%d:%d", a.Network, a.Node, a.Socket)
}
