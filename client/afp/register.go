package afp

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/client"
	aspclient "github.com/ObsoleteMadness/ClassicStack/client/asp"
	"github.com/ObsoleteMadness/ClassicStack/client/atalk"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// register.go plugs the AFP client into the client scheme registry. Importing this
// package registers "afp"; client.Connect then builds an *FS and (because AFP
// implements fs.ForkEngine natively) wraps it with the "passthrough" fork backend so
// resource forks come off the wire.

func init() {
	// AFP rides DDP, so its transports are the three AppleTalk segments: LToUDP
	// multicast (the default — needs no pcap/Npcap), EtherTalk over pcap, and TashTalk
	// serial. TCP (AFP-over-DSI) is intentionally absent — DSI does not exist yet. The
	// CLI validates -ifacetype against this set, so `smb over ltoudp` (etc.) is rejected
	// with a clear message rather than dialing a transport the protocol can't use.
	client.RegisterClient("afp", "passthrough",
		client.Transports{
			Kinds:   []string{clientlink.KindLToUDP, clientlink.KindPcap, clientlink.KindTashTalk},
			Default: clientlink.KindLToUDP,
		},
		connect,
		fs.Param{Key: "user", Doc: "AFP username (empty = guest login)"},
		fs.Param{Key: "pass", Secret: true, Doc: "AFP password (cleartext UAM)"},
	)
}

// afpListeningSocket is the well-known AFP/ASP server listening socket (SLS) on classic
// AppleTalk. The .XPP driver addresses ASPGetStatus/OpenSession here; the address is
// resolved from NBP (or the literal net.node) and this socket.
const afpListeningSocket uint8 = 251

// connect is the client.Factory for AFP: open the DDP transport, resolve the server
// address (NBP entity or literal net.node), open an ASP session, log in, and open the
// volume — returning the *FS (an fs.FileSystem + native fs.ForkEngine).
func connect(ctx context.Context, target uri.Target, opts client.Options) (fs.FileSystem, error) {
	_ = ctx
	dl, err := opts.Opener.DatagramLinkDDP()
	if err != nil {
		return nil, fmt.Errorf("afp: open transport: %w", err)
	}
	// The workstation asserts the opener's node; a real deployment runs an LLAP/AARP
	// claim above the FrameLink first (the LToUDP/EtherTalk framer already carries the
	// claimed node). For the in-process transport a static node is fine.
	ep := atalk.NewEndpoint(dl, atalk.Addr{Network: opts.Opener.Net, Node: opts.Opener.Node})

	srv, err := resolveServer(ep, target.Server)
	if err != nil {
		_ = ep.Close()
		return nil, err
	}
	sls := atalk.Addr{Network: srv.Network, Node: srv.Node, Socket: afpListeningSocket}
	if srv.Socket != 0 {
		sls.Socket = srv.Socket
	}

	a := atalk.NewATP(ep)
	sess, err := aspclient.Open(ep, a, sls)
	if err != nil {
		_ = ep.Close()
		return nil, err
	}
	if err := Login(sess, target.User, target.Pass, ""); err != nil {
		_ = sess.Close()
		_ = ep.Close()
		return nil, err
	}
	f, err := Open(sess, target.Volume)
	if err != nil {
		_ = sess.Close()
		_ = ep.Close()
		return nil, err
	}
	// Own the endpoint so Close tears everything down: FS.Close closes the session; wrap
	// it to also close the endpoint.
	f.onClose = func() { _ = ep.Close() }
	return f, nil
}

// resolveServer turns the URI server field into an AppleTalk address. A literal
// "net.node" (both decimal) is used directly; anything else is an NBP entity name
// ("object" or "object:zone") resolved by a broadcast lookup.
func resolveServer(ep *atalk.Endpoint, server string) (atalk.Addr, error) {
	if net, node, ok := parseNetNode(server); ok {
		return atalk.Addr{Network: net, Node: node}, nil
	}
	ent, err := ep.LookupOne(server)
	if err != nil {
		return atalk.Addr{}, fmt.Errorf("afp: NBP lookup %q: %w", server, err)
	}
	return ent.Addr, nil
}

// parseNetNode parses a literal "net.node" address (decimal network and node). ok is
// false when the string is not that form (so it is treated as an NBP name).
func parseNetNode(s string) (network uint16, node uint8, ok bool) {
	dot := strings.IndexByte(s, '.')
	if dot < 0 {
		return 0, 0, false
	}
	n, err1 := strconv.ParseUint(s[:dot], 10, 16)
	nd, err2 := strconv.ParseUint(s[dot+1:], 10, 8)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return uint16(n), uint8(nd), true
}
