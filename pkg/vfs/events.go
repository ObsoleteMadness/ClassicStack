package vfs

import (
	"sync"
	"sync/atomic"
	"time"
)

// Op enumerates the file-event kinds carried on the VFS bus.
type Op uint8

const (
	OpCreate Op = iota + 1
	OpRename
	OpModify
	OpDelete
	OpAttrChange
)

// String renders an Op for log messages and tests.
func (o Op) String() string {
	switch o {
	case OpCreate:
		return "create"
	case OpRename:
		return "rename"
	case OpModify:
		return "modify"
	case OpDelete:
		return "delete"
	case OpAttrChange:
		return "attr"
	default:
		return "unknown"
	}
}

// Event describes a filesystem mutation that may be of interest to
// other backends and services. HostPath is the canonical absolute
// host path; OldPath is populated only for OpRename.
//
// Origin is a free-form publisher tag (e.g. "smb", "afp", "fsnotify").
// Subscribers filter by Origin to avoid handling events they emitted
// themselves. The bus does not enforce loop avoidance; that is each
// subscriber's responsibility because a publisher may legitimately
// want to see another publisher's events even from the same backend.
type Event struct {
	Op       Op
	HostPath string
	OldPath  string
	Origin   string
	Time     time.Time
}

// Subscriber receives events published to a Bus.
type Subscriber interface {
	OnVFSEvent(ev Event)
}

// Bus is the publish/subscribe surface for filesystem events. Publish
// is non-blocking: each subscriber has its own bounded buffer and a
// slow subscriber loses events rather than stalling the data path.
type Bus interface {
	Subscribe(sub Subscriber) (cancel func())
	Publish(ev Event)
}

// BusOptions tunes the in-memory bus implementation.
type BusOptions struct {
	// SubscriberBuffer is the per-subscriber channel depth. Defaults
	// to 256 when zero. Higher values trade memory for tolerance of
	// transient subscriber stalls.
	SubscriberBuffer int
	// DropWarnInterval rate-limits "subscriber dropped events" warning
	// logs. Defaults to 30s when zero.
	DropWarnInterval time.Duration
	// DropLogger receives a single string per drop-warning interval
	// when a subscriber's buffer overflowed at least once. May be nil.
	DropLogger func(msg string)
}

// NewBus returns an in-memory Bus.
func NewBus(opts BusOptions) Bus {
	if opts.SubscriberBuffer <= 0 {
		opts.SubscriberBuffer = 256
	}
	if opts.DropWarnInterval <= 0 {
		opts.DropWarnInterval = 30 * time.Second
	}
	return &busImpl{opts: opts}
}

// DefaultBus is the process-wide bus used when callers do not inject
// their own. Constructors should accept a Bus parameter and fall back
// to DefaultBus only at the wiring layer (cmd/classicstack), not deep
// inside services.
var DefaultBus Bus = NewBus(BusOptions{})

type busImpl struct {
	opts BusOptions
	mu   sync.RWMutex
	subs []*subState
}

type subState struct {
	sub        Subscriber
	ch         chan Event
	stop       chan struct{}
	once       sync.Once
	wg         sync.WaitGroup
	dropCount  atomic.Uint64
	lastWarnNS atomic.Int64
}

func (b *busImpl) Subscribe(sub Subscriber) func() {
	if sub == nil {
		return func() {}
	}
	st := &subState{
		sub:  sub,
		ch:   make(chan Event, b.opts.SubscriberBuffer),
		stop: make(chan struct{}),
	}
	b.mu.Lock()
	b.subs = append(b.subs, st)
	b.mu.Unlock()
	st.wg.Add(1)
	go b.dispatch(st)
	return func() {
		st.once.Do(func() {
			close(st.stop)
			b.mu.Lock()
			for i, s := range b.subs {
				if s == st {
					b.subs = append(b.subs[:i], b.subs[i+1:]...)
					break
				}
			}
			b.mu.Unlock()
		})
		st.wg.Wait()
	}
}

func (b *busImpl) Publish(ev Event) {
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	b.mu.RLock()
	for _, st := range b.subs {
		select {
		case st.ch <- ev:
		default:
			st.dropCount.Add(1)
			b.maybeWarn(st)
		}
	}
	b.mu.RUnlock()
}

func (b *busImpl) maybeWarn(st *subState) {
	if b.opts.DropLogger == nil {
		return
	}
	now := time.Now().UnixNano()
	last := st.lastWarnNS.Load()
	if now-last < int64(b.opts.DropWarnInterval) {
		return
	}
	if !st.lastWarnNS.CompareAndSwap(last, now) {
		return
	}
	count := st.dropCount.Swap(0)
	b.opts.DropLogger("vfs: subscriber dropped " + itoaUint64(count) + " event(s)")
}

func (b *busImpl) dispatch(st *subState) {
	defer st.wg.Done()
	for {
		select {
		case <-st.stop:
			return
		case ev := <-st.ch:
			st.sub.OnVFSEvent(ev)
		}
	}
}

// itoaUint64 is a tiny strconv-free uint64 formatter used by the drop
// warning so the events module does not pull strconv into every
// service that imports vfs.
func itoaUint64(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// SubscriberFunc adapts a plain function to the Subscriber interface.
type SubscriberFunc func(ev Event)

// OnVFSEvent implements Subscriber.
func (f SubscriberFunc) OnVFSEvent(ev Event) { f(ev) }
