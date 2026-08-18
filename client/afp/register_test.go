package afp

import (
	"context"
	"errors"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/client"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
)

func TestConnectTCPReturnsDSIError(t *testing.T) {
	opener := clientlink.NewOpener(clientlink.Spec{Kind: clientlink.KindTCP, Name: "127.0.0.1"})
	_, err := connect(context.Background(), uri.Target{Scheme: "afp", Server: "127.0.0.1"}, client.Options{Opener: opener})
	if !errors.Is(err, errDSINotImplemented) {
		t.Fatalf("err = %v, want DSI-not-implemented", err)
	}
}
