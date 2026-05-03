package llap

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/protocol/ddp"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/port/localtalk"
)

func TestDirectedTransmitLogsRetryAndBackoff(t *testing.T) {
	p := localtalk.New(1, []byte("Test"), true, 0x44)
	p.SetSupportsRTSCTS(true)
	p.ClaimNode(0x44)

	var sent [][]byte
	p.ConfigureSendFrame(func(frame []byte) error {
		sent = append(sent, append([]byte(nil), frame...))
		return nil
	})

	svc := New()
	st := &portState{
		port:         p,
		claimed:      true,
		stop:         make(chan struct{}),
		lastActivity: time.Now().Add(-time.Second),
	}

	oldWriter := log.Writer()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(oldWriter)
	netlog.SetLevel(netlog.LevelDebug)

	d, err := p.BuildDataFrame(0x22, ddp.Datagram{
		DestinationNetwork: 1,
		SourceNetwork:      1,
		DestinationNode:    0x22,
		SourceNode:         0x44,
		DestinationSocket:  4,
		SourceSocket:       4,
		DDPType:            4,
		Data:               []byte{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("BuildDataFrame: %v", err)
	}

	err = svc.runDirectedTransmit(st, d)
	if err == nil {
		t.Fatal("runDirectedTransmit error = nil, want retry exhaustion")
	}
	if len(sent) == 0 {
		t.Fatal("expected at least one LLAP frame to be sent")
	}

	out := buf.String()
	if !strings.Contains(out, "CTS timeout retry=") {
		t.Fatalf("missing retry log in %q", out)
	}
	if !strings.Contains(out, "local-backoff=") {
		t.Fatalf("missing backoff log in %q", out)
	}
	if !strings.Contains(out, "transmit failed after") {
		t.Fatalf("missing retry exhaustion log in %q", out)
	}
}

func TestDatagramTransmitSkipsRTSCTSForSharedMedium(t *testing.T) {
	p := localtalk.New(1, []byte("Test"), true, 0x44)
	p.ClaimNode(0x44)

	var sent [][]byte
	p.ConfigureSendFrame(func(frame []byte) error {
		sent = append(sent, append([]byte(nil), frame...))
		return nil
	})

	svc := New()
	st := &portState{
		port:         p,
		claimed:      true,
		stop:         make(chan struct{}),
		lastActivity: time.Now().Add(-time.Second),
	}

	d, err := p.BuildDataFrame(0x22, ddp.Datagram{
		DestinationNetwork: 1,
		SourceNetwork:      1,
		DestinationNode:    0x22,
		SourceNode:         0x44,
		DestinationSocket:  6,
		SourceSocket:       6,
		DDPType:            6,
		Data:               []byte{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("BuildDataFrame: %v", err)
	}

	if err := svc.runDatagramTransmit(st, d); err != nil {
		t.Fatalf("runDatagramTransmit: %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("expected 1 LLAP frame, got %d", len(sent))
	}
	frame, err := localtalk.LLAPFrameFromBytes(sent[0])
	if err != nil {
		t.Fatalf("LLAPFrameFromBytes: %v", err)
	}
	if frame.Type == localtalk.LLAPTypeRTS || frame.Type == localtalk.LLAPTypeCTS {
		t.Fatalf("expected data frame, got control type 0x%02x", frame.Type)
	}
}

func TestDatagramTransmitPacesSequentialFrames(t *testing.T) {
	p := localtalk.New(1, []byte("Test"), true, 0x44)
	p.ClaimNode(0x44)
	p.ConfigureSendFrame(func(frame []byte) error { return nil })

	svc := New()
	st := &portState{
		port:         p,
		claimed:      true,
		stop:         make(chan struct{}),
		lastActivity: time.Now().Add(-time.Second),
	}

	frame := localtalk.LLAPFrame{
		DestinationNode: 0x22,
		SourceNode:      0x44,
		Type:            localtalk.LLAPTypeAppleTalkShortHeader,
		Payload:         make([]byte, 578),
	}

	if err := svc.runDatagramTransmit(st, frame); err != nil {
		t.Fatalf("first runDatagramTransmit: %v", err)
	}

	start := time.Now()
	if err := svc.runDatagramTransmit(st, frame); err != nil {
		t.Fatalf("second runDatagramTransmit: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Fatalf("expected LLAP pacing on sequential frames, got %v", elapsed)
	}
	if st.deferHistory == 0 {
		t.Fatal("expected sequential busy-link pacing to set defer history")
	}
}

func TestInboundFrameDropsMalformedControlFrame(t *testing.T) {
	p := localtalk.New(1, []byte("Test"), true, 0x44)
	p.ClaimNode(0x44)

	svc := New()
	svc.InboundFrame(p, localtalk.LLAPFrame{
		DestinationNode: 0x44,
		SourceNode:      0x22,
		Type:            localtalk.LLAPTypeCTS,
		Payload:         []byte{0x01},
	})

	svc.mu.Lock()
	_, exists := svc.ports[p]
	svc.mu.Unlock()
	if exists {
		t.Fatal("malformed frame should be dropped before LLAP state is touched")
	}
}
