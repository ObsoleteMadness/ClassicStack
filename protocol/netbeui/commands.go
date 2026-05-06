package netbeui

// NBF command codes from IBM SC30-3587, Chapter 5, Table 5-1/5-2.
//
// Commands 0x00–0x13 are carried as DLC UI frames (connectionless,
// broadcast or directed). Commands 0x14–0x1F are session-layer
// commands normally carried as DLC I-format LPDUs (connection-oriented);
// in this Ethernet-only implementation they ride UI frames with NBF-level
// acknowledgment (DATA_ACK).

// --- Name Management (UI frames) ---

const (
	// CmdAddGroupNameQuery (0x00) verifies that a group name to be
	// added does not already exist as a unique name on the network.
	// Broadcast to the NetBIOS functional address.
	CmdAddGroupNameQuery uint8 = 0x00

	// CmdAddNameQuery (0x01) verifies that a unique name to be added
	// is not already in use on the network. Broadcast to the NetBIOS
	// functional address.
	CmdAddNameQuery uint8 = 0x01

	// CmdNameInConflict (0x02) indicates that a duplicate name has
	// been detected — the same name is registered at more than one
	// adapter. Broadcast to the NetBIOS functional address.
	CmdNameInConflict uint8 = 0x02

	// CmdStatusQuery (0x03) requests adapter status from a remote
	// node. Broadcast (or directed after RND lookup).
	CmdStatusQuery uint8 = 0x03
)

// --- Trace / Misc (UI frames) ---

const (
	// CmdTerminateTraceRemote (0x07) terminates traces at remote nodes.
	CmdTerminateTraceRemote uint8 = 0x07
)

// --- Datagram (UI frames) ---

const (
	// CmdDatagram (0x08) carries an application datagram directed to
	// a specific name. Broadcast to the NetBIOS functional address (or
	// directed when the destination MAC is known).
	CmdDatagram uint8 = 0x08

	// CmdDatagramBroadcast (0x09) carries an application broadcast
	// datagram. Broadcast to the NetBIOS functional address.
	CmdDatagramBroadcast uint8 = 0x09
)

// --- Session Establishment / Name Resolution (UI frames) ---

const (
	// CmdNameQuery (0x0A) locates a name on the network, used both
	// for FIND.NAME and for CALL session establishment. Broadcast to
	// the NetBIOS functional address.
	CmdNameQuery uint8 = 0x0A

	// CmdAddNameResponse (0x0D) is a negative response indicating
	// that a name in an ADD_NAME_QUERY or ADD_GROUP_NAME_QUERY is
	// already in use. Directed UI to the query originator.
	CmdAddNameResponse uint8 = 0x0D

	// CmdNameRecognized (0x0E) responds to a NAME_QUERY, indicating
	// whether a session can be established. Directed UI with general
	// broadcast.
	CmdNameRecognized uint8 = 0x0E

	// CmdStatusResponse (0x0F) returns adapter status data in
	// response to a STATUS_QUERY. Directed UI, no broadcast.
	CmdStatusResponse uint8 = 0x0F

	// CmdTerminateTraceLocal (0x13) terminates traces at both local
	// and remote nodes. Broadcast to the NetBIOS functional address.
	CmdTerminateTraceLocal uint8 = 0x13
)

// --- Session Data Transfer (I-format LPDU / UI in this implementation) ---

const (
	// CmdDataAck (0x14) positively acknowledges a DATA_ONLY_LAST frame.
	CmdDataAck uint8 = 0x14

	// CmdDataFirstMiddle (0x15) carries a session data segment that
	// is not the last segment of a message (segmentation).
	CmdDataFirstMiddle uint8 = 0x15

	// CmdDataOnlyLast (0x16) carries a session data segment that is
	// the only or last segment of a message.
	CmdDataOnlyLast uint8 = 0x16

	// CmdSessionConfirm (0x17) acknowledges a SESSION_INITIALIZE,
	// completing session establishment.
	CmdSessionConfirm uint8 = 0x17

	// CmdSessionEnd (0x18) terminates a session.
	CmdSessionEnd uint8 = 0x18

	// CmdSessionInitialize (0x19) starts session setup after a
	// NAME_RECOGNIZED indicated willingness to establish a session.
	CmdSessionInitialize uint8 = 0x19

	// CmdNoReceive (0x1A) indicates the receiver has no RECEIVE
	// command pending to accept data.
	CmdNoReceive uint8 = 0x1A

	// CmdReceiveOutstanding (0x1B) requests retransmission of the
	// last data frame; a RECEIVE is now available.
	CmdReceiveOutstanding uint8 = 0x1B

	// CmdReceiveContinue (0x1C) indicates a RECEIVE is pending and
	// more data can be sent.
	CmdReceiveContinue uint8 = 0x1C

	// CmdSessionAlive (0x1F) is a keepalive probe verifying that a
	// session is still active.
	CmdSessionAlive uint8 = 0x1F
)

// IsSessionCommand returns true if cmd is a session-layer command
// (0x14–0x1F) that uses the 14-byte session header with destination
// and source session numbers instead of 16-byte names.
func IsSessionCommand(cmd uint8) bool {
	return cmd >= 0x14 && cmd <= 0x1F
}

// NonSessionHeaderLength is the total length of a non-session NBF
// frame header (commands 0x00–0x13): 12-byte common prefix +
// 16-byte dest name + 16-byte source name = 44 bytes.
const NonSessionHeaderLength = 44

// SessionHeaderLength is the total length of a session NBF frame
// header (commands 0x14–0x1F): 12-byte common prefix +
// 1-byte dest number + 1-byte source number = 14 bytes.
const SessionHeaderLength = 14

// NetBIOSMulticastMAC is the well-known Ethernet multicast address
// used for NetBIOS functional-address broadcasts on Ethernet
// (03:00:00:00:00:01). All NBF UI broadcasts target this address.
var NetBIOSMulticastMAC = [6]byte{0x03, 0x00, 0x00, 0x00, 0x00, 0x01}
