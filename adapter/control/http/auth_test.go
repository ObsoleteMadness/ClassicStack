package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	tomlcodec "github.com/ObsoleteMadness/ClassicStack/adapter/config/toml"
	filestore "github.com/ObsoleteMadness/ClassicStack/adapter/store/file"
	"github.com/ObsoleteMadness/ClassicStack/compose/supervisor"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
)

// newTestServer builds a gated HTTP server over a real supervisor + TOML codec + file
// store, so /setup actually persists server.toml (the end-to-end first-run path).
// Returns the server, its base URL, and the on-disk config path.
func newTestServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	m := config.NewModel()
	telemetry := bus.New(8)
	sup := supervisor.New(m, telemetry)
	cfgPath := filepath.Join(t.TempDir(), "server.toml")
	plane := control.New(sup, tomlcodec.New(), filestore.New(cfgPath), telemetry)

	srv := NewServer(plane, "127.0.0.1:0")
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(srv.Stop)
	return srv, "http://" + srv.Addr(), cfgPath
}

// get issues a bare GET (no credentials) and returns the status code.
func get(t *testing.T, url string) (int, []byte) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer res.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(res.Body)
	return res.StatusCode, buf.Bytes()
}

// getAuth issues a GET with Basic credentials and returns the status code.
func getAuth(t *testing.T, url, user, pass string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.SetBasicAuth(user, pass)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s (auth): %v", url, err)
	}
	res.Body.Close()
	return res.StatusCode
}

// postSetup POSTs a /setup body and returns the status code + raw body.
func postSetup(t *testing.T, base, user, pass string) (int, []byte) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"user": user, "password": pass})
	res, err := http.Post(base+"/setup", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /setup: %v", err)
	}
	defer res.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(res.Body)
	return res.StatusCode, buf.Bytes()
}

// TestFirstRunGate: with no admin, every non-/setup route returns 409 setup_required.
func TestFirstRunGate(t *testing.T) {
	_, base, _ := newTestServer(t)

	code, body := get(t, base+"/status")
	if code != http.StatusConflict {
		t.Fatalf("first-run /status = %d, want 409", code)
	}
	var got map[string]bool
	if err := json.Unmarshal(body, &got); err != nil || !got["setup_required"] {
		t.Fatalf("first-run body = %q, want {\"setup_required\":true}", body)
	}
}

// TestSetupCreatesAdminAndPersists: /setup derives a credential, writes server.toml
// (with an [adminauth] block, no plaintext), and flips the gate to enforce Basic auth.
func TestSetupCreatesAdminAndPersists(t *testing.T) {
	_, base, cfgPath := newTestServer(t)

	code, body := postSetup(t, base, "admin", "hunter2")
	if code != http.StatusOK {
		t.Fatalf("/setup = %d (%s), want 200", code, body)
	}

	// server.toml now carries the hash, never the plaintext.
	raw, err := filestore.New(cfgPath).Load()
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "adminauth") || !strings.Contains(text, "hash") {
		t.Fatalf("persisted config missing [adminauth]/hash:\n%s", text)
	}
	if strings.Contains(text, "hunter2") {
		t.Fatalf("plaintext password leaked into server.toml:\n%s", text)
	}
}

// TestBasicAuthEnforcedAfterSetup: post-setup, routes require valid Basic creds.
func TestBasicAuthEnforcedAfterSetup(t *testing.T) {
	_, base, _ := newTestServer(t)
	if code, body := postSetup(t, base, "admin", "hunter2"); code != http.StatusOK {
		t.Fatalf("/setup = %d (%s)", code, body)
	}

	// No credentials → 401 with a WWW-Authenticate challenge.
	res, err := http.Get(base + "/status")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-cred /status = %d, want 401", res.StatusCode)
	}
	if !strings.HasPrefix(res.Header.Get("WWW-Authenticate"), "Basic ") {
		t.Errorf("missing Basic challenge header: %q", res.Header.Get("WWW-Authenticate"))
	}

	// Wrong password → 401.
	if code := getAuth(t, base+"/status", "admin", "wrong"); code != http.StatusUnauthorized {
		t.Errorf("bad-cred /status = %d, want 401", code)
	}
	// Correct credentials → 200.
	if code := getAuth(t, base+"/status", "admin", "hunter2"); code != http.StatusOK {
		t.Errorf("good-cred /status = %d, want 200", code)
	}
	// Username is matched case-insensitively.
	if code := getAuth(t, base+"/status", "ADMIN", "hunter2"); code != http.StatusOK {
		t.Errorf("case-insensitive user /status = %d, want 200", code)
	}
}

// TestSetupRefusedOnceConfigured: /setup cannot re-bootstrap an existing admin.
func TestSetupRefusedOnceConfigured(t *testing.T) {
	_, base, _ := newTestServer(t)
	if code, _ := postSetup(t, base, "admin", "hunter2"); code != http.StatusOK {
		t.Fatal("initial /setup should succeed")
	}
	if code, _ := postSetup(t, base, "evil", "pw"); code != http.StatusConflict {
		t.Fatalf("second /setup = %d, want 409 already-configured", code)
	}
	// The original admin still works (the second setup did not overwrite it).
	if code := getAuth(t, base+"/status", "admin", "hunter2"); code != http.StatusOK {
		t.Errorf("original admin broken after refused re-setup: %d", code)
	}
}

// TestSetupRejectsEmpty: empty username or password is a 400.
func TestSetupRejectsEmpty(t *testing.T) {
	_, base, _ := newTestServer(t)
	if code, _ := postSetup(t, base, "", "pw"); code != http.StatusBadRequest {
		t.Errorf("empty user /setup = %d, want 400", code)
	}
	if code, _ := postSetup(t, base, "admin", ""); code != http.StatusBadRequest {
		t.Errorf("empty password /setup = %d, want 400", code)
	}
}

// TestAuthedClientRoundTrip: NewClientWithAuth talks to a gated server end-to-end.
func TestAuthedClientRoundTrip(t *testing.T) {
	_, base, _ := newTestServer(t)

	// First-run: a no-auth client sets the admin via Setup.
	boot := NewClient(base)
	if _, err := boot.Setup("admin", "hunter2"); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// An authed client can now reach a gated route; an unauthed one cannot.
	authed := NewClientWithAuth(base, "admin", "hunter2")
	if _, err := authed.Status(); err != nil {
		t.Fatalf("authed Status: %v", err)
	}
	if _, err := NewClient(base).Status(); err == nil {
		t.Fatal("unauthed Status should fail against a gated server")
	}
}
