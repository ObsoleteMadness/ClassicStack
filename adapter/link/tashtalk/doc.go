// Package tashtalk is the TashTalk serial FrameLink adapter (§2, M10): a
// LocalTalk segment reached through TashTalk hardware over a USB serial link at
// 1 Mbit/s (spec/08). It is the second LocalTalk transport behind the LLAP
// framer (adapter/link/framing.LocalTalk), the serial counterpart to
// adapter/link/ltoudp.
//
// On the wire the host↔TashTalk protocol frames each LLAP frame between a 0x01
// start marker and a 0x00 0xFD end marker, with 0x00 escaped as 0x00 0xFF, and
// a 2-byte CRC (FCS) trailer. That host↔device framing is THIS adapter's
// concern: Read runs the inbound escape state machine + FCS check and hands up a
// clean LLAP frame; Write prepends the start marker and appends the FCS. The
// LLAP framer above therefore sees only clean LLAP frames, exactly as it does
// over LToUDP.
//
// Ring: adapter. Imports github.com/jacobsa/go-serial; presents only the
// core/link.FrameLink interface upward. It sits OUTSIDE the cs-tinygo gate (the
// serial library is not TinyGo-safe), like the pcap/ltoudp adapters.
package tashtalk
