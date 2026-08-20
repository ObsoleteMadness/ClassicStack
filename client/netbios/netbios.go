// Package netbios is the client SDK's NetBIOS connectionless-datagram carrier: the
// second-class datagram half of NetBIOS a "net send" (Messenger) and a "net view"
// (browser) ride, as opposed to the connection-oriented session carriers the SMB file
// client uses (client/smb's NBIPX/NBF session transports). Where those establish a
// circuit and exchange request/response SMB messages, this carrier fires and receives
// one-way NetBIOS datagrams — a mailslot write to a named mailslot (\MAILSLOT\MESSNGR,
// \MAILSLOT\BROWSE) — over the same two raw-NIC carriers the SMB client supports:
//
//   - NBF  (NetBIOS Frames / NetBEUI): the mailslot write rides an 802.2 LLC UI frame
//     (DSAP=SSAP=0xF0) as an NBF DATAGRAM (directed, 0x08) or DATAGRAM_BROADCAST (0x09)
//     to the NetBIOS functional-address multicast MAC.
//   - NBIPX (NetBIOS-over-IPX / NWLink): the mailslot write rides an NMPI MailslotSend
//     (opcode 0xFC) inside an IPX type-20 datagram on the datagram socket (0x0553).
//   - IPX (direct-hosted SMB over IPX): the same NMPI MailslotSend on the same socket —
//     a direct-hosted station has no NetBIOS session layer but runs the full browser
//     protocol on the NBIPX datagram plane verbatim (golden nwlink-win98.pcap frames
//     26-41). It is a separate carrier only so a caller runs the follow-up SMB session
//     over the right transport.
//
// Both wire encodings mirror the SERVER's emitDatagram exactly (core/service/netbios
// nbf.go / nbipx.go), so a datagram this carrier sends is byte-indistinguishable from
// one ClassicStack itself emits, and one it receives is decoded by the same core codecs
// the server ingests with (core/protocol/{netbeui,netbios,mailslot,browser,messenger}).
//
// This is a client-SDK building block, not a CLI: cmd/csnetsend and cmd/csnetview are
// thin consumers of Conn.SendMessage / Conn.Browse. A third-party client embeds this
// package to send pop-up messages or enumerate a legacy Windows/OS-2 segment without
// re-deriving the mailslot/browser wire formats.
//
// Ring: CLIENT (may import adapter/ and core/, unlike core/).
package netbios

import (
	"fmt"
	"strings"

	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// Protocol selects which raw-NIC NetBIOS datagram carrier a Conn rides. The token
// vocabulary matches the SMB file client's -transport carriers (client/smb's
// CarrierNBF / CarrierNBIPX), so a user names the transport the same way across tools;
// only the two NetBIOS-datagram-capable carriers appear here (direct-hosted IPX and TCP
// carry SMB sessions, not connectionless mailslot datagrams, so they are absent).
type Protocol string

const (
	// NBF is NetBIOS Frames (NetBEUI) over 802.2 LLC — the mailslot rides an NBF
	// DATAGRAM / DATAGRAM_BROADCAST UI frame.
	NBF Protocol = "nbf"
	// NBIPX is NetBIOS-over-IPX (NWLink) — the mailslot rides an NMPI MailslotSend in an
	// IPX type-20 datagram on socket 0x0553.
	NBIPX Protocol = "nbipx"
	// IPX is direct-hosted SMB over IPX (NWLink "direct host", socket 0x0550). Its
	// BROWSER datagram plane is byte-identical to NBIPX's — a direct-hosted station
	// announces, elects and answers GetBackupList with the same NMPI MailslotSend on
	// socket 0x0553 (golden spec/captures/nwlink-win98.pcap frames 26-41 versus
	// nbipx-win98.pcap frames 16-60) — so a Conn opened on it sends and decodes exactly
	// what NBIPX does. What differs is the SESSION leg a caller runs afterwards: a
	// browse-list NetServerEnum2 goes over direct-hosted SMB (client/smb's
	// CarrierDirectIPX) instead of an NB-IPX session on socket 0x0455. The two are
	// separate carriers because a station binds one or the other: a direct-host-only
	// Win98 refuses an NB-IPX session and vice versa, so each must be swept and reported
	// on its own.
	IPX Protocol = "ipx"
	// TCP is the TCP/IP browse family (NBT name service + SMB-over-TCP). It is NOT a
	// datagram Conn carrier — Conn.Open rejects it — but browse.EnumerateTCP tags
	// servers with this so a UI can badge TCP/IP hits separately from NBF/NBIPX.
	TCP Protocol = "tcp"
)

// Protocols is every carrier a Conn can open, in a stable order. csnetview iterates it
// to sweep each transport; a UI renders it as the transport choices.
var Protocols = []Protocol{NBF, NBIPX, IPX}

// ipxFamily reports whether p rides the NWLink IPX datagram plane (NMPI MailslotSend on
// socket 0x0553) — true for both NBIPX and direct-hosted IPX, which share it verbatim.
func ipxFamily(p Protocol) bool { return p == NBIPX || p == IPX }

// NetBIOS name-type suffixes a datagram consumer needs, re-exported so a caller stamps a
// station or target name without reaching into the core codec. NameTypeWorkstation (<00>)
// is a client's own name; NameTypeFileServer (<20>) addresses a server; MessengerNameType
// (<03>, defined in messenger.go) addresses a "net send" recipient.
const (
	NameTypeWorkstation = nb.NameTypeWorkstation
	NameTypeFileServer  = nb.NameTypeFileServer
)

// ParseProtocol maps a token ("nbf", "nbipx", "ipx", case-insensitive) to a Protocol, or
// returns an error naming the accepted values. An empty token is rejected (the caller
// decides its own default) so a silent wrong-carrier send is impossible.
func ParseProtocol(s string) (Protocol, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(NBF):
		return NBF, nil
	case string(NBIPX):
		return NBIPX, nil
	case string(IPX):
		return IPX, nil
	default:
		return "", fmt.Errorf("netbios: unknown protocol %q (want %s, %s or %s)", s, NBF, NBIPX, IPX)
	}
}

// Target is a NetBIOS datagram recipient: a NetBIOS Name and the carrier Protocol to
// reach it over — the "<name>,<protocol>" address form the datagram tools accept (e.g.
// "SERVER,nbf"). It is the connectionless-datagram analogue of a file client's URI: it
// names WHO to reach and OVER WHAT, and nothing else (a datagram has no share/volume).
type Target struct {
	Name     nb.Name
	Protocol Protocol
}

// ParseTarget parses a "<name>,<protocol>" recipient into a Target. nameType is the
// NetBIOS name-type suffix to stamp on the name (nb.NameTypeMessenger for a "net send"
// recipient, nb.NameTypeFileServer to address a server); the caller supplies it because
// the same "<name>,<protocol>" syntax addresses different resource types. The protocol
// half is required — a datagram must name its carrier — and is validated by
// ParseProtocol so an unknown carrier fails up front with a clear message.
func ParseTarget(s string, nameType uint8) (Target, error) {
	name, proto, ok := strings.Cut(s, ",")
	if !ok {
		return Target{}, fmt.Errorf("netbios: target %q must be \"<name>,<protocol>\" (e.g. SERVER,%s)", s, NBF)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Target{}, fmt.Errorf("netbios: target %q has an empty name", s)
	}
	p, err := ParseProtocol(proto)
	if err != nil {
		return Target{}, err
	}
	return Target{Name: nb.NewName(name, nameType), Protocol: p}, nil
}
