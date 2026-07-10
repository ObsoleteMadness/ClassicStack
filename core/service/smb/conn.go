package smb

// conn.go is the SMB side of the (transport-agnostic) SMB session-data seam. A
// session transport carries one SMB virtual circuit per established session; on
// that circuit it reassembles whole SMB messages and hands them to the SMB command
// engine, writing the response back over the same circuit. The transport holds no
// SMB knowledge and SMB holds no transport knowledge: the only contract between
// them is "here is one SMB message on this circuit, give me the bytes to send
// back."
//
// The transport may be NetBIOS-based — NBF (NetBEUI), NBIPX (NetBIOS-over-IPX,
// socket 0x0455), or NBT (NetBIOS-over-TCP) — OR a DIRECT (NetBIOS-less) transport:
// SMB direct-hosted over IPX (socket 0x0550) or direct-TCP (:445). Both families
// drive THIS SAME seam; SMB does not distinguish them — the command core is reached
// the same way regardless of how the session was framed on the wire.
//
// The Conn is that per-circuit object. The transport calls NewConn once per
// established session, ServeMessage for each reassembled SMB request, and Close
// when the circuit tears down (so open file handles do not leak). One Conn owns
// one smbSession — the same per-connection state Dispatch already drives.

// Conn is one SMB virtual circuit: a transport-owned handle that turns a
// reassembled SMB request message into the response bytes to send back. It wraps
// a single smbSession so successive messages on the circuit share UID, tree
// connects, and open handles.
type Conn struct {
	svc  *Service
	sess *smbSession
}

// NewConn opens an SMB virtual circuit on the service for the transport
// remote-endpoint label client (e.g. an IPX node.socket, a MAC, or a TCP addr; ""
// when the transport supplies none). The transport calls this once per established
// session; the returned Conn is fed each reassembled SMB message via ServeMessage
// and released via Close. The client label keys the session in the management view.
func (s *Service) NewConn(client string) *Conn {
	sess := newSession(client)
	s.registerSession(sess) // §10d: track the circuit so async NOTIFY_CHANGE can reach it
	return &Conn{svc: s, sess: sess}
}

// ServeMessage dispatches one reassembled SMB request and returns the response
// bytes to send back over the circuit, or nil to send nothing (the silent-drop
// case some commands and malformed frames take). req begins at the "\xffSMB"
// header — the transport has already stripped its own framing.
//
// At debug level it narrates the exchange: one line for the decoded inbound command
// and one for the outbound response (its status + length, or a silent drop), keyed by
// the circuit's client label — so a `-log debug` run shows exactly which SMB requests
// reached the engine and what it answered (the diagnosis path for "the client tore the
// session down after NEGOTIATE" — see spec/errata.md).
func (c *Conn) ServeMessage(req []byte) []byte {
	c.svc.logSMBRequest(c.sess, req)
	resp := c.svc.Dispatch(c.sess, req)
	c.svc.logSMBResponse(c.sess, resp)
	return resp
}

// SetPushWriter installs the transport's server-initiated push channel: a function
// that frames and sends one unsolicited SMB message back over THIS circuit (§10d
// wire push). The SMB command engine uses it to complete a held NT_TRANSACT
// NOTIFY_CHANGE asynchronously when a watched tree changes. A transport that cannot
// push (no retained per-circuit addressing) simply never calls this; a held
// NOTIFY_CHANGE then never completes (the client times it out), which is benign.
func (c *Conn) SetPushWriter(w func([]byte)) {
	c.sess.setPush(w)
}

// Close releases the circuit, closing any file handles and searches the session
// still holds. The transport calls this when the NetBIOS session ends so a
// dropped circuit does not leak handles.
func (c *Conn) Close() {
	c.svc.unregisterSession(c.sess) // §10d: stop tracking this circuit for pushes
	c.sess.closeAll()
}

// SessionConsumer is the SMB-facing contract ANY session transport drives —
// NetBIOS-based (NBF/NBIPX/NBT) or direct (IPX-0x0550 / TCP-:445): open a circuit
// per session, serve each reassembled message, close on teardown. The SMB Service
// satisfies it through NewConn/Conn. It lets a transport hold the SMB command
// engine behind one small interface, so neither side imports the other's internals
// (the §3-bis command-core / session-transport split).
type SessionConsumer interface {
	// NewConn opens a circuit for the transport remote-endpoint label client ("" when
	// the transport supplies none). The transport passes its own natural endpoint
	// string (IPX node.socket, MAC, TCP addr) so the service can group sessions per
	// client in the management view.
	NewConn(client string) SessionCircuit
}

// SessionCircuit is one open SMB virtual circuit as the NetBIOS transport sees
// it: serve a message (returning the reply bytes), optionally accept a server-push
// writer (for asynchronous completions like NOTIFY_CHANGE), and close on teardown.
type SessionCircuit interface {
	ServeMessage(req []byte) []byte
	SetPushWriter(w func([]byte))
	Close()
}

// ConsumerAdapter wraps a *Service as a SessionConsumer whose circuits are the
// transport-agnostic SessionCircuit. NewConn returns the concrete *Conn (which
// satisfies SessionCircuit); the adapter exists so the netbios package can depend
// on the small SessionConsumer/SessionCircuit interfaces rather than *smb.Service.
type ConsumerAdapter struct{ Service *Service }

// NewConn opens a circuit for the transport remote-endpoint label client, returned
// through the SessionCircuit interface.
func (a ConsumerAdapter) NewConn(client string) SessionCircuit {
	return a.Service.NewConn(client)
}

// compile-time assertions: the concrete types satisfy the seam interfaces.
var (
	_ SessionCircuit  = (*Conn)(nil)
	_ SessionConsumer = ConsumerAdapter{}
)
