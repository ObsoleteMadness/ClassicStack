package ncp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/client"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	ipxport "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
)

// register.go plugs the NCP client into the client scheme registry. Importing this
// package registers "ncp"; client.Connect then builds an *FS and (because NCP has no
// native forks) wraps it with the "appledouble" fork backend so the server's own
// "._NAME" AppleDouble sidecars are read/written as ordinary 8.3 files.

func init() {
	// NCP rides one client transport: over-IPX on a raw NIC (pcap), the connectionless
	// NCP-over-IPX path (socket 0x0451) matching the NETx/VLM shells. LToUDP/TashTalk are
	// AFP-over-DDP only and correctly absent; NCP has no TCP transport in this client
	// (NCP/IP is a later slice), so `ncp over tcp` is rejected by the CLI.
	client.RegisterClient("ncp", "appledouble",
		client.Transports{
			Kinds:   []string{clientlink.KindPcap},
			Default: clientlink.KindPcap,
		},
		connect,
		fs.Param{Key: "user", Doc: "NetWare user name (empty = guest login)"},
		fs.Param{Key: "pass", Secret: true, Doc: "NetWare password (cleartext)"},
	)
}

// ipxBPF is the kernel capture filter for the IPX transport (libpcap's "ipx" primitive,
// matching all three legacy IPX framings), so the read loop is not fed the NIC's
// unrelated background traffic. It mirrors core/port/ipx.BPFFilter.
const ipxBPF = "ipx"

// connect is the client.Factory for NCP: open the transport, run the attach flow
// (CreateConnection / NegotiateBuffer / Login / GetVolumeNumber / AllocDirHandle), and
// return the *FS mounted on the volume named by the URI.
func connect(ctx context.Context, target uri.Target, opts client.Options) (fs.FileSystem, error) {
	_ = ctx

	tr, err := openTransport(opts.Opener, target.Server)
	if err != nil {
		return nil, fmt.Errorf("ncp: open transport: %w", err)
	}

	sess, err := Open(tr, DialParams{
		Volume:   target.Volume,
		User:     target.User,
		Password: target.Pass,
	})
	if err != nil {
		_ = tr.Close()
		return nil, err
	}
	f := New(sess)
	f.readOnly = opts.ReadOnly
	return f, nil
}

// openTransport builds an NCP Transport from the opener: over-IPX on a raw pcap NIC. The
// IPX path presents a virtual-station MAC — the opener's pinned MAC, or a synthesised
// locally-administered random one (RandomMAC) so the client never borrows the host NIC's
// identity. Spec.FrameType optionally PINS the Ethernet encapsulation; empty lets the
// transport learn the server's frame type from its first reply (the right default for a
// real NetWare server bound on raw-802.3 / 802.2).
//
// serverName is resolved to a routable IPX address via SAP FIRST (resolve.go), because a
// real NetWare server offers NCP on its internal network a router hop away — a net-0
// broadcast CreateConnection never reaches it. The transport is then pre-seeded with that
// address so the attach is addressed straight at the service. If SAP resolution fails
// (e.g. our own in-process server that answers a broadcast directly), it falls back to the
// broadcast-and-learn path so the existing e2e/self-server flow is unchanged.
func openTransport(opener *clientlink.Opener, serverName string) (Transport, error) {
	switch opener.Spec.Kind {
	case clientlink.KindPcap, "":
		fl, err := opener.FrameLink(ipxBPF)
		if err != nil {
			return nil, err
		}
		mac := opener.MAC
		if mac == ([6]byte{}) {
			mac = RandomMAC()
		}
		frameType, pinned, err := parseFrameType(opener.Spec.FrameType)
		if err != nil {
			_ = fl.Close()
			return nil, err
		}
		// Resolve the server via SAP so the attach is addressed to its real (internal) IPX
		// net/node and unicast to the next-hop MAC. A pinned frame type restricts the query
		// to that framing; otherwise all three are tried and the matching reply's framing is
		// adopted. Resolution failure is non-fatal — fall back to broadcast-and-learn.
		var queryTypes []ipxport.FrameType
		if pinned {
			queryTypes = []ipxport.FrameType{frameType}
		}
		if srv, rerr := resolveServer(fl, mac, serverName, queryTypes); rerr == nil {
			return DialIPXResolved(fl, mac, srv, pinned), nil
		}
		return DialIPXFrame(fl, mac, frameType, pinned), nil
	default:
		return nil, fmt.Errorf("ncp: transport kind %q not supported", opener.Spec.Kind)
	}
}

// parseFrameType maps the opener's optional frame-type override to a core/port/ipx
// FrameType and a "pinned" flag. An empty string yields (default, unpinned) so the
// transport learns the server's frame type from its reply; a non-empty value pins it.
func parseFrameType(s string) (ipxport.FrameType, bool, error) {
	if s == "" {
		return ipxport.DefaultFrameType, false, nil
	}
	ft, err := ipxport.ParseFrameType(s)
	if err != nil {
		return ipxport.DefaultFrameType, false, fmt.Errorf("ncp: %w", err)
	}
	return ft, true, nil
}

// ErrTransportClosed is returned when a Send races a Close.
var ErrTransportClosed = errors.New("ncp: transport closed")
