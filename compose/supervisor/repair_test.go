package supervisor

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// failingComp models a NIC-bound port whose Start fails while the device it was BUILT
// against does not exist (the missing-pcap-device case). openErr is what Start returns;
// a rebuild produces a fresh instance with whatever error the model now implies, so a
// test can flip it from "device missing" to "device present" the way correcting the
// interface entry does.
type failingComp struct {
	name    string
	log     *orderLog
	startEr error
	enabled bool
	running bool
}

func (c *failingComp) Name() string { return c.name }
func (c *failingComp) Start(context.Context) error {
	c.log.add("start:" + c.name)
	if c.startEr != nil {
		return c.startEr
	}
	c.running = true
	return nil
}
func (c *failingComp) Stop(context.Context) error {
	c.log.add("stop:" + c.name)
	c.running = false
	return nil
}
func (c *failingComp) Enabled() bool { return c.enabled }

var (
	_ component.Component  = (*failingComp)(nil)
	_ component.Enableable = (*failingComp)(nil)
)

// TestInterfaceRepairStartsPortThatFailedToStart is the reported scenario end to end: a
// config names a pcap device that does not exist, so IPX/NetBEUI/EtherTalk fail to start
// at boot; the operator corrects the device in the web UI, and the ports must come up
// WITHOUT a process restart.
//
// Before the fix this failed twice over. reconcileInterfaceRefsLocked offered the port a
// hot ApplyConfig it always accepted (its section still names the same interface), so
// nothing rebuilt; and even on the restart path restartNodeLocked only restarted a node
// that "was running" — which a port that never started never was.
func TestInterfaceRepairStartsPortThatFailedToStart(t *testing.T) {
	m := config.NewModel()
	m.SetInterface(config.InterfaceSection{Name: "br-lan", Kind: config.IfaceKindNIC, Device: "nosuchdev"})
	m.AddInstance(namedIfaceSection{key: "IPX", iface: config.InterfaceSection{Name: "br-lan"}})

	log := &orderLog{}
	s := New(m, nil)
	// The component built against the missing device: Start fails.
	broken := &failingComp{name: "IPX", log: log, enabled: true, startEr: errors.New("pcap: no such device nosuchdev")}
	// The rebuild the corrected interface produces: a fresh object that opens cleanly.
	fixed := &failingComp{name: "IPX", log: log, enabled: true}
	rebuilt := 0
	s.AddBuildable(broken, nil, func(*config.Model) (component.Component, error) {
		rebuilt++
		return fixed, nil
	})

	// Boot: the port fails to start, the rest of the stack comes up anyway.
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if st := s.Status(); len(st) != 1 || st[0].Running || st[0].Error == "" {
		t.Fatalf("after boot want the port stopped with an error, got %+v", st)
	}

	// The operator points the bridge at the right NIC.
	if err := s.SetInterface(context.Background(), config.InterfaceSection{
		Name: "br-lan", Kind: config.IfaceKindNIC, Backend: config.IfaceBackendPcap, Device: "en1",
	}); err != nil {
		t.Fatalf("SetInterface: %v", err)
	}

	if rebuilt != 1 {
		t.Errorf("rebuilt %d times, want 1 — the port must be reconstructed to re-resolve its device", rebuilt)
	}
	if !fixed.running {
		t.Error("the rebuilt port was never started; the repaired config never went live")
	}
	st := s.Status()
	if len(st) != 1 || !st[0].Running || st[0].Error != "" {
		t.Fatalf("after repair want the port running and clear of errors, got %+v", st)
	}
	// The failed Start is retried on the REBUILT object, so the broken one is never
	// started again.
	if got, want := log.snapshot(), []string{"start:IPX", "start:IPX"}; !slices.Equal(got, want) {
		t.Errorf("lifecycle = %v, want %v (no Stop: the port was never running)", got, want)
	}
}

// TestReconfigureLeavesDeliberatelyStoppedPortDown guards the other half of the retry
// rule: only a node whose last Start FAILED is re-armed. A node the operator stopped from
// the UI has no start error, so a later reconfigure rebuilds it but leaves it down —
// silently restarting it would override an explicit operator decision.
func TestReconfigureLeavesDeliberatelyStoppedPortDown(t *testing.T) {
	m := config.NewModel()
	m.SetInterface(config.InterfaceSection{Name: "br-lan", Kind: config.IfaceKindNIC})
	m.AddInstance(namedIfaceSection{key: "IPX", iface: config.InterfaceSection{Name: "br-lan"}})

	log := &orderLog{}
	s := New(m, nil)
	c := &failingComp{name: "IPX", log: log, enabled: true}
	next := &failingComp{name: "IPX", log: log, enabled: true}
	s.AddBuildable(c, nil, func(*config.Model) (component.Component, error) { return next, nil })

	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if err := s.Stop(context.Background(), "IPX"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := s.SetInterface(context.Background(), config.InterfaceSection{
		Name: "br-lan", Kind: config.IfaceKindNIC, Backend: config.IfaceBackendPcap, Device: "en1",
	}); err != nil {
		t.Fatalf("SetInterface: %v", err)
	}
	if next.running {
		t.Error("a deliberately stopped port was restarted by an interface edit")
	}
	if got, want := log.snapshot(), []string{"start:IPX", "stop:IPX"}; !slices.Equal(got, want) {
		t.Errorf("lifecycle = %v, want %v", got, want)
	}
}

// TestReconfigureRewiresRebuiltPort proves the cross-wiring seam follows the rebuild: the
// supervisor hands the compose root both the object it replaced and its replacement, so
// the mini-routers and the AppleTalk router can move their wires. Without the prev half
// the stale object stays at the head of a mini-router's port list and swallows every
// outbound frame.
func TestReconfigureRewiresRebuiltPort(t *testing.T) {
	m := config.NewModel()
	m.SetInterface(config.InterfaceSection{Name: "br-lan", Kind: config.IfaceKindNIC})
	m.AddInstance(namedIfaceSection{key: "IPX", iface: config.InterfaceSection{Name: "br-lan"}})

	log := &orderLog{}
	s := New(m, nil)
	before := &failingComp{name: "IPX", log: log, enabled: true}
	after := &failingComp{name: "IPX", log: log, enabled: true}
	s.AddBuildable(before, nil, func(*config.Model) (component.Component, error) { return after, nil })

	type swap struct{ prev, next component.Component }
	var swaps []swap
	s.SetTransportAttacher(func(prev, next component.Component) {
		swaps = append(swaps, swap{prev, next})
	})

	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if err := s.SetInterface(context.Background(), config.InterfaceSection{
		Name: "br-lan", Kind: config.IfaceKindNIC, Backend: config.IfaceBackendPcap, Device: "en1",
	}); err != nil {
		t.Fatalf("SetInterface: %v", err)
	}

	if len(swaps) != 1 {
		t.Fatalf("attacher called %d times, want 1", len(swaps))
	}
	if swaps[0].prev != component.Component(before) || swaps[0].next != component.Component(after) {
		t.Errorf("attacher got (prev=%p next=%p), want (prev=%p next=%p)",
			swaps[0].prev, swaps[0].next, before, after)
	}
}
