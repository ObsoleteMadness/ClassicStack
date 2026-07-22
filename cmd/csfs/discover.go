package main

import (
	"fmt"
	"os"

	"github.com/ObsoleteMadness/ClassicStack/client/atalk"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
)

// cmdDiscover runs a scheme's own discovery probe and prints each responder so it can be
// pasted into a URI. AFP uses an NBP broadcast lookup for the "AFPServer" type; the
// other schemes' probes (SMB browser, NCP SAP, EtherDFS broadcast) land with their
// clients in later phases.
func cmdDiscover(cfg config, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: csfs discover <scheme>  (afp | smb | ncp | etherdfs)")
		return 2
	}
	scheme := args[0]
	switch scheme {
	case "afp":
		return discoverAFP(cfg)
	case "smb", "ncp", "etherdfs":
		fmt.Fprintf(os.Stderr, "csfs: discover %s is not implemented yet\n", scheme)
		return 1
	default:
		fmt.Fprintf(os.Stderr, "csfs: unknown scheme %q\n", scheme)
		return 2
	}
}

// discoverAFP broadcasts an NBP lookup for AFPServer entities over the selected DDP
// transport (default ltoudp) and prints each responder's name, zone, and net.node.
func discoverAFP(cfg config) int {
	kind := cfg.ifaceType
	if kind == "" {
		kind = clientlink.KindLToUDP
	}
	opener := clientlink.NewOpener(clientlink.Spec{Kind: kind, Name: cfg.iface})
	dl, err := opener.DatagramLinkDDP()
	if err != nil {
		return fail(fmt.Errorf("open transport: %w", err))
	}
	ep := atalk.NewEndpoint(dl, atalk.Addr{Network: opener.Net, Node: opener.Node})
	defer ep.Close()

	ents, err := ep.Lookup("=", atalk.AFPServerType, "*")
	if err != nil {
		return fail(err)
	}
	if len(ents) == 0 {
		fmt.Println("no AFP servers responded")
		return 0
	}
	for _, e := range ents {
		fmt.Printf("%s:%s\tafp://%s:%s/  (%d.%d socket %d)\n",
			e.Object, e.Zone, e.Object, e.Zone, e.Addr.Network, e.Addr.Node, e.Addr.Socket)
	}
	return 0
}
