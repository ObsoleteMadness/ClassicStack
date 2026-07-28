//go:build windows

// Command csmount mounts a remote ClassicStack share (AFP/SMB/NCP/EtherDFS) as a Windows
// filesystem via WinFsp. It is a thin CLI over the client SDK: it resolves the transport
// with the shared cmd/internal/csconnect plumbing (so the scheme×ifacetype matrix and the
// -fork backend selection match csfs exactly), connects to an fs.ForkFS, and hands it to
// client/winfsp.
//
//	csmount [flags] <uri> <mountpoint>
//	csmount -ifacetype tcp afp://server/Volume X:
//	csmount smb://server,nbf/Share M:\mnt\share
//
// The WinFsp runtime must be installed on the machine. Ctrl-C unmounts cleanly.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/client/winfsp"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/csconnect"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"

	// Register the client schemes. Each blank import plugs a scheme into the registry.
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
	// ATP TReq/TResp spam drowns useful NBP/AFP lines under WinFsp's call volume.
	trace.SetScope("atp", false)
	if cfg.Verbose {
		winfsp.TraceTo(os.Stderr)
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

	remote, target, err := csconnect.Connect(context.Background(), cfg, rawURI)
	if err != nil {
		fmt.Fprintln(os.Stderr, "csmount: connect:", err)
		return 1
	}
	defer fs.CloseFS(remote)

	m, err := winfsp.MountAt(remote, mountpoint, winfsp.Options{
		VolumeLabel:        target.Volume,
		FileInfoTimeoutMs:  cfg.CacheMs,
		FileInfoTimeoutSet: cfg.CacheMsSet,
	})
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
	fmt.Fprint(os.Stderr, `csmount — mount a ClassicStack share on Windows (WinFsp)

Usage:
  csmount [flags] <uri> <mountpoint>

  <mountpoint> is a drive letter ("X:") or an empty directory.

Flags:
  -ifacetype  transport: ltoudp | tashtalk | pcap | tcp (scheme-validated)
  -iface      interface: IPv4 addr (ltoudp), device (pcap), COM3//dev/tty (tashtalk), host (tcp)
              (pcap: omit to auto-detect the host's primary/default-route NIC)
  -transport  SMB pcap carrier: ipx (default) | nbipx | nbf
  -mac        virtual-station MAC for raw-Ethernet SMB carriers (empty = random)
  -fork       fork container: appledouble | applesingle | macbinary | derez | passthrough | native | nofork
              On AFP, sidecar layouts (derez/appledouble) PROJECT remote forks into the
              mount as .rdump/.idump or ._name files so Windows can read them.
  -cache-ms   WinFsp FileInfoTimeout in ms (default 1000). 0 disables FSD metadata cache;
              -1 is infinite (also enables kernel data caching).
  -v          verbose: NBP + AFP wire-trace + WinFsp Behaviour* call names to stderr (ATP off)
  -list-ifaces  list the capturable pcap NICs (the names -iface accepts) and exit

Examples:
  csmount -ifacetype tcp afp://server/Volume X:
  csmount -fork derez afp://vmac1/System\ 7.5.3 X:
  csmount smb://server,nbf/Share M:
  csmount ncp://SERVER/SYS N:
`)
}
