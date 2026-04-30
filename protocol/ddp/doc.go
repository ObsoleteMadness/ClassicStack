/*
Package ddp defines the Datagram Delivery Protocol (DDP) wire format:
the long-header datagram struct, its marshal/unmarshal helpers, the
checksum algorithm, and the protocol's data-length cap.

DDP is the AppleTalk network-layer datagram protocol — every higher-level
AppleTalk protocol (ATP, ASP, AEP, RTMP, ZIP, NBP, AFP-over-ASP) is
encapsulated in DDP datagrams and routed by destination network/node.

This package is wire-format only — no I/O, no goroutines, no state.
Routing, port abstraction, and packet dispatch live elsewhere
(router/, port/, service/*).

References:
  - Inside AppleTalk, 2nd Edition, Chapter 4
  - Inside Macintosh: Networking, Chapter 1
*/
package ddp
