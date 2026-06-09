package control

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	httpctrl "github.com/ObsoleteMadness/ClassicStack/adapter/control/http"
	"github.com/ObsoleteMadness/ClassicStack/adapter/control/inproc"
	"github.com/ObsoleteMadness/ClassicStack/adapter/control/ubus"
	"github.com/ObsoleteMadness/ClassicStack/compose/supervisor"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

type dummyComp struct {
	name    string
	running bool
}

func (d *dummyComp) Name() string                { return d.name }
func (d *dummyComp) Start(context.Context) error { d.running = true; return nil }
func (d *dummyComp) Stop(context.Context) error  { d.running = false; return nil }

func TestMultiFrontEndParity(t *testing.T) {
	// Register a schema so the codecs can unmarshal sections for dummy-comp if needed
	config.Register(config.SectionSchema{
		Key: "dummy-comp",
		New: func() config.Section { return &port.Section{SKey: "dummy-comp"} },
	})

	m := config.NewModel()
	telemetry := bus.New(16)
	sup := supervisor.New(m, telemetry)

	comp := &dummyComp{name: "dummy-comp"}
	sup.Add(comp, nil)

	// Create Plane with standard mock/default TOML codec and file store fakes
	plane := control.New(sup, nil, nil, telemetry)

	// Start HTTP server
	httpSrv := httpctrl.NewServer(plane, "127.0.0.1:0")
	if err := httpSrv.Start(); err != nil {
		t.Fatalf("Failed to start HTTP server: %v", err)
	}
	defer httpSrv.Stop()

	// Start ubus server
	tmpDir, err := os.MkdirTemp("", "ubus-parity-test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sockPath := filepath.Join(tmpDir, "ubus.sock")
	ubusSrv := ubus.NewServer(plane, sockPath)
	if err := ubusSrv.Start(); err != nil {
		t.Fatalf("Failed to start ubus server: %v", err)
	}
	defer ubusSrv.Stop()

	// Build the 3 Client adapters
	clients := map[string]inproc.Client{
		"inproc": inproc.New(plane),
		"http":   httpctrl.NewClient("http://" + httpSrv.Addr()),
		"ubus":   ubus.NewClient(sockPath),
	}

	ctx := context.Background()

	// 1. Verify initial Status parity
	initialStatus := make(map[string][]control.Unit)
	for name, client := range clients {
		status, err := client.Status()
		if err != nil {
			t.Fatalf("[%s] Status failed: %v", name, err)
		}
		initialStatus[name] = status
	}

	// Verify all returned identical status array lengths and contents
	inprocStatus := initialStatus["inproc"]
	for name, status := range initialStatus {
		if !reflect.DeepEqual(status, inprocStatus) {
			t.Errorf("Parity mismatch on initial status for %s: got %+v, want %+v", name, status, inprocStatus)
		}
	}

	// 2. Subscribe to state transitions on all 3 clients
	subs := make(map[string]<-chan bus.Event)
	unsubs := make(map[string]func())
	for name, client := range clients {
		ch, unsub, err := client.Subscribe(bus.TopicState)
		if err != nil {
			t.Fatalf("[%s] Subscribe failed: %v", name, err)
		}
		subs[name] = ch
		unsubs[name] = unsub
	}
	defer func() {
		for _, unsub := range unsubs {
			unsub()
		}
	}()

	// 3. Trigger Start via one client (e.g. ubus)
	if err := clients["ubus"].Start(ctx, "dummy-comp"); err != nil {
		t.Fatalf("Start via ubus failed: %v", err)
	}

	// 4. Verify all 3 subscription channels receive the state transition
	for name, ch := range subs {
		select {
		case ev := <-ch:
			sc, ok := ev.(bus.StateChanged)
			if !ok {
				t.Fatalf("[%s] received unexpected event type %T", name, ev)
			}
			if sc.Component != "dummy-comp" || sc.To != "running" {
				t.Errorf("[%s] received unexpected event details: %+v", name, sc)
			}
		case <-time.After(3 * time.Second):
			t.Errorf("[%s] subscription timed out waiting for Start event", name)
		}
	}

	// 5. Verify final running Status parity
	runningStatus := make(map[string][]control.Unit)
	for name, client := range clients {
		status, err := client.Status()
		if err != nil {
			t.Fatalf("[%s] Status failed: %v", name, err)
		}
		runningStatus[name] = status
	}

	inprocRunningStatus := runningStatus["inproc"]
	for name, status := range runningStatus {
		if !reflect.DeepEqual(status, inprocRunningStatus) {
			t.Errorf("Parity mismatch on running status for %s: got %+v, want %+v", name, status, inprocRunningStatus)
		}
	}
}
