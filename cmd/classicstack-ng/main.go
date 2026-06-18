package main

import "github.com/ObsoleteMadness/ClassicStack/cmd/internal/cli"

// Build metadata injected at link time via -ldflags
// -X main.BuildVersion=... -X main.BuildCommit=... -X main.BuildDate=...
var (
	BuildVersion = "0.0.0-dev"
	BuildCommit  = "unknown"
	BuildDate    = "unknown"
)

// classicstack-ng is now an alias of cmd/classicstack: both are thin entry points
// over the shared new-ring run-core (cmd/internal/cli). It was the M-ng "minimal
// testable" harness while the compose runtime grew; now that cli IS the production
// run-core there is no behavioural difference. Kept as a distinct target through the
// M10 cutover so existing scripts/docs that invoke classicstack-ng keep working.
func main() {
	cli.Main(cli.Version{Version: BuildVersion, Commit: BuildCommit, Date: BuildDate})
}
