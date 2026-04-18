/*
Package dsi implements the Data Stream Interface (DSI).

DSI is a session-layer protocol that carries AppleTalk Filing Protocol (AFP)
over TCP/IP. It provides session management similar to ASP but for IP networks.

Refer: AppleTalk Filing Protocol 2.1 & 2.2 / AFP over TCP/IP Specification.
*/
package dsi

import (
	"encoding/binary"
	"io"
	"net"

	"github.com/pgodw/omnitalk/go/appletalk"
	"github.com/pgodw/omnitalk/go/netlog"
	"github.com/pgodw/omnitalk/go/port"
	"github.com/pgodw/omnitalk/go/service"
	"github.com/pgodw/omnitalk/go/service/afp"
)

// DSI Command Codes
const (
	CloseSession = 1
	Command      = 2
	GetStatus    = 3
	OpenSession  = 4
	Tickle       = 5
	Write        = 6
	Attention    = 8
)

// DSI Flags
const (
	Request = 0x00
	Reply   = 0x01
)

// Header represents a DSI header (16 bytes).
// Refer: AFP over TCP/IP Specification.
//
//	 0               1               2               3
//	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|     Flags     |    Command    |           Request ID          |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|             Error Offset (or Total Data Length)               |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                         Data Length                           |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                           Reserved                            |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type Header struct {
	Flags       uint8
	Command     uint8
	RequestID   uint16
	ErrorOffset uint32
	DataLen     uint32
	Reserved    uint32
}

const HeaderSize = 16

func (h *Header) Marshal() []byte {
	b := make([]byte, HeaderSize)
	b[0] = h.Flags
	b[1] = h.Command
	binary.BigEndian.PutUint16(b[2:4], h.RequestID)
	binary.BigEndian.PutUint32(b[4:8], h.ErrorOffset)
	binary.BigEndian.PutUint32(b[8:12], h.DataLen)
	binary.BigEndian.PutUint32(b[12:16], h.Reserved)
	return b
}

func (h *Header) Unmarshal(b []byte) error {
	if len(b) < HeaderSize {
		return io.ErrUnexpectedEOF
	}
	h.Flags = b[0]
	h.Command = b[1]
	h.RequestID = binary.BigEndian.Uint16(b[2:4])
	h.ErrorOffset = binary.BigEndian.Uint32(b[4:8])
	h.DataLen = binary.BigEndian.Uint32(b[8:12])
	h.Reserved = binary.BigEndian.Uint32(b[12:16])
	return nil
}

type AFPVersion struct {
	VersionName string
	Version     int
}

type Server struct {
	serverName string
	addr       string
	afpServer  afp.CommandHandler
	listener   net.Listener
	stop       chan struct{}
}

func NewServer(serverName string, addr string, afpHandler afp.CommandHandler) *Server {
	return &Server{
		serverName: serverName,
		addr:       addr,
		afpServer:  afpHandler,
		stop:       make(chan struct{}),
	}
}

// SetCommandHandler assigns the AFP command handler to this server.
func (s *Server) SetCommandHandler(handler afp.CommandHandler) {
	s.afpServer = handler
}

// Start implements afp.Transport.
func (s *Server) Start(router service.Router) error {
	l, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = l

	go func() {
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				select {
				case <-s.stop:
					return
				default:
				}
				netlog.Debug("[DSI] accept error: %v", err)
				continue
			}
			netlog.Debug("[DSI] connection accepted from %s", conn.RemoteAddr())
			go s.handleConn(conn)
		}
	}()
	return nil
}

// Stop implements afp.Transport.
func (s *Server) Stop() error {
	close(s.stop)
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// Inbound implements afp.Transport.
func (s *Server) Inbound(d appletalk.Datagram, p port.Port) {
	// DSI over TCP does not process DDP packets
}

func (s *Server) ListenAndServe() error {
	l, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = l
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer func() {
		netlog.Debug("[DSI] connection closed: %s", conn.RemoteAddr())
		conn.Close()
	}()
	for {
		headerBuf := make([]byte, HeaderSize)
		_, err := io.ReadFull(conn, headerBuf)
		if err != nil {
			if err != io.EOF {
				netlog.Debug("[DSI] error reading header from %s: %v", conn.RemoteAddr(), err)
			}
			return
		}

		var h Header
		h.Unmarshal(headerBuf)
		netlog.Debug("[DSI] <- req=%d cmd=%d flag=%d dataLen=%d from %s", h.RequestID, h.Command, h.Flags, h.DataLen, conn.RemoteAddr())

		payload := make([]byte, h.DataLen)
		_, err = io.ReadFull(conn, payload)
		if err != nil {
			if err != io.EOF {
				netlog.Debug("[DSI] error reading payload from %s: %v", conn.RemoteAddr(), err)
			}
			return
		}

		switch h.Command {
		case GetStatus:
			s.handleGetStatus(conn, h)
		case OpenSession:
			s.handleOpenSession(conn, h)
		case Command:
			s.handleCommand(conn, h, payload)
		case Write:
			s.handleWrite(conn, h, payload)
		case Tickle:
			s.handleTickle(conn, h)
		case CloseSession:
			s.handleCloseSession(conn, h)
			return // Session explicitly closed by client
		default:
			netlog.Debug("[DSI] unhandled command %d from %s", h.Command, conn.RemoteAddr())
		}
	}
}

func (s *Server) writeResponse(conn net.Conn, replyHdr Header, data []byte) {
	netlog.Debug("[DSI] -> req=%d cmd=%d flag=%d dataLen=%d to %s", replyHdr.RequestID, replyHdr.Command, replyHdr.Flags, replyHdr.DataLen, conn.RemoteAddr())
	conn.Write(replyHdr.Marshal())
	if len(data) > 0 {
		conn.Write(data)
	}
}

func (s *Server) handleTickle(conn net.Conn, h Header) {
	replyHdr := Header{
		Flags:     Reply,
		Command:   Tickle,
		RequestID: h.RequestID,
		DataLen:   0,
	}
	s.writeResponse(conn, replyHdr, nil)
}

func (s *Server) handleCloseSession(conn net.Conn, h Header) {
	replyHdr := Header{
		Flags:     Reply,
		Command:   CloseSession,
		RequestID: h.RequestID,
		DataLen:   0,
	}
	s.writeResponse(conn, replyHdr, nil)
}

func (s *Server) handleGetStatus(conn net.Conn, h Header) {
	// Inside Macintosh: Networking, Chapter 9.
	// https://dev.os9.ca/techpubs/mac/Networking/Networking-223.html
	// AFP over TCP/IP (DSI) expects a full FPGetSrvrInfo response.

	payload := afp.BuildServerInfo(s.serverName)

	replyHdr := Header{
		Flags:       Reply,
		Command:     GetStatus,
		RequestID:   h.RequestID,
		ErrorOffset: 0,
		DataLen:     uint32(len(payload)),
	}
	s.writeResponse(conn, replyHdr, payload)
}

func (s *Server) handleOpenSession(conn net.Conn, h Header) {
	replyHdr := Header{
		Flags:       Reply,
		Command:     OpenSession,
		RequestID:   h.RequestID,
		ErrorOffset: 0,
		DataLen:     0,
	}
	s.writeResponse(conn, replyHdr, nil)
}

func (s *Server) handleCommand(conn net.Conn, h Header, data []byte) {
	if s.afpServer == nil || len(data) == 0 {
		return
	}

	replyData, errCode := s.afpServer.HandleCommand(data)

	// For DSI, AFP errors are returned in the response header or prepended?
	// The original DSI code manually prepended the 4-byte error code to the payload.
	reply := make([]byte, 4+len(replyData))
	binary.BigEndian.PutUint32(reply[0:4], uint32(errCode))
	copy(reply[4:], replyData)

	replyHdr := Header{
		Flags:       Reply,
		Command:     Command,
		RequestID:   h.RequestID,
		ErrorOffset: 0,
		DataLen:     uint32(len(reply)),
	}
	s.writeResponse(conn, replyHdr, reply)
}

func (s *Server) handleWrite(conn net.Conn, h Header, data []byte) {
	if s.afpServer == nil || len(data) == 0 {
		return
	}

	replyData, errCode := s.afpServer.HandleCommand(data)

	reply := make([]byte, 4+len(replyData))
	binary.BigEndian.PutUint32(reply[0:4], uint32(errCode))
	copy(reply[4:], replyData)

	replyHdr := Header{
		Flags:       Reply,
		Command:     Write,
		RequestID:   h.RequestID,
		ErrorOffset: 0,
		DataLen:     uint32(len(reply)),
	}
	s.writeResponse(conn, replyHdr, reply)
}
