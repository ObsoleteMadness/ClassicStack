package fs

import (
	"strconv"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
)

type Op uint8

const (
	OpCreate Op = iota + 1
	OpRename
	OpModify
	OpDelete
	OpAttrChange
)

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
		return "attr-change"
	default:
		return "op(" + strconv.Itoa(int(o)) + ")"
	}
}

const TopicFSMutation = "fs"

// Event is a file-system mutation. OldPath is set only for OpRename.
type Event struct {
	Op       Op
	HostPath string
	OldPath  string
	Origin   string
	Time     time.Time
}

func (Event) Topic() string { return TopicFSMutation }

// NewBus returns the FS-domain bus instance.
func NewBus(buffer int) bus.Bus {
	return bus.New(buffer)
}

// originBus wraps a bus.Bus, stamping a fixed Origin onto every fs.Event it
// publishes that did not already carry one. It is how a file service tags the
// mutations its own FS produces (§10d): the FS backend publishes an Event with no
// Origin, and the service-supplied wrapper fills in "afp"/"smb" so the OTHER
// service's reactor can act and this service's own reactor (SkipOrigin) ignores it.
// Subscribe/forwarding pass straight through to the underlying shared bus, so two
// services wrapping the SAME shared bus with different origins still see each
// other's events on one fan-out.
type originBus struct {
	bus    bus.Bus
	origin string
}

// OriginBus returns b wrapped so every fs.Event it publishes is stamped with origin
// (unless the event already names one). A nil b yields nil (the caller treats that
// as "no bus"). An empty origin returns b unwrapped (nothing to stamp).
func OriginBus(b bus.Bus, origin string) bus.Bus {
	if b == nil || origin == "" {
		return b
	}
	return &originBus{bus: b, origin: origin}
}

// Publish stamps the origin onto an fs.Event (when unset) and forwards to the
// underlying bus; non-fs events pass through untouched.
func (o *originBus) Publish(ev bus.Event) {
	switch e := ev.(type) {
	case Event:
		if e.Origin == "" {
			e.Origin = o.origin
		}
		o.bus.Publish(e)
	case *Event:
		if e != nil && e.Origin == "" {
			cp := *e
			cp.Origin = o.origin
			o.bus.Publish(cp)
			return
		}
		o.bus.Publish(ev)
	default:
		o.bus.Publish(ev)
	}
}

// Subscribe forwards to the underlying shared bus, so a subscriber on the wrapper
// sees every publisher's events (including those stamped by a different wrapper of
// the same bus).
func (o *originBus) Subscribe(topics ...string) (<-chan bus.Event, func()) {
	return o.bus.Subscribe(topics...)
}

// SkipOrigin reports whether a subscriber should skip an event it originated.
func SkipOrigin(ev bus.Event, self string) bool {
	switch e := ev.(type) {
	case Event:
		return e.Origin == self
	case *Event:
		if e == nil {
			return false
		}
		return e.Origin == self
	default:
		return false
	}
}
