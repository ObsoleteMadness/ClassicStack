package afp

// Session is the transport-agnostic AFP command circuit a client dial satisfies — the
// client-side mirror of core/service/afp's CommandHandler/CommandCircuit split (see
// core/service/afp/conn.go). Two transports implement it: client/asp.Session
// (ASP-over-DDP, the classic transport) and client/dsi.Session (DSI-over-TCP, the
// modern one). FS holds one of these behind the interface so its command plumbing
// (sessCommand, sessWrite, reestablish) does not care which transport carried the
// session — exactly as the server side's AFP command core does not care whether ASP
// or DSI framed the request.
type Session interface {
	// Command runs one AFP command block and returns the reply block, the signed AFP
	// result code, and a transport error (client/asp.ErrSessionClosed on a dead
	// session — client/dsi returns the same sentinel so the reconnect logic in
	// afp.go's errors.Is checks work for either transport).
	Command(block []byte) (reply []byte, result int32, err error)
	// CommandMax is Command with a transport-specific reply-size budget (the ASP
	// quantum in ATP packets). A stream transport (DSI) has no such quantum and
	// ignores maxResp.
	CommandMax(block []byte, maxResp int) (reply []byte, result int32, err error)
	// Write runs a two-phase-shaped AFP write (FPWrite/FPAddIcon): header is the
	// fixed-length command header, data the bulk bytes that follow it on the wire.
	Write(header, data []byte) (reply []byte, result int32, err error)
	// Close tears down the circuit (and, for DSI, the underlying TCP connection).
	Close() error
	// SetAttentionHandler installs the callback for unsolicited server notifications
	// (message-waiting, server-going-down). A transport with no async delivery path
	// may simply store it and never call it.
	SetAttentionHandler(h func(code uint16))
}
