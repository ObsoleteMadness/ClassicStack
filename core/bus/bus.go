package bus

import (
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/component" // for Stats
)

// Event is anything publishable. Topic() is the subscription selector.
type Event interface{ Topic() string }

// Bus fans events to subscribers. Publish is non-blocking: a full/slow subscriber DROPS
// rather than stalls the publisher (back-pressure tolerance, §5). Subscribe returns a channel
// carrying ONLY the named topics — an event whose topic was not requested is never enqueued
// onto that channel (no alloc/wakeup for discarded events, §1). The returned func unsubscribes.
type Bus interface {
	Publish(Event)
	Subscribe(topics ...string) (<-chan Event, func())
}

// defaultBuffer is the per-subscriber channel depth used when New is called with buffer<=0.
const defaultBuffer = 64

// New constructs a bus instance. buffer is the per-subscriber channel depth (0 → default).
func New(buffer int) Bus {
	if buffer <= 0 {
		buffer = defaultBuffer
	}
	return &bus{buffer: buffer, subs: make(map[*subscription]struct{})}
}

type subscription struct {
	ch     chan Event
	topics map[string]struct{} // requested topics; nil/empty means "all topics"
}

// wants reports whether the subscription should receive an event on this topic.
func (s *subscription) wants(topic string) bool {
	if len(s.topics) == 0 {
		return true
	}
	_, ok := s.topics[topic]
	return ok
}

type bus struct {
	buffer int

	mu   sync.RWMutex
	subs map[*subscription]struct{}
}

func (b *bus) Publish(ev Event) {
	topic := ev.Topic()
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		if !s.wants(topic) {
			continue // not requested → no enqueue, no wakeup (§1)
		}
		select {
		case s.ch <- ev:
		default:
			// Full/slow subscriber: drop rather than block the publisher (§5).
		}
	}
}

func (b *bus) Subscribe(topics ...string) (<-chan Event, func()) {
	s := &subscription{ch: make(chan Event, b.buffer)}
	if len(topics) > 0 {
		s.topics = make(map[string]struct{}, len(topics))
		for _, t := range topics {
			s.topics[t] = struct{}{}
		}
	}

	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, s)
			b.mu.Unlock()
			close(s.ch)
		})
	}
	return s.ch, unsubscribe
}

// --- Telemetry topic constants + event types (topics are strings; consts avoid typos). ---

const (
	TopicState = "state"
	TopicStats = "stats"
	TopicLog   = "log"
	// TopicMessage carries Messenger Service ("net send" / WinPopup) pop-ups the
	// stack received, so a UI can surface them (§3-quater messenger consumer).
	// AFP login/attention messages the operator Finder fetched as a client use
	// the same topic (Kind distinguishes the source).
	TopicMessage = "message"
	// TopicFinder carries in-process client discovery updates (LAN scan results)
	// so the web SPA can refresh server listings without polling.
	TopicFinder = "finder"
)

// MessageKind values for MessageReceived.Kind.
const (
	MessageKindAFP       = "afp"       // FPGetSrvrMsg login greeting or operator attention
	MessageKindMessenger = "messenger" // NetBIOS Messenger / WinPopup / net send
)

// StateChanged is published on every component lifecycle transition. Topic()=="state".
type StateChanged struct{ Component, From, To string }

func (StateChanged) Topic() string { return TopicState }

// StatSample carries a point-in-time stats snapshot. Topic()=="stats".
type StatSample struct {
	Component string
	Stats     component.Stats
}

func (StatSample) Topic() string { return TopicStats }

// LogRecord carries TYPED fields — never []slog.Attr / ...any (no reflection, §6).
// Topic()=="log".
type LogRecord struct {
	Component string
	Level     uint8 // mirrors core/log.Level
	Msg       string
	Fields    []Field
	Time      time.Time
}

func (LogRecord) Topic() string { return TopicLog }

// MessageReceived carries one pop-up the stack received so a UI can display it:
// a Messenger Service "net send" / WinPopup off \MAILSLOT\MESSNGR, or an AFP
// server message the operator Finder fetched (FPGetSrvrMsg). From/To are the
// sender and recipient names on the wire (To is empty for AFP); Text is the
// message body. Kind is MessageKindAFP or MessageKindMessenger.
type MessageReceived struct {
	Kind string // MessageKindAFP or MessageKindMessenger
	From string
	To   string
	Text string
	Time time.Time
}

func (MessageReceived) Topic() string { return TopicMessage }

// FinderKind values for FinderUpdated.Kind.
const (
	FinderKindNetworks = "networks" // last-seen client list for one scheme changed
	FinderKindScanning = "scanning" // in-process LAN scan started or finished
)

// FinderVolume is one remote file server the in-process client discovered.
type FinderVolume struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
	Transport string `json:"transport,omitempty"`
	Address   string `json:"address,omitempty"`
	URI       string `json:"uri,omitempty"`
	OS        string `json:"os,omitempty"`
	Version   string `json:"version,omitempty"`
	ReadOnly  bool   `json:"readOnly"`
}

// FinderUpdated is published when the in-process client learns or forgets remote
// servers, or when a background LAN scan starts or finishes. Kind is
// FinderKindNetworks or FinderKindScanning. For networks, Scheme is the probe
// scheme (afp, smb, ncp, etherdfs) and Volumes is the new last-seen list.
type FinderUpdated struct {
	Kind     string
	Scheme   string
	Scanning bool
	Volumes  []FinderVolume
	Time     time.Time
}

func (FinderUpdated) Topic() string { return TopicFinder }

// Field is one scalar log field; rendered by switch on Kind, not reflection.
type Field struct {
	Key  string
	Kind FieldKind
	Str  string
	Int  int64
	Bool bool
}

type FieldKind uint8

const (
	KindStr FieldKind = iota
	KindInt
	KindBool
)
