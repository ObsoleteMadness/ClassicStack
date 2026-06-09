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

	"github.com/ObsoleteMadness/ClassicStack/adapter/control/inproc"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
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
	sockPath string
	listener net.Listener
	mu       sync.Mutex
	closed   bool
	conns    map[net.Conn]bool
	wg       sync.WaitGroup
}

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
		return fmt.Errorf("ubus error: %s", resp.Error)
	}

	if dest != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, dest)
	}
	return nil
}

// Status retrieves the unit status.
func (c *AdapterClient) Status() ([]control.Unit, error) {
	var out []control.Unit
	err := c.call("status", nil, &out)
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
		conn.Close()
		return nil, nil, err
	}

	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		conn.Close()
		return nil, nil, err
	}
	if resp.Error != "" {
		conn.Close()
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
