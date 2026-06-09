// Package framing is the FrameLink -> DatagramLink adapter (§2, M1): the
// link.Framer that turns raw L2 frames into pre-framed DDP datagrams and back,
// so the router sees a DatagramLink regardless of whether the bytes came from a
// kernel AF_APPLETALK socket or a libpcap FrameLink.
//
// SCOPE (M1): the Ethernet/SNAP DDP encapsulation (EtherTalk wire framing) is
// real here — encode wraps a ddp.Datagram in IEEE 802.2 + SNAP + the AppleTalk
// PID and decodes the inverse. What is DELIBERATELY DEFERRED to M3 is the
// stateful part of EtherTalk: AARP address resolution and node-claim, the
// src/dst hardware-address learning, and node acquisition. Until then this
// framer does the framing only; it does not resolve or claim addresses. Those
// hooks are marked TODO(M3) below.
//
// Ring: adapter.
package framing
