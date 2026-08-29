package ncp

import (
	"encoding/binary"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

func TestServerSectionEffectiveName(t *testing.T) {
	ss := &ServerSection{ServerName: "  Files  "}
	if got := ss.EffectiveServerName("host"); got != "Files" {
		t.Fatalf("EffectiveServerName = %q, want Files", got)
	}
	ss.ServerName = ""
	if got := ss.EffectiveServerName("classicstack"); got != "classicstack" {
		t.Fatalf("fallback = %q, want classicstack", got)
	}
}

func TestServerSectionInternalNetworkBytes(t *testing.T) {
	ss := &ServerSection{}
	if _, ok := ss.InternalNetworkBytes(); ok {
		t.Fatal("zero InternalNetwork should report ok=false")
	}
	ss.InternalNetwork = 0x10
	net, ok := ss.InternalNetworkBytes()
	if !ok {
		t.Fatal("nonzero InternalNetwork should report ok=true")
	}
	if binary.BigEndian.Uint32(net[:]) != 0x10 {
		t.Fatalf("bytes = %v, want 0x10", net)
	}
}

func TestServerSectionFromModelAndRegister(t *testing.T) {
	RegisterServer()
	m := config.NewModel()
	if ss := ServerSectionFromModel(m); ss.Key() != ServerKey {
		t.Fatalf("empty model Key = %q", ss.Key())
	}
	m.Set(&ServerSection{SKey: ServerKey, ServerName: "NW", InternalNetwork: 7})
	ss := ServerSectionFromModel(m)
	if ss.ServerName != "NW" || ss.InternalNetwork != 7 {
		t.Fatalf("FromModel = %+v", ss)
	}
	cp := ss.Clone().(*ServerSection)
	cp.ServerName = "X"
	if ss.ServerName != "NW" {
		t.Fatal("Clone aliased ServerName")
	}
}
