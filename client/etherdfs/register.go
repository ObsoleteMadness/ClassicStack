package etherdfs

import (
	"context"
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/client"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// register.go plugs the EtherDFS client into the client scheme registry. Importing this
// package registers "etherdfs"; client.Connect then builds an *FS and (because EtherDFS
// has no native forks) wraps it with the "appledouble" fork backend so the server's own
// "._NAME" AppleDouble sidecars are read/written as ordinary 8.3 files.

func init() {
	// EtherDFS rides one client transport: raw Ethernet on a NIC (pcap), the EtherType-
	// 0xEDF5 single-frame request/response protocol matching the DOS EtherDFS TSR. It has
	// no IPX/DDP/TCP transport, so any other -ifacetype is rejected by the CLI.
	client.RegisterClient("etherdfs", "appledouble",
		client.Transports{
			Kinds:   []string{clientlink.KindPcap},
			Default: clientlink.KindPcap,
		},
		connect,
	)
}

// bpfEtherDFS is the kernel capture filter for the EtherDFS transport (the custom
// EtherType), so the read loop is not fed unrelated background traffic. It mirrors
// core/port/etherdfs.BPFFilter.
const bpfEtherDFS = "ether proto 0xedf5"

// connect is the client.Factory for EtherDFS: open the raw-Ethernet transport, resolve
// the drive letter and probe the server (learning its MAC), and return the *FS mounted
// on the drive named by the URI's volume field (a single drive letter).
func connect(ctx context.Context, target uri.Target, opts client.Options) (fs.FileSystem, error) {
	_ = ctx

	tr, err := openTransport(opts.Opener)
	if err != nil {
		return nil, fmt.Errorf("etherdfs: open transport: %w", err)
	}

	sess, err := Open(tr, DialParams{Drive: target.Volume})
	if err != nil {
		_ = tr.Close()
		return nil, err
	}
	f := New(sess)
	f.readOnly = opts.ReadOnly
	return f, nil
}

// openTransport builds an EtherDFS Transport from the opener: raw Ethernet on a pcap
// NIC. The client presents a virtual-station MAC — the opener's pinned MAC, or a
// synthesised locally-administered random one (RandomMAC) so the client never borrows
// the host NIC's identity.
func openTransport(opener *clientlink.Opener) (Transport, error) {
	switch opener.Spec.Kind {
	case clientlink.KindPcap, "":
		fl, err := opener.FrameLink(bpfEtherDFS)
		if err != nil {
			return nil, err
		}
		mac := opener.MAC
		if mac == ([6]byte{}) {
			mac = RandomMAC()
		}
		return DialFrame(fl, mac), nil
	default:
		return nil, fmt.Errorf("etherdfs: transport kind %q not supported", opener.Spec.Kind)
	}
}
