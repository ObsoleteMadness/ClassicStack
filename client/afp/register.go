package afp

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/client"
	aspclient "github.com/ObsoleteMadness/ClassicStack/client/asp"
	"github.com/ObsoleteMadness/ClassicStack/client/atalk"
	dsiclient "github.com/ObsoleteMadness/ClassicStack/client/dsi"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
)

// register.go plugs the AFP client into the client scheme registry. Importing this
// package registers "afp"; client.Connect then builds an *FS and (because AFP
// implements fs.ForkEngine natively) wraps it with the "passthrough" fork backend so
// resource forks come off the wire.

func init() {
	// AFP rides DDP on the three AppleTalk segments (LToUDP multicast — the default,
	// needs no pcap/Npcap — EtherTalk over pcap, and TashTalk serial) or DSI over TCP
	// (-ifacetype tcp; -iface names the DSI host, conventionally :548).
	client.RegisterClient("afp", "passthrough",
		client.Transports{
			Kinds:   []string{clientlink.KindLToUDP, clientlink.KindPcap, clientlink.KindTashTalk, clientlink.KindTCP},
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

// dsiDefaultPort is the conventional AFP-over-TCP (DSI) port a URI/opener with no
// explicit port dials (Inside Macintosh: Networking, Ch. 9; spec/21-dsi.md).
const dsiDefaultPort = "548"

// connect is the client.Factory for AFP: dial the transport the opener selects (ASP
// over DDP, or DSI over TCP), resolve the server, log in, and open the volume —
// returning the *FS (an fs.FileSystem + native fs.ForkEngine).
func connect(ctx context.Context, target uri.Target, opts client.Options) (fs.FileSystem, error) {
	_ = ctx
	sess, srvInfo, redial, onClose, err := dialAndLogin(target, opts)
	if err != nil {
		return nil, err
	}
	f, err := Open(sess, target.Volume)
	if err != nil {
		_ = sess.Close()
		onClose()
		return nil, err
	}
	f.redial = redial
	f.user = target.User
	f.pass = target.Pass
	f.srvInfo = srvInfo
	f.onMessage = opts.OnServerMessage
	f.onClose = onClose
	f.installAttentionHandler()
	f.fetchLoginMessage()
	return f, nil
}

// dialAndLogin dispatches to the ASP-over-DDP or DSI-over-TCP dial path by the
// opener's transport kind, and runs FPLogin on the resulting session. It returns the
// live Session, the server's advertised info (for the reconnect path's re-login), a
// redial closure that opens a fresh session on the same transport target (ASP: a new
// session on the existing DDP endpoint; DSI: a fresh TCP dial), and an onClose closure
// releasing whatever transport-level resource outlives the Session itself.
func dialAndLogin(target uri.Target, opts client.Options) (Session, proto.ServerInfo, func() (Session, error), func(), error) {
	if opts.Opener != nil && opts.Opener.Spec.Kind == clientlink.KindTCP {
		return dialAndLoginDSI(target, opts)
	}
	return dialAndLoginASP(target, opts)
}

// dialAndLoginASP runs the classic AFP connect prologue shared by a volume mount
// (connect) and a server-root browse (Browse): open the DDP transport, resolve the
// server, negotiate the login from FPGetSrvrInfo, open the ASP session, and log in.
func dialAndLoginASP(target uri.Target, opts client.Options) (Session, proto.ServerInfo, func() (Session, error), func(), error) {
	dl, err := opts.Opener.DatagramLinkDDP()
	if err != nil {
		return nil, proto.ServerInfo{}, nil, nil, fmt.Errorf("afp: open transport: %w", err)
	}
	// The workstation asserts the opener's node; a real deployment runs an LLAP/AARP
	// claim above the FrameLink first (the LToUDP/EtherTalk framer already carries the
	// claimed node). For the in-process transport a static node is fine.
	ep := atalk.NewEndpoint(dl, atalk.Addr{Network: opts.Opener.Net, Node: opts.Opener.Node})

	srv, err := resolveServer(ep, target.Server)
	if err != nil {
		_ = ep.Close()
		return nil, proto.ServerInfo{}, nil, nil, err
	}
	sls := atalk.Addr{Network: srv.Network, Node: srv.Node, Socket: afpListeningSocket}
	if srv.Socket != 0 {
		sls.Socket = srv.Socket
	}
	if atalk.Verbose() {
		fmt.Fprintf(os.Stderr, "[afp] resolved server %q → SLS %s (local %s)\n",
			target.Server, sls, ep.LocalAddr())
	}

	a := atalk.NewATP(ep)

	// Negotiate the login from the server's own FPGetSrvrInfo: a classic Mac server
	// SILENTLY IGNORES an FPLogin that names an AFP version string or UAM it did not
	// advertise (observed: System 7.5 offers "AFPVersion 2.1", not "AFP2.2", and
	// "Cleartxt passwrd" with a lower-case p — spec/errata.md). GetStatus needs no
	// session, so it runs before OpenSession. A GetStatus failure is non-fatal — the
	// login falls back to the client defaults.
	var srvInfo proto.ServerInfo
	if status, gerr := aspclient.GetStatus(a, sls); gerr == nil {
		srvInfo, _ = proto.ParseServerInfo(status)
		if atalk.Verbose() {
			fmt.Fprintf(os.Stderr, "[afp] server %q machine=%q versions=%v uams=%v\n",
				srvInfo.ServerName, srvInfo.MachineType, srvInfo.AFPVersions, srvInfo.UAMs)
		}
	} else if atalk.Verbose() {
		fmt.Fprintf(os.Stderr, "[afp] GetStatus failed (%v); using default version/UAM\n", gerr)
	}

	sess, err := aspclient.Open(ep, a, sls)
	if err != nil {
		_ = ep.Close()
		return nil, proto.ServerInfo{}, nil, nil, err
	}
	if err := LoginNegotiated(sess, target.User, target.Pass, srvInfo); err != nil {
		_ = sess.Close()
		_ = ep.Close()
		return nil, proto.ServerInfo{}, nil, nil, err
	}

	redial := func() (Session, error) {
		a := atalk.NewATP(ep)
		return aspclient.Open(ep, a, sls)
	}
	onClose := func() { _ = ep.Close() }
	return sess, srvInfo, redial, onClose, nil
}

// dialAndLoginDSI runs the modern AFP-over-TCP connect prologue: dial the opener's TCP
// target (the DSI host, conventionally :548), run DSI GetStatus + OpenSession
// (client/dsi.Dial), negotiate the login from the returned FPGetSrvrInfo (identical
// negotiation to the ASP path — LoginNegotiated does not care which transport it runs
// over), and log in. There is no NBP/SLS resolution: the TCP target IS the address.
func dialAndLoginDSI(target uri.Target, opts client.Options) (Session, proto.ServerInfo, func() (Session, error), func(), error) {
	redial := func() (Session, error) {
		conn, err := opts.Opener.Dial(dsiDefaultPort)
		if err != nil {
			return nil, fmt.Errorf("afp: dial DSI: %w", err)
		}
		_, sess, err := dsiclient.Dial(conn)
		if err != nil {
			return nil, err
		}
		return sess, nil
	}

	conn, err := opts.Opener.Dial(dsiDefaultPort)
	if err != nil {
		return nil, proto.ServerInfo{}, nil, nil, fmt.Errorf("afp: dial DSI: %w", err)
	}
	status, sess, err := dsiclient.Dial(conn)
	if err != nil {
		return nil, proto.ServerInfo{}, nil, nil, fmt.Errorf("afp: open DSI session: %w", err)
	}
	srvInfo, _ := proto.ParseServerInfo(status)
	if atalk.Verbose() {
		fmt.Fprintf(os.Stderr, "[afp] server %q (DSI) machine=%q versions=%v uams=%v\n",
			srvInfo.ServerName, srvInfo.MachineType, srvInfo.AFPVersions, srvInfo.UAMs)
	}

	if err := LoginNegotiated(sess, target.User, target.Pass, srvInfo); err != nil {
		_ = sess.Close()
		return nil, proto.ServerInfo{}, nil, nil, err
	}

	// DSI has no separate endpoint object to close (Session.Close already closes the
	// TCP connection), unlike ASP's DDP endpoint that outlives any one session.
	return sess, srvInfo, redial, func() {}, nil
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
