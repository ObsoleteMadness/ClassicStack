// Package diagflags is the shared CLI-flag surface for the AppleTalk/IPX diagnostic
// probe utilities (csecho, csgetzones, csnbp, cspap, csipxping, csncpinfo, csnetsend,
// csnetview). Before this package existed, each of the eight commands hand-rolled its
// own flag.* calls for the same handful of option groups — -v/-version everywhere,
// -net/-src for the four LLAP-addressed probes (csecho/csgetzones/csnbp/cspap), and
// -iface/-mac/-list-ifaces/-ifacetype for the four raw-Ethernet ones
// (csipxping/csncpinfo/csnetsend/csnetview) — with drifting help text and inconsistent
// coverage (only some had -v; only some had -ifacetype). This package gives each option
// group a single definition; every probe's main.go now calls the same registration
// functions instead of retyping flag.*/help text by hand.
//
// Each probe still owns its own protocol-specific flags (-timeout, -count, -dst, ...)
// directly; only the option groups that were genuinely duplicated across commands live
// here. The AppleTalk transport-selection flags (-transport/-iface/-device/-baud
// /-list-ifaces/-claim) for the LLAP probes are already shared via
// cmd/internal/atlink.Flags and are NOT duplicated here.
package diagflags

import (
	"flag"
	"fmt"
	"io"

	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/buildinfo"
)

// Common is the -v/-version pair every diagnostic probe registers.
type Common struct {
	Verbose *bool
	Version *bool
}

// RegisterCommon binds -v (verbose wire trace to stderr) and -version (print version
// information and exit) on fs.
func RegisterCommon(fs *flag.FlagSet) *Common {
	return &Common{
		Verbose: fs.Bool("v", false, "verbose wire trace to stderr"),
		Version: fs.Bool("version", false, "print version information and exit"),
	}
}

// HandleVersion writes tool's version banner to w and reports true when -version was
// passed, so main can do `if c.HandleVersion(os.Stdout, "csecho", ...) { return nil }`
// before doing any real work.
func (c *Common) HandleVersion(w io.Writer, tool, version, commit, date string) bool {
	if !*c.Version {
		return false
	}
	buildinfo.Print(w, tool, version, commit, date)
	return true
}

// ApplyVerbose wires -v through to the client SDK's shared wire-trace gate
// (client/trace), which the client/atalk-based probes (csecho, csgetzones, csnbp,
// cspap) and the client/netbios and client/browse based ones (csnetsend, csnetview)
// narrate through. It is a no-op to call on a probe with no client-SDK trace hooks
// (csipxping, csncpinfo hand-roll raw pcap I/O and register no -v).
func (c *Common) ApplyVerbose() {
	trace.SetVerbose(*c.Verbose)
}

// LLAPSource is the -net/-src pair shared by every LLAP-addressed probe (csecho,
// csgetzones, csnbp, cspap): the AppleTalk network/node the probe asserts as its own
// source address before opening the transport. Used alongside atlink.Flags, whose
// -claim decides whether -src is only a first LLAP node-claim candidate (the default)
// or an outright assertion with no negotiation.
type LLAPSource struct {
	Network *uint
	SrcNode *uint
}

// RegisterLLAPSource binds -net and -src on fs.
func RegisterLLAPSource(fs *flag.FlagSet) *LLAPSource {
	return &LLAPSource{
		Network: fs.Uint("net", 0, "AppleTalk network number we claim as our source (0 = the AppleTalk \"startup range\" placeholder — a strict peer may legitimately ignore requests from a node still asserting network 0; pass the segment's real network number, e.g. -net 1, if a peer that answers a real client doesn't answer this probe)"),
		SrcNode: fs.Uint("src", 0, "our LocalTalk source node — 0 (default) picks a random workstation-range candidate (1..127) for the LLAP node-claim; 1..254 requests a specific candidate instead. With -claim (the default) this is only the desired first candidate: the node actually used may differ if it's taken. Requires -claim when 0."),
	}
}

// Validate reports whether -src is in the accepted range: 0 (let -claim pick a
// candidate), or 1..254. SrcNode is a flag.Uint, so it can never be negative; the only
// out-of-range values are > 254.
func (s *LLAPSource) Validate() error {
	if *s.SrcNode > 254 {
		return fmt.Errorf("src node %d out of range (0, or 1..254)", *s.SrcNode)
	}
	return nil
}

// RegisterIface binds -iface on fs. help lets each raw-Ethernet probe phrase the
// description for its own traffic (send on / browse on / …) while keeping the flag
// name and default (auto-detect) consistent.
func RegisterIface(fs *flag.FlagSet, help string) *string {
	return fs.String("iface", "", help)
}

// RegisterIfaceType binds -ifacetype on fs, for the raw-Ethernet probes that can also
// run over a libpcap-free TUN/TAP device (csnetsend, csnetview) rather than only pcap
// (csipxping, csncpinfo, which don't register this flag at all).
func RegisterIfaceType(fs *flag.FlagSet) *string {
	return fs.String("ifacetype", "pcap", "interface type: pcap | tap")
}

// RegisterMAC binds -mac on fs: the synthetic source MAC a raw-Ethernet probe sends
// from, so it never borrows the host NIC's identity by default.
func RegisterMAC(fs *flag.FlagSet) *string {
	return fs.String("mac", "", "source MAC for our virtual station (default: random locally-administered)")
}

// RegisterListIfaces binds -list-ifaces on fs: print the capturable pcap NICs (the
// names -iface accepts) and exit.
func RegisterListIfaces(fs *flag.FlagSet) *bool {
	return fs.Bool("list-ifaces", false, "list the capturable pcap NICs (the names -iface accepts) and exit")
}
