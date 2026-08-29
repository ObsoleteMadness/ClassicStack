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
// This adapter is a FRAMER over a serial byte stream, NOT a device owner (§3b/D7,
// M11.c): NewStream wraps an already-open io.ReadWriteCloser (sending the reset/init
// sequence) and frames over it. The serial device-open — port name, baud, 8N1 —
// lives in adapter/serial, the one shared serial opener the compose layer dispatches
// to for a `kind = "serial"` interface. So this package imports no serial library
// and is stdlib-only at the byte-stream seam.
//
// Ring: adapter. Presents only the core/link.FrameLink interface upward.
package tashtalk
