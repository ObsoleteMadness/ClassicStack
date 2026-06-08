// Package link defines the two byte-slice link altitudes (FrameLink and
// DatagramLink), the optional link capabilities, the FrameLink->DatagramLink
// framing contract, and the frame-altitude decorator signatures (§2).
//
// Ring: CORE (stdlib + core/protocol/ddp only). No pcap/gopacket/capture
// backends here — those are adapters. Real types land in step B2.
package link
