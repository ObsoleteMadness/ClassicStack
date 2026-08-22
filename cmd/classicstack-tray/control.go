//go:build darwin || windows

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// streamHTTPClient has no timeout, unlike controlClient.http (5s) — used
// only for the long-lived SSE connection in notify.go, which would
// otherwise get cut off mid-stream.
var streamHTTPClient = &http.Client{}

// errUnauthorized is returned by post when the control API rejects the
// request for missing/bad HTTP Basic credentials (adapter/control/http/auth.go
// authGate, once an admin is configured).
var errUnauthorized = errors.New("classicstack-tray: control API requires admin credentials")

// errSetupRequired is returned by post when no admin has been configured yet
// (adapter/control/http/auth.go authGate refuses every route but /setup with
// 409 in that state) — Restart/Shutdown cannot work until setup completes.
var errSetupRequired = errors.New("classicstack-tray: complete initial setup via Open Interface first")

// controlBaseURL turns an [http] listen address (e.g. ":1984", the
// core/config.DefaultHTTPAddr) into a base URL the tray can call. An empty
// addr falls back to the default control port.
func controlBaseURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = config.DefaultHTTPAddr
	}
	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr
	}
	return "http://" + addr
}

// controlClient talks to the ClassicStack web-admin control API
// (adapter/control/http) with just the handful of calls the tray needs —
// not the full AdapterClient, which pulls in the whole control plane.
type controlClient struct {
	baseURL string
	http    *http.Client

	mu         sync.Mutex
	user, pass string
}

func newControlClient(baseURL string) *controlClient {
	return &controlClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

// setAuth attaches HTTP Basic credentials to subsequent restart/shutdown
// calls, mirroring how the web admin authenticates once an admin exists
// (adapter/control/http/auth.go authGate).
func (c *controlClient) setAuth(user, pass string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.user, c.pass = user, pass
}

// stackState is what the tray's Status item reports.
type stackState int

const (
	stateStopped stackState = iota
	stateRunning
	// stateSetupRequired means the process is up but authGate is refusing
	// every route with 409 until an admin is created via /setup (see
	// AdapterClient.SetupRequired) — Restart/Shutdown will fail until then.
	stateSetupRequired
)

// status probes /status — the same route the web admin dashboard reads
// (adapter/control/http/http.go handleStatus) — to determine the process
// state. A connection error or timeout means the process isn't running;
// any HTTP response, even a non-200/401 one, means it is.
func (c *controlClient) status() stackState {
	resp, err := c.http.Get(c.baseURL + "/status")
	if err != nil {
		return stateStopped
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusConflict {
		return stateSetupRequired
	}
	return stateRunning
}

func (c *controlClient) post(path string) error {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	c.mu.Lock()
	user, pass := c.user, c.pass
	c.mu.Unlock()
	if user != "" {
		req.SetBasicAuth(user, pass)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return errUnauthorized
	case http.StatusConflict:
		return errSetupRequired
	default:
		return fmt.Errorf("%s: %s", path, resp.Status)
	}
}

// waitUntil polls status until ok reports true or timeout elapses.
// handleShutdown/handleStackRestart run the stop/restart asynchronously (`go
// s.lifecycle.Shutdown()`) and return 200 immediately, so a 200 response only
// means the request was accepted, not that the process has actually stopped
// (or come back up) yet — callers that need to know the real end state
// (Shutdown, Quit, Start) must poll.
func (c *controlClient) waitUntil(timeout time.Duration, ok func(stackState) bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if ok(c.status()) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func (c *controlClient) waitUntilStopped(timeout time.Duration) bool {
	return c.waitUntil(timeout, func(s stackState) bool { return s == stateStopped })
}

func (c *controlClient) waitUntilRunning(timeout time.Duration) bool {
	return c.waitUntil(timeout, func(s stackState) bool { return s != stateStopped })
}

// subscribe opens the control API's SSE event stream (adapter/control/http
// handleSubscribe) for the topics notify.go cares about. The caller owns the
// response body and must close it; ctx cancellation is how notify.go tears
// down the connection.
func (c *controlClient) subscribe(ctx context.Context) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/subscribe?topics=log,message", nil)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	user, pass := c.user, c.pass
	c.mu.Unlock()
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	return streamHTTPClient.Do(req)
}

// restart triggers a graceful whole-process restart via
// adapter/control/http/lifecycle.go handleStackRestart.
func (c *controlClient) restart() error { return c.post("/stack_restart") }

// shutdown triggers a graceful whole-process stop via
// adapter/control/http/lifecycle.go handleShutdown.
func (c *controlClient) shutdown() error { return c.post("/shutdown") }
