// Package atalk is the client-side AppleTalk endpoint: a DDP endpoint over a
// link.DatagramLink with an ATP REQUESTER (the workstation half of ATP — the server
// ring only has the responder) and an NBP name-lookup. It is what an AFP client stands
// on to reach a server: claim/assert a node address, look the server up by NBP entity
// name, then run ASP-over-ATP transactions.
//
// The genuinely new engine here is the ATP requester (atp.go): build a TReq requesting
// up to 8 response packets, collect the TResp packets by sequence bit, detect EOM,
// reassemble in order, retry the whole transaction (or just the missing packets) on
// timeout, and release an exactly-once transaction with a TRel. The server's responder
// (core/service/afp/atp.go) is the mirror this was written against.
//
// Ring: CLIENT.
package atalk

import (
	"errors"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// Well-known DDP sockets a client uses.
const (
	// NamesInfoSocket is the NBP names-information socket (DDP socket 2).
	NamesInfoSocket uint8 = 2
	// firstDynamicSocket is the low end of the dynamic-socket range (128) the
	// workstation allocates its own reply sockets from.
	firstDynamicSocket uint8 = 128
)

// Addr is an AppleTalk internet address (network.node.socket).
type Addr struct {
	Network uint16
	Node    uint8
	Socket  uint8
}

// Endpoint is a DDP endpoint over a DatagramLink: it runs one read loop that demuxes
// inbound datagrams to the registered per-socket queues, and sends datagrams stamped
// with the local address. The ATP requester and NBP lookup are built on it.
type Endpoint struct {
	link link.DatagramLink

	mu       sync.Mutex
	local    Addr
	sockets  map[uint8]chan ddp.Datagram
	nextSock uint8
	closed   bool
	done     chan struct{}
}

// NewEndpoint wraps a DatagramLink with a DDP endpoint asserting local as this
// workstation's address. The caller has already framed the link (LToUDP/EtherTalk/
// TashTalk) with the same address; Endpoint stamps outbound datagram Src fields to
// match. It starts the read loop immediately; Close stops it.
func NewEndpoint(dl link.DatagramLink, local Addr) *Endpoint {
	e := &Endpoint{
		link:     dl,
		local:    local,
		sockets:  make(map[uint8]chan ddp.Datagram),
		nextSock: firstDynamicSocket,
		done:     make(chan struct{}),
	}
	go e.readLoop()
	return e
}

// LocalAddr returns the workstation's asserted address.
func (e *Endpoint) LocalAddr() Addr {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.local
}

// SetLocalNode updates the local node (e.g. after a successful LLAP/AARP claim). The
// LToUDP framer already carries the claimed node; this keeps the endpoint's Src stamp
// consistent for datagrams it builds itself.
func (e *Endpoint) SetLocalNode(network uint16, node uint8) {
	e.mu.Lock()
	e.local.Network = network
	e.local.Node = node
	e.mu.Unlock()
}

// Bind allocates a dynamic reply socket and returns it with a receive channel that the
// read loop delivers matching inbound datagrams to. The caller Unbinds when done.
func (e *Endpoint) Bind() (uint8, <-chan ddp.Datagram) {
	e.mu.Lock()
	defer e.mu.Unlock()
	sock := e.allocSocketLocked()
	ch := make(chan ddp.Datagram, 16)
	e.sockets[sock] = ch
	return sock, ch
}

// BindSocket binds a SPECIFIC socket (e.g. NamesInfoSocket for NBP replies), returning
// its receive channel. If the socket is already bound its existing channel is returned.
func (e *Endpoint) BindSocket(sock uint8) <-chan ddp.Datagram {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ch, ok := e.sockets[sock]; ok {
		return ch
	}
	ch := make(chan ddp.Datagram, 16)
	e.sockets[sock] = ch
	return ch
}

// Unbind releases a socket and closes its channel.
func (e *Endpoint) Unbind(sock uint8) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ch, ok := e.sockets[sock]; ok {
		delete(e.sockets, sock)
		close(ch)
	}
}

// allocSocketLocked returns a free dynamic socket (caller holds e.mu).
func (e *Endpoint) allocSocketLocked() uint8 {
	for i := 0; i < 128; i++ {
		s := e.nextSock
		e.nextSock++
		if e.nextSock == 0 {
			e.nextSock = firstDynamicSocket
		}
		if _, taken := e.sockets[s]; !taken {
			return s
		}
	}
	return firstDynamicSocket
}

// Send stamps a datagram with the local Src address and the given source socket and
// writes it to the link. dst is the destination; srcSocket is the reply socket the
// response should come back to.
func (e *Endpoint) Send(dst Addr, srcSocket, ddpType uint8, data []byte) error {
	e.mu.Lock()
	local := e.local
	e.mu.Unlock()
	d := ddp.Datagram{
		DestNetwork: dst.Network,
		SrcNetwork:  local.Network,
		DestNode:    dst.Node,
		SrcNode:     local.Node,
		DestSocket:  dst.Socket,
		SrcSocket:   srcSocket,
		DDPType:     ddpType,
		Data:        data,
	}
	return e.link.WriteDatagram(d)
}

// readLoop reads datagrams and delivers each to the channel bound to its destination
// socket (dropping datagrams for unbound sockets). It exits on Close or a terminal
// link error.
func (e *Endpoint) readLoop() {
	for {
		d, err := e.link.ReadDatagram()
		if err != nil {
			if errors.Is(err, link.ErrTimeout) {
				select {
				case <-e.done:
					return
				default:
					continue
				}
			}
			return // terminal (ErrClosed or other)
		}
		e.mu.Lock()
		ch, ok := e.sockets[d.DestSocket]
		e.mu.Unlock()
		if !ok {
			continue
		}
		select {
		case ch <- d:
		case <-e.done:
			return
		default:
			// Slow consumer: drop rather than block the whole endpoint. ATP retries
			// cover a dropped response packet.
		}
	}
}

// Close stops the read loop and closes the link.
func (e *Endpoint) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	close(e.done)
	socks := make([]chan ddp.Datagram, 0, len(e.sockets))
	for _, ch := range e.sockets {
		socks = append(socks, ch)
	}
	e.sockets = map[uint8]chan ddp.Datagram{}
	e.mu.Unlock()
	for _, ch := range socks {
		close(ch)
	}
	return e.link.Close()
}

// drain reads and discards any queued datagrams on ch without blocking (used to clear
// stale response packets before a retry).
func drain(ch <-chan ddp.Datagram) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// deadlineTimer returns a timer channel firing after d, or a never-firing channel when
// d <= 0.
func deadlineTimer(d time.Duration) <-chan time.Time {
	if d <= 0 {
		return make(chan time.Time)
	}
	return time.After(d)
}
