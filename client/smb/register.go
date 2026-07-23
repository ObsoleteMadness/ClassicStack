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
	// SMB rides several client transports over two link kinds. On a raw NIC (pcap) the
	// carrier (Spec.Carrier) selects among: direct-hosted straight on IPX (socket 0x0550,
	// the DEFAULT — the connectionless direct-SMB-over-IPX path matching the classic
	// DOS/WfW/OS-2 redirectors); NetBIOS-over-IPX (NBIPX/NWLink, socket 0x0455, a
	// sequenced session); and raw NetBIOS-over-802.2 (NBF/NetBEUI). On TCP it is
	// direct-hosted-over-TCP (:445/:139). LToUDP/TashTalk are AFP-over-DDP ONLY and are
	// correctly absent, so `smb over ltoudp` is rejected by the CLI with a clear message.
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

// SMB pcap carriers: the Spec.Carrier value selecting which framing SMB rides over a raw
// NIC. Empty defaults to direct-hosted IPX. The CLI -transport flag threads one of these.
const (
	CarrierDirectIPX = "ipx"   // direct-hosted SMB straight on IPX (socket 0x0550), the default
	CarrierNBIPX     = "nbipx" // SMB over NetBIOS-over-IPX (NWLink, socket 0x0455)
	CarrierNBF       = "nbf"   // SMB over raw NetBIOS-over-802.2 (NetBEUI)
)

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

	tr, err := openTransport(opts.Opener, target.Server)
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

// openTransport builds an SMB Transport from the opener. On a raw pcap NIC the carrier
// (Spec.Carrier) selects the framing: direct-hosted straight on IPX (the default),
// NetBIOS-over-IPX (NBIPX/NWLink), or raw NetBIOS-over-802.2 (NBF). On TCP it is
// direct-hosted-over-TCP. serverName is the \\SERVER label from the URI, needed by the
// session carriers (NBIPX/NBF) to address the NetBIOS called name. The raw-NIC path
// presents a virtual-station MAC — the opener's pinned MAC, or a synthesised
// locally-administered random one (RandomMAC) so the client never borrows the host NIC's
// identity.
func openTransport(opener *clientlink.Opener, serverName string) (Transport, error) {
	switch opener.Spec.Kind {
	case clientlink.KindPcap, "":
		return openPcapTransport(opener, serverName)
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

// openPcapTransport opens the raw-NIC SMB carrier the opener's Spec.Carrier selects:
// direct-hosted IPX (default/"ipx"), NBIPX ("nbipx"), or NBF ("nbf"). All open one pcap
// FrameLink (filtered to the carrier's kernel BPF) and present the virtual-station MAC.
func openPcapTransport(opener *clientlink.Opener, serverName string) (Transport, error) {
	mac := opener.MAC
	if mac == ([6]byte{}) {
		mac = RandomMAC()
	}
	switch opener.Spec.Carrier {
	case CarrierDirectIPX, "":
		fl, err := opener.FrameLink(ipxBPF)
		if err != nil {
			return nil, err
		}
		return DialIPX(fl, mac), nil
	case CarrierNBIPX:
		fl, err := opener.FrameLink(ipxBPF)
		if err != nil {
			return nil, err
		}
		return DialNBIPX(fl, mac, serverName)
	case CarrierNBF:
		fl, err := opener.FrameLink(nbfBPF)
		if err != nil {
			return nil, err
		}
		return DialNBF(fl, mac, serverName)
	default:
		return nil, fmt.Errorf("smb: carrier %q not supported over pcap (want %s|%s|%s)",
			opener.Spec.Carrier, CarrierDirectIPX, CarrierNBIPX, CarrierNBF)
	}
}
