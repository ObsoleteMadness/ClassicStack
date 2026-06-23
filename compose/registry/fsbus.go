//go:build afp || smb || ncp || etherdfs || fswatch || all

package registry

import (
	"strings"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// fsBusBroker hands out one FS-mutation bus per distinct host path (§10d). When an
// AFP volume and an SMB share resolve to the same host path, both file-service
// factories ask the broker for that path and receive the SAME *bus.Bus, so a
// mutation published by one service is delivered to the other's reactor. Paths that
// differ get independent buses (no cross-talk between unrelated shares).
//
// The broker is a single process-wide instance shared by the afp and smb factories
// (separate init()s, one broker). It is concurrency-safe; the file-service factories
// run at compose time but the resolver closures they install are consulted later
// whenever a share is (re)built.
type fsBusBroker struct {
	mu     sync.Mutex
	bufN   int
	byPath map[string]bus.Bus
}

// fsBus is the shared broker every file-service factory resolves buses through.
// Buffered modestly: FS publishes are fire-and-forget, and a slow reactor must not
// stall a mutation (a dropped event is a missed notify, not a corrupted store).
var fsBus = &fsBusBroker{bufN: 64, byPath: map[string]bus.Bus{}}

// busForPath returns the shared bus for a host path (the §10e host-watcher resolves
// by raw path, not a ShareSpec). It must key identically to busFor so a watcher event
// and a file service's own publish land on the SAME bus.
func (b *fsBusBroker) busForPath(hostPath string) bus.Bus {
	return b.busFor(fs.ShareSpec{Path: hostPath})
}

// busFor returns the shared bus for a share's host path, creating it on first use.
// A share with no host path (e.g. an in-memory backend) keys on the empty string, so
// two pathless shares still share a bus — harmless, as a pathless backend has no
// external mutator and publishes nothing a reactor must coordinate on.
func (b *fsBusBroker) busFor(spec fs.ShareSpec) bus.Bus {
	key := hostPathKey(spec.Path)
	b.mu.Lock()
	defer b.mu.Unlock()
	bb, ok := b.byPath[key]
	if !ok {
		bb = fs.NewBus(b.bufN)
		b.byPath[key] = bb
	}
	return bb
}

// hostPathKey normalises a host path for same-path matching: trimmed and (case-
// insensitively folded) so "/srv/Shared" and "/srv/shared/" key together on a
// case-insensitive host. It is a best-effort match for the coordination bus, not a
// security boundary — the cost of a miss is a missed cross-service notify, never a
// wrong-share bind.
func hostPathKey(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimRight(p, `/\`)
	return strings.ToLower(p)
}
