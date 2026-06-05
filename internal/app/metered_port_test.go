package app

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/pkg/metrics"
	"github.com/ObsoleteMadness/ClassicStack/port"
	"github.com/ObsoleteMadness/ClassicStack/protocol/ddp"
)

// fakeMeteredPort is a minimal port.Port that also implements
// port.TrafficMetered so attachPortMeter installs an observer it can drive.
type fakeMeteredPort struct {
	obs port.TrafficObserver
}

func (f *fakeMeteredPort) ShortString() string                  { return "fake" }
func (f *fakeMeteredPort) Start(port.RouterHooks) error         { return nil }
func (f *fakeMeteredPort) Stop() error                          { return nil }
func (f *fakeMeteredPort) Unicast(uint16, uint8, ddp.Datagram)  {}
func (f *fakeMeteredPort) Broadcast(ddp.Datagram)               {}
func (f *fakeMeteredPort) Multicast([]byte, ddp.Datagram)       {}
func (f *fakeMeteredPort) SetNetworkRange(uint16, uint16) error { return nil }
func (f *fakeMeteredPort) Network() uint16                      { return 0 }
func (f *fakeMeteredPort) Node() uint8                          { return 0 }
func (f *fakeMeteredPort) NetworkMin() uint16                   { return 0 }
func (f *fakeMeteredPort) NetworkMax() uint16                   { return 0 }
func (f *fakeMeteredPort) ExtendedNetwork() bool                { return false }

func (f *fakeMeteredPort) SetTrafficObserver(obs port.TrafficObserver) { f.obs = obs }

// plainPort implements port.Port but NOT port.TrafficMetered, to verify
// attachPortMeter returns nil for un-meterable ports.
type plainPort struct{}

func (plainPort) ShortString() string                  { return "plain" }
func (plainPort) Start(port.RouterHooks) error         { return nil }
func (plainPort) Stop() error                          { return nil }
func (plainPort) Unicast(uint16, uint8, ddp.Datagram)  {}
func (plainPort) Broadcast(ddp.Datagram)               {}
func (plainPort) Multicast([]byte, ddp.Datagram)       {}
func (plainPort) SetNetworkRange(uint16, uint16) error { return nil }
func (plainPort) Network() uint16                      { return 0 }
func (plainPort) Node() uint8                          { return 0 }
func (plainPort) NetworkMin() uint16                   { return 0 }
func (plainPort) NetworkMax() uint16                   { return 0 }
func (plainPort) ExtendedNetwork() bool                { return false }

// collectSink records every sample written to it for assertion.
type collectSink struct{ samples map[string]metrics.Sample }

func (s *collectSink) Write(sample metrics.Sample) { s.samples[sample.Name] = sample }

// TestPortMeterCountsTxRx verifies the meter accumulates sent and received
// traffic from the observer and publishes the namespaced counter metrics the
// dashboard reads.
func TestPortMeterCountsTxRx(t *testing.T) {
	sink := &collectSink{samples: map[string]metrics.Sample{}}
	metrics.Default.AddSink(sink)

	p := &fakeMeteredPort{}
	m := attachPortMeter("EtherTalk", p)
	if m == nil {
		t.Fatal("attachPortMeter returned nil for a TrafficMetered port")
	}
	if p.obs == nil {
		t.Fatal("observer was not installed on the port")
	}

	// Two sent datagrams (30 + 40 wire bytes) and one received (18 wire bytes).
	p.obs(port.Tx, 30)
	p.obs(port.Tx, 40)
	p.obs(port.Rx, 18)

	m.publish()

	want := map[string]int64{
		"unit:EtherTalk:tx.packets": 2,
		"unit:EtherTalk:tx.bytes":   70,
		"unit:EtherTalk:rx.packets": 1,
		"unit:EtherTalk:rx.bytes":   18,
	}
	for name, v := range want {
		got, ok := sink.samples[name]
		if !ok {
			t.Fatalf("missing sample %q", name)
		}
		if got.Value != v {
			t.Fatalf("%s = %d, want %d", name, got.Value, v)
		}
		if got.Kind != metrics.KindCounter {
			t.Fatalf("%s kind = %v, want counter", name, got.Kind)
		}
	}
}

// TestAttachPortMeterPlainPort verifies a port without TrafficMetered yields a
// nil meter (it simply reports no throughput).
func TestAttachPortMeterPlainPort(t *testing.T) {
	if m := attachPortMeter("X", &plainPort{}); m != nil {
		t.Fatal("attachPortMeter should return nil for a non-metered port")
	}
}
