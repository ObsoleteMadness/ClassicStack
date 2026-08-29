package tashtalk

import (
	"errors"
	"io"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// TashTalk host↔device wire constants (spec/08 §"Wire Protocol").
const (
	// startMarker prefixes HOST→DEVICE frames only. The device does NOT send it, so
	// the inbound state machine must never wait for it (see feed).
	startMarker = 0x01
	escapePfx   = 0x00 // inbound escape prefix; the next byte is the escape code
	escDataNull = 0xFF // escape code: a data byte 0x00
	escEndFrame = 0xFD // escape code: end of frame
	// escFramingErr / escFrameAbort are ERROR terminators the firmware sends in
	// place of escEndFrame. Both mean the frame that was accumulating is rubbish
	// and must be dropped — which the default branch already does — but they are
	// distinguished here so the reason is LOGGED rather than silently swallowed.
	//
	// escFramingErr (0x00 0xFE): six consecutive '1' bits that are not a flag byte,
	// i.e. line-level corruption on the LocalTalk side.
	// escFrameAbort (0x00 0xFA): a sender began a frame and stopped without a
	// closing flag — the signature of a transmitter that gave up mid-frame.
	escFramingErr = 0xFE
	escFrameAbort = 0xFA
	// escCRCFail (0x00 0xFC) needs firmware >= 2.1.0 AND the CRC-checking feature
	// bit; we do not enable it, so it should never arrive. Named so that if it ever
	// does, it is reported instead of resetting the frame as a generic protocol
	// error.
	escCRCFail = 0xFC
	// setNodeAddrCmd (host→device) introduces a 33-byte command: the opcode plus a
	// 32-byte (256-bit) bitmap of the node addresses the hardware should RECEIVE.
	// It is not a standalone reset byte — sending it without the 32-byte payload
	// leaves the device eating the next 32 wire bytes as bitmap data.
	setNodeAddrCmd = 0x02
	// nodeAddrCmdLen is the full command length: 1 opcode + 32 bitmap bytes.
	nodeAddrCmdLen = 33
	// maxNodeAddr is the highest assignable LLAP node address (255 is broadcast).
	maxNodeAddr = 254

	// closeWriteWait caps how long Close waits for an in-flight serial Write before
	// forcing the port shut. Without this, s.Close can block forever when RTS/CTS
	// flow control stalls a write and the shutdown path never reaches the runtime's
	// stop deadline.
	closeWriteWait = 500 * time.Millisecond

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

	// logger narrates the serial write path; nil → silent (NewStream leaves it nil,
	// NewStreamLogged sets it). Guarded once in logf, not at each call site.
	logger log.Logger

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
	return NewStreamLogged(s, nil)
}

// NewStreamLogged is NewStream with a logger installed, so the serial write path is
// traceable. A nil logger is silent, making this the single implementation.
//
// The logging exists to answer one question the .pcap cannot: the capture sink sits
// ABOVE this framer, so a captured frame proves only that the HOST produced it, not
// that it survived the serial handoff to the device. Comparing "tashtalk: tx frame"
// records against the capture separates "we never wrote it" from "we wrote it and it
// vanished" — and a SHORT serial write (the documented overrun mode: truncated frame
// → failed FCS → silently gone) is now an explicit error rather than a discarded
// byte count.
func NewStreamLogged(s io.ReadWriteCloser, logger log.Logger) (link.FrameLink, error) {
	if s == nil {
		return nil, errors.New("tashtalk: nil serial stream")
	}
	fl := &frameLink{s: s, rdBuf: make([]byte, 0, 1024), logger: logger}
	init := buildInitSequence()
	n, err := s.Write(init)
	if err != nil {
		_ = s.Close()
		return nil, errors.New("tashtalk: init write failed: " + err.Error())
	}
	// A short init write desynchronises the device's command stream: the 0x02
	// set-node-address command is 33 bytes, so a truncated init leaves the firmware
	// consuming subsequent LLAP bytes as bitmap data. Fail loudly rather than
	// running on against a device in an unknown state.
	if n != len(init) {
		_ = s.Close()
		return nil, errors.New("tashtalk: short init write — device state indeterminate")
	}
	fl.logf(log.Debug, "tashtalk: device initialised", log.Int("init_bytes", int64(n)))
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
			case escFramingErr:
				// Line-level corruption on the LocalTalk side. Previously indistinguishable
				// from any other protocol error; now named, because a run of these is direct
				// evidence of a bad line/adaptor rather than a logic bug upstream.
				l.logf(log.Debug, "tashtalk: framing error from device — frame discarded",
					log.Int("bytes", int64(len(l.rdBuf))))
				l.rdBuf = l.rdBuf[:0]
			case escFrameAbort:
				// A transmitter began a frame and stopped without a closing flag. On a
				// segment where our own writes are suspect, this is the signal that a SEND
				// died mid-frame rather than never starting.
				l.logf(log.Debug, "tashtalk: frame aborted by sender — frame discarded",
					log.Int("bytes", int64(len(l.rdBuf))))
				l.rdBuf = l.rdBuf[:0]
			case escCRCFail:
				// Only reachable if the CRC-checking feature bit is set, which we never
				// set — so this arriving means the device is configured differently than
				// we believe (stale firmware state from a previous run, say).
				l.logf(log.Warn, "tashtalk: device reported CRC failure — CRC checking was never enabled",
					log.Int("bytes", int64(len(l.rdBuf))))
				l.rdBuf = l.rdBuf[:0]
			default:
				l.logf(log.Debug, "tashtalk: unknown escape code — frame discarded",
					log.Int("code", int64(b)), log.Int("bytes", int64(len(l.rdBuf))))
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
		// Not necessarily noise: a TRUNCATED frame arrives here too, which is the
		// documented signature of a device-side buffer overrun.
		if len(frame) > 0 {
			l.logf(log.Debug, "tashtalk: inbound frame too short — discarded",
				log.Int("bytes", int64(len(frame))))
		}
		return
	}
	body := frame[:len(frame)-fcsLen]
	if !fcsMatches(body, frame[len(frame)-fcsLen], frame[len(frame)-1]) {
		// An FCS mismatch is how a frame corrupted or truncated in transit
		// DISAPPEARS. Never silent: this is the failure spec/08 warns about when
		// serial flow control is off.
		dst, src, typ := llapHeaderOf(body)
		l.logf(log.Debug, "tashtalk: inbound FCS mismatch — frame discarded",
			log.Int("dst", int64(dst)), log.Int("src", int64(src)),
			log.Int("llap_type", int64(typ)), log.Int("bytes", int64(len(frame))))
		return
	}
	out := make([]byte, len(body))
	copy(out, body)
	l.pending = append(l.pending, out)

	// batch reports how many frames this one serial read has now yielded. A single
	// read routinely carries several frames, and every frame in a batch lands in the
	// .pcap with an IDENTICAL timestamp — so a run of same-microsecond frames in a
	// capture is a host read-batching artifact, NOT that many separate wire events.
	// (Observed: nine byte-identical CTS frames at one timestamp read as a storm.)
	dst, src, typ := llapHeaderOf(out)
	l.logf(log.Trace, "tashtalk: rx frame",
		log.Int("dst", int64(dst)), log.Int("src", int64(src)),
		log.Int("llap_type", int64(typ)),
		log.Int("llap_len", int64(len(out))),
		log.Int("batch", int64(len(l.pending))))
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

	// Trace the LLAP header BEFORE the write so a frame the wire never carries
	// still leaves a host-side record. The capture sink sits ABOVE this framer,
	// so a .pcap shows what the host intended to send, not what survived the
	// serial handoff — comparing this trace against the pcap is what separates
	// "we never wrote it" from "we wrote it and the device dropped it".
	dst, src, typ := llapHeaderOf(frame)
	l.logf(log.Trace, "tashtalk: tx frame",
		log.Int("dst", int64(dst)), log.Int("src", int64(src)),
		log.Int("llap_type", int64(typ)),
		log.Int("llap_len", int64(len(frame))),
		log.Int("wire_len", int64(len(packet))))

	return l.writeRaw(packet, "frame")
}

// writeRaw writes already-framed bytes to the serial stream under the write lock,
// mapping post-Close use to link.ErrClosed. Shared by Write (LLAP frames) and
// SetNodeAddress (device commands), which must not interleave mid-write. kind
// names the caller for the log record.
func (l *frameLink) writeRaw(packet []byte, kind string) error {
	l.mu.RLock()
	if l.closed {
		l.mu.RUnlock()
		return link.ErrClosed
	}
	s := l.s
	l.mu.RUnlock()

	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	n, err := s.Write(packet)

	// A SHORT WRITE is the failure this logging exists to catch. spec/08 §"Hardware
	// flow control": the host feeds the device at 1 Mbit/s while it clocks LocalTalk
	// at 230.4 kbaud, so without serial RTS/CTS the device's buffer overruns
	// mid-frame — and a truncated LLAP frame simply fails FCS and DISAPPEARS, with
	// no error anywhere. The byte count was previously discarded, making that
	// silent. io.Writer permits n < len(p) only with a non-nil error, but a serial
	// driver that under-delivers without erroring is exactly the bug we are hunting.
	switch {
	case err != nil:
		l.logf(log.Error, "tashtalk: serial write failed",
			log.Str("kind", kind), log.Int("want", int64(len(packet))),
			log.Int("wrote", int64(n)), log.Str("err", err.Error()))
	case n != len(packet):
		l.logf(log.Error, "tashtalk: SHORT serial write — frame truncated on the wire",
			log.Str("kind", kind), log.Int("want", int64(len(packet))),
			log.Int("wrote", int64(n)))
	default:
		l.logf(log.Trace, "tashtalk: serial write",
			log.Str("kind", kind), log.Int("bytes", int64(n)))
	}
	return err
}

// logf emits one record when a logger is installed and the level is wanted. The
// single nil/Enabled guard lives here so the write path stays uncluttered.
func (l *frameLink) logf(lvl log.Level, msg string, fields ...log.Field) {
	if l.logger == nil || !l.logger.Enabled(lvl) {
		return
	}
	l.logger.Log(lvl, msg, fields...)
}

// llapHeaderOf returns the 3-byte LLAP header fields, or zeroes for a frame too
// short to have one (logged rather than dropped silently — a sub-header frame
// reaching the device is itself a bug worth seeing).
func llapHeaderOf(frame []byte) (dst, src, typ uint8) {
	if len(frame) < 3 {
		return 0, 0, 0
	}
	return frame[0], frame[1], frame[2]
}

// Close shuts the serial port; a blocked Read unblocks with an error → ErrClosed.
// Idempotent.
func (l *frameLink) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	s := l.s
	l.mu.Unlock()

	// Nudge any blocked Read/Write out of the driver before closing the port.
	l.abortBlockedIO()

	// Do not call s.Close while writeMu is held by a blocked serial Write — some
	// drivers hang until the write completes. Wait briefly, then force the close.
	writeDone := make(chan struct{})
	go func() {
		// Intentional empty critical section: this is a "wait for writeMu to be
		// free" barrier, not a bug — SA2001 doesn't know the point is the
		// Lock/Unlock pair itself, not any work done while held.
		l.writeMu.Lock()   //nolint:staticcheck
		l.writeMu.Unlock() //nolint:staticcheck
		close(writeDone)
	}()
	select {
	case <-writeDone:
	case <-time.After(closeWriteWait):
		l.logf(log.Warn, "tashtalk: serial write did not finish before close — forcing port shut")
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.Close()
}

// abortBlockedIO asks the serial driver to return promptly from a blocked Read or
// Write. jacobsa/go-serial uses InterCharacterTimeout for reads; some OS drivers
// still need an explicit deadline on shutdown.
func (l *frameLink) abortBlockedIO() {
	now := time.Now()
	type deadliner interface {
		SetReadDeadline(time.Time) error
		SetWriteDeadline(time.Time) error
	}
	if d, ok := l.s.(deadliner); ok {
		_ = d.SetReadDeadline(now)
		_ = d.SetWriteDeadline(now)
	}
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
	// The bitmap gates more than reception: per the TashTalk protocol doc it also
	// decides "on which node IDs' behalf the firmware will respond to ENQ and RTS
	// frames". An unarmed (or wrongly armed) filter means the device never answers
	// a directed RTS with a CTS, so peers cannot send to us at all.
	l.logf(log.Debug, "tashtalk: arming hardware node filter", log.Int("node", int64(node)))
	return l.writeRaw(cmd, "set-node-address")
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
