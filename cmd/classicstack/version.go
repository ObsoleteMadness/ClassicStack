package main

// Build metadata injected at link time via -ldflags.
var (
	BuildVersion = "0.0.0-dev"
	BuildCommit  = "unknown"
	BuildDate    = "unknown"
)
