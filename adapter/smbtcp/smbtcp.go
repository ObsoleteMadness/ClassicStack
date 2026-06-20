package smbtcp

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// Name is the component name for the SMB-over-TCP transport. It is its own supervised
// component (a listener with a lifecycle), distinct from the SMB command service.
const Name = "SMB-TCP"

// maxMessage caps a single SMB message at 16 MiB — well above any real SMB1 PDU — so a
// malformed length header cannot drive an unbounded allocation.
const maxMessage = 16 << 20

// nbtSessionMessage is the NetBIOS Session Service message type for a session message
// (the high byte of the 4-byte header). Session request/keep-alive use other types; we
// only carry session messages and tolerate the rest.
const (
	nbtSessionMessage = 0x00
	nbtSessionRequest = 0x81
	nbtPositiveResp   = 0x82
	headerLen         = 4
)

// Transport is a TCP listener that drives the SMB session seam. One accept loop spawns
// a goroutine per connection; each connection opens one SMB circuit and serves its
// length-prefixed messages until the peer closes.
type Transport struct {
	addr     string
	consumer smb.SessionConsumer
	logger   log.Logger

	mu       sync.Mutex
	listener net.Listener
	conns    map[net.Conn]struct{}
	running  bool
}

// New builds a TCP SMB transport bound to addr (e.g. ":445" or ":139"), driving the
// given SMB session consumer. A nil consumer makes Start a no-op (nothing to serve).
func New(addr string, consumer smb.SessionConsumer, logger log.Logger) *Transport {
	return &Transport{addr: addr, consumer: consumer, logger: logger, conns: make(map[net.Conn]struct{})}
}

// SetConsumer installs the SMB session consumer after construction. The compose
// transport cross-wire calls it once the SMB service is resolved (the transport is
// registered inert, like the browser/messenger sinks). Must be called before Start; a
// nil consumer leaves Start a no-op. Idempotent.
func (t *Transport) SetConsumer(c smb.SessionConsumer) {
	t.mu.Lock()
	t.consumer = c
	t.mu.Unlock()
}

// SetAddr sets/overrides the listen address before Start (compose supplies it from the
// SMB config — ":445" for direct-TCP). An empty address keeps Start a no-op.
func (t *Transport) SetAddr(addr string) {
	t.mu.Lock()
	t.addr = addr
	t.mu.Unlock()
}

// Name returns the component name.
func (t *Transport) Name() string { return Name }

// Binding reports the listen address (component.Bindable), so the dashboard shows it.
func (t *Transport) Binding() string { return t.addr }

// Start opens the listener and begins accepting. Idempotent (§3). A nil consumer or an
// empty address makes Start a no-op so a build that wires the transport but disables the
// TCP binding stays inert rather than erroring.
//
// A bind failure is NON-FATAL: on Windows the OS lanmanserver already owns :445, and an
// operator may have another process on the chosen port. Rather than abort the whole
// stack's bring-up, Start logs a warning and returns nil — the component reports running
// (so the supervisor's lifecycle is consistent) but serves nothing. This matches the
// graceful-degradation posture of the other transports (a NIC with no link comes up
// inert, not failed).
func (t *Transport) Start(_ context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running || t.consumer == nil || t.addr == "" {
		return nil
	}
	l, err := net.Listen("tcp", t.addr)
	if err != nil {
		if t.logger != nil {
			t.logger.Log(log.Warn, "SMB-over-TCP bind failed; transport inert (is another server, e.g. the OS, on this port?)",
				log.Str("addr", t.addr), log.Str("error", err.Error()))
		}
		t.running = true // lifecycle-consistent: "running" but unbound
		return nil
	}
	t.listener = l
	t.running = true
	go t.acceptLoop(l)
	if t.logger != nil {
		t.logger.Log(log.Info, "SMB-over-TCP listening", log.Str("addr", l.Addr().String()))
	}
	return nil
}

// Stop closes the listener and every live connection. Safe after a partial Start (§3).
func (t *Transport) Stop(_ context.Context) error {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return nil
	}
	t.running = false
	l := t.listener
	t.listener = nil
	conns := make([]net.Conn, 0, len(t.conns))
	for c := range t.conns {
		conns = append(conns, c)
	}
	t.mu.Unlock()

	if l != nil {
		_ = l.Close()
	}
	for _, c := range conns {
		_ = c.Close()
	}
	return nil
}

func (t *Transport) acceptLoop(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return // listener closed (Stop) or a fatal accept error
		}
		t.mu.Lock()
		if !t.running {
			t.mu.Unlock()
			_ = conn.Close()
			return
		}
		t.conns[conn] = struct{}{}
		t.mu.Unlock()
		go t.serve(conn)
	}
}

// serve runs one connection: open an SMB circuit, then loop reading length-prefixed
// messages, serving each, and writing the framed reply, until the peer closes or errors.
func (t *Transport) serve(conn net.Conn) {
	defer func() {
		_ = conn.Close()
		t.mu.Lock()
		delete(t.conns, conn)
		t.mu.Unlock()
	}()

	circuit := t.consumer.NewConn()
	defer circuit.Close()

	// A server-push writer lets asynchronous completions (NOTIFY_CHANGE) reach this
	// peer; frame and write them with the same length header.
	circuit.SetPushWriter(func(msg []byte) {
		_ = writeFramed(conn, msg)
	})

	hdr := make([]byte, headerLen)
	for {
		if _, err := io.ReadFull(conn, hdr); err != nil {
			return
		}
		msgType := hdr[0]
		// Length is the low 24 bits (NBT) / 17 bits (direct-TCP); 24 bits is the safe
		// superset and matches the cap check below.
		n := int(hdr[1])<<16 | int(hdr[2])<<8 | int(hdr[3])

		// NBT :139 sends a SESSION REQUEST before the first SMB message; answer with a
		// positive session response (4-byte header, type 0x82, length 0) and continue.
		if msgType == nbtSessionRequest {
			if n > 0 {
				if _, err := io.CopyN(io.Discard, conn, int64(n)); err != nil {
					return
				}
			}
			if _, err := conn.Write([]byte{nbtPositiveResp, 0, 0, 0}); err != nil {
				return
			}
			continue
		}
		if msgType != nbtSessionMessage {
			// Keep-alive (0x85) or an unknown type: skip its payload and continue.
			if n > 0 {
				if _, err := io.CopyN(io.Discard, conn, int64(n)); err != nil {
					return
				}
			}
			continue
		}
		if n == 0 || n > maxMessage {
			return
		}

		req := make([]byte, n)
		if _, err := io.ReadFull(conn, req); err != nil {
			return
		}
		resp := circuit.ServeMessage(req)
		if resp == nil {
			continue // no reply for this message (one-way)
		}
		if err := writeFramed(conn, resp); err != nil {
			return
		}
	}
}

// writeFramed writes a 4-byte session header (type 0, 24-bit length) followed by msg.
func writeFramed(w io.Writer, msg []byte) error {
	if len(msg) > maxMessage {
		return errors.New("smbtcp: message too large")
	}
	var hdr [headerLen]byte
	hdr[0] = nbtSessionMessage
	hdr[1] = byte(len(msg) >> 16)
	hdr[2] = byte(len(msg) >> 8)
	hdr[3] = byte(len(msg))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(msg)
	return err
}

var (
	_ component.Component = (*Transport)(nil)
	_ component.Bindable  = (*Transport)(nil)
)
