// Package aarp implements the AppleTalk Address Resolution Protocol (Inside AppleTalk,
// 2nd edition, chapter 2) as a PURE, transport-neutral protocol: the packet codec, the
// Address Mapping Table (AMT), and the node-claim + address-resolution decision logic.
// It is the AARP peer of core/protocol/ddp.
//
// Ring: CORE. It has no I/O, no goroutines, and no timers of its own — like the DDP
// codec it hand-rolls big-endian (no encoding/binary, which pulls reflect) and stays
// TinyGo-clean (archtest-enforced). The package exposes step/decision methods; the
// adapter that owns the wire (adapter/link/framing, the EtherTalk AARP framer) supplies
// the frame send/receive loop and drives the timers, passing an explicit `now int64`
// (UnixNano) into Age/Tick — the same split core/service/rtmp uses for routing-table
// aging.
//
// Wire layout cross-checked against the Wireshark AARP dissector (packet-aarp.c): an
// 8-byte fixed header (hardware type, protocol type, hardware-addr len, protocol-addr
// len, opcode) followed by the uniform variable block senderHW · senderProto · targetHW
// · targetProto for EVERY opcode (request=1, reply=2, probe=3) — a probe/request simply
// leaves targetHW zero. On EtherTalk the AARP packet rides the 802.2/SNAP header with
// PID 00:00:00:80:F3 (the adapter adds/strips that; this package handles the AARP bytes
// after it).
package aarp
