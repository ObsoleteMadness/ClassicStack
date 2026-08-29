package diag

import (
	"errors"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/ethertalk"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/aarp"
)

// fakeSource is a minimal componentSource: a name→component map plus the build order.
type fakeSource struct {
	comps map[string]component.Component
	order []string
}

func (f *fakeSource) Component(name string) component.Component { return f.comps[name] }
func (f *fakeSource) Built() []string                           { return f.order }

// newEtherTalkPort builds an inert (enabled, no frame) EtherTalk port under instance name
// and seeds its AARP-table source with the given entries.
func newEtherTalkPort(t *testing.T, name string, entries []aarp.Entry) *ethertalk.Port {
	t.Helper()
	sec := &port.Section{SKey: ethertalk.Name, Name: name, IsEnabled: true}
	logger := log.New(name, log.NewStderrSink(log.NewLevelVar(log.Info)))
	comp, err := ethertalk.NewInstance(sec, nil, nil, nil, logger)
	if err != nil {
		t.Fatalf("NewInstance(%q): %v", name, err)
	}
	p, ok := comp.(*ethertalk.Port)
	if !ok {
		t.Fatalf("NewInstance(%q) returned %T, want *ethertalk.Port", name, comp)
	}
	p.SetAARPTableSource(func() []aarp.Entry { return entries })
	return p
}

// TestAARPTableUnavailable proves the probe reports ErrUnavailable when no EtherTalk port
// was built (a nil source, or a source with no EtherTalk component).
func TestAARPTableUnavailable(t *testing.T) {
	if _, err := New(nil).AARPTable(); !errors.Is(err, control.ErrUnavailable) {
		t.Fatalf("nil source AARPTable err = %v, want ErrUnavailable", err)
	}
	empty := New(&fakeSource{comps: map[string]component.Component{}})
	if _, err := empty.AARPTable(); !errors.Is(err, control.ErrUnavailable) {
		t.Fatalf("no-EtherTalk AARPTable err = %v, want ErrUnavailable", err)
	}
}

// TestAARPTableDecodesAndSorts proves the provider collects every EtherTalk instance's AMT,
// decodes the MAC to colon-hex, tags each row with its port, and sorts by port/network/node.
func TestAARPTableDecodesAndSorts(t *testing.T) {
	one := newEtherTalkPort(t, "EtherTalk", []aarp.Entry{
		{Addr: aarp.ProtoAddr{Network: 0xFE02, Node: 0x10}, HW: mac(0xAA, 0xBB, 0xCC, 0x00, 0x11, 0x22), Seen: 5},
		{Addr: aarp.ProtoAddr{Network: 0xFE01, Node: 0x20}, HW: mac(0x01, 0x02, 0x03, 0x04, 0x05, 0x06), Seen: 6},
	})
	two := newEtherTalkPort(t, "EtherTalk2", []aarp.Entry{
		{Addr: aarp.ProtoAddr{Network: 0xFE01, Node: 0x05}, HW: mac(0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01), Seen: 7},
	})
	src := &fakeSource{
		comps: map[string]component.Component{"EtherTalk": one, "EtherTalk2": two},
		order: []string{"EtherTalk", "EtherTalk2"},
	}

	got, err := New(src).AARPTable()
	if err != nil {
		t.Fatalf("AARPTable: %v", err)
	}
	want := []AARPEntry{
		{Port: "EtherTalk", Network: 0xFE01, Node: 0x20, MAC: "01:02:03:04:05:06", SeenNs: 6},
		{Port: "EtherTalk", Network: 0xFE02, Node: 0x10, MAC: "aa:bb:cc:00:11:22", SeenNs: 5},
		{Port: "EtherTalk2", Network: 0xFE01, Node: 0x05, MAC: "de:ad:be:ef:00:01", SeenNs: 7},
	}
	if len(got) != len(want) {
		t.Fatalf("AARPTable = %d rows, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// mac builds a 6-byte MAC.
func mac(b ...byte) [6]byte {
	var m [6]byte
	copy(m[:], b)
	return m
}
