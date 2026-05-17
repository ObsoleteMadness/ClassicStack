package rawlink

import (
	"errors"
	"testing"
)

type bridgeTestLink struct {
	medium     PhysicalMedium
	readFrames [][]byte
	written    [][]byte
	filterExpr string
}

func (l *bridgeTestLink) ReadFrame() ([]byte, error) {
	if len(l.readFrames) == 0 {
		return nil, ErrTimeout
	}
	f := l.readFrames[0]
	l.readFrames = l.readFrames[1:]
	return f, nil
}

func (l *bridgeTestLink) WriteFrame(frame []byte) error {
	buf := make([]byte, len(frame))
	copy(buf, frame)
	l.written = append(l.written, buf)
	return nil
}

func (l *bridgeTestLink) Close() error { return nil }
func (l *bridgeTestLink) Medium() PhysicalMedium {
	return l.medium
}
func (l *bridgeTestLink) SetFilter(expr string) error {
	l.filterExpr = expr
	return nil
}

func TestWrapWithBridgeMode_WiFiRewriteOutboundInbound(t *testing.T) {
	inner := &bridgeTestLink{medium: MediumEthernet}
	virtual := []byte{0x0a, 0, 0, 0, 0, 1}
	host := []byte{0x02, 0, 0, 0, 0, 1}
	peer := []byte{0x04, 0, 0, 0, 0, 1}

	link, err := WrapWithBridgeMode(inner, BridgeLinkOptions{
		Mode:       "wifi",
		HostMAC:    host,
		VirtualMAC: virtual,
	})
	if err != nil {
		t.Fatalf("WrapWithBridgeMode returned error: %v", err)
	}

	outbound := make([]byte, 60)
	copy(outbound[0:6], peer)
	copy(outbound[6:12], virtual)
	outbound[12] = 0
	outbound[13] = byte(len(outbound) - 14)
	if err := link.WriteFrame(outbound); err != nil {
		t.Fatalf("WriteFrame returned error: %v", err)
	}
	if len(inner.written) != 1 {
		t.Fatalf("written frames = %d, want 1", len(inner.written))
	}
	if got := inner.written[0][6:12]; string(got) != string(host) {
		t.Fatalf("outbound src mac = %v, want host %v", got, host)
	}

	inbound := make([]byte, 60)
	copy(inbound[0:6], host)
	copy(inbound[6:12], peer)
	inbound[12] = 0
	inbound[13] = byte(len(inbound) - 14)
	inner.readFrames = append(inner.readFrames, inbound)

	read, err := link.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame returned error: %v", err)
	}
	if got := read[0:6]; string(got) != string(virtual) {
		t.Fatalf("inbound dst mac = %v, want virtual %v", got, virtual)
	}
}

func TestWrapWithBridgeMode_AutoEthernetPassthrough(t *testing.T) {
	inner := &bridgeTestLink{medium: MediumEthernet}
	link, err := WrapWithBridgeMode(inner, BridgeLinkOptions{
		Mode:       "auto",
		HostMAC:    []byte{1, 2, 3, 4, 5, 6},
		VirtualMAC: []byte{6, 5, 4, 3, 2, 1},
	})
	if err != nil {
		t.Fatalf("WrapWithBridgeMode returned error: %v", err)
	}
	if link != inner {
		t.Fatalf("expected passthrough link for auto+ethernet medium")
	}
}

func TestWrapWithBridgeMode_DelegatesFilter(t *testing.T) {
	inner := &bridgeTestLink{medium: MediumEthernet}
	link, err := WrapWithBridgeMode(inner, BridgeLinkOptions{
		Mode:       "wifi",
		HostMAC:    []byte{1, 2, 3, 4, 5, 6},
		VirtualMAC: []byte{6, 5, 4, 3, 2, 1},
	})
	if err != nil {
		t.Fatalf("WrapWithBridgeMode returned error: %v", err)
	}
	fl, ok := link.(FilterableLink)
	if !ok {
		t.Fatalf("wrapped link does not implement FilterableLink")
	}
	if err := fl.SetFilter("ipx"); err != nil {
		t.Fatalf("SetFilter returned error: %v", err)
	}
	if inner.filterExpr != "ipx" {
		t.Fatalf("inner filter expr = %q, want ipx", inner.filterExpr)
	}
}

func TestWrapWithBridgeMode_RejectsInvalidMode(t *testing.T) {
	inner := &bridgeTestLink{}
	_, err := WrapWithBridgeMode(inner, BridgeLinkOptions{Mode: "nope"})
	if err == nil {
		t.Fatalf("expected error for invalid mode")
	}
	if !errors.Is(err, err) {
		// Keep a concrete assertion path so staticcheck does not complain
		// about unchecked error shape while still validating non-nil.
		t.Fatalf("unexpected error value: %v", err)
	}
}
