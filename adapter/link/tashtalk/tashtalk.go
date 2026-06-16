package tashtalk

import (
	"errors"
	"fmt"
	"io"
	"sync"

	serial "github.com/jacobsa/go-serial/serial"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// TashTalk host↔device wire constants (spec/08 §"Wire Protocol").
const (
	startMarker = 0x01 // outbound frame start marker (and inbound IDLE→IN_FRAME trigger)
	escapePfx   = 0x00 // inbound escape prefix; the next byte is the escape code
	escDataNull = 0xFF // escape code: a data byte 0x00
	escEndFrame = 0xFD // escape code: end of frame
	resetCmd    = 0x02 // port reset (host→device), used in the init sequence

	// fcsLen is the 2-byte CRC (FCS) trailer the device appends to inbound frames
	// and the host appends to outbound frames.
	fcsLen = 2

	// minLLAPFrame is the shortest valid inbound LLAP frame the state machine will
	// dispatch (3-byte LLAP header + at least 2 bytes), AFTER the FCS is stripped.
	minLLAPFrame = 3

	// Serial line parameters (spec/08 §"Serial Connection Parameters").
	baudRate           = 1000000
	dataBits           = 8
	stopBits           = 1
	interCharTimeoutMs = 250
)

// Config holds parameters for opening a TashTalk serial link.
type Config struct {
	// Port is the OS serial device path (e.g. "COM3" or "/dev/ttyUSB0").
	Port string
}

// DefaultConfig returns a Config for the given serial port path.
func DefaultConfig(port string) Config { return Config{Port: port} }

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
	inFrame bool     // IDLE (false) vs IN_FRAME (true): set by the 0x01 start marker
	escaped bool     // an escape prefix (0x00) was seen; next byte is the escape code

	// mu guards closed against a concurrent Close; writeMu serialises writers (the
	// runport may Write from any goroutine while the read loop owns Read).
	mu      sync.RWMutex
	closed  bool
	writeMu sync.Mutex
}

// Compile-time assertion: *frameLink satisfies core/link.FrameLink.
var _ link.FrameLink = (*frameLink)(nil)

// Open opens the TashTalk serial port, sends the reset/init sequence, and
// returns it as a core/link.FrameLink. The caller frames the result with the
// LLAP framer. Mirrors the legacy TashTalkPort.Start serial setup.
func Open(cfg Config) (link.FrameLink, error) {
	if cfg.Port == "" {
		return nil, errors.New("tashtalk: empty serial port name")
	}
	s, err := serial.Open(serial.OpenOptions{
		PortName:              normalizeSerialPortName(cfg.Port),
		BaudRate:              baudRate,
		DataBits:              dataBits,
		StopBits:              stopBits,
		ParityMode:            serial.PARITY_NONE,
		InterCharacterTimeout: interCharTimeoutMs,
		MinimumReadSize:       1,
	})
	if err != nil {
		return nil, fmt.Errorf("tashtalk: open %s: %w", cfg.Port, err)
	}
	fl := &frameLink{s: s, rdBuf: make([]byte, 0, 1024)}
	if _, err := s.Write(buildInitSequence()); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("tashtalk: init %s: %w", cfg.Port, err)
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

// feed runs the inbound escape state machine (spec/08 §"Inbound Frame Format")
// over the just-read bytes, appending any completed LLAP frames (FCS verified +
// stripped) to l.pending. The IDLE/IN_FRAME/ESCAPED state lives on the frameLink
// so a frame — or even a lone escape prefix — split across serial reads still
// reassembles. Malformed or short frames are silently discarded.
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
				l.inFrame = false
			default:
				l.rdBuf = l.rdBuf[:0] // protocol error: discard, back to IDLE
				l.inFrame = false
			}
		case !l.inFrame:
			// IDLE: only a start marker enters a frame; any other byte is ignored.
			if b == startMarker {
				l.inFrame = true
				l.rdBuf = l.rdBuf[:0]
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
	l.mu.RLock()
	if l.closed {
		l.mu.RUnlock()
		return link.ErrClosed
	}
	s := l.s
	l.mu.RUnlock()

	b1, b2 := fcsBytes(frame)
	packet := make([]byte, 0, 1+len(frame)+fcsLen)
	packet = append(packet, startMarker)
	packet = append(packet, frame...)
	packet = append(packet, b1, b2)

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

// buildInitSequence is the reset/init bytes sent after opening: 1024 nulls to
// flush partial device state, then a 0x02 reset (spec/08 §"Initialization
// Sequence").
func buildInitSequence() []byte {
	buf := make([]byte, 1024, 1024+1)
	return append(buf, resetCmd)
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
