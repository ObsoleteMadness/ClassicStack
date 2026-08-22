package afp

import (
	"context"
	"net"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/client"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
)

// TestConnectTCPRejectsUnreachableHost is a narrow regression guard: the TCP/DSI dial
// path (dialAndLoginDSI) must actually attempt a real dial and surface its error,
// rather than silently falling back to something else or hanging. The full DSI
// client↔server round trip (dial, login, volume open, file ops) is proven by
// test/e2e's "afp/dsi" case, which exercises the real client/dsi.Session against a
// real afp.Service — that is a far stronger guarantee than anything a unit test in
// this package could add without duplicating that harness.
func TestConnectTCPRejectsUnreachableHost(t *testing.T) {
	// Port 0 on loopback is never a live listener; net.Dial fails fast rather than
	// hanging, so this test does not need a timeout/deadline dance.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close() // closed immediately: nothing is listening on addr by the time we dial

	opener := clientlink.NewOpener(clientlink.Spec{Kind: clientlink.KindTCP, Name: addr})
	_, err = connect(context.Background(), uri.Target{Scheme: "afp", Server: "irrelevant"}, client.Options{Opener: opener})
	if err == nil {
		t.Fatal("expected a dial error against a closed port, got nil")
	}
}
