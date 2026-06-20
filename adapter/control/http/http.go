package http

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/control/inproc"
	"github.com/ObsoleteMadness/ClassicStack/adapter/extmap"
	"github.com/ObsoleteMadness/ClassicStack/adapter/serial"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
)

// statusForErr maps a control error to an HTTP status. control.ErrUnavailable —
// "not in this build / no store wired" — becomes 501 Not Implemented, which the
// client maps back to control.ErrUnavailable so errors.Is works across the wire.
// Everything else is a 500.
func statusForErr(err error) int {
	if errors.Is(err, control.ErrUnavailable) {
		return http.StatusNotImplemented
	}
	return http.StatusInternalServerError
}

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
	mux.HandleFunc("/config", s.handleConfig)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/start", s.handleStart)
	mux.HandleFunc("/stop", s.handleStop)
	mux.HandleFunc("/restart", s.handleRestart)
	mux.HandleFunc("/save", s.handleSave)
	mux.HandleFunc("/list_fs_types", s.handleListFSTypes)
	mux.HandleFunc("/params_for", s.handleParamsFor)
	mux.HandleFunc("/list_interfaces", s.handleListInterfaces)
	mux.HandleFunc("/list_zones", s.handleListZones)
	mux.HandleFunc("/reconfigure", s.handleReconfigure)
	mux.HandleFunc("/add_instance", s.handleAddInstance)
	mux.HandleFunc("/remove_instance", s.handleRemoveInstance)
	mux.HandleFunc("/extmap", s.handleExtMap)
	mux.HandleFunc("/config_download", s.handleConfigDownload)
	mux.HandleFunc("/list_serial_ports", s.handleListSerialPorts)
	mux.HandleFunc("/browse_path", s.handleBrowsePath)
	mux.HandleFunc("/users", s.handleUsers)
	mux.HandleFunc("/set_user", s.handleSetUser)
	mux.HandleFunc("/set_user_disabled", s.handleSetUserDisabled)
	mux.HandleFunc("/remove_user", s.handleRemoveUser)
	mux.HandleFunc("/setup", s.handleSetup)
	mux.HandleFunc("/subscribe", s.handleSubscribe)

	// Mount the embedded SPA at "/" (webui||all builds only; a no-op otherwise). The
	// specific API routes above win over "/" by ServeMux longest-prefix, so the page
	// is served only for the static paths and the index.
	s.mountSPA(mux)

	// Wrap every route in the web-admin access gate (§4-ter): first-run setup until an
	// admin is configured, HTTP Basic auth thereafter. The SPA's own static assets are
	// exempted inside the gate so the page can load to drive setup/login.
	s.server = &http.Server{Handler: s.authGate(mux)}

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

func (s *Server) handleParamsFor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	res := s.plane.ParamsFor(r.URL.Query().Get("fs_type"))
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

// handleAddInstance stages a new repeated-section instance (an AFP volume / SMB share)
// and reconciles the owning service. The body is {owner, key, section}: owner is the
// component that consumes the list ("AFP"/"SMB"), key is the schema key the section is
// registered under ("AFPVolumes"/"SMBShares"), section is the instance. The section is
// unmarshalled through the schema registry (like handleReconfigure) and must be a
// NamedSection.
func (s *Server) handleAddInstance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Owner   string          `json:"owner"`
		Key     string          `json:"key"`
		Section json.RawMessage `json:"section"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	schema, ok := config.SchemaFor(body.Key)
	if !ok {
		http.Error(w, "unknown section key: "+body.Key, http.StatusBadRequest)
		return
	}
	sec := schema.New()
	if err := json.Unmarshal(body.Section, sec); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ns, ok := sec.(config.NamedSection)
	if !ok {
		http.Error(w, "section is not a repeated (named) instance: "+body.Key, http.StatusBadRequest)
		return
	}
	if err := s.plane.AddInstance(r.Context(), body.Owner, ns); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleRemoveInstance drops a named instance and reconciles the owner. Body is
// {owner, key, name}.
func (s *Server) handleRemoveInstance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Owner string `json:"owner"`
		Key   string `json:"key"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.plane.RemoveInstance(r.Context(), body.Owner, body.Key, body.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleExtMap reads (GET ?path=…) or writes (POST {path, content}) an AFP extension-
// map file. A path is server-local, so this lives on the HTTP server, not the
// transport-agnostic control surface. POST validates the content (it must parse as a
// Netatalk extension map) and writes a numbered backup of any prior file.
func (s *Server) handleExtMap(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		path := r.URL.Query().Get("path")
		if path == "" {
			writeJSONError(w, http.StatusBadRequest, "missing path")
			return
		}
		data, err := extmap.Read(path)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}{Path: path, Content: string(data)})
	case http.MethodPost:
		var body struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.Path == "" {
			writeJSONError(w, http.StatusBadRequest, "missing path")
			return
		}
		backup, err := extmap.Save(body.Path, []byte(body.Content))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error()) // validation / write error
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Saved  bool   `json:"saved"`
			Backup string `json:"backup"`
		}{Saved: true, Backup: backup})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleConfigDownload serves the live (masked) model serialised through the codec —
// the on-disk TOML/UCI form — as a downloadable attachment, the faithful "backup
// server.toml" the JSON Config() shape cannot provide.
func (s *Server) handleConfigDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := s.plane.MarshalConfig()
	if err != nil {
		http.Error(w, err.Error(), statusForErr(err))
		return
	}
	w.Header().Set("Content-Type", "application/toml")
	w.Header().Set("Content-Disposition", `attachment; filename="server.toml"`)
	_, _ = w.Write(data)
}

// handleListSerialPorts returns the host serial ports (the TashTalk dropdown). A
// server-local enumeration, so it lives on the HTTP server, not the shared control
// surface. Errors degrade to an empty list (no serial ports / no permission).
func (s *Server) handleListSerialPorts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ports, err := serial.ListPorts()
	if err != nil {
		ports = nil // best-effort: an enumeration failure is an empty dropdown
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ports)
}

// handleBrowsePath lists the DIRECTORIES under dir so the operator can pick a volume /
// share path without typing. The path is cleaned and resolved; only directories are
// returned (files are not pickable share roots). An empty dir starts at the server's
// working directory. SECURITY: this exposes the server's directory tree to an
// authenticated admin — acceptable under the honest-security posture (the admin already
// edits paths), but it is gated by the auth gate like every data route, returns only
// directory names, and never reads file contents.
func (s *Server) handleBrowsePath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		if wd, err := os.Getwd(); err == nil {
			dir = wd
		} else {
			dir = "."
		}
	}
	dir = filepath.Clean(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	type entry struct {
		Name string `json:"name"`
		Dir  bool   `json:"dir"`
	}
	out := make([]entry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, entry{Name: e.Name(), Dir: true})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	parent := filepath.Dir(dir)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Path    string  `json:"path"`
		Parent  string  `json:"parent"`
		Entries []entry `json:"entries"`
	}{Path: dir, Parent: parent, Entries: out})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	m, err := s.plane.Config()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(m)
}

func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rev, err := s.plane.Save(r.Context())
	if err != nil {
		http.Error(w, err.Error(), statusForErr(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Revision string `json:"revision"`
	}{Revision: rev})
}

func (s *Server) handleListInterfaces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	res, err := s.plane.ListInterfaces()
	if err != nil {
		http.Error(w, err.Error(), statusForErr(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *Server) handleListZones(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	res, err := s.plane.Diagnostics().ListZones(r.Context())
	if err != nil {
		http.Error(w, err.Error(), statusForErr(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	res, err := s.plane.Users()
	if err != nil {
		http.Error(w, err.Error(), statusForErr(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *Server) handleSetUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.plane.SetUser(body.Name, body.Password); err != nil {
		http.Error(w, err.Error(), statusForErr(err))
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleSetUserDisabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Name     string `json:"name"`
		Disabled bool   `json:"disabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.plane.SetUserDisabled(body.Name, body.Disabled); err != nil {
		http.Error(w, err.Error(), statusForErr(err))
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleRemoveUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.plane.RemoveUser(body.Name); err != nil {
		http.Error(w, err.Error(), statusForErr(err))
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

// NewClient returns a new HTTP client adapter with no credentials — the form used for
// first-run (/setup) and for a server with no admin gate.
func NewClient(baseURL string) *AdapterClient {
	return &AdapterClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// NewClientWithAuth returns a client that attaches HTTP Basic credentials to every
// request (including the SSE subscribe stream) via a RoundTripper, so a gated server
// accepts it. Used once an admin is configured.
func NewClientWithAuth(baseURL, user, pass string) *AdapterClient {
	return &AdapterClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client: &http.Client{
			Timeout:   10 * time.Second,
			Transport: &basicAuthTransport{user: user, pass: pass, base: http.DefaultTransport},
		},
	}
}

// basicAuthTransport injects an Authorization: Basic header on every request, so all
// client paths (the helper-based ones and the direct Get/Post/SSE ones) carry creds
// without per-method edits.
type basicAuthTransport struct {
	user, pass string
	base       http.RoundTripper
}

func (t *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone so we never mutate the caller's request (RoundTripper contract).
	r2 := req.Clone(req.Context())
	r2.SetBasicAuth(t.user, t.pass)
	return t.base.RoundTrip(r2)
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
		return errForStatus(res.StatusCode, res.Status)
	}
	return nil
}

// getJSON GETs path and decodes the JSON body into dest, mapping 501 to
// control.ErrUnavailable (the round-trip of the "not in this build" sentinel).
func (c *AdapterClient) getJSON(path string, dest any) error {
	res, err := c.client.Get(c.baseURL + path)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errForStatus(res.StatusCode, res.Status)
	}
	if dest == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(dest)
}

// errForStatus turns a non-200 into an error, surfacing control.ErrUnavailable for
// 501 so a caller can errors.Is it exactly as the in-process adapter reports it.
func errForStatus(code int, status string) error {
	if code == http.StatusNotImplemented {
		return control.ErrUnavailable
	}
	return fmt.Errorf("HTTP error: %s", status)
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

// AddInstance adds a repeated-section instance (an AFP volume / SMB share).
func (c *AdapterClient) AddInstance(ctx context.Context, owner string, section config.NamedSection) error {
	secBytes, _ := json.Marshal(section)
	body := struct {
		Owner   string          `json:"owner"`
		Key     string          `json:"key"`
		Section json.RawMessage `json:"section"`
	}{Owner: owner, Key: section.Key(), Section: secBytes}
	return c.post("/add_instance", body)
}

// RemoveInstance drops a named repeated-section instance.
func (c *AdapterClient) RemoveInstance(ctx context.Context, owner, key, instanceName string) error {
	return c.post("/remove_instance", struct {
		Owner string `json:"owner"`
		Key   string `json:"key"`
		Name  string `json:"name"`
	}{Owner: owner, Key: key, Name: instanceName})
}

// ExtMap reads the AFP extension-map file at path (HTTP-server-side surface, not on the
// shared Client interface). Returns the file content (empty for a missing file).
func (c *AdapterClient) ExtMap(path string) (string, error) {
	var out struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := c.getJSON("/extmap?path="+url.QueryEscape(path), &out); err != nil {
		return "", err
	}
	return out.Content, nil
}

// SaveExtMap validates and writes the extension-map file, returning the backup path.
func (c *AdapterClient) SaveExtMap(path, content string) (string, error) {
	b, _ := json.Marshal(struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}{Path: path, Content: content})
	res, err := c.client.Post(c.baseURL+"/extmap", "application/json", bytesReader(b))
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", errForStatus(res.StatusCode, res.Status)
	}
	var out struct {
		Backup string `json:"backup"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	return out.Backup, nil
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

// ParamsFor returns the config-param schema for one fs_type (GET /params_for).
func (c *AdapterClient) ParamsFor(fsType string) ([]control.ParamInfo, error) {
	var out []control.ParamInfo
	if err := c.getJSON("/params_for?fs_type="+url.QueryEscape(fsType), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Config fetches a snapshot of the live config model.
func (c *AdapterClient) Config() (*config.Model, error) {
	m := config.NewModel()
	if err := c.getJSON("/config", m); err != nil {
		return nil, err
	}
	return m, nil
}

// Setup creates the first-run admin credential (POST /setup), returning the new config
// revision. HTTP-only surface (Basic auth is an HTTP-transport concern), so it is on
// the concrete client, not the shared Client interface. Fails (409) if already set.
func (c *AdapterClient) Setup(user, password string) (string, error) {
	body, _ := json.Marshal(struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}{User: user, Password: password})
	res, err := c.client.Post(c.baseURL+"/setup", "application/json", bytesReader(body))
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", errForStatus(res.StatusCode, res.Status)
	}
	var out struct {
		Revision string `json:"revision"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Revision, nil
}

// SetupRequired reports whether the server is in first-run state (no admin set). It
// probes /status: a 409 means setup is required, 200/401 means an admin exists.
func (c *AdapterClient) SetupRequired() (bool, error) {
	res, err := c.client.Get(c.baseURL + "/status")
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	return res.StatusCode == http.StatusConflict, nil
}

// Save validates and persists the live model server-side, returning the revision.
func (c *AdapterClient) Save(ctx context.Context) (string, error) {
	_ = ctx
	res, err := c.client.Post(c.baseURL+"/save", "application/json", strings.NewReader("{}"))
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", errForStatus(res.StatusCode, res.Status)
	}
	var out struct {
		Revision string `json:"revision"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Revision, nil
}

// ListInterfaces returns the enumerable network interfaces.
func (c *AdapterClient) ListInterfaces() ([]control.InterfaceInfo, error) {
	var out []control.InterfaceInfo
	err := c.getJSON("/list_interfaces", &out)
	return out, err
}

// ListZones runs the Diagnostics zone probe (control.ErrUnavailable when unsupported).
func (c *AdapterClient) ListZones(ctx context.Context) ([]string, error) {
	_ = ctx
	var out []string
	err := c.getJSON("/list_zones", &out)
	return out, err
}

// Users lists stored identities (control.ErrUnavailable when no store is wired).
func (c *AdapterClient) Users() ([]control.UserInfo, error) {
	var out []control.UserInfo
	err := c.getJSON("/users", &out)
	return out, err
}

// SetUser adds a user or resets a password.
func (c *AdapterClient) SetUser(name, password string) error {
	return c.post("/set_user", struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}{Name: name, Password: password})
}

// SetUserDisabled parks/unparks an account.
func (c *AdapterClient) SetUserDisabled(name string, disabled bool) error {
	return c.post("/set_user_disabled", struct {
		Name     string `json:"name"`
		Disabled bool   `json:"disabled"`
	}{Name: name, Disabled: disabled})
}

// RemoveUser deletes a user.
func (c *AdapterClient) RemoveUser(name string) error {
	return c.post("/remove_user", struct {
		Name string `json:"name"`
	}{Name: name})
}

// Subscribe returns event stream.
func (c *AdapterClient) Subscribe(topics ...string) (<-chan bus.Event, func(), error) {
	url := fmt.Sprintf("%s/subscribe?topics=%s", c.baseURL, strings.Join(topics, ","))
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}

	res, err := c.client.Do(req)
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
