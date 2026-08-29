// Package smbtcp is a TCP session transport for the SMB service: it accepts TCP
// connections, frames each as a stream of length-prefixed SMB messages, and drives the
// transport-agnostic smb.SessionConsumer seam (NewConn / ServeMessage / Close) — the
// same seam the NetBEUI/IPX transports use. It is the direct-hosted-SMB-over-TCP path
// (port 445) and the substrate for NBT (port 139); both put a 4-byte big-endian length
// header in front of every SMB message (the NetBIOS Session Service header, whose
// message-type byte is 0 for a session message), so the framing is identical and this
// transport serves either port. The RFC 1001 session-REQUEST/RESPONSE handshake that
// :139 adds before the first SMB message is accepted-and-ignored (an all-zero positive
// response), which real clients tolerate; :445 has no handshake at all.
//
// Ring: ADAPTER. It uses net (forbidden in core), so the listener lives here, not in
// core/service/smb — mirroring how pcap/serial device I/O lives in adapters. It reaches
// SMB only through the small smb.SessionConsumer/SessionCircuit interfaces, so it never
// imports the SMB command internals.
package smbtcp
