package control

import (
	"context"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/pkg/logbuf"
)

// fakeModel is a minimal ConfigModel for lifecycle tests.
type fakeModel struct{ toml string }

func (f *fakeModel) ToTOML() ([]byte, error) { return []byte(f.toml), nil }

// fakeSup records Apply/Restart calls.
type fakeSup struct {
	applied  int
	restarts []string
}

func (s *fakeSup) Apply(_ context.Context, _ ConfigModel) error   { s.applied++; return nil }
func (s *fakeSup) StartService(_ context.Context, _ string) error { return nil }
func (s *fakeSup) StopService(_ string) error                     { return nil }
func (s *fakeSup) RestartService(_ context.Context, name string) error {
	s.restarts = append(s.restarts, name)
	return nil
}
func (s *fakeSup) ListInterfaces() ([]InterfaceInfo, error) {
	return []InterfaceInfo{{Name: "eth0", Description: "Ethernet"}}, nil
}
func (s *fakeSup) ListFSTypes() []string { return []string{"local_fs"} }

func TestListInterfacesAndFSTypes(t *testing.T) {
	p := New(Deps{Supervisor: &fakeSup{}})

	ifaces, err := p.ListInterfaces()
	if err != nil {
		t.Fatalf("ListInterfaces: %v", err)
	}
	if len(ifaces) != 1 || ifaces[0].Name != "eth0" || ifaces[0].Description != "Ethernet" {
		t.Fatalf("ListInterfaces = %+v, want one eth0/Ethernet", ifaces)
	}

	fsTypes := p.ListFSTypes()
	if len(fsTypes) != 1 || fsTypes[0] != "local_fs" {
		t.Fatalf("ListFSTypes = %v, want [local_fs]", fsTypes)
	}
}

func TestDirtyLifecycle(t *testing.T) {
	sup := &fakeSup{}
	live := &fakeModel{toml: "live"}
	p := New(Deps{Supervisor: sup, Config: live, ConfigPath: ""})

	if p.Dirty() {
		t.Fatal("new plane should not be dirty")
	}

	// Stage marks dirty and Config returns the staged model.
	staged := &fakeModel{toml: "staged"}
	p.Stage(staged)
	if !p.Dirty() {
		t.Fatal("plane should be dirty after Stage")
	}
	cfg, dirty := p.Config()
	if !dirty || cfg != staged {
		t.Fatalf("Config after Stage = (%v, %v), want (staged, true)", cfg, dirty)
	}

	// Apply pushes to supervisor and promotes staged to live but stays dirty.
	if err := p.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if sup.applied != 1 {
		t.Errorf("supervisor Apply called %d times, want 1", sup.applied)
	}
	if !p.Dirty() {
		t.Error("plane should remain dirty after Apply (only Save clears it)")
	}
}

func TestSaveClearsDirty(t *testing.T) {
	sup := &fakeSup{}
	p := New(Deps{Supervisor: sup, Config: &fakeModel{}, ConfigPath: "/tmp/x.toml"})
	p.Stage(&fakeModel{toml: "edited"})

	var savedPath string
	SetSaver(func(path string, _ ConfigModel) (string, error) {
		savedPath = path
		return path + ".0001", nil
	})

	backup, err := p.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if savedPath != "/tmp/x.toml" || backup != "/tmp/x.toml.0001" {
		t.Errorf("Save path=%q backup=%q unexpected", savedPath, backup)
	}
	if p.Dirty() {
		t.Error("plane should be clean after Save")
	}
}

func TestSaveWithoutPath(t *testing.T) {
	p := New(Deps{Config: &fakeModel{}})
	if _, err := p.Save(); err != ErrNoConfigPath {
		t.Errorf("Save without path = %v, want ErrNoConfigPath", err)
	}
}

func TestLogHistoryAndSubscribe(t *testing.T) {
	buf := logbuf.New(8)
	p := New(Deps{Config: &fakeModel{}, Logs: buf})

	buf.Append(logbuf.Entry{Message: "first"})

	hist := p.LogHistory()
	if len(hist) != 1 || hist[0].Message != "first" {
		t.Fatalf("LogHistory = %+v, want [first]", hist)
	}

	ch, cancel := p.SubscribeLogs()
	defer cancel()
	buf.Append(logbuf.Entry{Message: "live"})
	select {
	case e := <-ch:
		if e.Message != "live" {
			t.Fatalf("subscriber got %q, want live", e.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("log subscriber did not receive entry")
	}
}

func TestLogHistoryDefaultsToGlobal(t *testing.T) {
	p := New(Deps{Config: &fakeModel{}})
	// Should not panic and should return the global buffer's snapshot.
	_ = p.LogHistory()
}

func TestDiagnosticsFallback(t *testing.T) {
	p := New(Deps{Config: &fakeModel{}})
	if _, err := p.Diagnostics().ListZones(context.Background()); err != ErrDiagUnavailable {
		t.Errorf("unset diagnostics = %v, want ErrDiagUnavailable", err)
	}
	if _, err := p.Diagnostics().MacIPLeases(context.Background()); err != ErrDiagUnavailable {
		t.Errorf("unset MacIPLeases = %v, want ErrDiagUnavailable", err)
	}
}
