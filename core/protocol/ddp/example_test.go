package ddp_test

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// ExampleDatagram_Encode builds one DDP datagram and encodes it to its
// long-header wire form, the shape a link-layer framer wraps in a frame
// before sending it.
func ExampleDatagram_Encode() {
	dg := ddp.Datagram{
		DestNetwork: 0x1234,
		SrcNetwork:  0x1234,
		DestNode:    10,
		SrcNode:     20,
		DestSocket:  4, // ZIP
		SrcSocket:   4,
		DDPType:     6, // ZIP
		Data:        []byte("hello"),
	}

	wire, err := dg.Encode(nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(len(wire))
	// Output: 18
}

// ExampleDecode parses a long-header datagram back out of its wire form —
// the inverse of Encode, as a link-layer reader does on ingress.
func ExampleDecode() {
	dg := ddp.Datagram{
		DestNetwork: 0x1234,
		SrcNetwork:  0x1234,
		DestNode:    10,
		SrcNode:     20,
		DestSocket:  4,
		SrcSocket:   4,
		DDPType:     6,
		Data:        []byte("hello"),
	}
	wire, err := dg.Encode(nil)
	if err != nil {
		fmt.Println(err)
		return
	}

	got, err := ddp.Decode(wire)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%d -> %d: %s\n", got.SrcNode, got.DestNode, got.Data)
	// Output: 20 -> 10: hello
}
