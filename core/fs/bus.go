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
