package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

type badSection struct{ key string }

func (s badSection) Key() string           { return s.key }
func (s badSection) Clone() config.Section { return s }
func (s badSection) Validate() error       { return errors.New("bad section") }

func TestReconfigureRejectsInvalidSection(t *testing.T) {
	m := config.NewModel()
	s := New(m, nil)
	c := &configurableComp{name: "AFP", log: &orderLog{}}
	s.Add(c, nil)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	if err := s.Reconfigure(context.Background(), "AFP", badSection{key: "AFP"}); err == nil {
		t.Fatal("Reconfigure should reject a section that fails Validate")
	}
	if _, ok := m.Get("AFP"); ok {
		t.Fatal("invalid section must not be written to the live model")
	}
	if c.applied != 0 {
		t.Fatalf("ApplyConfig called %d times on invalid section", c.applied)
	}
}

func TestSetWellKnownRejectsInvalidLogging(t *testing.T) {
	s := New(config.NewModel(), nil)
	err := s.SetWellKnown(context.Background(), "Logging", json.RawMessage(`{"Level":"nope"}`))
	if err == nil {
		t.Fatal("SetWellKnown should reject an unknown log level")
	}
	if !strings.Contains(err.Error(), "log level") {
		t.Fatalf("error %q should mention log level", err)
	}
	if s.Model().Logging.Level != "" {
		t.Fatalf("live Logging mutated: %+v", s.Model().Logging)
	}
}

func TestSetWellKnownRejectsInvalidHTTP(t *testing.T) {
	s := New(config.NewModel(), nil)
	err := s.SetWellKnown(context.Background(), "HTTP", json.RawMessage(`{"Enabled":true,"Addr":"not-a-port"}`))
	if err == nil {
		t.Fatal("SetWellKnown should reject an invalid HTTP listen address")
	}
}

func TestSetWellKnownLoggingAppliesLevel(t *testing.T) {
	s := New(config.NewModel(), nil)
	var got string
	s.SetLogLevelApplier(func(level string) { got = level })
	if err := s.SetWellKnown(context.Background(), "Logging", json.RawMessage(`{"Level":"debug"}`)); err != nil {
		t.Fatalf("SetWellKnown Logging: %v", err)
	}
	if got != "debug" {
		t.Fatalf("log-level applier got %q, want debug", got)
	}
	if s.Model().Logging.Level != "debug" {
		t.Fatalf("model Logging.Level = %q", s.Model().Logging.Level)
	}
}

func TestSetWellKnownFUSEReconfiguresClient(t *testing.T) {
	log := &orderLog{}
	s := New(config.NewModel(), nil)
	c := &configurableComp{name: config.ClientKey, log: log}
	s.Add(c, nil)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	c.applied = 0
	log.seq = nil

	if err := s.SetWellKnown(context.Background(), config.FUSEKey, json.RawMessage(`{"MountTimeoutSeconds":12}`)); err != nil {
		t.Fatalf("SetWellKnown FUSE: %v", err)
	}
	if c.applied != 1 {
		t.Fatalf("Client ApplyConfig called %d times, want 1", c.applied)
	}
	if s.Model().FUSE.MountTimeoutSeconds != 12 {
		t.Fatalf("FUSE timeout = %d", s.Model().FUSE.MountTimeoutSeconds)
	}
}

func TestSetWellKnownIdentityRestartsConsumers(t *testing.T) {
	log := &orderLog{}
	s := New(config.NewModel(), nil)
	smb := &recordingComponent{name: "SMB", log: log}
	s.Add(smb, nil)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	log.seq = nil

	var stamped string
	s.SetIdentityStamper(func(c component.Component, m *config.Model) {
		if c.Name() == "SMB" {
			stamped = m.Identity.Hostname
		}
	})
	if err := s.SetWellKnown(context.Background(), config.IdentityKey, json.RawMessage(`{"Hostname":"FILEBOX"}`)); err != nil {
		t.Fatalf("SetWellKnown Identity: %v", err)
	}
	if stamped != "FILEBOX" {
		t.Fatalf("identity stamper hostname = %q", stamped)
	}
	if s.Model().Identity.Hostname != "FILEBOX" {
		t.Fatalf("model hostname = %q", s.Model().Identity.Hostname)
	}
	want := []string{"stop:SMB", "start:SMB"}
	if got := log.seq; len(got) < 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("SMB lifecycle = %v, want restart %v", got, want)
	}
}

func TestReplaceModelCopiesFUSE(t *testing.T) {
	s := New(config.NewModel(), nil)
	next := config.NewModel()
	next.FUSE.MountTimeoutSeconds = 9
	if err := s.ReplaceModel(context.Background(), next); err != nil {
		t.Fatalf("ReplaceModel: %v", err)
	}
	if s.Model().FUSE.MountTimeoutSeconds != 9 {
		t.Fatalf("FUSE not copied: %+v", s.Model().FUSE)
	}
}
