// Package smb is the SMB (SMB1/CIFS) file client: it drives one virtual circuit to an
// SMB server through the client-direction codec (core/protocol/smb) and presents the
// mounted share as an fs.FileSystem, so a remote SMB share is an ordinary ForkFS to
// client/xfer and cmd/csfs — the same interface an AFP or local share exposes. SMB has
// no native resource fork, so client.Connect wraps this base with the AppleDouble fork
// backend, which reads/writes the server's own "._name" sidecars as ordinary files.
//
// The client speaks SMB1 exclusively (the classic servers this project targets — WfW,
// OS/2, DOS LAN Manager, and ClassicStack itself — are SMB1), negotiating NT LM 0.12
// with Unicode when the server offers it and falling back to the LANMAN/CORE shapes
// otherwise. It sends cleartext credentials (or none, for guest) — the honest posture
// documented for the server side.
//
// Ring: CLIENT.
package smb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

// Transport is one SMB virtual circuit as the client sees it: send a whole SMB message
// (starting at the "\xffSMB" header) and get the whole response message back, blocking
// until it arrives. It is the mirror of the server's smb.SessionCircuit — the framing
// (NBT length prefix on TCP, in-process hand-off for the e2e bridge) lives in the
// implementation; the session code above it only ever sees complete messages.
type Transport interface {
	// Send writes one SMB request and returns the matching response message. A
	// transport that multiplexes by MID may reorder, but this client sends one request
	// at a time per circuit, so a strict request→response transport is sufficient.
	Send(req []byte) (resp []byte, err error)
	// MaxResponse is the largest SMB response message the transport can carry back in
	// one exchange. A stream transport (TCP/NBT) reassembles, so it returns a large
	// value; a connectionless datagram transport (direct SMB over IPX) has no
	// reassembly and must fit one datagram, so it returns a datagram-safe cap the
	// session uses to bound TRANS2 MaxDataCount and READ/WRITE sizes.
	MaxResponse() int
	Close() error
}

// maxMessage caps a single inbound SMB message at 16 MiB (matching the server-side
// transport), so a malformed length header cannot drive an unbounded allocation.
const maxMessage = 16 << 20

// nbtHeaderLen is the 4-byte NetBIOS Session Service header every SMB-over-TCP message
// carries: a 1-byte message type (0 = session message) then a 3-byte big-endian length
// ([RFC 1002] §4.3.1). Direct-hosted SMB on :445 uses the same framing.
const nbtHeaderLen = 4

// tcpTransport frames SMB messages over a TCP connection with the NBT session-message
// length prefix. One request is written and the single matching response read back; the
// client serialises requests per circuit so no MID demux is needed.
type tcpTransport struct {
	mu   sync.Mutex
	conn net.Conn
}

// DialTCP opens an SMB-over-TCP circuit to conn (already dialled by the caller's
// Opener). It performs no NBT session-request handshake: :445 has none, and a
// ClassicStack :139 listener accepts-and-ignores it, so sending SMB messages directly
// works against both.
func DialTCP(conn net.Conn) Transport {
	return &tcpTransport{conn: conn}
}

// Send writes req with its NBT length prefix and reads back one framed response.
func (t *tcpTransport) Send(req []byte) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var hdr [nbtHeaderLen]byte
	// Message type 0 (session message) in hdr[0]; 24-bit length in hdr[1:4].
	binary.BigEndian.PutUint32(hdr[:], uint32(len(req)))
	hdr[0] = 0x00
	if _, err := t.conn.Write(hdr[:]); err != nil {
		return nil, err
	}
	if _, err := t.conn.Write(req); err != nil {
		return nil, err
	}
	return readNBTMessage(t.conn)
}

// MaxResponse: TCP/NBT reassembles a message from its length prefix, so the client may
// request a large reply (the server still bounds it by its own MaxBufferSize).
func (t *tcpTransport) MaxResponse() int { return maxMessage }

// Close closes the underlying connection.
func (t *tcpTransport) Close() error { return t.conn.Close() }

// readNBTMessage reads one NBT-framed message: the 4-byte header then Length bytes. It
// skips non-session-message frames (keep-alives, type != 0) transparently.
func readNBTMessage(r io.Reader) ([]byte, error) {
	for {
		var hdr [nbtHeaderLen]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return nil, err
		}
		// Length is the low 24 bits; the high byte is the message type.
		length := int(uint32(hdr[1])<<16 | uint32(hdr[2])<<8 | uint32(hdr[3]))
		msgType := hdr[0]
		if length == 0 {
			if msgType == nbtSessionMessage {
				return nil, nil
			}
			continue // keep-alive / handshake with no payload
		}
		if length > maxMessage {
			return nil, fmt.Errorf("smb: inbound message length %d exceeds cap", length)
		}
		buf := make([]byte, length)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		if msgType != nbtSessionMessage {
			continue // drain and ignore non-session frames
		}
		return buf, nil
	}
}

// nbtSessionMessage is the NBT message type for a session message (payload is an SMB
// message). Other types (session request/response, keep-alive) are tolerated.
const nbtSessionMessage = 0x00

// ErrTransportClosed is returned when a Send races a Close.
var ErrTransportClosed = errors.New("smb: transport closed")
