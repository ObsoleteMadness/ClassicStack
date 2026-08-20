package ipx

// types.go holds the IPX-level wire constants every IPX-carried protocol shares:
// the packet-type byte (IPX header offset 5) and the broadcast node address. They
// live HERE, in the protocol ring, because both sides of each protocol need them —
// the server transports (core/service/smb DirectIPX, core/service/ncp OverIPX,
// core/service/netbios NBIPX, core/service/sap, core/service/rip) and the client
// transports (client/smb, client/ncp) were each carrying a private copy of the same
// literals.

// IPX packet types (the Type byte of the IPX header). NetWare assigns a small set
// of well-known values; a value of 0 ("unknown") is also accepted by most stacks
// and is what several DOS shells emit.
const (
	// TypeUnknown (0) is the "no type" value older shells send. Receivers that
	// key off the type generally accept it alongside the specific type.
	TypeUnknown uint8 = 0x00
	// TypeRIP (1) is the Routing Information Protocol packet type (socket 0x0453).
	TypeRIP uint8 = 0x01
	// TypeEcho (2) is the IPX echo/diagnostic packet type.
	TypeEcho uint8 = 0x02
	// TypeError (3) is the IPX error packet type.
	TypeError uint8 = 0x03
	// TypePEP (4) is the Packet Exchange Protocol type. SAP, the NB-IPX session
	// protocol, direct-hosted SMB and the IPX diagnostic responder all ride it.
	TypePEP uint8 = 0x04
	// TypeSPX (5) is the Sequenced Packet Exchange type.
	TypeSPX uint8 = 0x05
	// TypeNCP (17 = 0x11) is the NetWare Core Protocol type (socket 0x0451).
	TypeNCP uint8 = 0x11
	// TypeNetBIOS (20 = 0x14) is the NetBIOS broadcast/WAN-forwarding type NBIPX
	// name service uses (propagated by routers up to 8 hops).
	TypeNetBIOS uint8 = 0x14
)

// BroadcastNode is the IPX node-ID broadcast address (all-ones). On Ethernet the
// IPX node IS the MAC address, so a datagram addressed to it is encapsulated to
// the broadcast MAC.
var BroadcastNode = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
