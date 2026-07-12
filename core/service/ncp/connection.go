package ncp

import (
	"fmt"
	"sync"
	"time"
)

// connection.go holds the NCP service-connection table. A NetWare server assigns
// each client a numbered service connection (1..maxConnections) on its
// create-connection request; the number is carried (split low/high) in every
// subsequent NCP header and identifies the per-client state: the logged-in
// identity, the open directory handles, and the open file handles. This is the NCP
// analogue of an SMB session keyed by transport endpoint.
//
// Connections are keyed by the client's IPX endpoint (network+node) so a
// retransmitted create-connection from the same station reuses its slot rather
// than leaking a new one, and an inbound request can find its connection from the
// datagram source as well as from the header number.

// maxConnections is the highest connection number the server hands out. NetWare
// 3.x servers were licensed per-connection; the cap here is generous for a
// compatibility server and bounds the table.
const maxConnections = 250

// connIdleTimeout is how long a connection may go without traffic before the reaper
// reclaims it (the client vanished without a destroy-connection). NetWare uses an
// SPX watchdog for this; absent SPX we age on inactivity.
const connIdleTimeout = 15 * time.Minute

// endpoint keys a connection by the remote IPX address (network+node).
type endpoint struct {
	net  [4]byte
	node [6]byte
}

// String renders the endpoint in the conventional IPX net.node form
// (e.g. "00000000.02608c531b97") for the diagnostic logs.
func (ep endpoint) String() string {
	return fmt.Sprintf("%x.%x", ep.net, ep.node)
}

// dirHandle is one allocated directory handle: the volume it is bound to and the
// store path (the seam's '/'-separated form) it currently points at. NetWare
// clients allocate a handle, set it to a directory, then issue path operations
// relative to it.
type dirHandle struct {
	volume *Volume
	path   string // store path relative to the volume root ("" = root)
}

// openFile is one open file handle: the volume, the store path, and the seam
// handle the read/write functions act on. The seam handle type is held as any so
// this file does not couple to a concrete fs.Handle shape; the dispatch type-
// asserts it.
type openFile struct {
	volume *Volume
	path   string
	handle any
}

// connection is one client's service-connection state.
type connection struct {
	number   uint16
	ep       endpoint
	sock     [2]byte // client's IPX socket (Get Connection Internet Address reports it)
	user     string  // logged-in bindery user; "" = not logged in (guest)
	loggedIn bool

	// The logged-in bindery identity + login instant, reported by the
	// connection-information family (0x17/0x16, 0x17/0x1C — mars_nwe nwbind.c).
	// objectID 0 = not logged in.
	objectID   uint32
	objectType uint16
	loginTime  time.Time

	// rwBufferSize is the Negotiate Buffer Size (0x21) result for this
	// connection (mars_nwe's rw_buffer_size); 0 = not yet negotiated
	// (treated as maxRWBufferSize).
	rwBufferSize uint16

	mu       sync.Mutex
	dirs     map[uint8]*dirHandle
	bases    map[uint32]*dirHandle // 4-byte name-space dir bases (function 0x57)
	files    map[uint16]*openFile
	nextDir  uint8
	nextBase uint32
	nextFile uint16
	lastSeen time.Time
}

// AllocDir reserves a directory handle bound to vol at path and returns its id.
func (c *connection) AllocDir(vol *Volume, path string) uint8 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextDir++
	if c.nextDir == 0 {
		c.nextDir = 1
	}
	id := c.nextDir
	c.dirs[id] = &dirHandle{volume: vol, path: path}
	return id
}

// SeedDir installs a well-known directory handle (the connection-init LOGIN
// handle) and keeps AllocDir from reusing its id.
func (c *connection) SeedDir(id uint8, vol *Volume, path string) {
	c.mu.Lock()
	c.dirs[id] = &dirHandle{volume: vol, path: path}
	if c.nextDir < id {
		c.nextDir = id
	}
	c.mu.Unlock()
}

// SetDir rebinds directory handle id to vol at path, creating the handle when the
// client names one it never allocated (Set Directory Handle 0x16/0x00 retargets a
// client-held handle in place — DOS shells SET handles they were given at login).
func (c *connection) SetDir(id uint8, vol *Volume, path string) {
	c.mu.Lock()
	c.dirs[id] = &dirHandle{volume: vol, path: path}
	c.mu.Unlock()
}

// Dir returns the directory handle for id, if allocated.
func (c *connection) Dir(id uint8) (*dirHandle, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d, ok := c.dirs[id]
	return d, ok
}

// FreeDir releases a directory handle. Idempotent.
func (c *connection) FreeDir(id uint8) {
	c.mu.Lock()
	delete(c.dirs, id)
	c.mu.Unlock()
}

// AllocBase reserves a 4-byte name-space directory base bound to vol at path and
// returns its id (function 0x57 Generate-Dir-Base / Initialize-Search). The base
// space is separate from the 1-byte DOS dir handles.
func (c *connection) AllocBase(vol *Volume, path string) uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextBase++
	if c.nextBase == 0 {
		c.nextBase = 1
	}
	id := c.nextBase
	c.bases[id] = &dirHandle{volume: vol, path: path}
	return id
}

// Base returns the name-space dir base for id, if allocated.
func (c *connection) Base(id uint32) (*dirHandle, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d, ok := c.bases[id]
	return d, ok
}

// AllocFile reserves an open-file handle and returns its id.
func (c *connection) AllocFile(of *openFile) uint16 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextFile++
	if c.nextFile == 0 {
		c.nextFile = 1
	}
	id := c.nextFile
	c.files[id] = of
	return id
}

// File returns the open file for id, if allocated.
func (c *connection) File(id uint16) (*openFile, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f, ok := c.files[id]
	return f, ok
}

// FreeFile releases an open-file handle and returns it (so the caller can close
// the underlying seam handle). Idempotent: a second free returns nil,false.
func (c *connection) FreeFile(id uint16) (*openFile, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f, ok := c.files[id]
	if ok {
		delete(c.files, id)
	}
	return f, ok
}

// connTable is the server's set of live connections, keyed both by number (for
// header lookup) and by endpoint (for create-connection idempotency and inbound
// source lookup). Safe for concurrent use.
type connTable struct {
	mu    sync.Mutex
	byNum map[uint16]*connection
	byEP  map[endpoint]*connection
	next  uint16
}

func newConnTable() *connTable {
	return &connTable{
		byNum: make(map[uint16]*connection),
		byEP:  make(map[endpoint]*connection),
	}
}

// Create allocates (or reuses) a connection for the remote endpoint and returns
// it. A retransmitted create-connection from a station that already holds a
// connection returns the existing one (idempotent). Returns nil,false when the
// connection cap is reached.
func (t *connTable) Create(net [4]byte, node [6]byte, sock [2]byte) (*connection, bool) {
	ep := endpoint{net: net, node: node}
	t.mu.Lock()
	defer t.mu.Unlock()
	if c, ok := t.byEP[ep]; ok {
		c.touch()
		return c, true
	}
	num, ok := t.allocNumberLocked()
	if !ok {
		return nil, false
	}
	c := &connection{
		number:   num,
		ep:       ep,
		sock:     sock,
		dirs:     make(map[uint8]*dirHandle),
		bases:    make(map[uint32]*dirHandle),
		files:    make(map[uint16]*openFile),
		lastSeen: time.Now(),
	}
	t.byNum[num] = c
	t.byEP[ep] = c
	return c, true
}

// allocNumberLocked finds a free connection number 1..maxConnections; caller holds
// t.mu.
func (t *connTable) allocNumberLocked() (uint16, bool) {
	for range maxConnections {
		t.next++
		if t.next == 0 || t.next > maxConnections {
			t.next = 1
		}
		if _, taken := t.byNum[t.next]; !taken {
			return t.next, true
		}
	}
	return 0, false
}

// ByNumber returns the connection with the given number, if live.
func (t *connTable) ByNumber(num uint16) (*connection, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.byNum[num]
	if ok {
		c.touch()
	}
	return c, ok
}

// Peek returns the connection with the given number, if live, WITHOUT touching
// its idle clock — for the connection-information family, where one station asks
// about another and must not keep it alive.
func (t *connTable) Peek(num uint16) (*connection, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.byNum[num]
	return c, ok
}

// ByEndpoint returns the connection for an IPX endpoint, if live.
func (t *connTable) ByEndpoint(net [4]byte, node [6]byte) (*connection, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.byEP[endpoint{net: net, node: node}]
	return c, ok
}

// Destroy removes a connection (the client's destroy-connection request, or the
// reaper). It returns the removed connection so the caller can close any open file
// handles. Idempotent.
func (t *connTable) Destroy(num uint16) (*connection, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.byNum[num]
	if !ok {
		return nil, false
	}
	delete(t.byNum, num)
	delete(t.byEP, c.ep)
	return c, true
}

// touch records activity on the connection (resets the idle clock).
func (c *connection) touch() {
	c.mu.Lock()
	c.lastSeen = time.Now()
	c.mu.Unlock()
}

// Reap removes connections idle longer than connIdleTimeout and returns them so
// the caller can release their handles.
func (t *connTable) Reap(now time.Time) []*connection {
	t.mu.Lock()
	defer t.mu.Unlock()
	var dead []*connection
	for num, c := range t.byNum {
		c.mu.Lock()
		idle := now.Sub(c.lastSeen)
		c.mu.Unlock()
		if idle > connIdleTimeout {
			delete(t.byNum, num)
			delete(t.byEP, c.ep)
			dead = append(dead, c)
		}
	}
	return dead
}

// Snapshot returns counts for the stats gauges: live connections, logged-in
// connections, and the total open-file handles across all connections.
func (t *connTable) Snapshot() (conns, loggedIn, openFiles int) {
	t.mu.Lock()
	cs := make([]*connection, 0, len(t.byNum))
	for _, c := range t.byNum {
		cs = append(cs, c)
	}
	t.mu.Unlock()
	conns = len(cs)
	for _, c := range cs {
		c.mu.Lock()
		if c.loggedIn {
			loggedIn++
		}
		openFiles += len(c.files)
		c.mu.Unlock()
	}
	return conns, loggedIn, openFiles
}

// All returns a snapshot of the live connections (for teardown on Stop).
func (t *connTable) All() []*connection {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*connection, 0, len(t.byNum))
	for _, c := range t.byNum {
		out = append(out, c)
	}
	return out
}
