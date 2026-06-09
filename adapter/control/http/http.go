package http

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/control/inproc"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
)

// Client is the HTTP control Client interface.
type Client = inproc.Client

// Server exposes control.Plane over HTTP.
type Server struct {
	plane    control.Plane
	addr     string
	listener net.Listener
	server   *http.Server
	mu       sync.Mutex
	closed   bool
	wg       sync.WaitGroup
}

// NewServer builds an HTTP Server for the plane on address addr.
func NewServer(plane control.Plane, addr string) *Server {
	return &Server{
		plane: plane,
		addr:  addr,
	}
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	l, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = l
	s.addr = l.Addr().String() // update to resolved address (e.g. if :0 was used)

	mux := http.NewServeMux()
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/start", s.handleStart)
	mux.HandleFunc("/stop", s.handleStop)
	mux.HandleFunc("/restart", s.handleRestart)
	mux.HandleFunc("/list_fs_types", s.handleListFSTypes)
	mux.HandleFunc("/reconfigure", s.handleReconfigure)
	mux.HandleFunc("/subscribe", s.handleSubscribe)

	s.server = &http.Server{Handler: mux}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		_ = s.server.Serve(s.listener)
	}()

	return nil
}

// Addr returns the listener address.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// Stop shuts down the HTTP server.
func (s *Server) Stop() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		if err := s.server.Shutdown(ctx); err != nil {
			_ = s.server.Close()
		}
		cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	res := s.plane.Status()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct{ Name string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.plane.Start(r.Context(), body.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct{ Name string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.plane.Stop(r.Context(), body.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct{ Name string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.plane.Restart(r.Context(), body.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleListFSTypes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	res := s.plane.ListFSTypes()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *Server) handleReconfigure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Name    string          `json:"name"`
		Section json.RawMessage `json:"section"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var typedSec config.Section
	if schema, ok := config.SchemaFor(body.Name); ok {
		typedSec = schema.New()
		if err := json.Unmarshal(body.Section, typedSec); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	if err := s.plane.Reconfigure(r.Context(), body.Name, typedSec); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	topicsStr := r.URL.Query().Get("topics")
	var topics []string
	if topicsStr != "" {
		topics = strings.Split(topicsStr, ",")
	}

	ch, cancel := s.plane.Subscribe(topics...)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, open := <-ch:
			if !open {
				return
			}
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Topic(), b)
			flusher.Flush()
		}
	}
}

// AdapterClient implements Client (inproc.Client) over HTTP/SSE.
type AdapterClient struct {
	baseURL string
	client  *http.Client
}

// NewClient returns a new HTTP client adapter.
func NewClient(baseURL string) *AdapterClient {
	return &AdapterClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

var _ Client = (*AdapterClient)(nil)

func (c *AdapterClient) post(path string, body any) error {
	b, _ := json.Marshal(body)
	res, err := c.client.Post(c.baseURL+path, "application/json", bytesReader(b))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP error: %s", res.Status)
	}
	return nil
}

func bytesReader(b []byte) *strings.Reader {
	return strings.NewReader(string(b))
}

// Status returns status of components.
func (c *AdapterClient) Status() ([]control.Unit, error) {
	res, err := c.client.Get(c.baseURL + "/status")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %s", res.Status)
	}
	var out []control.Unit
	err = json.NewDecoder(res.Body).Decode(&out)
	return out, err
}

// Reconfigure configures component.
func (c *AdapterClient) Reconfigure(ctx context.Context, name string, section config.Section) error {
	secBytes, _ := json.Marshal(section)
	body := struct {
		Name    string          `json:"name"`
		Section json.RawMessage `json:"section"`
	}{Name: name, Section: secBytes}
	return c.post("/reconfigure", body)
}

// Start starts component.
func (c *AdapterClient) Start(ctx context.Context, name string) error {
	return c.post("/start", struct{ Name string }{Name: name})
}

// Stop stops component.
func (c *AdapterClient) Stop(ctx context.Context, name string) error {
	return c.post("/stop", struct{ Name string }{Name: name})
}

// Restart restarts component.
func (c *AdapterClient) Restart(ctx context.Context, name string) error {
	return c.post("/restart", struct{ Name string }{Name: name})
}

// ListFSTypes returns FS types.
func (c *AdapterClient) ListFSTypes() ([]string, error) {
	res, err := c.client.Get(c.baseURL + "/list_fs_types")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %s", res.Status)
	}
	var out []string
	err = json.NewDecoder(res.Body).Decode(&out)
	return out, err
}

// Subscribe returns event stream.
func (c *AdapterClient) Subscribe(topics ...string) (<-chan bus.Event, func(), error) {
	url := fmt.Sprintf("%s/subscribe?topics=%s", c.baseURL, strings.Join(topics, ","))
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}

	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, nil, fmt.Errorf("SSE connection failed: %s", res.Status)
	}

	outCh := make(chan bus.Event, 16)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		defer close(outCh)
		defer res.Body.Close()
		scanner := bufio.NewScanner(res.Body)

		var eventType string
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()
			if line == "" {
				continue
			}

			if strings.HasPrefix(line, "event: ") {
				eventType = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				dataStr := strings.TrimPrefix(line, "data: ")
				var ev bus.Event
				switch eventType {
				case bus.TopicState:
					var sc bus.StateChanged
					_ = json.Unmarshal([]byte(dataStr), &sc)
					ev = sc
				case bus.TopicStats:
					var ss bus.StatSample
					_ = json.Unmarshal([]byte(dataStr), &ss)
					ev = ss
				case bus.TopicLog:
					var lr bus.LogRecord
					_ = json.Unmarshal([]byte(dataStr), &lr)
					ev = lr
				}

				if ev != nil {
					select {
					case outCh <- ev:
					default:
					}
				}
			}
		}
	}()

	return outCh, func() { cancel() }, nil
}
