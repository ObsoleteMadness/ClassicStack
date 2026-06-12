package smb

// conn.go is the SMB side of the NetBIOS→SMB session-data seam. A NetBIOS
// transport (NBF/NBT/IPX) carries one SMB virtual circuit per established
// session; on that circuit it reassembles whole SMB messages and hands them to
// the SMB command engine, writing the response back over the same circuit. The
// transport holds no SMB knowledge and SMB holds no transport knowledge: the
// only contract between them is "here is one SMB message on this circuit, give me
// the bytes to send back."
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

// NewConn opens an SMB virtual circuit on the service. The transport calls this
// once per established NetBIOS session; the returned Conn is fed each reassembled
// SMB message via ServeMessage and released via Close.
func (s *Service) NewConn() *Conn {
	return &Conn{svc: s, sess: newSession()}
}

// ServeMessage dispatches one reassembled SMB request and returns the response
// bytes to send back over the circuit, or nil to send nothing (the silent-drop
// case some commands and malformed frames take). req begins at the "\xffSMB"
// header — the transport has already stripped its own framing.
func (c *Conn) ServeMessage(req []byte) []byte {
	return c.svc.Dispatch(c.sess, req)
}

// Close releases the circuit, closing any file handles and searches the session
// still holds. The transport calls this when the NetBIOS session ends so a
// dropped circuit does not leak handles.
func (c *Conn) Close() {
	c.sess.closeAll()
}

// SessionConsumer is the SMB-facing contract a NetBIOS transport drives: open a
// circuit per session, serve each reassembled message, close on teardown. The
// SMB Service satisfies it through NewConn/Conn. It lets the NetBIOS service hold
// the SMB command engine behind one small interface, so neither side imports the
// other's internals (the §3-bis command-core / session-transport split).
type SessionConsumer interface {
	NewConn() SessionCircuit
}

// SessionCircuit is one open SMB virtual circuit as the NetBIOS transport sees
// it: serve a message (returning the reply bytes), and close on teardown.
type SessionCircuit interface {
	ServeMessage(req []byte) []byte
	Close()
}

// ConsumerAdapter wraps a *Service as a SessionConsumer whose circuits are the
// transport-agnostic SessionCircuit. NewConn returns the concrete *Conn (which
// satisfies SessionCircuit); the adapter exists so the netbios package can depend
// on the small SessionConsumer/SessionCircuit interfaces rather than *smb.Service.
type ConsumerAdapter struct{ Service *Service }

// NewConn opens a circuit, returned through the SessionCircuit interface.
func (a ConsumerAdapter) NewConn() SessionCircuit { return a.Service.NewConn() }

// compile-time assertions: the concrete types satisfy the seam interfaces.
var (
	_ SessionCircuit  = (*Conn)(nil)
	_ SessionConsumer = ConsumerAdapter{}
)
