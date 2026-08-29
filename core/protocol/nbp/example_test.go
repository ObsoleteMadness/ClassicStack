package nbp_test

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/nbp"
)

// ExampleBuildLkUp builds a name-lookup request for "*:AFPServer@*" (any AFP
// server in the requester's zone) — the packet a discovery client broadcasts,
// and the shape ParsePacket decodes on the responder side.
func ExampleBuildLkUp() {
	req := nbp.BuildLkUp(nbp.CtrlLkUp, 1, 0xFF00, 10, 4,
		[]byte{nbp.NameWildcard}, []byte("AFPServer"), []byte{nbp.ZoneWildcard})

	pkt, err := nbp.ParsePacket(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("fn=%d obj=%s typ=%s zone=%s\n",
		pkt.Function, pkt.Tuple.Object, pkt.Tuple.Type, pkt.Tuple.Zone)
	// Output: fn=2 obj== typ=AFPServer zone=*
}

// ExampleBuildLkUpRply builds the reply a responder sends back to a matching
// lookup: the resolved network/node/socket plus the entity's name.
func ExampleBuildLkUpRply() {
	rply := nbp.BuildLkUpRply(1, 0xFF00, 10, 4,
		[]byte("MemFS"), []byte("AFPServer"), []byte("Demo Zone"))

	pkt, err := nbp.ParsePacket(rply)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%s:%s@%s -> net=%#04x node=%d socket=%d\n",
		pkt.Tuple.Object, pkt.Tuple.Type, pkt.Tuple.Zone,
		pkt.Tuple.Network, pkt.Tuple.Node, pkt.Tuple.Socket)
	// Output: MemFS:AFPServer@Demo Zone -> net=0xff00 node=10 socket=4
}
