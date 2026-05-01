/*
Package port defines the Port interface — the link-layer abstraction the
router uses to send and receive DDP datagrams. Concrete implementations
live in subpackages (port/ethertalk, port/localtalk, port/rawlink, …).

A Port owns a single network attachment: it knows its AppleTalk network
range and node number, can unicast/broadcast/multicast DDP datagrams,
and delivers inbound datagrams up through the RouterHooks callback the
router supplies at Start.

The optional BridgeConfigurable interface lets EtherTalk-style ports
expose bridge-mode configuration without requiring every Port to grow
the same surface; main.go type-asserts and configures only when needed.
*/
package port
