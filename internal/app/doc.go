// Package app is the ClassicStack run-core: it parses CLI flags and the
// optional TOML config, builds the Supervisor (ports, the AppleTalk router and
// its DDP service set, and the standalone IPX/NetBEUI/NetBIOS/SMB/WebUI hooks),
// wires the management plane, and runs the stack until its context is
// cancelled.
//
// It exposes two entry points so the interactive binary and the
// service/daemon wrappers share one runtime: Main(Version) for foreground use
// (Ctrl-C / SIGTERM) and Run(ctx, args, Version) for callers that drive the
// lifecycle themselves (the Windows service and the Unix daemon). Build tags
// gate the optional subsystems exactly as before the package was split out of
// cmd/classicstack.
package app
