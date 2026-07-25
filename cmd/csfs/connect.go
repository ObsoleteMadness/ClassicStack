package main

import (
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/csconnect"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// config is the csfs-local alias for the shared csconnect.Config. The transport/URI
// plumbing lives in cmd/internal/csconnect so csfs and csmount share one source of truth
// for the scheme×ifacetype matrix and the -fork backend selection; these thin wrappers
// keep csfs's existing internal call sites (openerFor, connect, hostShare) unchanged.
type config = csconnect.Config

// parseGlobalFlags peels the leading -flag/value pairs off args. See csconnect.
func parseGlobalFlags(args []string) (config, []string, error) {
	return csconnect.ParseGlobalFlags(args)
}

// openerFor builds a validated client/link.Opener for a target. See csconnect.
func openerFor(cfg config, target uri.Target) (*clientlink.Opener, error) {
	return csconnect.OpenerFor(cfg, target)
}

// parseMAC parses a MAC address into a 6-byte array. See csconnect.
func parseMAC(s string) ([6]byte, error) { return csconnect.ParseMAC(s) }

// connect parses a URI and opens it as an fs.ForkFS via the client SDK. See csconnect.
func connect(cfg config, rawURI string) (fs.ForkFS, uri.Target, error) {
	return csconnect.Connect(contextForRun(), cfg, rawURI)
}

// hostShare opens a host directory as an fs.ForkFS (a local_fs share), so a host path is
// an ordinary ForkFS endpoint for cp — the same code path as a remote. The -fork flag
// selects the container (default appledouble). This stays csfs-local: csmount has no
// host-side leg.
func hostShare(cfg config, hostPath string) (fs.ForkFS, error) {
	fork := cfg.Fork
	if fork == "" {
		fork = "appledouble"
	}
	return fs.BuildShare(fs.ShareSpec{
		FSType:      "local_fs",
		Path:        hostPath,
		ForkBackend: fork,
	}, nil)
}
