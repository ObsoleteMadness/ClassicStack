package etherdfs

import (
	"errors"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	etherport "github.com/ObsoleteMadness/ClassicStack/core/port/etherdfs"
)

// newPortedService builds a service over a real (inert, nil-opener) port so the
// ApplyConfig paths that touch the embedded port can be exercised.
func newPortedService(t *testing.T, enabled bool) *Service {
	t.Helper()
	sec := (&ServerSection{SKey: ServerKey, IsEnabled: enabled}).PortSection()
	p, err := etherport.NewInstanceFromOpener(sec, nil, [6]byte{}, log.New(Name))
	if err != nil {
		t.Fatalf("NewInstanceFromOpener: %v", err)
	}
	s := New(p, log.New(Name))
	if s == nil {
		t.Fatal("New returned nil service for a built port")
	}
	return s
}

// TestApplyConfigServerSection: the advertised name applies live; an enabled-flag
// flip answers ErrNeedsRestart (the link is opened/closed per Start); a same-flag
// apply is a hot no-restart apply.
func TestApplyConfigServerSection(t *testing.T) {
	s := newPortedService(t, false)

	// Same enabled state: hot apply, name set live.
	if err := s.ApplyConfig(&ServerSection{SKey: ServerKey, IsEnabled: false, ServerName: "DOSBOX"}); err != nil {
		t.Fatalf("hot apply: %v", err)
	}
	if got := s.serverName(); got != "DOSBOX" {
		t.Errorf("serverName = %q, want DOSBOX", got)
	}

	// Enabled flip: needs a restart so the opener re-evaluates the flag.
	err := s.ApplyConfig(&ServerSection{SKey: ServerKey, IsEnabled: true, ServerName: "DOSBOX"})
	if !errors.Is(err, component.ErrNeedsRestart) {
		t.Errorf("enabled flip error = %v, want ErrNeedsRestart", err)
	}
	if !s.Enabled() {
		t.Error("Enabled() = false after enabling apply")
	}

	// An empty name re-resolves through the installed fallback (Identity.Hostname).
	s.SetServerNameResolver(func() string { return "HOSTNAME" })
	if err := s.ApplyConfig(&ServerSection{SKey: ServerKey, IsEnabled: true}); err != nil {
		t.Fatalf("hot apply (empty name): %v", err)
	}
	if got := s.serverName(); got != "HOSTNAME" {
		t.Errorf("serverName = %q, want HOSTNAME (resolver fallback)", got)
	}
}

// TestApplyConfigReconcilesDrives: a nil section (the owner-notify after an
// EtherDFSDrives add/remove/edit) re-resolves the drive set via the resolver.
func TestApplyConfigReconcilesDrives(t *testing.T) {
	s := newPortedService(t, true)
	dir := t.TempDir()
	s.SetDriveResolver(func() ([]DriveSpec, error) {
		return []DriveSpec{{Name: "E", Share: fs.ShareSpec{
			FSType:      "local_fs",
			MetaBackend: "metastore",
			Metastore:   "mem",
			Path:        dir,
		}}}, nil
	})
	if err := s.ApplyConfig(nil); err != nil {
		t.Fatalf("ApplyConfig(nil): %v", err)
	}
	if got := s.driveCount(); got != 1 {
		t.Fatalf("driveCount = %d, want 1", got)
	}
	if _, ok := s.drive(4); !ok { // E = 4
		t.Error("drive E not bound after reconcile")
	}
}

// TestApplyConfigNoResolverNeedsRestart: without a drive resolver a non-server
// section cannot be absorbed live.
func TestApplyConfigNoResolverNeedsRestart(t *testing.T) {
	s := newPortedService(t, true)
	if err := s.ApplyConfig(nil); !errors.Is(err, component.ErrNeedsRestart) {
		t.Errorf("ApplyConfig(nil) without resolver = %v, want ErrNeedsRestart", err)
	}
}
