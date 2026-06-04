package control

import "github.com/ObsoleteMadness/ClassicStack/pkg/logbuf"

// LogHistory returns the retained recent log entries oldest-first, for the
// initial load of a log viewer.
func (p *Plane) LogHistory() []logbuf.Entry { return p.logs.Snapshot() }

// SubscribeLogs registers a log subscriber and returns the receive channel
// plus a cancel func that unsubscribes and closes the channel. New entries
// are pushed as they are logged; the caller typically sends LogHistory()
// first, then streams these.
func (p *Plane) SubscribeLogs() (<-chan logbuf.Entry, func()) { return p.logs.Subscribe() }
