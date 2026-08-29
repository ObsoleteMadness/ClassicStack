// Package framing is the FrameLink -> DatagramLink adapter (§2, M1): the
// link.Framer that turns raw L2 frames into pre-framed DDP datagrams and back,
// so the router sees a DatagramLink regardless of whether the bytes came from a
// kernel AF_APPLETALK socket or a libpcap FrameLink.
//
// SCOPE: the Ethernet/SNAP DDP encapsulation (EtherTalk wire framing) is real
// here — encode wraps a ddp.Datagram in IEEE 802.2 + SNAP + the AppleTalk PID and
// decodes the inverse. The STATEFUL link protocols are now implemented too, each
// in its own file beside the plain framers:
//
//   - aarp.go (EtherTalkAARP): AARP address resolution + node-claim over a pure
//     core/protocol/aarp.Engine — claims a node by probing, resolves peer node→MAC
//     via the AMT for unicast, gleans/answers AARP.
//   - localtalk.go (LocalTalk, EnableClaim): the LLAP ENQ/ACK node-claim over a
//     pure core/protocol/llap.Engine — the LocalTalk analogue of AARP node-claim.
//
// Both publish the claimed address via the shared LiveAddr + an OnClaimed callback
// (compose wires that to port.SetAddress), and both keep their probe/aging TIMING
// in the adapter while the pure engine stays deterministic. The plain EtherTalk /
// LocalTalk framers (no claim) remain as the stateless framing-only fallback.
//
//   - proxyaarp.go (ProxyRewriteFrame): the FRAME-level half of the proxy-AARP transform
//     the Wi-Fi/tunnel bridge (adapter/bridge) applies — it finds an AARP Reply inside an
//     Ethernet/SNAP frame and rewrites both its AARP sender-hardware and outer Ethernet
//     source MAC to the egress MAC (the pure decoded-packet rule lives in
//     core/protocol/aarp.ProxyReply).
//
// Ring: adapter.
package framing
