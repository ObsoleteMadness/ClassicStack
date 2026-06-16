package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	httpctrl "github.com/ObsoleteMadness/ClassicStack/adapter/control/http"
	"github.com/ObsoleteMadness/ClassicStack/adapter/control/inproc"
	"github.com/ObsoleteMadness/ClassicStack/adapter/control/ubus"
	"github.com/ObsoleteMadness/ClassicStack/compose/supervisor"
	"github.com/ObsoleteMadness/ClassicStack/core/auth"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

// parityAdminUser/Pass seed the HTTP server's web-admin gate so the parity flows can
// authenticate; inproc/ubus carry no Basic-auth gate (different trust boundaries).
const (
	parityAdminUser = "admin"
	parityAdminPass = "parity-pw"
)

// seedAdmin stamps a configured AdminAuth into the model so the gated HTTP front-end
// admits NewClientWithAuth(parityAdminUser, parityAdminPass).
func seedAdmin(m *config.Model) {
	salt := make([]byte, auth.SaltLen)
	for i := range salt {
		salt[i] = byte(i + 1)
	}
	cred := auth.DeriveCredential(parityAdminPass, salt)
	m.AdminAuth = config.AdminAuth{User: parityAdminUser, SaltHex: cred.SaltHex(), HashHex: cred.HashHex()}
}

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
	seedAdmin(m) // configure the web-admin gate so the HTTP client can authenticate
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
		"http":   httpctrl.NewClientWithAuth("http://"+httpSrv.Addr(), parityAdminUser, parityAdminPass),
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

// newParityClients spins up all three front-ends over one Plane and returns the
// client trio plus a cleanup. Mirrors the setup in TestMultiFrontEndParity.
func newParityClients(t *testing.T, plane control.Plane) (map[string]inproc.Client, func()) {
	t.Helper()
	httpSrv := httpctrl.NewServer(plane, "127.0.0.1:0")
	if err := httpSrv.Start(); err != nil {
		t.Fatalf("http start: %v", err)
	}
	tmpDir, err := os.MkdirTemp("", "ctrl-parity")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	sockPath := filepath.Join(tmpDir, "ubus.sock")
	ubusSrv := ubus.NewServer(plane, sockPath)
	if err := ubusSrv.Start(); err != nil {
		t.Fatalf("ubus start: %v", err)
	}
	clients := map[string]inproc.Client{
		"inproc": inproc.New(plane),
		"http":   httpctrl.NewClientWithAuth("http://"+httpSrv.Addr(), parityAdminUser, parityAdminPass),
		"ubus":   ubus.NewClient(sockPath),
	}
	cleanup := func() {
		httpSrv.Stop()
		ubusSrv.Stop()
		_ = os.RemoveAll(tmpDir)
	}
	return clients, cleanup
}

// TestMultiFrontEndParity_NewMethods checks the methods the catch-up added — Config,
// ListFSTypes, and the ErrUnavailable-bearing ListZones / Users — return parity
// results across inproc/http/ubus, including the ErrUnavailable sentinel round-trip.
func TestMultiFrontEndParity_NewMethods(t *testing.T) {
	m := config.NewModel()
	m.Identity = config.Identity{Hostname: "CLASSICSTACK", Workgroup: "WG"}
	seedAdmin(m) // gate the HTTP front-end so its authed client is admitted
	telemetry := bus.New(8)
	sup := supervisor.New(m, telemetry)
	// No user store wired and the default Diagnostics → Users / ListZones are
	// control.ErrUnavailable; that sentinel must survive every transport.
	plane := control.New(sup, nil, nil, telemetry)

	clients, cleanup := newParityClients(t, plane)
	defer cleanup()

	// Config: every front-end returns the same hostname.
	for name, c := range clients {
		got, err := c.Config()
		if err != nil {
			t.Fatalf("[%s] Config: %v", name, err)
		}
		if got.Identity.Hostname != "CLASSICSTACK" {
			t.Errorf("[%s] Config hostname = %q, want CLASSICSTACK", name, got.Identity.Hostname)
		}
	}

	// ListFSTypes parity (empty here, but every transport agrees and errors are nil).
	for name, c := range clients {
		if _, err := c.ListFSTypes(); err != nil {
			t.Fatalf("[%s] ListFSTypes: %v", name, err)
		}
	}

	// ListZones: default Diagnostics is unavailable → ErrUnavailable on all three.
	for name, c := range clients {
		_, err := c.ListZones(context.Background())
		if !errors.Is(err, control.ErrUnavailable) {
			t.Errorf("[%s] ListZones err = %v, want ErrUnavailable", name, err)
		}
	}

	// Users CRUD: no store wired → ErrUnavailable on all three, on every verb.
	for name, c := range clients {
		if _, err := c.Users(); !errors.Is(err, control.ErrUnavailable) {
			t.Errorf("[%s] Users err = %v, want ErrUnavailable", name, err)
		}
		if err := c.SetUser("alice", "pw"); !errors.Is(err, control.ErrUnavailable) {
			t.Errorf("[%s] SetUser err = %v, want ErrUnavailable", name, err)
		}
		if err := c.SetUserDisabled("alice", true); !errors.Is(err, control.ErrUnavailable) {
			t.Errorf("[%s] SetUserDisabled err = %v, want ErrUnavailable", name, err)
		}
		if err := c.RemoveUser("alice"); !errors.Is(err, control.ErrUnavailable) {
			t.Errorf("[%s] RemoveUser err = %v, want ErrUnavailable", name, err)
		}
	}
}

// TestMultiFrontEndParity_UserCRUD drives the full add→list→disable→remove cycle
// through each front-end against a Plane whose supervisor DOES expose a user store,
// proving the user-admin surface round-trips over http and ubus, not just in-proc.
func TestMultiFrontEndParity_UserCRUD(t *testing.T) {
	telemetry := bus.New(8)
	m := config.NewModel()
	seedAdmin(m) // gate the HTTP front-end so its authed client is admitted
	sup := &userStoreSupervisor{
		Supervisor: supervisor.New(m, telemetry),
		users:      map[string]bool{}, // name → disabled
	}
	plane := control.New(sup, nil, nil, telemetry)

	clients, cleanup := newParityClients(t, plane)
	defer cleanup()

	// Add via http, observe via ubus, disable via inproc, remove via http.
	if err := clients["http"].SetUser("bob", "secret"); err != nil {
		t.Fatalf("http SetUser: %v", err)
	}
	users, err := clients["ubus"].Users()
	if err != nil || len(users) != 1 || users[0].Name != "bob" {
		t.Fatalf("ubus Users after add = %v, err %v", users, err)
	}
	if err := clients["inproc"].SetUserDisabled("bob", true); err != nil {
		t.Fatalf("inproc SetUserDisabled: %v", err)
	}
	users, _ = clients["http"].Users()
	if len(users) != 1 || !users[0].Disabled {
		t.Fatalf("Users after disable = %v, want bob disabled", users)
	}
	if err := clients["http"].RemoveUser("bob"); err != nil {
		t.Fatalf("http RemoveUser: %v", err)
	}
	if users, _ := clients["inproc"].Users(); len(users) != 0 {
		t.Fatalf("Users after remove = %v, want empty", users)
	}
}

// userStoreSupervisor is a supervisor.Supervisor that also satisfies
// control.UserAdmin, so the Plane exposes the user surface (otherwise it reports
// ErrUnavailable). It delegates lifecycle/model to the embedded real supervisor.
type userStoreSupervisor struct {
	*supervisor.Supervisor
	users map[string]bool
}

func (s *userStoreSupervisor) Users() ([]control.UserInfo, error) {
	out := make([]control.UserInfo, 0, len(s.users))
	for name, disabled := range s.users {
		out = append(out, control.UserInfo{Name: name, Disabled: disabled})
	}
	return out, nil
}
func (s *userStoreSupervisor) SetUser(name, _ string) error { s.users[name] = false; return nil }
func (s *userStoreSupervisor) SetUserDisabled(name string, d bool) error {
	s.users[name] = d
	return nil
}
func (s *userStoreSupervisor) RemoveUser(name string) error { delete(s.users, name); return nil }
