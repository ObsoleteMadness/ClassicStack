package netbios

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	corelink "github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	mailslotproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	nbf "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// conn.go is the raw-NIC datagram carrier: it owns one pcap FrameLink narrowed to the
// carrier's kernel filter, encapsulates an outbound mailslot write in the carrier's L2
// framing, and strips inbound frames back to the mailslot payload. Encapsulation mirrors
// the server's emitDatagram (core/service/netbios) so the wire bytes match ClassicStack.

// dtrace narrates the datagram carrier at log.Trace through the shared client/trace sink
// (scope "netbios-dg"), so a tool's -v shows the send/listen steps alongside every other
// client transport's trace. It is the connectionless-datagram analogue of client/smb's
// per-transport tracers.
var dtrace = trace.Logger("netbios-dg")

func dtracef(format string, args ...any) {
	if !dtrace.Enabled(log.Trace) {
		return
	}
	dtrace.Log0(log.Trace, fmt.Sprintf(format, args...))
}

// Ethernet / IPX framing constants (mirror client/smb's ipx.go + core/port encodings).
const (
	ethHdrLen     = 14
	etherTypeIPX  = 0x8137
	ipxNetBIOSTyp = nb.IPXTypeNetBIOS // 0x14 — IPX type-20 NetBIOS broadcast/forwarding
	ipxPEPTyp     = nb.IPXTypePEP     // 0x04 — PEP, the type a DIRECTED NMPI datagram uses
)

// llcNetBIOS is the 802.2 LLC UI header for NBF (DSAP=SSAP=0xF0, control=0x03) — the
// encapsulation every NBF UI datagram rides (mirrors core/port/netbeui).
var llcNetBIOS = [3]byte{0xF0, 0xF0, 0x03}

// nbDatagramSocket is the IPX socket NB-IPX datagrams (NMPI mailslot sends) ride
// (0x0553) — the single core definition, shared with the server's session engine.
var nbDatagramSocket = nb.NBIPXDatagramSocket

// broadcastMAC is the Ethernet broadcast address; on Ethernet the IPX broadcast node is
// all-ones, so an NBIPX broadcast datagram frames to it.
var broadcastMAC = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

// bpfFor returns the kernel capture filter for a carrier: "llc" for NBF (all 802.2 LLC,
// re-validated to the NetBIOS DSAP on decode), "ipx" for NBIPX. Matches client/smb's
// nbfBPF / ipxBPF so the read loop is not fed the NIC's unrelated background traffic.
func bpfFor(p Protocol) string {
	if p == NBF {
		return "llc"
	}
	return "ipx"
}

// Conn is an open NetBIOS datagram carrier over one raw NIC: it sends mailslot writes
// (the Messenger / browser datagram form) and receives inbound ones. It carries no
// session state — every datagram is independent and connectionless — so a single Conn
// serves both a one-shot SendMessage and a listen-and-collect Browse.
type Conn struct {
	proto  Protocol
	fl     corelink.FrameLink
	srcMAC [6]byte
	// srcName is this station's NetBIOS name, stamped as the datagram Source so a
	// responder (a browser answering an AnnouncementRequest) can direct its reply.
	srcName nb.Name
}

// Open opens a datagram carrier for proto over the pcap device the opener addresses.
// srcName is this station's NetBIOS name (the datagram Source / NMPI SourceName). The
// opener supplies the virtual-station MAC (a pinned -mac or a synthesised
// locally-administered RandomMAC), so the client never borrows the host NIC's identity —
// the same rule client/smb's raw-NIC transports follow. Only a pcap opener has a raw
// FrameLink; ltoudp/tashtalk/tcp are rejected (they carry no NetBIOS datagrams).
func Open(opener *link.Opener, proto Protocol, srcName nb.Name) (*Conn, error) {
	fl, err := opener.FrameLink(bpfFor(proto))
	if err != nil {
		return nil, fmt.Errorf("netbios: open %s carrier: %w", proto, err)
	}
	mac := opener.MAC
	if mac == ([6]byte{}) {
		mac = RandomMAC()
	}
	dtracef("opened %s datagram carrier (station %s, MAC %s)", proto, srcName.String(), macString(mac))
	return &Conn{proto: proto, fl: fl, srcMAC: mac, srcName: srcName}, nil
}

// Close releases the underlying FrameLink.
func (c *Conn) Close() error { return c.fl.Close() }

// Protocol reports the carrier this Conn rides.
func (c *Conn) Protocol() Protocol { return c.proto }

// SendMailslot transmits one mailslot write: it wraps body in the SMB_COM_TRANSACTION
// mailslot envelope for the named mailslot (\MAILSLOT\MESSNGR, \MAILSLOT\BROWSE) and
// emits it as a NetBIOS datagram to dst over this carrier. broadcast picks a
// group/broadcast datagram (fans to every station on the segment) versus a directed one
// (a single named recipient) — a "net send" to one machine is directed; a browser
// AnnouncementRequest is broadcast. The envelope + framing mirror the server's
// emitDatagram, so the bytes match ClassicStack's own.
func (c *Conn) SendMailslot(mailslotName string, dst nb.Name, body []byte, broadcast bool) error {
	payload := mailslotproto.Write{Name: mailslotName, Body: body}.Marshal()
	dtracef("%s mailslot %s → %s (%d bytes, broadcast=%t)", c.proto, mailslotName, dst.String(), len(payload), broadcast)
	switch {
	case c.proto == NBF:
		return c.sendNBF(dst, payload, broadcast)
	case ipxFamily(c.proto):
		return c.sendNBIPX(dst, payload, broadcast)
	default:
		return fmt.Errorf("netbios: carrier %q cannot send datagrams", c.proto)
	}
}

// sendNBF emits a mailslot payload as an NBF UI datagram to the NetBIOS
// functional-address multicast MAC. It ALWAYS uses the DATAGRAM (0x08) command, never
// DATAGRAM_BROADCAST (0x09): a real Windows/WfW/Win98 browser routes an inbound datagram
// by its destination NetBIOS name (WORKGROUP<1D>, WORKGROUP<1E>, <computer><00>) and
// dispatches ONLY 0x08 frames — every browser datagram in captures/win98nbf-win31nbf.pcapng
// (Host/Domain announcements, GetBackupList request AND response, RequestAnnouncement) is a
// 0x08 Datagram, none is a 0x09 broadcast. A 0x09 addressed to a group name the master is
// not registered for is silently dropped, which is why our GetBackupList drew no reply. The
// name in the frame does the routing; the multicast MAC just fans it to every node so the
// named recipient sees it. The broadcast flag now only affects addressing decisions in the
// callers (which destination NAME to use), not the wire command.
func (c *Conn) sendNBF(dst nb.Name, payload []byte, broadcast bool) error {
	_ = broadcast // browser datagrams are always CmdDatagram (0x08); the dst NAME routes them.
	frame := &nbf.Frame{Payload: payload}
	frame.DestinationName = [16]byte(dst)
	frame.SourceName = [16]byte(c.srcName)
	frame.Command = nbf.CmdDatagram
	body, err := frame.Encode()
	if err != nil {
		return err
	}
	return c.writeLLC(nbf.NetBIOSMulticastMAC, body)
}

// sendNBIPX emits a mailslot payload as an NMPI MailslotSend (opcode 0xFC) inside an IPX
// type-20 datagram on the datagram socket (0x0553), broadcast to the IPX broadcast node.
// The NameType marks a workgroup (group name) versus a machine, matching the server's
// nmpiNameType. Mirrors core/service/netbios/nbipx.go emitDatagram.
func (c *Conn) sendNBIPX(dst nb.Name, payload []byte, broadcast bool) error {
	body := nb.EncodeNMPIPacket(&nb.NMPIPacket{
		Opcode:        nb.NMPIOpMailslotSend,
		NameType:      nmpiNameType(dst, broadcast),
		RequestedName: dst,
		SourceName:    c.srcName,
		Payload:       payload,
	})
	d := &ipxproto.Datagram{
		Type:    ipxNetBIOSTyp,
		DstNode: broadcastMAC,
		DstSock: nbDatagramSocket,
		SrcNode: c.srcMAC,
		SrcSock: nbDatagramSocket,
		Payload: body,
	}
	ipxBytes, err := d.Encode(nil)
	if err != nil {
		return err
	}
	return c.writeEther(broadcastMAC, etherTypeIPX, ipxBytes)
}

// nmpiNameType is the NMPI name-type byte stamped on an outbound MailslotSend.
//
// It is the FAN-OUT of the datagram that picks the value, not the name's suffix: every
// golden browser datagram addressed to the whole workgroup carries NMPINameTypeWorkgroup
// (0x02) even though its RequestedName is <workgroup><00>, a suffix indistinguishable
// from a machine name — spec/captures/nwlink-win98.pcap frames 26-40 and
// nbipx-win98.pcap frames 16/48/58 all read "fc 02" ahead of "WORKGROUP      \x00". Only
// the master's UNICAST answer back to one station uses NMPINameTypeMachine (0x01)
// (nbipx-win98.pcap frame 60, nwlink-win98.pcap frame 41). A group suffix still forces
// the workgroup type, so the NBF-shaped names a caller may pass are typed correctly too.
func nmpiNameType(name nb.Name, broadcast bool) uint8 {
	if broadcast || name.Type() == nb.NameTypeGroup {
		return nb.NMPINameTypeWorkgroup
	}
	return nb.NMPINameTypeMachine
}

// writeLLC frames an NBF body (802.2 LLC UI, DSAP=SSAP=0xF0) to dstMAC as an 802.3
// length-typed Ethernet frame and writes it. The EtherType field carries the 802.2
// length (payload = LLC header + body), matching core/port/netbeui's egress.
func (c *Conn) writeLLC(dstMAC [6]byte, body []byte) error {
	llcLen := len(llcNetBIOS) + len(body)
	frame := make([]byte, 0, ethHdrLen+llcLen)
	frame = append(frame, dstMAC[:]...)
	frame = append(frame, c.srcMAC[:]...)
	frame = append(frame, byte(llcLen>>8), byte(llcLen&0xFF)) // 802.3 length in the type field
	frame = append(frame, llcNetBIOS[:]...)
	frame = append(frame, body...)
	return c.fl.Write(padEthernet(frame))
}

// writeEther frames payload in an Ethernet II frame (dstMAC, our srcMAC, etherType) and
// writes it.
func (c *Conn) writeEther(dstMAC [6]byte, etherType uint16, payload []byte) error {
	frame := make([]byte, 0, ethHdrLen+len(payload))
	frame = append(frame, dstMAC[:]...)
	frame = append(frame, c.srcMAC[:]...)
	frame = append(frame, byte(etherType>>8), byte(etherType&0xFF))
	frame = append(frame, payload...)
	return c.fl.Write(padEthernet(frame))
}

// ethMinFrame is the minimum Ethernet frame length (excluding FCS); shorter frames are
// zero-padded so the NIC/driver does not reject a runt. Matches client/smb's nbfEthMin.
const ethMinFrame = 60

// padEthernet zero-pads a frame to the Ethernet minimum length.
func padEthernet(frame []byte) []byte {
	if len(frame) < ethMinFrame {
		frame = append(frame, make([]byte, ethMinFrame-len(frame))...)
	}
	return frame
}

// macString formats a 6-byte MAC as aa:bb:cc:dd:ee:ff for trace lines.
func macString(m [6]byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 0, 17)
	for i, b := range m {
		if i > 0 {
			out = append(out, ':')
		}
		out = append(out, hex[b>>4], hex[b&0x0F])
	}
	return string(out)
}
