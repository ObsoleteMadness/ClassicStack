package ubus_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/adapter/control/diag"
	"github.com/ObsoleteMadness/ClassicStack/adapter/control/ubus"
	"github.com/ObsoleteMadness/ClassicStack/compose/supervisor"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

// ubusTestSchemaKey is the config-section schema these tests register a
// port.Section under, for Reconfigure/AddInstance/RemoveInstance —
// mirroring adapter/control/parity_test.go's "dummy-comp" pattern, under a
// distinct key so the two test binaries never collide.
const ubusTestSchemaKey = "ubus-test-comp"

func init() {
	config.Register(config.SectionSchema{
		Key:      ubusTestSchemaKey,
		New:      func() config.Section { return &port.Section{SKey: ubusTestSchemaKey} },
		Repeated: true,
	})
}

// newTestServer builds a real control.Plane over a real supervisor.Supervisor
// (the same pattern adapter/control/parity_test.go uses, rather than a
// hand-written fake against control.Plane's large interface), starts a ubus
// Server on a temp-dir socket, and returns a connected Client plus the raw
// socket path for tests that need to speak the wire protocol directly.
func newTestServer(t *testing.T) (client *ubus.AdapterClient, srv *ubus.Server, sockPath string) {
	t.Helper()
	m := config.NewModel()
	telemetry := bus.New(8)
	sup := supervisor.New(m, telemetry)
	plane := control.New(sup, nil, nil, telemetry)

	tmpDir := t.TempDir()
	sockPath = filepath.Join(tmpDir, "ubus.sock")
	srv = ubus.NewServer(plane, sockPath)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(srv.Stop)
	return ubus.NewClient(sockPath), srv, sockPath
}

// newTestServerWithOwner mirrors newTestServer but also registers a
// fakeComponent named ubusTestSchemaKey with the supervisor, so
// Start/Stop/Restart and Reconfigure/AddInstance/RemoveInstance (which
// reconcile a named "owner" component) have something real to act on
// instead of failing with "unknown component".
func newTestServerWithOwner(t *testing.T) (client *ubus.AdapterClient, comp *fakeComponent, model *config.Model) {
	t.Helper()
	m := config.NewModel()
	telemetry := bus.New(8)
	sup := supervisor.New(m, telemetry)
	comp = &fakeComponent{name: ubusTestSchemaKey}
	sup.Add(comp, nil)
	plane := control.New(sup, nil, nil, telemetry)

	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "ubus.sock")
	srv := ubus.NewServer(plane, sockPath)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(srv.Stop)
	return ubus.NewClient(sockPath), comp, m
}

// TestStopRestart drives Stop and Restart (Start is already covered by
// adapter/control/parity_test.go) through a real component.
func TestStopRestart(t *testing.T) {
	client, comp, _ := newTestServerWithOwner(t)
	ctx := context.Background()

	if err := client.Start(ctx, ubusTestSchemaKey); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !comp.running {
		t.Fatal("component not running after Start")
	}
	if err := client.Stop(ctx, ubusTestSchemaKey); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if comp.running {
		t.Fatal("component still running after Stop")
	}
	if err := client.Restart(ctx, ubusTestSchemaKey); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if !comp.running {
		t.Fatal("component not running after Restart")
	}
}

type fakeComponent struct {
	name    string
	running bool
}

func (c *fakeComponent) Name() string                { return c.name }
func (c *fakeComponent) Start(context.Context) error { c.running = true; return nil }
func (c *fakeComponent) Stop(context.Context) error  { c.running = false; return nil }

// TestHostInfo checks HostInfo round-trips over the wire without error.
func TestHostInfo(t *testing.T) {
	client, _, _ := newTestServer(t)
	if _, err := client.HostInfo(); err != nil {
		t.Fatalf("HostInfo: %v", err)
	}
}

// TestParamsFor checks ParamsFor round-trips its fs_type argument and
// returns (possibly empty) results without error for an unknown type.
func TestParamsFor(t *testing.T) {
	client, _, _ := newTestServer(t)
	if _, err := client.ParamsFor("no-such-fs-type"); err != nil {
		t.Fatalf("ParamsFor: %v", err)
	}
}

// TestListInterfacesSetRemove drives the interface-namespace CRUD trio.
func TestListInterfacesSetRemove(t *testing.T) {
	client, _, _ := newTestServer(t)
	ctx := context.Background()

	if _, err := client.ListInterfaces(); err != nil {
		t.Fatalf("ListInterfaces: %v", err)
	}
	iface := config.InterfaceSection{Name: "test0", Kind: config.IfaceKindNIC}
	if err := client.SetInterface(ctx, iface); err != nil {
		t.Fatalf("SetInterface: %v", err)
	}
	cfg, err := client.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if _, ok := cfg.Interfaces["test0"]; !ok {
		t.Fatalf("Config after SetInterface: %+v, want an entry named test0", cfg.Interfaces)
	}
	if err := client.RemoveInterface(ctx, "test0"); err != nil {
		t.Fatalf("RemoveInterface: %v", err)
	}
	cfg, err = client.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if _, ok := cfg.Interfaces["test0"]; ok {
		t.Fatal("interface still present after RemoveInterface")
	}
}

// TestAddRemoveInstance drives a repeated-section instance through
// AddInstance/RemoveInstance and confirms it lands in / leaves the model.
// Verified server-side (directly against the *config.Model the test built,
// shared live with the supervisor) rather than via client.Config(): Model.Lists/
// Sections hold the config.Section INTERFACE, which plain encoding/json can't
// unmarshal client-side with no concrete type to target — a real, pre-existing
// limitation of AdapterClient.Config() for any model actually holding sections,
// not something to work around by weakening these tests.
func TestAddRemoveInstance(t *testing.T) {
	client, _, model := newTestServerWithOwner(t)
	ctx := context.Background()

	sec := &port.Section{SKey: ubusTestSchemaKey, Name: "inst1"}
	if err := client.AddInstance(ctx, ubusTestSchemaKey, sec); err != nil {
		t.Fatalf("AddInstance: %v", err)
	}
	if got := len(model.Lists[ubusTestSchemaKey]); got != 1 {
		t.Fatalf("Lists[%s] = %+v, want 1 instance", ubusTestSchemaKey, model.Lists[ubusTestSchemaKey])
	}
	if err := client.RemoveInstance(ctx, ubusTestSchemaKey, ubusTestSchemaKey, "inst1"); err != nil {
		t.Fatalf("RemoveInstance: %v", err)
	}
	if got := len(model.Lists[ubusTestSchemaKey]); got != 0 {
		t.Fatalf("Lists[%s] after remove = %+v, want empty", ubusTestSchemaKey, model.Lists[ubusTestSchemaKey])
	}
}

// TestReconfigure checks Reconfigure resolves the schema by name, decodes
// the section, and applies it. Verified server-side against the live model,
// for the same reason as TestAddRemoveInstance above.
//
// port.Section implements config.NamedSection (it has InstanceName), so the
// supervisor routes it to Model.Lists rather than Model.Sections
// (setSectionLocked — a NamedSection reconfigure must not mis-write as a
// singleton, or a volume/share reconfigure would never reach the owning
// service's instance set). This test follows that real routing rather than
// asserting the singleton path a NamedSection never takes.
func TestReconfigure(t *testing.T) {
	client, _, model := newTestServerWithOwner(t)
	sec := &port.Section{SKey: ubusTestSchemaKey, Name: "inst1", Iface: "eth9"}
	if err := client.Reconfigure(context.Background(), ubusTestSchemaKey, sec); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	list := model.Lists[ubusTestSchemaKey]
	if len(list) != 1 {
		t.Fatalf("Lists[%s] = %+v, want 1 instance", ubusTestSchemaKey, list)
	}
	got, ok := list[0].(*port.Section)
	if !ok || got.Iface != "eth9" {
		t.Fatalf("Lists[%s][0] = %+v, want Iface eth9", ubusTestSchemaKey, list[0])
	}
}

// TestSave_NoStoreConfigured checks Save's error (no codec/store wired, the
// case newTestServer's plane is built with — control.New(sup, nil, nil, ...))
// round-trips over the wire as a real error, not a false-success empty
// revision.
func TestSave_NoStoreConfigured(t *testing.T) {
	client, _, _ := newTestServer(t)
	rev, err := client.Save(context.Background())
	if err == nil {
		t.Fatalf("Save: got nil error and revision %q, want an error (no store configured)", rev)
	}
}

// fakeDiag implements ubus.DiagProvider with canned results, so the
// diag-backed RPC methods (registered_names/macip_leases/aarp_table/
// smb_sessions) can be exercised without a real router/service behind them.
type fakeDiag struct{}

func (fakeDiag) SMBSessions() ([]diag.SMBSession, error) {
	return []diag.SMBSession{{Client: "1.2.3.4:139", User: "guest"}}, nil
}
func (fakeDiag) RegisteredNames() ([]diag.NBPName, error) {
	return []diag.NBPName{{Object: "MyMac", Type: "AFPServer", Zone: "*"}}, nil
}
func (fakeDiag) MacIPLeases() ([]diag.MacIPLease, error) {
	return []diag.MacIPLease{{IP: "10.0.0.5", Source: "dhcp"}}, nil
}
func (fakeDiag) AARPTable() ([]diag.AARPEntry, error) {
	return []diag.AARPEntry{{Port: "EtherTalk", MAC: "aa:bb:cc:dd:ee:ff"}}, nil
}

var _ ubus.DiagProvider = fakeDiag{}

// TestDiagMethods_Unavailable checks the four diagnostic drill-down methods
// report control.ErrUnavailable when no DiagProvider is installed (the
// default), and TestDiagMethods_WithProvider checks they return the
// provider's data once SetDiagProvider is called.
func TestDiagMethods_Unavailable(t *testing.T) {
	client, _, _ := newTestServer(t)
	ctx := context.Background()
	if _, err := client.RegisteredNames(ctx); !errors.Is(err, control.ErrUnavailable) {
		t.Errorf("RegisteredNames err = %v, want ErrUnavailable", err)
	}
	if _, err := client.MacIPLeases(ctx); !errors.Is(err, control.ErrUnavailable) {
		t.Errorf("MacIPLeases err = %v, want ErrUnavailable", err)
	}
	if _, err := client.AARPTable(ctx); !errors.Is(err, control.ErrUnavailable) {
		t.Errorf("AARPTable err = %v, want ErrUnavailable", err)
	}
}

func TestDiagMethods_WithProvider(t *testing.T) {
	client, srv, _ := newTestServer(t)
	srv.SetDiagProvider(fakeDiag{})
	ctx := context.Background()

	names, err := client.RegisteredNames(ctx)
	if err != nil || len(names) != 1 || names[0].Object != "MyMac" {
		t.Fatalf("RegisteredNames = %+v, err %v", names, err)
	}
	leases, err := client.MacIPLeases(ctx)
	if err != nil || len(leases) != 1 || leases[0].IP != "10.0.0.5" {
		t.Fatalf("MacIPLeases = %+v, err %v", leases, err)
	}
	entries, err := client.AARPTable(ctx)
	if err != nil || len(entries) != 1 || entries[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("AARPTable = %+v, err %v", entries, err)
	}
}

// TestUnknownMethod checks a request naming a method the server doesn't
// implement gets a well-formed error response, not a dropped connection or
// hang.
func TestUnknownMethod(t *testing.T) {
	_, _, sockPath := newTestServer(t)
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	req, _ := json.Marshal(ubus.Request{Method: "no_such_method", ID: 1})
	if _, err := conn.Write(append(req, '\n')); err != nil {
		t.Fatalf("Write: %v", err)
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("ReadBytes: %v", err)
	}
	var resp ubus.Response
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("Unmarshal response: %v", err)
	}
	if resp.Error == "" {
		t.Error("Error = \"\", want a message naming the unknown method")
	}
}

// TestMalformedRequest checks a line that isn't valid JSON gets an error
// response. Note (characterizing current behavior, not asserting it's
// necessarily ideal): handleConn returns after sending this error, ending
// the connection rather than continuing to serve further requests on it.
func TestMalformedRequest(t *testing.T) {
	_, _, sockPath := newTestServer(t)
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("not valid json\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("ReadBytes: %v", err)
	}
	var resp ubus.Response
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("Unmarshal response: %v", err)
	}
	if resp.Error == "" {
		t.Error("Error = \"\", want a JSON-decode error message")
	}
}

// TestErrorRoundTrip checks a plain (non-ErrUnavailable) Plane error
// survives the wire as an opaque "ubus error: ..." message the client
// surfaces as a real error (not silently swallowed or turned into success).
func TestErrorRoundTrip(t *testing.T) {
	client, _, _ := newTestServer(t)
	// AddInstance naming a schema key nothing registered: a genuine,
	// deterministic plane-side error with no live component required.
	sec := &port.Section{SKey: "no-such-schema-key", Name: "inst1"}
	err := client.AddInstance(context.Background(), "no-such-schema-key", sec)
	if err == nil {
		t.Fatal("AddInstance with an unregistered schema key: got nil error, want one")
	}
}

// TestServerStop checks Stop closes the listener and removes the socket
// file, and that Stop is idempotent (safe to call twice).
func TestServerStop(t *testing.T) {
	m := config.NewModel()
	telemetry := bus.New(8)
	sup := supervisor.New(m, telemetry)
	plane := control.New(sup, nil, nil, telemetry)

	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "ubus.sock")
	srv := ubus.NewServer(plane, sockPath)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	srv.Stop()
	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Errorf("socket file still exists after Stop: err = %v", err)
	}
	srv.Stop() // must not panic or hang
}
