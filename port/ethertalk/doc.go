// Package ethertalk implements EtherTalk (AppleTalk Phase 2 over
// Ethernet) as an ClassicStack port.
//
// Frames are sent and received via libpcap/Npcap on the host
// interface. The package also implements AARP (RFC 1742, Appendix A)
// for AppleTalk-to-Ethernet address resolution.
package ethertalk
