package atalk_test

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/client/atalk"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// ExampleAddr formats an AppleTalk internet address the way trace/log output
// does: network.node:socket.
func ExampleAddr() {
	a := atalk.Addr{Network: 0xFF00, Node: 10, Socket: 4}
	fmt.Println(a)
	// Output: 65280.10:4
}

// loopbackLink is a minimal link.DatagramLink that echoes every write straight
// back to its own reader — just enough plumbing for ExampleEndpoint_Send to
// show a real Send/receive round trip without a physical link.
type loopbackLink struct{ ch chan ddp.Datagram }

func (l *loopbackLink) ReadDatagram() (ddp.Datagram, error) { return <-l.ch, nil }
func (l *loopbackLink) WriteDatagram(d ddp.Datagram) error  { l.ch <- d; return nil }
func (l *loopbackLink) Close() error                        { close(l.ch); return nil }

var _ link.DatagramLink = (*loopbackLink)(nil)

// ExampleEndpoint_Send builds an Endpoint over a loopback DatagramLink, binds a
// reply socket, sends a datagram to its own address, and reads it back — the
// same Bind/Send/receive shape a real client uses against a wire transport.
func ExampleEndpoint_Send() {
	ll := &loopbackLink{ch: make(chan ddp.Datagram, 1)}
	ep := atalk.NewEndpoint(ll, atalk.Addr{Network: 1, Node: 10})
	defer ep.Close()

	sock, replies := ep.Bind()
	dst := atalk.Addr{Network: 1, Node: 10, Socket: sock}
	if err := ep.Send(dst, sock, 6, []byte("hello")); err != nil {
		fmt.Println(err)
		return
	}

	got := <-replies
	fmt.Println(string(got.Data))
	// Output: hello
}
