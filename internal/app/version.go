package app

// Version carries the link-time build metadata into the run-core. Each
// command binary (cmd/classicstack, cmd/classicstackd, cmd/classicstack-svc)
// holds its own `-ldflags -X main.Build*` vars and passes them in, so the
// ldflags target stays `main.*` regardless of which binary is built.
type Version struct {
	Version string
	Commit  string
	Date    string
}
