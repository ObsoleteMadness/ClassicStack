package afp

// conn.go is the transport-agnostic AFP command-core seam — the AFP analogue of
// the SMB conn.go split (§3-bis command-core / session-transport). An AFP session
// transport (ASP-over-ATP, or DSI-over-TCP — adapter/dsi, spec/21-dsi.md) carries one
// AFP virtual circuit per logged-in client; on that circuit it hands whole AFP command
// blocks to the command engine and writes the reply block back over the same
// circuit. The transport holds no AFP knowledge and AFP holds no transport
// knowledge: the only contract between them is "here is one AFP command block on
// this circuit, give me the reply block and result code to send back."
//
// AFP commands are framed two ways on the wire — an ASPCommand (request/reply) or
// an ASPWrite (a two-phase exchange that fetches the bulk write data in a second
// transaction). BOTH ultimately produce one command block that runs through the
// SAME Command method here; the difference (how the data block is assembled) is the
// transport's concern, not the command core's. A DSI transport frames the same two
// shapes (DSICommand / DSIWrite) over TCP and drives this identical seam — which is
// the whole point of the split: the AFP command set is implemented once and reached
// the same way regardless of how the session was framed (ASP over DDP, or DSI over
// TCP).
//
// The Conn is that per-circuit object. The transport calls NewConn once per
// established session, Command for each AFP request block, and Close when the
// circuit tears down (so open forks do not leak). One Conn owns one afpSession —
// the same per-circuit AFP state (logged-in user, open volumes, open forks, Desktop
// refs) the dispatcher drives.

// Conn is one AFP virtual circuit: a transport-owned handle that turns an AFP
// command block into the reply block + result code to send back. It wraps a single
// afpSession so successive commands on the circuit share the login identity, open
// volumes, and open forks.
type Conn struct {
	svc *Service
	afp *afpSession
}

// NewConn opens an AFP virtual circuit on the service. A session transport calls
// this once per established session (ASP OpenSession, or a DSI session open); the
// returned Conn is fed each AFP command block via Command and released via Close.
func (s *Service) NewConn() *Conn {
	return &Conn{svc: s, afp: newAFPSession()}
}

// Command dispatches one AFP command block and returns the AFP reply block plus the
// signed result code (kFP*) to send back over the circuit. block begins at the AFP
// command byte — the transport has already stripped its own (ASP/DSI) framing. For
// an FPWrite-family command the block must already carry its bulk data spliced on
// (the transport assembles it; see splitWriteData); a bare FPWrite header with
// unfetched data is the transport's two-phase concern, not the command core's.
func (c *Conn) Command(block []byte) (reply []byte, result int32) {
	return c.svc.dispatchAFP(c.afp, block)
}

// Close releases the circuit, closing any forks the client left open so a dropped
// circuit does not leak file handles. The transport calls this when the session
// ends (ASP CloseSession, or a DSI session close / dropped TCP connection).
func (c *Conn) Close() {
	c.afp.forks.closeAll()
}

// CommandHandler is the AFP-facing contract ANY session transport drives — ASP over
// ATP/DDP or DSI over TCP: open a circuit per session, run each command block, close
// on teardown. The AFP Service satisfies it through
// NewConn/Conn. It lets a transport hold the AFP command engine behind one small
// interface so neither side imports the other's internals (the §3-bis
// command-core / session-transport split). GetServerInfo is the one sessionless
// call (ASPGetStatus / DSIGetStatus, served before any circuit is opened).
type CommandHandler interface {
	GetServerInfo() []byte
	NewConn() CommandCircuit
}

// CommandCircuit is one open AFP virtual circuit as a transport sees it: run a
// command block (returning the reply block + result code) and close on teardown.
type CommandCircuit interface {
	Command(block []byte) (reply []byte, result int32)
	Close()
}

// HandlerAdapter wraps a *Service as a CommandHandler whose circuits are the
// transport-agnostic CommandCircuit. NewConn returns the concrete *Conn (which
// satisfies CommandCircuit); the adapter exists so a transport package can depend
// on the small CommandHandler/CommandCircuit interfaces rather than *afp.Service.
type HandlerAdapter struct{ Service *Service }

// GetServerInfo returns the FPGetSrvrInfo block (the sessionless ASPGetStatus /
// DSIGetStatus reply).
func (a HandlerAdapter) GetServerInfo() []byte { return a.Service.serverInfoBlock() }

// NewConn opens a circuit, returned through the CommandCircuit interface.
func (a HandlerAdapter) NewConn() CommandCircuit { return a.Service.NewConn() }

// compile-time assertions: the concrete types satisfy the seam interfaces.
var (
	_ CommandCircuit = (*Conn)(nil)
	_ CommandHandler = HandlerAdapter{}
)
