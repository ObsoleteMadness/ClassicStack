package etherdfs

import (
	"fmt"
	"strings"
	"sync"

	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/etherdfs"
)

// session.go holds the EtherDFS client session: the bound drive number, the transport,
// and the request/reply serialisation. EtherDFS has no login or connection handshake —
// Open resolves the drive letter to its number and probes the server (an AL_DISKSPACE
// query for that drive) so the transport learns the server MAC before any file
// operation. Every request the adapter issues carries the bound drive number.

// Session is an EtherDFS circuit bound to one drive. It owns the Transport and
// serialises requests so the sequence correlation (one in flight) holds.
type Session struct {
	tr    Transport
	drive uint8 // DOS drive number (0=A … 25=Z)

	mu sync.Mutex
}

// DialParams carries what Open needs beyond the transport: the drive to mount, named by
// its DOS drive letter ("C", "E", …). EtherDFS has no credentials.
type DialParams struct {
	Drive string // one letter A–Z; the DOS drive letter the server exported
}

// Open resolves the drive letter to its number and probes the server so the transport
// learns its MAC (an AL_DISKSPACE query, the reference client's discovery). It returns a
// Session bound to the drive. A probe failure is fatal (no server answered).
func Open(tr Transport, p DialParams) (*Session, error) {
	num, ok := driveNumber(p.Drive)
	if !ok {
		return nil, fmt.Errorf("etherdfs: %q is not a drive letter (A–Z)", p.Drive)
	}
	s := &Session{tr: tr, drive: num}

	// Discovery: an AL_DISKSPACE query for the drive draws a reply from the server,
	// which the transport uses to learn the server MAC. The reply's AX is the fixed
	// DiskSpaceStatus data value (not an error), so any reply confirms the server.
	if _, _, err := s.tr.Send(num, proto.OpDiskspace, nil); err != nil {
		return nil, fmt.Errorf("etherdfs: discover drive %s: %w", p.Drive, err)
	}
	return s, nil
}

// command serialises one request/reply exchange for the bound drive: send (opcode,
// body), return the reply's AX status and body. Holding the mutex across the exchange
// keeps the transport's sequence correlation consistent.
func (s *Session) command(opcode uint8, body []byte) (uint16, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tr.Send(s.drive, opcode, body)
}

// MaxPayload is the largest READ/WRITE payload the client issues, bounded by the
// transport's one-frame ceiling.
func (s *Session) MaxPayload() int { return s.tr.MaxPayload() }

// Close closes the transport (EtherDFS has no session teardown message).
func (s *Session) Close() error { return s.tr.Close() }

// driveNumber maps a one-letter drive name ("A".."Z", case-insensitive) to its DOS
// drive number (A=0 … Z=25), mirroring the server's driveNumber. ok is false for any
// name that is not a single A–Z letter.
func driveNumber(name string) (uint8, bool) {
	name = strings.TrimSpace(name)
	if len(name) != 1 {
		return 0, false
	}
	c := name[0]
	switch {
	case c >= 'A' && c <= 'Z':
		return c - 'A', true
	case c >= 'a' && c <= 'z':
		return c - 'a', true
	}
	return 0, false
}
