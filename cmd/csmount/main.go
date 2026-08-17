//go:build windows || darwin || linux

// Command csmount mounts a remote ClassicStack share (AFP/SMB/NCP/EtherDFS) as a
// host filesystem: WinFsp on Windows, macFUSE on macOS, libfuse on Linux. It is a
// thin CLI over the client SDK: it resolves the transport with the shared
// cmd/internal/csconnect plumbing (so the scheme×ifacetype matrix and the -fork
// backend selection match csfs exactly), connects to an fs.ForkFS, and hands it
// to the platform mount adapter.
//
//	csmount [flags] <uri> <mountpoint>
//
// Ctrl-C unmounts cleanly.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/csconnect"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"

	_ "github.com/ObsoleteMadness/ClassicStack/client/afp"
	_ "github.com/ObsoleteMadness/ClassicStack/client/etherdfs"
	_ "github.com/ObsoleteMadness/ClassicStack/client/ncp"
	_ "github.com/ObsoleteMadness/ClassicStack/client/smb"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	cfg, rest, err := csconnect.ParseGlobalFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "csmount:", err)
		return 2
	}
	trace.SetVerbose(cfg.Verbose)
	trace.SetScope("atp", false)
	if cfg.Verbose {
		traceMount(os.Stderr)
	}

	if cfg.ListIfaces {
		csconnect.PrintInterfaces(os.Stdout)
		return 0
	}

	if len(rest) == 1 && (rest[0] == "help" || rest[0] == "-h" || rest[0] == "--help") {
		usage()
		return 0
	}
	if len(rest) != 2 {
		usage()
		return 2
	}
	rawURI, mountpoint := rest[0], rest[1]

	if runtime.GOOS == "linux" {
		fmt.Fprintln(os.Stderr, "csmount: Linux FUSE support is experimental and has not been tested.")
	}

	remote, target, err := csconnect.Connect(context.Background(), cfg, rawURI)
	if err != nil {
		fmt.Fprintln(os.Stderr, "csmount: connect:", err)
		return 1
	}
	defer fs.CloseFS(remote)

	m, err := mountAt(remote, mountpoint, target.Volume, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "csmount: mount:", err)
		return 1
	}
	fmt.Printf("mounted %s at %s (Ctrl-C to unmount)\n", rawURI, mountpoint)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() { <-sig; m.Unmount() }()
	m.Wait()
	fmt.Println("unmounted")
	return 0
}

func usage() {
	fmt.Fprint(os.Stderr, usageText())
}

// nativeForksUnix reports whether -fork should present forks as host xattrs
// (Apple on Darwin, Netatalk on Linux) rather than projecting sidecars.
func nativeForksUnix(fork string) bool {
	switch strings.ToLower(fork) {
	case "", "passthrough", "native", "hfs", "ads", "xattr":
		return true
	default:
		return false
	}
}
