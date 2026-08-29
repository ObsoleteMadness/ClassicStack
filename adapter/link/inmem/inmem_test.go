package inmem

import (
	"errors"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

func TestPairLoopback(t *testing.T) {
	a, b := Pair(1)
	defer a.Close()

	if err := a.Write([]byte{1, 2, 3}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := b.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Fatalf("Read = %v, want [1 2 3]", got)
	}
}

func TestWriteDoesNotRetainSlice(t *testing.T) {
	a, b := Pair(1)
	defer a.Close()

	buf := []byte{9}
	if err := a.Write(buf); err != nil {
		t.Fatalf("Write: %v", err)
	}
	buf[0] = 0 // mutate after Write; the peer must see the original value
	got, err := b.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got[0] != 9 {
		t.Fatalf("Write retained caller slice: got %d, want 9", got[0])
	}
}

func TestCloseIsTerminalAndIdempotent(t *testing.T) {
	a, b := Pair(1)
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close (idempotent): %v", err)
	}
	if _, err := a.Read(); !errors.Is(err, link.ErrClosed) {
		t.Fatalf("Read after Close = %v, want ErrClosed", err)
	}
	if err := b.Write([]byte{1}); !errors.Is(err, link.ErrClosed) {
		t.Fatalf("Write after peer Close = %v, want ErrClosed", err)
	}
}

func TestLoopbackEcho(t *testing.T) {
	l := Loopback(1)
	defer l.Close()
	if err := l.Write([]byte{7}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := l.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got[0] != 7 {
		t.Fatalf("Loopback Read = %v, want [7]", got)
	}
}
