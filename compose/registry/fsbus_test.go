//go:build afp || smb || all

package registry

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// TestFSBusBrokerSamePathSharesBus: two specs whose host paths normalise equal get
// the SAME bus (so a same-path AFP volume + SMB share coordinate), while a different
// path gets a different bus (no cross-talk between unrelated shares).
func TestFSBusBrokerSamePathSharesBus(t *testing.T) {
	b := &fsBusBroker{bufN: 8, byPath: map[string]bus.Bus{}}

	a1 := b.busFor(fs.ShareSpec{Path: "/srv/shared"})
	a2 := b.busFor(fs.ShareSpec{Path: "/srv/shared/"})  // trailing slash
	a3 := b.busFor(fs.ShareSpec{Path: "/srv/SHARED"})   // case
	other := b.busFor(fs.ShareSpec{Path: "/srv/other"}) // different path

	if a1 == nil || a1 != a2 || a1 != a3 {
		t.Fatalf("same host path should share one bus: a1=%p a2=%p a3=%p", a1, a2, a3)
	}
	if other == a1 {
		t.Fatal("different host paths should get different buses")
	}
}

// TestFSBusBrokerPathlessSharesOneBus: pathless specs (synthetic backends) all key
// on the empty string — harmless, as a pathless backend publishes nothing.
func TestFSBusBrokerPathlessSharesOneBus(t *testing.T) {
	b := &fsBusBroker{bufN: 4, byPath: map[string]bus.Bus{}}
	if b.busFor(fs.ShareSpec{}) != b.busFor(fs.ShareSpec{}) {
		t.Fatal("two pathless specs should resolve to one bus")
	}
}
