package tashtalk

import (
	"errors"
	"io"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// TashTalk host↔device wire constants (spec/08 §"Wire Protocol").
const (
	// startMarker prefixes HOST→DEVICE frames only. The device does NOT send it, so
	// the inbound state machine must never wait for it (see feed).
	startMarker = 0x01
	escapePfx   = 0x00 // inbound escape prefix; the next byte is the escape code
	escDataNull = 0xFF // escape code: a data byte 0x00
	escEndFrame = 0xFD // escape code: end of frame
	// setNodeAddrCmd (host→device) introduces a 33-byte command: the opcode plus a
	// 32-byte (256-bit) bitmap of the node addresses the hardware should RECEIVE.
	// It is not a standalone reset byte — sending it without the 32-byte payload
	// leaves the device eating the next 32 wire bytes as bitmap data.
	setNodeAddrCmd = 0x02
	// nodeAddrCmdLen is the full command length: 1 opcode + 32 bitmap bytes.
	nodeAddrCmdLen = 33
	// maxNodeAddr is the highest assignable LLAP node address (255 is broadcast).
	maxNodeAddr = 254

	// fcsLen is the 2-byte CRC (FCS) trailer the device appends to inbound frames
	// and the host appends to outbound frames.
	fcsLen = 2

	// minLLAPFrame is the shortest valid inbound LLAP frame the state machine will
	// dispatch (3-byte LLAP header + at least 2 bytes), AFTER the FCS is stripped.
	minLLAPFrame = 3
)

// frameLink implements core/link.FrameLink over a TashTalk serial connection. It
// runs the inbound escape state machine + FCS check on Read and the outbound
// start-marker + FCS framing on Write, so the framer above sees clean LLAP
// frames.
type frameLink struct {
	s io.ReadWriteCloser

	// inbound buffers the byte→frame state machine across Read calls (a single
	// serial read can split or coalesce frames). Owned by the Read goroutine.
	rdBuf   []byte
	pending [][]byte // fully-decoded LLAP frames awaiting return from Read
	escaped bool     // an escape prefix (0x00) was seen; next byte is the escape code

	// mu guards closed against a concurrent Close; writeMu serialises writers (the
	// runport may Write from any goroutine while the read loop owns Read).
	mu      sync.RWMutex
	closed  bool
	writeMu sync.Mutex
}

// Compile-time assertion: *frameLink satisfies core/link.FrameLink.
var _ link.FrameLink = (*frameLink)(nil)

// NewStream wraps an already-open serial byte stream in the TashTalk FrameLink: it
// sends the reset/init sequence on the stream, then frames inbound/outbound LLAP per
// spec/08. The device-open (port name, baud, 8N1) lives in adapter/serial (§3b/D7);
// this adapter is a FRAMER over the byte stream and owns no serial-library
// dependency. The caller frames the result further with the LLAP framer above.
// A nil stream is rejected. On an init-write error the stream is closed and the
// error returned.
func NewStream(s io.ReadWriteCloser) (link.FrameLink, error) {
	if s == nil {
		return nil, errors.New("tashtalk: nil serial stream")
	}
	fl := &frameLink{s: s, rdBuf: make([]byte, 0, 1024)}
	if _, err := s.Write(buildInitSequence()); err != nil {
		_ = s.Close()
		return nil, errors.New("tashtalk: init write failed: " + err.Error())
	}
	return fl, nil
}

// Read returns the next inbound LLAP frame (FCS stripped, start/escape framing
// removed). It reads serial bytes until a complete frame decodes, mapping post-
// Close use to link.ErrClosed and a serial timeout/empty read to a retry. The
// state machine is fed across calls, so a frame split across serial reads still
// reassembles.
func (l *frameLink) Read() (link.Frame, error) {
	for {
		// Drain any frames already decoded from a previous serial read.
		if len(l.pending) > 0 {
			f := l.pending[0]
			l.pending = l.pending[1:]
			return f, nil
		}

		l.mu.RLock()
		if l.closed {
			l.mu.RUnlock()
			return nil, link.ErrClosed
		}
		s := l.s
		l.mu.RUnlock()

		buf := make([]byte, 1024)
		n, err := s.Read(buf)
		if err != nil {
			l.mu.RLock()
			closed := l.closed
			l.mu.RUnlock()
			if closed || errors.Is(err, io.EOF) {
				return nil, link.ErrClosed
			}
			// A transient read error / timeout: surface as a timeout so the runport
			// loop keeps polling Stop rather than tearing the port down.
			return nil, link.ErrTimeout
		}
		if n == 0 {
			return nil, link.ErrTimeout
		}
		l.feed(buf[:n])
	}
}

// feed runs the inbound escape state machine over the just-read bytes, appending any
// completed LLAP frames (FCS verified + stripped) to l.pending. The ESCAPED state
// lives on the frameLink so a frame — or even a lone escape prefix — split across
// serial reads still reassembles. Malformed or short frames are silently discarded.
//
// Frames are delimited by the 0x00 0xFD END-of-frame escape, NOT by a start marker:
// the 0x01 start marker is HOST→DEVICE ONLY. The device does not prefix its frames
// with it, so bytes are accumulated unconditionally from the first byte received.
//
// REGRESSION (2026-08): a refactor added an IDLE state that waited for a 0x01 before
// accumulating. Against real hardware the port then transmitted normally and received
// NOTHING — the state machine sat in IDLE forever discarding every inbound byte,
// because the device never sends 0x01. Pre-refactor code (and spec/08's note that the
// start marker is not validated inbound) accumulates unconditionally. Do not
// "tighten" this by enforcing a start marker.
func (l *frameLink) feed(data []byte) {
	for _, b := range data {
		switch {
		case l.escaped:
			l.escaped = false
			switch b {
			case escDataNull:
				l.rdBuf = append(l.rdBuf, 0x00) // escaped data null
			case escEndFrame:
				l.completeFrame()
			default:
				l.rdBuf = l.rdBuf[:0] // protocol error: discard accumulated frame
			}
		case b == escapePfx:
			l.escaped = true // next byte is the escape code (may be in the next read)
		default:
			l.rdBuf = append(l.rdBuf, b)
		}
	}
}

// completeFrame validates the accumulated frame's FCS and, if valid and long
// enough, queues the LLAP payload (FCS stripped). It always resets rdBuf.
func (l *frameLink) completeFrame() {
	frame := l.rdBuf
	l.rdBuf = make([]byte, 0, 1024)
	if len(frame) < minLLAPFrame+fcsLen {
		return // too short to hold an LLAP header + FCS
	}
	body := frame[:len(frame)-fcsLen]
	if !fcsMatches(body, frame[len(frame)-fcsLen], frame[len(frame)-1]) {
		return // FCS mismatch: corrupt frame, discard
	}
	out := make([]byte, len(body))
	copy(out, body)
	l.pending = append(l.pending, out)
}

// Write frames a clean LLAP frame for the device: 0x01 start marker + frame +
// 2-byte FCS. It does not retain frame past the call. Per spec/08 the outbound
// direction is NOT escape-encoded; the firmware accepts raw bytes after 0x01.
func (l *frameLink) Write(frame link.Frame) error {
	b1, b2 := fcsBytes(frame)
	packet := make([]byte, 0, 1+len(frame)+fcsLen)
	packet = append(packet, startMarker)
	packet = append(packet, frame...)
	packet = append(packet, b1, b2)
	return l.writeRaw(packet)
}

// writeRaw writes already-framed bytes to the serial stream under the write lock,
// mapping post-Close use to link.ErrClosed. Shared by Write (LLAP frames) and
// SetNodeAddress (device commands), which must not interleave mid-write.
func (l *frameLink) writeRaw(packet []byte) error {
	l.mu.RLock()
	if l.closed {
		l.mu.RUnlock()
		return link.ErrClosed
	}
	s := l.s
	l.mu.RUnlock()

	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	_, err := s.Write(packet)
	return err
}

// Close shuts the serial port; a blocked Read unblocks with an error → ErrClosed.
// Idempotent.
func (l *frameLink) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	return l.s.Close()
}

// buildInitSequence is the reset/init bytes sent after opening: 1024 nulls to flush
// partial device state, then a COMPLETE set-node-address command with an empty
// bitmap (0x02 + 32 zero bytes), then 0x03 0x00.
//
// 0x02 is NOT a bare reset byte: it is a 33-byte command whose 32-byte payload is a
// 256-bit bitmap of the node addresses the hardware should receive. Sending 0x02
// alone (as this did before) leaves the device consuming the NEXT 32 bytes on the
// wire as bitmap data, desynchronising the command stream. Observed against real
// hardware: the port transmits fine but receives NOTHING, because the receive
// bitmap is never validly set. See SetNodeAddress, which arms the real filter once
// LLAP claims a node.
func buildInitSequence() []byte {
	buf := make([]byte, 0, 1024+nodeAddrCmdLen+2)
	buf = append(buf, make([]byte, 1024)...)           // flush partial device state
	buf = append(buf, make([]byte, nodeAddrCmdLen)...) // 0x02 + 32-byte empty bitmap
	buf[1024] = setNodeAddrCmd
	return append(buf, 0x03, 0x00)
}

// SetNodeAddress arms the TashTalk hardware receive filter for node, so the device
// forwards frames addressed to us (plus broadcasts) up the serial link. It is called
// when the LLAP node-claim succeeds.
//
// THIS IS REQUIRED FOR ANY INBOUND TRAFFIC AT ALL. TashTalk filters in hardware
// against a 256-bit node bitmap that starts empty, so until this lands every inbound
// frame is dropped by the device and the host sees a silent line while its own
// transmits go out normally.
//
// node 0 clears the filter (an empty bitmap). Otherwise bit `node` is set: byte
// node/8 of the bitmap, bit node%8. A node outside 1..254 is rejected.
func (l *frameLink) SetNodeAddress(node uint8) error {
	cmd, err := buildSetNodeAddressCmd(node)
	if err != nil {
		return err
	}
	return l.writeRaw(cmd)
}

// buildSetNodeAddressCmd builds the 33-byte set-node-address command (0x02 + a
// 32-byte node bitmap) for node. Mirrors main's buildSetNodeAddressCmd.
func buildSetNodeAddressCmd(node uint8) ([]byte, error) {
	cmd := make([]byte, nodeAddrCmdLen)
	cmd[0] = setNodeAddrCmd
	if node == 0 {
		return cmd, nil // empty bitmap: receive nothing
	}
	if node > maxNodeAddr {
		return nil, errors.New("tashtalk: node address not between 1 and 254")
	}
	cmd[1+node/8] = 1 << (node % 8)
	return cmd, nil
}

// fcsMatches reports whether the trailing FCS bytes match the frame body's CRC.
func fcsMatches(frame []byte, b1, b2 byte) bool {
	e1, e2 := fcsBytes(frame)
	return b1 == e1 && b2 == e2
}

// fcsBytes computes the TashTalk FCS (CRC-16/X-25: poly 0x8408 reflected, init
// 0xFFFF, final XOR 0xFFFF) over frame, returning the low and high bytes.
// Mirrors the legacy port's fcsBytes.
func fcsBytes(frame []byte) (byte, byte) {
	crc := uint16(0xFFFF)
	for _, b := range frame {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	crc = ^crc
	return byte(crc & 0xFF), byte(crc >> 8)
}
