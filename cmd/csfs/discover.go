package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/client/atalk"
	etherdfsclient "github.com/ObsoleteMadness/ClassicStack/client/etherdfs"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	ncpclient "github.com/ObsoleteMadness/ClassicStack/client/ncp"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	etherdfsproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/etherdfs"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

// cmdDiscover runs a scheme's own discovery probe and prints each responder so it can be
// pasted into a URI. AFP uses an NBP broadcast lookup for the "AFPServer" type; NCP
// broadcasts a SAP General Query for file servers. The remaining schemes' probes (SMB
// browser, EtherDFS broadcast) land with their clients in later phases.
func cmdDiscover(cfg config, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: csfs discover <scheme>  (afp | smb | ncp | etherdfs)")
		return 2
	}
	scheme := args[0]
	switch scheme {
	case "afp":
		return discoverAFP(cfg)
	case "ncp":
		return discoverNCP(cfg)
	case "etherdfs":
		return discoverEtherDFS(cfg)
	case "smb":
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
	kind := cfg.IfaceType
	if kind == "" {
		kind = clientlink.KindLToUDP
	}
	opener := clientlink.NewOpener(clientlink.Spec{Kind: kind, Name: cfg.Iface})
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

// SAP/IPX discovery framing constants (mirroring the client/ncp IPX transport and
// core/port/ipx). NCP servers advertise themselves via SAP on socket 0x0452; a General
// Query for the File Server type (0x0004) draws a response from each server naming its
// NetWare name and IPX address.
const (
	sapDiscoverEtherType = 0x8137 // Ethernet II EtherType for IPX
	sapDiscoverEthHdrLen = 14     // dst6 + src6 + type2
	sapDiscoverWait      = 2 * time.Second
	sapDiscoverIPXType   = 0x04 // IPX packet type SAP rides (PEP; the server accepts type 0/4)
)

// discoverNCP broadcasts a SAP General Query for NetWare file servers over the selected
// raw NIC (pcap) and prints each responder's server name and IPX address so it can be
// pasted into an ncp:// URI. NCP discovery is IPX-only (SAP rides IPX), so it needs a
// pcap interface just like the ncp client transport.
func discoverNCP(cfg config) int {
	kind := cfg.IfaceType
	if kind == "" {
		kind = clientlink.KindPcap
	}
	if kind != clientlink.KindPcap {
		return fail(fmt.Errorf("discover ncp needs a pcap interface (SAP rides IPX); got -ifacetype %q", kind))
	}
	opener := clientlink.NewOpener(clientlink.Spec{Kind: kind, Name: cfg.Iface})
	if cfg.MAC != "" {
		mac, err := parseMAC(cfg.MAC)
		if err != nil {
			return fail(err)
		}
		opener.MAC = mac
	}
	fl, err := opener.FrameLink("ipx")
	if err != nil {
		return fail(fmt.Errorf("open transport: %w", err))
	}
	defer fl.Close()

	srcMAC := opener.MAC
	if srcMAC == ([6]byte{}) {
		srcMAC = ncpclient.RandomMAC()
	}

	// Broadcast the SAP General Query for the File Server type.
	query := ncpproto.MarshalQuery(ncpproto.SAPGeneralQuery, ncpproto.SAPServerTypeFileServer, nil)
	d := &ipxproto.Datagram{
		Type:    sapDiscoverIPXType,
		DstNode: [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		DstSock: ncpproto.SAPSocket,
		SrcNode: srcMAC,
		SrcSock: ncpproto.SAPSocket,
		Payload: query,
	}
	if err := writeSAPFrame(fl, d, srcMAC); err != nil {
		return fail(fmt.Errorf("send SAP query: %w", err))
	}

	// Collect responses for a short window.
	seen := map[string]bool{}
	deadline := time.Now().Add(sapDiscoverWait)
	count := 0
	for time.Now().Before(deadline) {
		frame, err := fl.Read()
		if err != nil {
			if err == link.ErrTimeout {
				continue
			}
			break
		}
		payload, ok := stripSAPFrame(frame)
		if !ok {
			continue
		}
		dd, derr := ipxproto.Decode(payload)
		if derr != nil || dd.DstSock != ncpproto.SAPSocket && dd.SrcSock != ncpproto.SAPSocket {
			continue
		}
		op, entries, perr := ncpproto.ParseSAPResponse(dd.Payload)
		if perr != nil || (op != ncpproto.SAPGeneralResponse && op != ncpproto.SAPNearestResponse) {
			continue
		}
		for _, e := range entries {
			if e.Type != ncpproto.SAPServerTypeFileServer || seen[e.Name] {
				continue
			}
			seen[e.Name] = true
			count++
			fmt.Printf("%s\tncp://%s/SYS  (net %02X%02X%02X%02X node %02X%02X%02X%02X%02X%02X hops %d)\n",
				e.Name, e.Name,
				e.Network[0], e.Network[1], e.Network[2], e.Network[3],
				e.Node[0], e.Node[1], e.Node[2], e.Node[3], e.Node[4], e.Node[5], e.Hops)
		}
	}
	if count == 0 {
		fmt.Println("no NetWare servers responded")
	}
	return 0
}

// writeSAPFrame encapsulates an IPX datagram in an Ethernet II frame and writes it.
func writeSAPFrame(fl link.FrameLink, d *ipxproto.Datagram, srcMAC [6]byte) error {
	ipxBytes, err := d.Encode(nil)
	if err != nil {
		return err
	}
	frame := make([]byte, 0, sapDiscoverEthHdrLen+len(ipxBytes))
	frame = append(frame, d.DstNode[:]...)
	frame = append(frame, srcMAC[:]...)
	frame = append(frame, byte(sapDiscoverEtherType>>8), byte(sapDiscoverEtherType&0xFF))
	frame = append(frame, ipxBytes...)
	return fl.Write(frame)
}

// stripSAPFrame returns the IPX datagram bytes from an Ethernet II IPX frame (the
// 0x8137 EtherType path), or false when the frame is not IPX-over-Ethernet-II.
func stripSAPFrame(frame []byte) ([]byte, bool) {
	if len(frame) < sapDiscoverEthHdrLen {
		return nil, false
	}
	if uint16(frame[12])<<8|uint16(frame[13]) != sapDiscoverEtherType {
		return nil, false
	}
	return frame[sapDiscoverEthHdrLen:], true
}

// etherdfsDiscoverWait bounds how long the EtherDFS discovery collects replies.
const etherdfsDiscoverWait = 2 * time.Second

// discoverEtherDFS broadcasts an AL_INSTALLCHK over the selected raw NIC (pcap) and
// prints each responder's server name and MAC so a drive can be pasted into an
// etherdfs:// URI. EtherDFS discovery is raw-Ethernet-only (EtherType 0xEDF5), so it
// needs a pcap interface. The reference client learns the server MAC from an ordinary
// AL_DISKSPACE reply; AL_INSTALLCHK additionally draws the server NAME from a
// ClassicStack server, which this prints for a friendlier listing.
func discoverEtherDFS(cfg config) int {
	kind := cfg.IfaceType
	if kind == "" {
		kind = clientlink.KindPcap
	}
	if kind != clientlink.KindPcap {
		return fail(fmt.Errorf("discover etherdfs needs a pcap interface (raw Ethernet); got -ifacetype %q", kind))
	}
	opener := clientlink.NewOpener(clientlink.Spec{Kind: kind, Name: cfg.Iface})
	if cfg.MAC != "" {
		mac, err := parseMAC(cfg.MAC)
		if err != nil {
			return fail(err)
		}
		opener.MAC = mac
	}
	// The EtherDFS BPF filter (mirrors core/port/etherdfs.BPFFilter); narrows the pcap
	// handle to the custom EtherType so the read loop is not fed unrelated traffic.
	fl, err := opener.FrameLink("ether proto 0xedf5")
	if err != nil {
		return fail(fmt.Errorf("open transport: %w", err))
	}
	defer fl.Close()

	srcMAC := opener.MAC
	if srcMAC == ([6]byte{}) {
		srcMAC = etherdfsclient.RandomMAC()
	}

	// Broadcast an AL_INSTALLCHK for drive 0 (A:): the server answers with its name.
	req := etherdfsproto.Frame{
		DstMAC:   [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		SrcMAC:   srcMAC,
		Sequence: 1,
		Drive:    0,
		Opcode:   etherdfsproto.OpInstallChk,
	}
	if err := fl.Write(req.Encode(nil)); err != nil {
		return fail(fmt.Errorf("send install check: %w", err))
	}

	seen := map[[6]byte]bool{}
	deadline := time.Now().Add(etherdfsDiscoverWait)
	count := 0
	for time.Now().Before(deadline) {
		frame, err := fl.Read()
		if err != nil {
			if err == link.ErrTimeout {
				continue
			}
			break
		}
		f, perr := etherdfsproto.ParseFrame(frame)
		if perr != nil || f.SrcMAC == srcMAC || seen[f.SrcMAC] {
			continue
		}
		seen[f.SrcMAC] = true
		count++
		name := strings.TrimRight(string(f.Payload), "\x00")
		if name == "" {
			name = "(unnamed EtherDFS server)"
		}
		fmt.Printf("%s\tetherdfs://%02x:%02x:%02x:%02x:%02x:%02x/C\n",
			name, f.SrcMAC[0], f.SrcMAC[1], f.SrcMAC[2], f.SrcMAC[3], f.SrcMAC[4], f.SrcMAC[5])
	}
	if count == 0 {
		fmt.Println("no EtherDFS servers responded")
	}
	return 0
}
