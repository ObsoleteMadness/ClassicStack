package afp

import (
	"bytes"
	"context"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// fakeNBP records RegisterName/UnregisterName calls so a test can assert AFP advertises
// (and withdraws) its AFPServer name.
type fakeNBP struct {
	obj, typ, zone []byte
	socket         uint8
	registered     bool
}

func (f *fakeNBP) RegisterName(obj, typ, zone []byte, socket uint8) {
	f.obj = append([]byte(nil), obj...)
	f.typ = append([]byte(nil), typ...)
	f.zone = append([]byte(nil), zone...)
	f.socket = socket
	f.registered = true
}

func (f *fakeNBP) UnregisterName(obj, typ, zone []byte) {
	f.registered = false
}

// TestStart_RegistersAFPServerNBPName is the regression guard for the "zone shows but no
// server in Chooser" bug: on Start, AFP must register serverName:AFPServer@zone at its ASP
// socket so a Chooser lookup resolves the file server; Stop must withdraw it.
func TestStart_RegistersAFPServerNBPName(t *testing.T) {
	svc, err := NewWithVolumes(nil, VolumeSpec{
		ID:    1,
		Name:  "Share",
		Share: fs.ShareSpec{Name: "Share", FSType: "memfs", ForkBackend: "appledouble", FilenameCodec: "macroman-utf8"},
	})
	if err != nil {
		t.Fatalf("NewWithVolumes: %v", err)
	}
	svc.SetRouter(&fakeRouter{})
	svc.SetServerName("MyServer")
	svc.SetZone("MyZone")
	names := &fakeNBP{}
	svc.SetNBP(names)

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !names.registered {
		t.Fatal("AFP did not register an NBP name on Start")
	}
	if string(names.obj) != "MyServer" {
		t.Errorf("NBP object = %q, want MyServer", names.obj)
	}
	if !bytes.Equal(names.typ, afpServerType) {
		t.Errorf("NBP type = %q, want AFPServer", names.typ)
	}
	if string(names.zone) != "MyZone" {
		t.Errorf("NBP zone = %q, want MyZone", names.zone)
	}
	if names.socket != svc.Socket() {
		t.Errorf("NBP socket = %d, want %d (AFP ASP socket)", names.socket, svc.Socket())
	}

	if err := svc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if names.registered {
		t.Error("AFP did not withdraw its NBP name on Stop")
	}
}
