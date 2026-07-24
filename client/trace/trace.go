// Package trace is the client SDK's shared verbose wire-trace facility, built on the
// SAME core/log logging library the server uses — not an ad-hoc printf. Every client
// transport (AppleTalk/ATP/ASP, direct-IPX, NBIPX, NBF, NCP, EtherDFS) narrates its
// protocol steps through a scope-named core/log.Logger obtained here, so `csfs -v` turns
// on a single, uniformly-formatted trace across all of them ("ipx [trace] …",
// "nbf [trace] …") and a stuck connect is diagnosable without a packet capture.
//
// Design: one process-wide stderr sink whose threshold is a core/log.LevelVar. `-v`
// flips the threshold between Trace (emit the per-request narration) and a level above
// Error (emit nothing) via SetVerbose. Loggers built by Logger(scope) all share that one
// sink, so the toggle retunes them live with no constructor-signature churn — the same
// package-level-toggle ergonomics the previous atalk-only trace had, now on the real
// logging library and shared by every transport.
//
// Raw wire bytes are deliberately NOT traced here (that is a pcap capture's job, per the
// core/log Trace doc); Logger narrates the human-readable protocol event — an NBP lookup,
// an ATP TReq/TResp, an NBIPX SESSION_INITIALIZE, an NBF NAME_QUERY.
//
// Ring: CLIENT.
package trace

import "github.com/ObsoleteMadness/ClassicStack/core/log"

// levelOff is a threshold above every real level, so the shared sink emits nothing when
// verbose is off. core/log.Error is the highest defined level; Error+1 sits above it.
const levelOff = log.Error + 1

// shared is the one stderr sink every client-transport logger writes through. Its
// LevelVar starts at levelOff (quiet); SetVerbose(true) drops it to Trace.
var (
	sharedLevel = log.NewLevelVar(levelOff)
	sharedSink  = log.NewStderrSink(sharedLevel)
)

// SetVerbose turns the client wire-trace on (Trace) or off (silent). csfs's `-v` flag
// calls it once at startup; it is safe to call concurrently and retunes every logger
// already handed out (they all share one sink threshold).
func SetVerbose(on bool) {
	if on {
		sharedLevel.Set(log.Trace)
		return
	}
	sharedLevel.Set(levelOff)
}

// Verbose reports whether the client wire-trace is currently enabled.
func Verbose() bool { return sharedLevel.Level() <= log.Trace }

// Logger returns a core/log.Logger for a client transport, scoped by name (e.g. "atalk",
// "ipx", "nbipx", "nbf", "ncp", "etherdfs"). All loggers share the one verbose-gated
// stderr sink, so their output is uniform and the `-v` toggle governs them together. A
// transport holds the returned logger and narrates at log.Trace; the Enabled() guard on
// the hot path means a disabled trace costs nothing.
func Logger(scope string) log.Logger { return log.New(scope, sharedSink) }
