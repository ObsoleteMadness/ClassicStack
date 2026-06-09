package uci

import (
	"reflect"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

func TestUCICodec_RoundTrip(t *testing.T) {
	// Register a schema for EtherTalk port section so it can be unmarshalled
	config.Register(config.SectionSchema{
		Key: "EtherTalk",
		New: func() config.Section { return &port.Section{SKey: "EtherTalk"} },
	})

	m := config.NewModel()
	m.Logging = config.LoggingSection{Level: "info"}
	m.Router = config.RouterSection{DefaultZone: "EtherZone"}
	m.Bridge = config.InterfaceSection{Name: "br-lan", Addr: "192.168.1.1"}
	m.Set(&port.Section{SKey: "EtherTalk", Iface: "eth0", IsEnabled: true})

	codec := New()
	data, err := codec.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got := config.NewModel()
	if err := codec.Unmarshal(data, got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Logging != m.Logging {
		t.Errorf("Logging: got %+v want %+v", got.Logging, m.Logging)
	}
	if got.Router != m.Router {
		t.Errorf("Router: got %+v want %+v", got.Router, m.Router)
	}
	if got.Bridge != m.Bridge {
		t.Errorf("Bridge: got %+v want %+v", got.Bridge, m.Bridge)
	}

	wantSec, _ := m.Get("EtherTalk")
	gotSec, ok := got.Get("EtherTalk")
	if !ok {
		t.Fatal("EtherTalk section missing after round-trip")
	}

	if !reflect.DeepEqual(wantSec, gotSec) {
		t.Errorf("EtherTalk: got %+v want %+v", gotSec, wantSec)
	}
}
