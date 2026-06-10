package router

import (
	"context"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// fakePort is a minimal RoutedPort for table/router tests: a named, addressed port that
// records the datagrams sent to it. It lives in-test so core tests stay core-only.
type fakePort struct {
	name           string
	network        uint16
	node           uint8
	netMin, netMax uint16

	unicast   []ddp.Datagram
	broadcast []ddp.Datagram
	multicast []ddp.Datagram
}

func newFakePort(name string, network uint16, node uint8, netMin, netMax uint16) *fakePort {
	return &fakePort{name: name, network: network, node: node, netMin: netMin, netMax: netMax}
}

func (p *fakePort) Name() string                { return p.name }
func (p *fakePort) Start(context.Context) error { return nil }
func (p *fakePort) Stop(context.Context) error  { return nil }
func (p *fakePort) Network() uint16             { return p.network }
func (p *fakePort) Node() uint8                 { return p.node }
func (p *fakePort) NetworkMin() uint16          { return p.netMin }
func (p *fakePort) NetworkMax() uint16          { return p.netMax }
func (p *fakePort) Broadcast(d ddp.Datagram)    { p.broadcast = append(p.broadcast, d) }
func (p *fakePort) Multicast(_ []byte, d ddp.Datagram) {
	p.multicast = append(p.multicast, d)
}
func (p *fakePort) Unicast(network uint16, node uint8, d ddp.Datagram) {
	d.DestNetwork = network
	d.DestNode = node
	p.unicast = append(p.unicast, d)
}

func newTestTable() *RoutingTable {
	zit := NewZoneInformationTable()
	return NewRoutingTable(zit, nil)
}

func TestSetPortRangeInstallsConnectedRoute(t *testing.T) {
	rt := newTestTable()
	p := newFakePort("EtherTalk", 10, 0x80, 10, 12)
	rt.SetPortRange(p, 10, 12)

	for n := uint16(10); n <= 12; n++ {
		e, bad := rt.GetByNetwork(n)
		if e == nil {
			t.Fatalf("network %d: no entry installed", n)
		}
		if e.Distance != 0 {
			t.Errorf("network %d: Distance = %d, want 0 (directly connected)", n, e.Distance)
		}
		if bad {
			t.Errorf("network %d: directly-connected route reported bad", n)
		}
		if !e.ExtendedNetwork {
			t.Errorf("network %d: range 10-12 should be ExtendedNetwork", n)
		}
	}
}

func TestSetPortRangeReplacesPriorRange(t *testing.T) {
	rt := newTestTable()
	p := newFakePort("EtherTalk", 10, 0x80, 10, 12)
	rt.SetPortRange(p, 10, 12)
	rt.SetPortRange(p, 20, 21) // re-claim a different range

	if e, _ := rt.GetByNetwork(10); e != nil {
		t.Errorf("old network 10 still present after re-claim")
	}
	if e, _ := rt.GetByNetwork(20); e == nil {
		t.Errorf("new network 20 missing after re-claim")
	}
	if got := len(rt.Entries()); got != 1 {
		t.Errorf("Entries() = %d, want 1 (one connected route)", got)
	}
}

func TestConsiderLearnedRoute(t *testing.T) {
	rt := newTestTable()
	via := newFakePort("EtherTalk", 10, 0x80, 10, 10)
	rt.SetPortRange(via, 10, 10)

	ok := rt.Consider(&RoutingTableEntry{
		NetworkMin: 50, NetworkMax: 50, Distance: 1, Port: via, NextNetwork: 10, NextNode: 0x81,
	})
	if !ok {
		t.Fatalf("Consider rejected a fresh learned route")
	}
	e, bad := rt.GetByNetwork(50)
	if e == nil || e.Distance != 1 {
		t.Fatalf("learned route not installed correctly: %+v", e)
	}
	if bad {
		t.Errorf("freshly learned route reported bad")
	}
}

func TestAgingWalksGoodToRemoved(t *testing.T) {
	rt := newTestTable()
	via := newFakePort("EtherTalk", 10, 0x80, 10, 10)
	rt.SetPortRange(via, 10, 10)
	rt.Consider(&RoutingTableEntry{
		NetworkMin: 50, NetworkMax: 50, Distance: 1, Port: via, NextNetwork: 10, NextNode: 0x81,
	})

	// Good -> Suspect (still present, not bad)
	rt.Age()
	if _, bad := rt.GetByNetwork(50); bad {
		t.Errorf("after 1 tick (suspect), route reported bad")
	}
	// Suspect -> Bad
	rt.Age()
	if _, bad := rt.GetByNetwork(50); !bad {
		t.Errorf("after 2 ticks (bad), route not reported bad")
	}
	// Bad -> Worst
	rt.Age()
	if e, _ := rt.GetByNetwork(50); e == nil {
		t.Errorf("after 3 ticks (worst), route removed too early")
	}
	// Worst -> removed
	rt.Age()
	if e, _ := rt.GetByNetwork(50); e != nil {
		t.Errorf("after 4 ticks, route should be aged out, got %+v", e)
	}

	// The directly-connected route never ages.
	if e, _ := rt.GetByNetwork(10); e == nil {
		t.Errorf("directly-connected route aged out (it must not)")
	}
}

func TestConsiderResetsAgingToGood(t *testing.T) {
	rt := newTestTable()
	via := newFakePort("EtherTalk", 10, 0x80, 10, 10)
	rt.SetPortRange(via, 10, 10)
	learned := &RoutingTableEntry{
		NetworkMin: 50, NetworkMax: 50, Distance: 1, Port: via, NextNetwork: 10, NextNode: 0x81,
	}
	rt.Consider(learned)
	rt.Age() // -> suspect
	rt.Age() // -> bad
	if _, bad := rt.GetByNetwork(50); !bad {
		t.Fatalf("precondition: route should be bad before refresh")
	}
	// Receiving the route again resets it to good.
	rt.Consider(&RoutingTableEntry{
		NetworkMin: 50, NetworkMax: 50, Distance: 1, Port: via, NextNetwork: 10, NextNode: 0x81,
	})
	if _, bad := rt.GetByNetwork(50); bad {
		t.Errorf("route still bad after a refreshing Consider")
	}
}

func TestRemoveEntriesForPortWithdrawsAll(t *testing.T) {
	rt := newTestTable()
	a := newFakePort("EtherTalk", 10, 0x80, 10, 10)
	b := newFakePort("LToUDP", 20, 0x80, 20, 20)
	rt.SetPortRange(a, 10, 10)
	rt.SetPortRange(b, 20, 20)
	// A remote network learned via port a.
	rt.Consider(&RoutingTableEntry{
		NetworkMin: 99, NetworkMax: 99, Distance: 1, Port: a, NextNetwork: 10, NextNode: 0x81,
	})

	rt.RemoveEntriesForPort(a)

	if e, _ := rt.GetByNetwork(10); e != nil {
		t.Errorf("connected route via removed port still present")
	}
	if e, _ := rt.GetByNetwork(99); e != nil {
		t.Errorf("learned route via removed port still present")
	}
	if e, _ := rt.GetByNetwork(20); e == nil {
		t.Errorf("unrelated port b's route was withdrawn")
	}
}

func TestSnapshotReportsState(t *testing.T) {
	rt := newTestTable()
	via := newFakePort("EtherTalk", 10, 0x80, 10, 10)
	rt.SetPortRange(via, 10, 10)
	rt.Consider(&RoutingTableEntry{
		NetworkMin: 50, NetworkMax: 50, Distance: 1, Port: via, NextNetwork: 10, NextNode: 0x81,
	})
	rt.Age() // learned -> suspect; connected stays good

	states := map[uint16]string{}
	for _, s := range rt.Snapshot() {
		states[s.Entry.NetworkMin] = s.State
	}
	if states[10] != "good" {
		t.Errorf("connected route state = %q, want good", states[10])
	}
	if states[50] != "suspect" {
		t.Errorf("learned route state = %q, want suspect", states[50])
	}
}
