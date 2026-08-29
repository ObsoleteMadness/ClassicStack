package dsi

import (
	"context"
	"io"
	"net"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	dsiproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/dsi"
	"github.com/ObsoleteMadness/ClassicStack/core/service/afp"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// Name is the component name for the AFP-over-TCP (DSI) transport. It is its own
// supervised component (a listener with a lifecycle), distinct from the AFP command
// service.
const Name = "DSI"

// maxMessage caps a single DSI data block at 16 MiB — well above any real AFP
// command/write payload — so a malformed DataLen header cannot drive an unbounded
// allocation.
const maxMessage = 16 << 20

// Transport is a TCP listener that drives the AFP command-core seam over DSI framing.
// One accept loop spawns a goroutine per connection; each connection opens one AFP
// circuit (on OpenSession) and serves DSI requests until the peer closes or sends
// CloseSession.
type Transport struct {
	addr    string
	handler afp.CommandHandler
	logger  log.Logger

	mu       sync.Mutex
	listener net.Listener
	conns    map[net.Conn]struct{}
	running  bool
}

// New builds a DSI transport. addr/handler may be empty/nil at construction (the
// registry builds it inert); the compose transport cross-wire installs the AFP
// command handler and the listen address once the AFP service and its tcp_addr are
// resolved (mirrors adapter/smbtcp.New).
func New(addr string, handler afp.CommandHandler, logger log.Logger) *Transport {
	return &Transport{addr: addr, handler: handler, logger: logger, conns: make(map[net.Conn]struct{})}
}

// SetHandler installs the AFP command handler after construction. Must be called
// before Start; a nil handler leaves Start a no-op.
func (t *Transport) SetHandler(h afp.CommandHandler) {
	t.mu.Lock()
	t.handler = h
	t.mu.Unlock()
}

// SetAddr sets/overrides the listen address before Start (compose supplies it from
// the AFP server section's tcp_addr). An empty address keeps Start a no-op.
func (t *Transport) SetAddr(addr string) {
	t.mu.Lock()
	t.addr = addr
	t.mu.Unlock()
}

// Name returns the component name.
func (t *Transport) Name() string { return Name }

// Binding reports the listen address (component.Bindable), so the dashboard shows it.
func (t *Transport) Binding() string { return t.addr }

// Dependencies declares the DSI listener's start-order edge: the AFP service must be
// running first, since the listener drives its command-core seam (and must stop
// before it). Drops in a build without the AFP service.
func (t *Transport) Dependencies() []string { return []string{afp.Name} }

// Start opens the listener and begins accepting. Idempotent (§3). A nil handler or an
// empty address makes Start a no-op so a build that wires the transport but does not
// configure tcp_addr stays inert rather than erroring.
//
// A bind failure is NON-FATAL, matching the other transports' graceful-degradation
// posture: Start logs a warning and returns nil rather than aborting the whole
// stack's bring-up.
func (t *Transport) Start(_ context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running || t.handler == nil || t.addr == "" {
		return nil
	}
	l, err := net.Listen("tcp", t.addr)
	if err != nil {
		if t.logger != nil {
			t.logger.Log(log.Warn, "AFP-over-TCP (DSI) bind failed; transport inert",
				log.Str("addr", t.addr), log.Str("error", err.Error()))
		}
		t.running = true // lifecycle-consistent: "running" but unbound
		return nil
	}
	t.listener = l
	t.running = true
	go t.acceptLoop(l)
	if t.logger != nil {
		t.logger.Log(log.Info, "AFP-over-TCP (DSI) listening", log.Str("addr", l.Addr().String()))
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

// serve runs one connection: answer sessionless GetStatus directly, open an AFP
// circuit on OpenSession, dispatch Command/Write through it, and close the circuit on
// CloseSession or when the peer disconnects.
func (t *Transport) serve(conn net.Conn) {
	var circuit afp.CommandCircuit
	defer func() {
		if circuit != nil {
			circuit.Close()
		}
		_ = conn.Close()
		t.mu.Lock()
		delete(t.conns, conn)
		t.mu.Unlock()
	}()

	handler := t.handlerRef()
	hdrBuf := make([]byte, dsiproto.HeaderSize)
	for {
		if _, err := io.ReadFull(conn, hdrBuf); err != nil {
			return
		}
		var h dsiproto.Header
		if !h.Unmarshal(hdrBuf) {
			return
		}
		if h.DataLen > maxMessage {
			return
		}
		payload := make([]byte, h.DataLen)
		if h.DataLen > 0 {
			if _, err := io.ReadFull(conn, payload); err != nil {
				return
			}
		}

		switch h.Command {
		case dsiproto.GetStatus:
			t.reply(conn, h.RequestID, dsiproto.GetStatus, 0, handler.GetServerInfo())
		case dsiproto.OpenSession:
			if circuit != nil {
				circuit.Close()
			}
			circuit = handler.NewConn()
			t.reply(conn, h.RequestID, dsiproto.OpenSession, 0, nil)
		case dsiproto.Command, dsiproto.Write:
			if circuit == nil {
				// A Command/Write before OpenSession is a protocol violation; there is
				// no AFP result code for "no session" (that is a DSI-level concern), so
				// the connection is simply dropped, matching how the ATP spine answers
				// an unknown ASP session id with a hard error rather than serving.
				return
			}
			reply, result := circuit.Command(payload)
			t.reply(conn, h.RequestID, h.Command, uint32(result), reply)
		case dsiproto.Tickle:
			// No reply required (mirrors ASP's SPTickle) — Tickle exists only to reset
			// the peer's idle timer, whichever direction it travels.
		case dsiproto.CloseSession:
			if circuit != nil {
				circuit.Close()
				circuit = nil
			}
			t.reply(conn, h.RequestID, dsiproto.CloseSession, 0, nil)
			return
		default:
			// Unknown command: ignore and keep the connection open, matching the old
			// server's tolerance of unrecognised DSI commands.
		}
	}
}

// reply writes one DSI reply frame. The AFP/DSI result code goes in the header's
// ErrorOffset field (its reply-side "ErrorCode" role) — NOT prepended to the payload —
// per the DSI header contract documented in core/protocol/dsi; see spec/21-dsi.md.
func (t *Transport) reply(conn net.Conn, reqID uint16, cmd uint8, errCode uint32, data []byte) {
	h := dsiproto.Header{
		Flags:       dsiproto.Reply,
		Command:     cmd,
		RequestID:   reqID,
		ErrorOffset: errCode,
		DataLen:     uint32(len(data)),
	}
	if _, err := conn.Write(h.Marshal()); err != nil {
		return
	}
	if len(data) > 0 {
		_, _ = conn.Write(data)
	}
}

func (t *Transport) handlerRef() afp.CommandHandler {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.handler
}

var (
	_ component.Component = (*Transport)(nil)
	_ component.Bindable  = (*Transport)(nil)
	_ component.DependsOn = (*Transport)(nil)
)
