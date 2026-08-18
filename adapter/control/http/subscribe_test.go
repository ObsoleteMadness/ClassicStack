package http

import (
	"path/filepath"
	"testing"
	"time"

	tomlcodec "github.com/ObsoleteMadness/ClassicStack/adapter/config/toml"
	logbus "github.com/ObsoleteMadness/ClassicStack/adapter/log/bus"
	filestore "github.com/ObsoleteMadness/ClassicStack/adapter/store/file"
	"github.com/ObsoleteMadness/ClassicStack/compose/supervisor"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// TestSubscribeStreamsLogRecords proves log lines written through the bus sink
// (the path the CLI wires for the web UI) reach an SSE /subscribe?topics=log
// client — what the Logs tab consumes.
func TestSubscribeStreamsLogRecords(t *testing.T) {
	m := config.NewModel()
	telemetry := bus.New(16)
	sup := supervisor.New(m, telemetry)
	cfgPath := filepath.Join(t.TempDir(), "server.toml")
	plane := control.New(sup, tomlcodec.New(), filestore.New(cfgPath), telemetry)

	sink := logbus.New(telemetry, log.NewLevelVar(log.Info))
	logger := log.New("control", sink)
	plane.SetLogger(logger)

	srv := NewServer(plane, "127.0.0.1:0")
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(srv.Stop)
	base := "http://" + srv.Addr()

	if code, _ := postSetup(t, base, "admin", "secret"); code != 200 {
		t.Fatalf("setup = %d, want 200", code)
	}

	client := NewClientWithAuth(base, "admin", "secret")
	ch, cancel, err := client.Subscribe(bus.TopicLog)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	// Let the SSE reader attach before publishing.
	time.Sleep(50 * time.Millisecond)
	logger.Log1(log.Info, "control: started", log.Str("component", "AFP"))

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			rec, ok := ev.(bus.LogRecord)
			if !ok {
				continue
			}
			if rec.Component != "control" || rec.Msg != "control: started" {
				t.Fatalf("LogRecord = %+v, want control/control: started", rec)
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for log SSE event")
		}
	}
}

func TestSubscribeStreamsMessages(t *testing.T) {
	m := config.NewModel()
	telemetry := bus.New(16)
	sup := supervisor.New(m, telemetry)
	cfgPath := filepath.Join(t.TempDir(), "server.toml")
	plane := control.New(sup, tomlcodec.New(), filestore.New(cfgPath), telemetry)

	srv := NewServer(plane, "127.0.0.1:0")
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(srv.Stop)
	base := "http://" + srv.Addr()

	if code, _ := postSetup(t, base, "admin", "secret"); code != 200 {
		t.Fatalf("setup = %d, want 200", code)
	}

	client := NewClientWithAuth(base, "admin", "secret")
	ch, cancel, err := client.Subscribe(bus.TopicMessage)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	time.Sleep(50 * time.Millisecond)
	telemetry.Publish(bus.MessageReceived{
		Kind: bus.MessageKindMessenger,
		From: "ALICE",
		To:   "BOB",
		Text: "hello",
		Time: time.Now(),
	})

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			rec, ok := ev.(bus.MessageReceived)
			if !ok {
				continue
			}
			if rec.Kind != bus.MessageKindMessenger || rec.From != "ALICE" || rec.Text != "hello" {
				t.Fatalf("MessageReceived = %+v", rec)
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for message SSE event")
		}
	}
}

func TestSubscribeStreamsFinderNetworks(t *testing.T) {
	m := config.NewModel()
	telemetry := bus.New(16)
	sup := supervisor.New(m, telemetry)
	cfgPath := filepath.Join(t.TempDir(), "server.toml")
	plane := control.New(sup, tomlcodec.New(), filestore.New(cfgPath), telemetry)

	srv := NewServer(plane, "127.0.0.1:0")
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(srv.Stop)
	base := "http://" + srv.Addr()

	if code, _ := postSetup(t, base, "admin", "secret"); code != 200 {
		t.Fatalf("setup = %d, want 200", code)
	}

	client := NewClientWithAuth(base, "admin", "secret")
	ch, cancel, err := client.Subscribe(bus.TopicFinder)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	time.Sleep(50 * time.Millisecond)
	telemetry.Publish(bus.FinderUpdated{
		Kind:   bus.FinderKindNetworks,
		Scheme: "afp",
		Volumes: []bus.FinderVolume{
			{ID: "afp://Mac,tcp/", Kind: "afp", Title: "Mac"},
		},
		Time: time.Now(),
	})

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			fu, ok := ev.(bus.FinderUpdated)
			if !ok {
				continue
			}
			if fu.Kind != bus.FinderKindNetworks || fu.Scheme != "afp" || len(fu.Volumes) != 1 {
				t.Fatalf("FinderUpdated = %+v", fu)
			}
			if fu.Volumes[0].Title != "Mac" {
				t.Fatalf("volume = %+v", fu.Volumes[0])
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for finder SSE event")
		}
	}
}
