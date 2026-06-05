package main

// Build metadata injected at link time via -ldflags
// -X main.BuildVersion=... -X main.BuildCommit=... -X main.BuildDate=...
var (
	BuildVersion = "0.0.0-dev"
	BuildCommit  = "unknown"
	BuildDate    = "unknown"
)
