package main

import "github.com/ObsoleteMadness/ClassicStack/cmd/internal/cli"

// Build metadata injected at link time via -ldflags
// -X main.BuildVersion=... -X main.BuildCommit=... -X main.BuildDate=...
var (
	BuildVersion = "0.0.0-dev"
	BuildCommit  = "unknown"
	BuildDate    = "unknown"
)

// main is the thin interactive entry point: it hands the link-time build metadata to
// the shared new-ring run-core (cmd/internal/cli), which loads server.toml, builds
// and supervises the compose runtime, optionally serves the web-admin control API,
// and runs until SIGINT/SIGTERM.
func main() {
	cli.Main(cli.Version{Version: BuildVersion, Commit: BuildCommit, Date: BuildDate})
}
