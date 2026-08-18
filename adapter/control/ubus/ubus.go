package ubus

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/control/diag"
	"github.com/ObsoleteMadness/ClassicStack/adapter/control/inproc"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
	"github.com/ObsoleteMadness/ClassicStack/core/hostinfo"
)

// Client is the ubus Client interface.
type Client = inproc.Client

// Request represents a JSON-RPC request over the ubus socket shim.
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	ID     int64           `json:"id"`
}

// Response represents a JSON-RPC response.
type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	ID     int64           `json:"id"`
}

// EventMessage represents a streamed ubus event.
type EventMessage struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// Server exposes the control.Plane over a UNIX domain socket.
type Server struct {
	plane    control.Plane
	diag     DiagProvider // protocol-specific diagnostic drill-downs (adapter/control/diag); nil = unavailable
	sockPath string
	listener net.Listener
	mu       sync.Mutex
	closed   bool
	conns    map[net.Conn]bool
	wg       sync.WaitGroup
}

// DiagProvider is the protocol diagnostics surface the ubus server answers on the
// registered_names / macip_leases / aarp_table methods. Satisfied by
// *adapter/control/diag.Provider, kept out of core/control so the neutral plane carries no
// protocol type. nil leaves those methods reporting unavailable.
type DiagProvider interface {
	SMBSessions() ([]diag.SMBSession, error)
	RegisteredNames() ([]diag.NBPName, error)
	MacIPLeases() ([]diag.MacIPLease, error)
	AARPTable() ([]diag.AARPEntry, error)
}

// SetDiagProvider installs the protocol diagnostics provider (the cmd edge builds it
// over the runtime). Safe before Serve; nil leaves the drill-down methods unavailable.
func (s *Server) SetDiagProvider(d DiagProvider) { s.diag = d }

// NewServer builds a ubus socket Server for the plane.
func NewServer(plane control.Plane, sockPath string) *Server {
	return &Server{
		plane:    plane,
		sockPath: sockPath,
		conns:    make(map[net.Conn]bool),
	}
}

// Start starts the server listening on the UNIX socket.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clean up existing socket file
	_ = os.Remove(s.sockPath)

	l, err := net.Listen("unix", s.sockPath)
	if err != nil {
		return err
	}
	s.listener = l

	s.wg.Add(1)
	go s.acceptLoop()
	return nil
}

// Stop shuts down the server and cleans up the UNIX socket.
func (s *Server) Stop() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	if s.listener != nil {
		_ = s.listener.Close()
	}
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.mu.Unlock()

	s.wg.Wait()
	_ = os.Remove(s.sockPath)
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}
			continue
		}
		s.mu.Lock()
		if s.closed {
			_ = conn.Close()
			s.mu.Unlock()
			continue
		}
		s.conns[conn] = true
		s.mu.Unlock()

		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		_ = conn.Close()
	}()

	reader := bufio.NewReader(conn)
	writer := json.NewEncoder(conn)

	var activeSub func() // unsubscribe func if client is subscribed
	defer func() {
		if activeSub != nil {
			activeSub()
		}
	}()

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = writer.Encode(Response{Error: err.Error()})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var res any
		var methodErr error

		switch req.Method {
		case "status":
			res = s.plane.Status()
		case "host_info":
			info, err := s.plane.HostInfo()
			if err != nil {
				methodErr = err
			} else {
				res = info
			}
		case "start":
			var args struct{ Name string }
			if err := json.Unmarshal(req.Params, &args); err == nil {
				methodErr = s.plane.Start(ctx, args.Name)
			} else {
				methodErr = err
			}
		case "stop":
			var args struct{ Name string }
			if err := json.Unmarshal(req.Params, &args); err == nil {
				methodErr = s.plane.Stop(ctx, args.Name)
			} else {
				methodErr = err
			}
		case "restart":
			var args struct{ Name string }
			if err := json.Unmarshal(req.Params, &args); err == nil {
				methodErr = s.plane.Restart(ctx, args.Name)
			} else {
				methodErr = err
			}
		case "list_fs_types":
			res = s.plane.ListFSTypes()
		case "share_backends":
			res = s.plane.ShareBackends()
		case "params_for":
			var args struct {
				FSType string `json:"fs_type"`
			}
			if err := json.Unmarshal(req.Params, &args); err == nil {
				res = s.plane.ParamsFor(args.FSType)
			} else {
				methodErr = err
			}
		case "subscribe":
			var args struct{ Topics []string }
			if err := json.Unmarshal(req.Params, &args); err == nil {
				if activeSub != nil {
					activeSub() // cancel prior subscription
				}
				ch, cancelSub := s.plane.Subscribe(args.Topics...)
				activeSub = cancelSub
				go func() {
					for ev := range ch {
						data, _ := json.Marshal(ev)
						msg := EventMessage{Event: ev.Topic(), Data: data}
						_ = writer.Encode(msg)
					}
				}()
				res = "subscribed"
			} else {
				methodErr = err
			}
		case "reconfigure":
			var args struct {
				Name    string
				Section json.RawMessage
			}
			if err := json.Unmarshal(req.Params, &args); err == nil {
				// We resolve the schema and unmarshal the section type-safely.
				var typedSec config.Section
				if schema, ok := config.SchemaFor(args.Name); ok {
					typedSec = schema.New()
					if err := json.Unmarshal(args.Section, typedSec); err != nil {
						methodErr = err
					}
				}
				if methodErr == nil {
					methodErr = s.plane.Reconfigure(ctx, args.Name, typedSec)
				}
			} else {
				methodErr = err
			}
		case "add_instance":
			var args struct {
				Owner   string
				Key     string
				Section json.RawMessage
			}
			if err := json.Unmarshal(req.Params, &args); err != nil {
				methodErr = err
			} else if schema, ok := config.SchemaFor(args.Key); !ok {
				methodErr = fmt.Errorf("unknown section key: %s", args.Key)
			} else {
				sec := schema.New()
				if err := json.Unmarshal(args.Section, sec); err != nil {
					methodErr = err
				} else if ns, ok := sec.(config.NamedSection); !ok {
					methodErr = fmt.Errorf("section is not a named instance: %s", args.Key)
				} else {
					methodErr = s.plane.AddInstance(ctx, args.Owner, ns)
				}
			}
		case "remove_instance":
			var args struct{ Owner, Key, Name string }
			if err := json.Unmarshal(req.Params, &args); err != nil {
				methodErr = err
			} else {
				methodErr = s.plane.RemoveInstance(ctx, args.Owner, args.Key, args.Name)
			}
		case "config":
			m, err := s.plane.Config()
			if err != nil {
				methodErr = err
			} else {
				res = m
			}
		case "save":
			rev, err := s.plane.Save(ctx)
			if err != nil {
				methodErr = err
			} else {
				res = struct {
					Revision string `json:"revision"`
				}{Revision: rev}
			}
		case "list_interfaces":
			ifaces, err := s.plane.ListInterfaces()
			if err != nil {
				methodErr = err
			} else {
				res = ifaces
			}
		case "set_interface":
			var iface config.InterfaceSection
			if err := json.Unmarshal(req.Params, &iface); err != nil {
				methodErr = err
			} else {
				methodErr = s.plane.SetInterface(ctx, iface)
			}
		case "remove_interface":
			var args struct{ Name string }
			if err := json.Unmarshal(req.Params, &args); err != nil {
				methodErr = err
			} else {
				methodErr = s.plane.RemoveInterface(ctx, args.Name)
			}
		case "list_zones":
			zones, err := s.plane.Diagnostics().ListZones(ctx)
			if err != nil {
				methodErr = err
			} else {
				res = zones
			}
		case "registered_names":
			if s.diag == nil {
				methodErr = control.ErrUnavailable
			} else if names, err := s.diag.RegisteredNames(); err != nil {
				methodErr = err
			} else {
				res = names
			}
		case "macip_leases":
			if s.diag == nil {
				methodErr = control.ErrUnavailable
			} else if leases, err := s.diag.MacIPLeases(); err != nil {
				methodErr = err
			} else {
				res = leases
			}
		case "aarp_table":
			if s.diag == nil {
				methodErr = control.ErrUnavailable
			} else if entries, err := s.diag.AARPTable(); err != nil {
				methodErr = err
			} else {
				res = entries
			}
		case "smb_sessions":
			if s.diag == nil {
				methodErr = control.ErrUnavailable
			} else if sessions, err := s.diag.SMBSessions(); err != nil {
				methodErr = err
			} else {
				res = sessions
			}
		case "users":
			users, err := s.plane.Users()
			if err != nil {
				methodErr = err
			} else {
				res = users
			}
		case "set_user":
			var args struct {
				Name     string `json:"name"`
				Password string `json:"password"`
			}
			if err := json.Unmarshal(req.Params, &args); err == nil {
				methodErr = s.plane.SetUser(args.Name, args.Password)
			} else {
				methodErr = err
			}
		case "set_user_disabled":
			var args struct {
				Name     string `json:"name"`
				Disabled bool   `json:"disabled"`
			}
			if err := json.Unmarshal(req.Params, &args); err == nil {
				methodErr = s.plane.SetUserDisabled(args.Name, args.Disabled)
			} else {
				methodErr = err
			}
		case "remove_user":
			var args struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(req.Params, &args); err == nil {
				methodErr = s.plane.RemoveUser(args.Name)
			} else {
				methodErr = err
			}
		default:
			methodErr = fmt.Errorf("unknown method: %s", req.Method)
		}
		cancel()

		var resp Response
		resp.ID = req.ID
		if methodErr != nil {
			resp.Error = methodErr.Error()
		} else if res != nil {
			b, _ := json.Marshal(res)
			resp.Result = b
		}
		if err := writer.Encode(resp); err != nil {
			return
		}
	}
}

// AdapterClient implements Client (inproc.Client) over the ubus UNIX socket.
type AdapterClient struct {
	sockPath string
}

// NewClient builds a ubus Client connecting to the UNIX socket at path.
func NewClient(sockPath string) *AdapterClient {
	return &AdapterClient{sockPath: sockPath}
}

// compile-time assertion: *AdapterClient satisfies Client.
var _ Client = (*AdapterClient)(nil)

func (c *AdapterClient) call(method string, params any, dest any) error {
	conn, err := net.Dial("unix", c.sockPath)
	if err != nil {
		return err
	}
	defer conn.Close()

	paramBytes, _ := json.Marshal(params)
	req := Request{Method: method, Params: paramBytes, ID: 1}
	reqBytes, _ := json.Marshal(req)
	_, _ = conn.Write(append(reqBytes, '\n'))

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return err
	}

	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return errFromUbus(resp.Error)
	}

	if dest != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, dest)
	}
	return nil
}

// errFromUbus reconstitutes a transported error string. control.ErrUnavailable is
// surfaced as itself so a caller can errors.Is it across the socket exactly as the
// in-process adapter reports it; any other string is wrapped opaquely.
func errFromUbus(msg string) error {
	if msg == control.ErrUnavailable.Error() {
		return control.ErrUnavailable
	}
	return fmt.Errorf("ubus error: %s", msg)
}

// Status retrieves the unit status.
func (c *AdapterClient) Status() ([]control.Unit, error) {
	var out []control.Unit
	err := c.call("status", nil, &out)
	return out, err
}

// HostInfo retrieves the host info.
func (c *AdapterClient) HostInfo() (hostinfo.HostInfo, error) {
	var out hostinfo.HostInfo
	err := c.call("host_info", nil, &out)
	return out, err
}

// Reconfigure triggers reconfiguration.
func (c *AdapterClient) Reconfigure(ctx context.Context, name string, section config.Section) error {
	_ = ctx
	secBytes, _ := json.Marshal(section)
	args := struct {
		Name    string          `json:"name"`
		Section json.RawMessage `json:"section"`
	}{Name: name, Section: secBytes}
	return c.call("reconfigure", args, nil)
}

// AddInstance adds a repeated-section instance (an AFP volume / SMB share).
func (c *AdapterClient) AddInstance(ctx context.Context, owner string, section config.NamedSection) error {
	_ = ctx
	secBytes, _ := json.Marshal(section)
	return c.call("add_instance", struct {
		Owner   string          `json:"owner"`
		Key     string          `json:"key"`
		Section json.RawMessage `json:"section"`
	}{Owner: owner, Key: section.Key(), Section: secBytes}, nil)
}

// RemoveInstance drops a named repeated-section instance.
func (c *AdapterClient) RemoveInstance(ctx context.Context, owner, key, instanceName string) error {
	_ = ctx
	return c.call("remove_instance", struct {
		Owner string `json:"owner"`
		Key   string `json:"key"`
		Name  string `json:"name"`
	}{Owner: owner, Key: key, Name: instanceName}, nil)
}

// Start starts a component.
func (c *AdapterClient) Start(ctx context.Context, name string) error {
	_ = ctx
	return c.call("start", struct{ Name string }{Name: name}, nil)
}

// Stop stops a component.
func (c *AdapterClient) Stop(ctx context.Context, name string) error {
	_ = ctx
	return c.call("stop", struct{ Name string }{Name: name}, nil)
}

// Restart restarts a component.
func (c *AdapterClient) Restart(ctx context.Context, name string) error {
	_ = ctx
	return c.call("restart", struct{ Name string }{Name: name}, nil)
}

// ListFSTypes retrieves FS types.
func (c *AdapterClient) ListFSTypes() ([]string, error) {
	var out []string
	err := c.call("list_fs_types", nil, &out)
	return out, err
}

// ParamsFor returns the config-param schema for one fs_type (the per-share form).
func (c *AdapterClient) ParamsFor(fsType string) ([]control.ParamInfo, error) {
	var out []control.ParamInfo
	err := c.call("params_for", struct {
		FSType string `json:"fs_type"`
	}{FSType: fsType}, &out)
	return out, err
}

// Config fetches a snapshot of the live config model.
func (c *AdapterClient) Config() (*config.Model, error) {
	m := config.NewModel()
	if err := c.call("config", nil, m); err != nil {
		return nil, err
	}
	return m, nil
}

// Save validates and persists the live model server-side, returning the revision.
func (c *AdapterClient) Save(ctx context.Context) (string, error) {
	_ = ctx
	var out struct {
		Revision string `json:"revision"`
	}
	err := c.call("save", nil, &out)
	return out.Revision, err
}

// ListInterfaces returns the enumerable network interfaces.
func (c *AdapterClient) ListInterfaces() ([]control.InterfaceInfo, error) {
	var out []control.InterfaceInfo
	err := c.call("list_interfaces", nil, &out)
	return out, err
}

// SetInterface adds/replaces a named interface-namespace entry.
func (c *AdapterClient) SetInterface(ctx context.Context, iface config.InterfaceSection) error {
	_ = ctx
	return c.call("set_interface", iface, nil)
}

// RemoveInterface drops a named interface-namespace entry.
func (c *AdapterClient) RemoveInterface(ctx context.Context, name string) error {
	_ = ctx
	return c.call("remove_interface", struct {
		Name string `json:"name"`
	}{Name: name}, nil)
}

// ListZones runs the Diagnostics zone probe (control.ErrUnavailable when unsupported).
func (c *AdapterClient) ListZones(ctx context.Context) ([]string, error) {
	_ = ctx
	var out []string
	err := c.call("list_zones", nil, &out)
	return out, err
}

// RegisteredNames runs the NBP name-table drill-down (control.ErrUnavailable when no NBP).
func (c *AdapterClient) RegisteredNames(ctx context.Context) ([]diag.NBPName, error) {
	_ = ctx
	var out []diag.NBPName
	err := c.call("registered_names", nil, &out)
	return out, err
}

// MacIPLeases runs the MacIP lease drill-down (control.ErrUnavailable when no MacIP gateway).
func (c *AdapterClient) MacIPLeases(ctx context.Context) ([]diag.MacIPLease, error) {
	_ = ctx
	var out []diag.MacIPLease
	err := c.call("macip_leases", nil, &out)
	return out, err
}

// AARPTable runs the AARP address-mapping-table drill-down (control.ErrUnavailable when
// no EtherTalk port).
func (c *AdapterClient) AARPTable(ctx context.Context) ([]diag.AARPEntry, error) {
	_ = ctx
	var out []diag.AARPEntry
	err := c.call("aarp_table", nil, &out)
	return out, err
}

// Users lists stored identities (control.ErrUnavailable when no store is wired).
func (c *AdapterClient) Users() ([]control.UserInfo, error) {
	var out []control.UserInfo
	err := c.call("users", nil, &out)
	return out, err
}

// SetUser adds a user or resets a password.
func (c *AdapterClient) SetUser(name, password string) error {
	return c.call("set_user", struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}{Name: name, Password: password}, nil)
}

// SetUserDisabled parks/unparks an account.
func (c *AdapterClient) SetUserDisabled(name string, disabled bool) error {
	return c.call("set_user_disabled", struct {
		Name     string `json:"name"`
		Disabled bool   `json:"disabled"`
	}{Name: name, Disabled: disabled}, nil)
}

// RemoveUser deletes a user.
func (c *AdapterClient) RemoveUser(name string) error {
	return c.call("remove_user", struct {
		Name string `json:"name"`
	}{Name: name}, nil)
}

// Subscribe listens for live telemetry.
func (c *AdapterClient) Subscribe(topics ...string) (<-chan bus.Event, func(), error) {
	conn, err := net.Dial("unix", c.sockPath)
	if err != nil {
		return nil, nil, err
	}

	args := struct {
		Topics []string `json:"topics"`
	}{Topics: topics}
	paramBytes, _ := json.Marshal(args)
	req := Request{Method: "subscribe", Params: paramBytes, ID: 1}
	reqBytes, _ := json.Marshal(req)
	_, _ = conn.Write(append(reqBytes, '\n'))

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		_ = conn.Close() // best-effort cleanup; returning the read error
		return nil, nil, err
	}

	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		_ = conn.Close() // best-effort cleanup; returning the unmarshal error
		return nil, nil, err
	}
	if resp.Error != "" {
		_ = conn.Close() // best-effort cleanup; returning the ubus error
		return nil, nil, fmt.Errorf("ubus subscribe error: %s", resp.Error)
	}

	outCh := make(chan bus.Event, 16)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		defer close(outCh)
		defer conn.Close()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line, err := reader.ReadBytes('\n')
			if err != nil {
				return
			}

			var msg EventMessage
			if err := json.Unmarshal(line, &msg); err != nil {
				continue
			}

			// Unmarshal the specific telemetry event based on the topic
			var ev bus.Event
			switch msg.Event {
			case bus.TopicState:
				var sc bus.StateChanged
				_ = json.Unmarshal(msg.Data, &sc)
				ev = sc
			case bus.TopicStats:
				var ss bus.StatSample
				_ = json.Unmarshal(msg.Data, &ss)
				ev = ss
			case bus.TopicLog:
				var lr bus.LogRecord
				_ = json.Unmarshal(msg.Data, &lr)
				ev = lr
			}

			if ev != nil {
				select {
				case outCh <- ev:
				default:
					// drop on backpressure
				}
			}
		}
	}()

	unsub := func() {
		cancel()
	}

	return outCh, unsub, nil
}
