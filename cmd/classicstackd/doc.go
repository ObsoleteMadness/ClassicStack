/*
Command classicstackd runs ClassicStack as a background daemon on Unix.

It shares the same run-core as the interactive classicstack binary
(internal/app). It does not depend on any init system: `start` re-execs
itself detached into a new session, writes a PID file, and redirects output
to a log file; `stop` signals that PID; `run` stays in the foreground.

	classicstackd start  -config <path> [-pidfile <p>] [-log <p>]  daemonize
	classicstackd stop          [-pidfile <p>]                     signal the daemon
	classicstackd status        [-pidfile <p>]                     report liveness
	classicstackd run    -config <path>                            run in the foreground

On macOS, `install`/`uninstall` additionally manage a LaunchAgent plist so
the daemon auto-starts at login (headless):

	classicstackd install -config <path> [-log <p>]   write + load the LaunchAgent
	classicstackd uninstall                           unload + remove the LaunchAgent

On Windows this binary is a stub; use classicstack-svc instead.
*/
package main
