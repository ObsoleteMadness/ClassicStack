package atalk

import (
	"errors"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/service/aep"
)

// echo.go is the client-side AppleTalk Echo Protocol (AEP) requester over an Endpoint:
// it sends an echo REQUEST to a node and waits for the matching REPLY, the AppleTalk
// analogue of ping. The server ring only has the AEP responder (core/service/aep); this
// is the requester half, so the csecho probe stands on the same Endpoint (and the same
// verbose trace) every other client transport uses instead of hand-rolling the DDP send
// and receive loop.
//
// AEP (Inside Macintosh: Networking, ch. 3): DDP type 4 on socket 4. A request carries
// command byte aep.CmdRequest (1) followed by an arbitrary payload; the responder
// reflects it with command byte aep.CmdReply (2) and the identical payload.

// ErrEchoTimeout is returned by Echo when no matching reply arrives within the timeout.
var ErrEchoTimeout = errors.New("atalk: AEP echo timed out")

// Echo sends one AEP echo request to dst carrying payload and returns the reply's echoed
// payload (the bytes after the command byte) and the node that answered. It binds the AEP
// socket (4) for the reply — the responder returns the reply to the request's source
// socket, and AEP's well-known socket is the source — and filters inbound datagrams for a
// CmdReply addressed to us, ignoring our own request (which carries CmdRequest) and
// unrelated traffic. A dst node of 0xFF broadcasts the request to every node on the
// segment; the first matching reply wins.
func (e *Endpoint) Echo(dst Addr, payload []byte, timeout time.Duration) (reply []byte, from Addr, err error) {
	dst.Socket = aep.Socket
	ch := e.BindSocket(aep.Socket)
	defer e.Unbind(aep.Socket)

	local := e.LocalAddr()
	tracef("AEP request → %s (%d bytes payload)", dst, len(payload))
	req := append([]byte{aep.CmdRequest}, payload...)
	if err := e.Send(dst, aep.Socket, aep.DDPType, req); err != nil {
		return nil, Addr{}, err
	}

	deadline := time.After(timeout)
	for {
		select {
		case d, ok := <-ch:
			if !ok {
				return nil, Addr{}, errors.New("atalk: endpoint closed")
			}
			if d.DDPType != aep.DDPType || len(d.Data) == 0 || d.Data[0] != aep.CmdReply {
				continue // not an echo reply (skips our own CmdRequest)
			}
			if d.DestNode != local.Node && d.DestNode != nbpBroadcastNode {
				continue // a reply meant for some other requester
			}
			from = Addr{Network: d.SrcNetwork, Node: d.SrcNode, Socket: d.SrcSocket}
			tracef("AEP reply ← %s (%d bytes)", from, len(d.Data)-1)
			return append([]byte(nil), d.Data[1:]...), from, nil
		case <-deadline:
			return nil, Addr{}, ErrEchoTimeout
		}
	}
}
