package smb

import (
	"context"
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/client"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// register.go plugs the SMB client into the client scheme registry. Importing this
// package registers "smb"; client.Connect then builds an *FS and (because SMB has no
// native forks) wraps it with the "appledouble" fork backend so the server's "._name"
// AppleDouble sidecars are read/written over the data fork.

func init() {
	// SMB rides two client transports: direct-hosted-over-IPX on a raw NIC (pcap) — the
	// DEFAULT, the connectionless direct-SMB-over-IPX path (socket 0x0550) matching the
	// classic DOS/WfW/OS-2 redirectors — and direct-hosted-over-TCP (:445/:139). Both are
	// NetBIOS-session-less. LToUDP/TashTalk are AFP-over-DDP ONLY and are correctly absent,
	// so `smb over ltoudp` is rejected by the CLI with a clear message.
	client.RegisterClient("smb", "appledouble",
		client.Transports{
			Kinds:   []string{clientlink.KindPcap, clientlink.KindTCP},
			Default: clientlink.KindPcap,
		},
		connect,
		fs.Param{Key: "user", Doc: "SMB username (empty = guest login)"},
		fs.Param{Key: "pass", Secret: true, Doc: "SMB password (cleartext)"},
	)
}

// smbTCPPort is the default TCP port the client dials when the URI names no port:
// direct-hosted SMB over TCP ([MS-SMB] §2.1). :445 has no NetBIOS session handshake,
// which this client's DialTCP relies on.
const smbTCPPort = "445"

// ipxBPF is the kernel capture filter for the IPX transport (libpcap's "ipx" primitive,
// matching all three legacy IPX framings), so the read loop is not fed the NIC's
// unrelated background traffic. It mirrors core/port/ipx.BPFFilter.
const ipxBPF = "ipx"

// connect is the client.Factory for SMB: open the transport, run the session
// handshake (NEGOTIATE / SESSION_SETUP / TREE_CONNECT), and return the *FS mounted on
// the share named by the URI.
func connect(ctx context.Context, target uri.Target, opts client.Options) (fs.FileSystem, error) {
	_ = ctx

	tr, err := openTransport(opts.Opener)
	if err != nil {
		return nil, fmt.Errorf("smb: open transport: %w", err)
	}

	sess, err := Open(tr, DialParams{
		ServerName: target.Server,
		Share:      target.Volume,
		User:       target.User,
		Password:   target.Pass,
		Domain:     "",
	})
	if err != nil {
		_ = tr.Close()
		return nil, err
	}
	f := New(sess)
	f.readOnly = opts.ReadOnly
	return f, nil
}

// openTransport builds an SMB Transport from the opener: direct-hosted-over-IPX on a raw
// pcap NIC (the default), or direct-hosted-over-TCP. The IPX path presents a virtual-
// station MAC — the opener's pinned MAC, or a synthesised locally-administered random one
// (RandomMAC) so the client never borrows the host NIC's identity.
func openTransport(opener *clientlink.Opener) (Transport, error) {
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
		return DialIPX(fl, mac), nil
	case clientlink.KindTCP:
		conn, err := opener.Dial(smbTCPPort)
		if err != nil {
			return nil, err
		}
		return DialTCP(conn), nil
	default:
		return nil, fmt.Errorf("smb: transport kind %q not supported", opener.Spec.Kind)
	}
}
