package http

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestStackLifecycleRequiresAuth(t *testing.T) {
	_, base, _ := newTestServer(t)
	if code, body := postSetup(t, base, "admin", "hunter2"); code != http.StatusOK {
		t.Fatalf("/setup = %d (%s)", code, body)
	}

	for _, path := range []string{"/shutdown", "/stack_restart"} {
		code, _ := get(t, base+path)
		if code != http.StatusUnauthorized {
			t.Fatalf("no-cred POST %s = %d, want 401", path, code)
		}
	}
}

func TestStackLifecycleUnavailableWithoutHooks(t *testing.T) {
	_, base, _ := newTestServer(t)
	if code, body := postSetup(t, base, "admin", "hunter2"); code != http.StatusOK {
		t.Fatalf("/setup = %d (%s)", code, body)
	}

	for _, path := range []string{"/shutdown", "/stack_restart"} {
		code := postAuth(t, base+path, "admin", "hunter2")
		if code != http.StatusNotImplemented {
			t.Fatalf("POST %s = %d, want 501", path, code)
		}
	}
}

func TestStackLifecycleInvokesHooks(t *testing.T) {
	srv, base, _ := newTestServer(t)
	if code, body := postSetup(t, base, "admin", "hunter2"); code != http.StatusOK {
		t.Fatalf("/setup = %d (%s)", code, body)
	}

	var shutdownCalled atomic.Bool
	var restartCalled atomic.Bool
	srv.SetLifecycle(Lifecycle{
		Shutdown: func() { shutdownCalled.Store(true) },
		Restart:  func() { restartCalled.Store(true) },
	})

	if code := postAuth(t, base+"/shutdown", "admin", "hunter2"); code != http.StatusOK {
		t.Fatalf("POST /shutdown = %d, want 200", code)
	}
	waitFor(t, &shutdownCalled)

	if code := postAuth(t, base+"/stack_restart", "admin", "hunter2"); code != http.StatusOK {
		t.Fatalf("POST /stack_restart = %d, want 200", code)
	}
	waitFor(t, &restartCalled)
}

func postAuth(t *testing.T, url, user, pass string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, http.NoBody)
	req.SetBasicAuth(user, pass)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s (auth): %v", url, err)
	}
	res.Body.Close()
	return res.StatusCode
}

func waitFor(t *testing.T, flag *atomic.Bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for !flag.Load() {
		select {
		case <-deadline:
			t.Fatal("lifecycle hook was not invoked")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestRelistenRebinds(t *testing.T) {
	srv, _, _ := newTestServer(t)
	if err := srv.Relisten("127.0.0.1:0"); err != nil {
		t.Fatalf("Relisten: %v", err)
	}
	if srv.Addr() == "" {
		t.Fatal("Relisten left no listen address")
	}
	if err := srv.Relisten(""); err != nil {
		t.Fatalf("Relisten stop: %v", err)
	}
}
