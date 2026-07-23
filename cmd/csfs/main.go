// Command csfs is the ClassicStack file client: it addresses a legacy AFP/SMB/NCP/
// EtherDFS server by URI and runs ls / cp / mv / rm / attrib / type / creator against
// it, copying to and from the host filesystem with resource forks, Finder type/creator
// and DOS attributes preserved. It is a thin CLI over the client/ SDK ring — every
// operation is a core/fs.ForkFS operation via client/xfer, so remote↔host↔remote is one
// code path.
//
// One-shot:   csfs ls afp://server/Vol
//
//	csfs -ifacetype ltoudp cp afp://server/Vol/f ./f
//	csfs discover afp
//
// Interactive: csfs afp://server/Vol   (no command → REPL)
//
// Global flags select the transport (-iface, -ifacetype), the host fork container
// (-fork), and — for the raw-Ethernet SMB-over-IPX transport — the virtual station's
// hardware address (-mac, empty = a synthesised locally-administered random MAC, so the
// client never borrows the host NIC's identity). The -ifacetype is validated against the
// URI scheme's declared transports, so an invalid combo (e.g. smb over ltoudp) is
// rejected up front.
//
//	csfs -iface eth0 ls "smb://server/Share"          # SMB over IPX (pcap, default)
//	csfs -mac 02:11:22:33:44:55 -iface eth0 ls smb://server/Share
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/client/trace"

	// Register the client schemes. Each blank import plugs a scheme into the registry.
	_ "github.com/ObsoleteMadness/ClassicStack/client/afp"
	_ "github.com/ObsoleteMadness/ClassicStack/client/etherdfs"
	_ "github.com/ObsoleteMadness/ClassicStack/client/ncp"
	_ "github.com/ObsoleteMadness/ClassicStack/client/smb"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cfg, rest, err := parseGlobalFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "csfs:", err)
		return 2
	}
	// -v turns on the client wire-trace across EVERY transport (AppleTalk NBP/ATP/ASP,
	// direct-IPX, NBIPX, NBF, NCP, EtherDFS) — one shared verbose toggle on the core/log
	// library, rendered to stderr.
	trace.SetVerbose(cfg.verbose)
	if len(rest) == 0 {
		usage()
		return 2
	}

	cmd := rest[0]
	switch cmd {
	case "help", "-h", "--help":
		usage()
		return 0
	case "discover":
		return cmdDiscover(cfg, rest[1:])
	case "ls", "cp", "get", "put", "mv", "rm", "attrib", "type", "creator":
		return runOneShot(cfg, cmd, rest[1:])
	default:
		// A bare URI (or host path) with no command → interactive REPL against it.
		if looksLikeTarget(cmd) {
			return runREPL(cfg, cmd)
		}
		fmt.Fprintf(os.Stderr, "csfs: unknown command %q\n", cmd)
		usage()
		return 2
	}
}

// looksLikeTarget reports whether s is a URI (has "://") the REPL can open.
func looksLikeTarget(s string) bool { return strings.Contains(s, "://") }

func usage() {
	fmt.Fprint(os.Stderr, `csfs — ClassicStack file client

Usage:
  csfs [flags] <command> [args]
  csfs [flags] <uri>                 open an interactive session (REPL)

Commands:
  ls <uri>                           list a directory
  cp <src> <dst>                     copy (uri or host path; either side)
  get <uri> <host-path>              copy remote → host
  put <host-path> <uri>              copy host → remote
  mv <uri> <newname>                 rename/move on the server
  rm <uri>                           delete
  attrib <uri> [+r|-r|+h|-h|...]     show or set DOS attributes
  type <uri> [CODE]                  show or set the Finder type (4 chars)
  creator <uri> [CODE]               show or set the Finder creator (4 chars)
  discover <scheme>                  find servers (NBP/SAP/browser/broadcast)

Flags:
  -ifacetype  transport: ltoudp | tashtalk | pcap | tcp (scheme-validated)
  -iface      interface: IPv4 addr (ltoudp), device (pcap), COM3//dev/tty (tashtalk), host (tcp)
  -transport  SMB pcap carrier: ipx (default) | nbipx | nbf
  -mac        virtual-station MAC for raw-Ethernet SMB carriers (empty = random)
  -fork       host fork container: appledouble | applesingle | macbinary | derez | native | nofork
  -v          verbose: print the client wire-trace (NBP/ATP/ASP) to stderr

URI grammar:
  <scheme>://[[user][:pass]@]<server>[,<transport>]/<volume>[/<path>]
  afp://classicstack:MyZone/Volume     smb://pete:secret@host,tcp/share
  ncp://SERVER,ipx/SYS                 etherdfs://02-1a-4d-11-22-33/C
`)
}

// contextForRun returns a background context (a place to hang cancellation later).
func contextForRun() context.Context { return context.Background() }
