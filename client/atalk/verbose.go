package atalk

import (
	"fmt"
	"os"
	"sync/atomic"
)

// verbose.go is an opt-in wire-trace for the AppleTalk client stack: when enabled it
// prints each NBP lookup, ATP transaction (TReq send, per-attempt timeout, TResp
// arrival), and the resolved server address to stderr, so a stuck connect (e.g. an ATP
// transaction that times out) can be diagnosed without a packet capture. It is off by
// default; the csfs `-v` flag turns it on via SetVerbose. Kept as a package-level toggle
// (not a threaded logger) so the existing constructors — NewEndpoint/NewATP/Open — need
// no signature change.

// verboseOn is the atomic verbose toggle (0 = off, 1 = on).
var verboseOn atomic.Bool

// SetVerbose enables or disables the client's wire-trace output on stderr.
func SetVerbose(on bool) { verboseOn.Store(on) }

// Verbose reports whether wire-trace output is enabled.
func Verbose() bool { return verboseOn.Load() }

// tracef prints a wire-trace line to stderr when verbose is enabled. The lines are
// prefixed so they are distinguishable from the command's normal output.
func tracef(format string, args ...any) {
	if verboseOn.Load() {
		fmt.Fprintf(os.Stderr, "[atalk] "+format+"\n", args...)
	}
}

// String renders an address as net.node:socket for trace output.
func (a Addr) String() string {
	return fmt.Sprintf("%d.%d:%d", a.Network, a.Node, a.Socket)
}
