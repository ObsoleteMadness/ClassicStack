# Phase 1 — Harness, structure, interfaces, buses, placeholders

**Goal:** stand up the new architecture as an empty, compiling, tested skeleton. At the end
of Phase 1 we have the three rings (`core/` / `adapter/` / `compose/`), every core interface
defined, both bus primitives, a working component registry + supervisor that can start/stop
*placeholder* components, and a test harness that proves the structure — **with no real
protocol or service logic ported.**

**Hard rules for the whole phase** (see [README](README.md)): core imports stdlib only;
no reflection in core; placeholders only; tree builds & tests green after every step; do not
touch/break the existing `internal/app` stack.

Reference section numbers (e.g. §3) point at [00-DESIGN.md](00-DESIGN.md).

Steps are grouped. Within a group, later steps may depend on earlier ones; the **Deps**
field on each step says what must land first. Steps with no shared deps can run in parallel.

---

## Group A — Skeleton & guardrails (do first; everything depends on these)

### A1. Create the ring layout (empty packages)
- **Goal:** materialise the package tree from §14 as empty packages with a `doc.go` each
  stating the package's role and its layering ring.
- **Creates:** directory skeleton under `core/`, `adapter/`, `compose/` (e.g. `core/component`,
  `core/link`, `core/bus`, `core/fs`, `core/config`, `core/control`, `core/log`, `core/buf`,
  `core/metastore`, `core/router`, `core/port`, `core/service`, `core/protocol/...`;
  `adapter/...`; `compose/...`). Each holds only `doc.go` (+ `package` decl).
- **Accept:** `go build ./...` green; `go vet ./...` clean; the existing tree is untouched.
- **Must not:** add any real types yet, or move existing code.
- **Deps:** none.

### A2. Import-graph CI gate (the dependency rule, executable)
- **Goal:** a test that walks the import graph of every `core/...` package and **fails** if it
  imports a forbidden package: pcap, gopacket, koanf, `net/http`, sqlite/`database/sql`,
  `reflect`, `encoding/json`, `slog`. This *is* §1 made executable (and the no-reflection rule).
- **Creates:** `core/internal/archtest/archtest_test.go` (or a `compose`-level test) using
  `go/packages` or `golang.org/x/tools/go/packages` — note: the *test* may use heavier deps;
  only `core/` runtime packages are constrained.
- **Accept:** test passes against the empty A1 tree; deliberately adding `import "net/http"`
  to a `core/` package makes it fail (verify once, then revert).
- **Must not:** exempt anything silently — additions to the allowlist need a comment + reviewer.
- **Deps:** A1.

### A3. Per-target buffer sizing (`core/buf`)
- **Goal:** build-tagged buffer-size constants (§1 allocation discipline): small on
  `tinygo`/embedded, large on desktop. One file per target tag + a default.
- **Creates:** `core/buf/buf.go` (default consts: `FrameMax`, `ReadChunk`, `LogField…`),
  `core/buf/buf_tinygo.go` (`//go:build tinygo`, smaller values), helper `Get()`/pooled
  buffer accessor if useful.
- **Accept:** `go build ./...` green for default; `go build -tags tinygo ./core/buf` green.
- **Deps:** A1.

### A4. CI build matrix scaffold (incl. TinyGo amd64 gates)
- **Goal:** CI invocations that prove the portability + gating claims early, before there's
  much to break. The TinyGo builds are **gates, not informational** — they are how we verify
  the no-reflection / no-forbidden-import discipline is real (a forbidden import or a
  reflection-using package makes TinyGo fail to compile), without needing ESP32 hardware.
- **Creates:** CI script / Make targets:
  - `build-default` (host `go build ./...`), `build-tags-all`, `vet`, the A2 archtest.
  - **`build-tinygo-linux-amd64`** — native amd64 (not a wasm/embedded target):
    `GOOS=linux GOARCH=amd64 tinygo build -o /dev/null ./<tinygo-target-pkg>`.
  - **`build-tinygo-windows-amd64`** — `GOOS=windows GOARCH=amd64 tinygo build -o cs.exe ./<tinygo-target-pkg>`.
  - (Later, informational until hardware/CI exists) an ESP32 target build.
  - The `<tinygo-target-pkg>` is a dedicated minimal main (see D5 / a `cmd/cs-tinygo` stub)
    that imports the TinyGo-safe core subset so the gate has something real to compile.
- **Accept:** all host targets green; **both TinyGo amd64 builds (linux + windows) green** —
  initially compiling `core/buf` + `core/component` + the TinyGo-safe interface packages via
  the minimal main. Deliberately adding `reflect`/`net/http` to a core package on the TinyGo
  path makes the gate fail (verify once, revert) — this proves the gate works, which is the
  user's explicit requirement.
- **Note:** keep a `tinygo`-build-tagged minimal main so packages that legitimately can't
  compile under TinyGo yet (full services in Phase 2) are simply not imported on that path;
  the gate grows its import surface as more of core becomes TinyGo-clean.
- **Deps:** A1, A2, A3.

---

## Group B — Core interfaces (the contracts; parallelisable once A1 lands)

Each step defines **interfaces and value types only** — no behaviour beyond trivial
constructors. Keep them documented (spec-style comments per CLAUDE.md).

### B1. Component model + capabilities (`core/component`) — §3
- **Defines:** the lifecycle contract every port/service/transport satisfies, plus the
  optional capability interfaces the supervisor/UI type-assert. **Implement exactly these
  signatures:**

```go
// Package component — the one lifecycle contract + optional capabilities (§3).
package component

import (
    "context"
    "errors"
)

// Component is the lifecycle every port, service, router, and transport satisfies.
// Start MUST be idempotent (calling it on a started component returns nil). Stop MUST be
// safe after a failed/partial Start. Neither blocks indefinitely; honour ctx.
type Component interface {
    Name() string
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
}

// --- Optional capabilities. A component implements only those that apply; callers
// --- discover them via type assertion. NEVER widen Component to include these.

type Enableable interface{ Enabled() bool }          // configured-enabled (≠ running)
type Bindable   interface{ Binding() string }        // "eth0", ":548", "ipx:0550"
type Statful    interface{ Stats() Stats }           // point-in-time snapshot (§5)
type Bridged    interface{ SetBridgeMode(string) error }            // §2
type Metered    interface{ SetTrafficObserver(func(rxBytes, txBytes int)) } // §5

// Configurable hot-applies a new config section without restart when it can. It MUST
// return ErrNeedsRestart (not some other error) when the change can't be applied live,
// so the supervisor falls back to restart-and-notify (§11). `section` is the component's
// typed config.Section (§4), passed as any to avoid a core import cycle.
type Configurable interface{ ApplyConfig(section any) error }

// Attachable models a SOFT binding (e.g. a transport into NetBIOS, §11d): attach/detach
// are re-runnable side effects of the OWNER's start/stop, not a hard DAG dependency.
type Attachable interface {
    Attach(ctx context.Context) error
    Detach(ctx context.Context) error
}

// Stats is the typed (no-reflection) snapshot Statful returns and StatSample carries (§5).
type Stats struct {
    Counters map[string]uint64  // monotonic: frames_rx, bytes_tx, decode_errors, …
    Gauges   map[string]float64 // point-in-time: routes, active_leases, open_sessions, …
}

// ErrNeedsRestart is the sentinel ApplyConfig returns for structural changes (errors.Is).
var ErrNeedsRestart = errors.New("component: change needs restart")
```

- **Note:** `section any` is a *carrier*, not reflection — the component type-asserts it to
  its own `config.Section`. `any` avoids the `component ← config ← components` cycle.
- **Accept:** compiles; `var _ Component = (*noopComponent)(nil)` + one capability assertion
  in a test. No `reflect`.
- **Deps:** A1.

### B2. Link interfaces + decorators surface (`core/link`) — §2
- **Defines:** the two link altitudes (a `DatagramLink` is obtained either from a kernel
  socket or from `Framing(FrameLink)`), the optional capabilities adapters expose, and the
  frame-altitude decorator signatures. **Implement exactly these signatures:**

```go
// Package link — byte-slice link altitudes + decorators (§2). Ports/framers see FrameLink;
// services/router see DatagramLink. NO pcap/capture/gopacket imports here.
package link

import (
    "errors"
    "github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp" // B7
)

type Frame = []byte

// FrameLink is a raw L2 frame transport: pcap, TAP, PPP/SLIP, esp32-raw. Implementations
// are safe for one reader + one writer goroutine concurrently. Read returns ErrTimeout on
// deadline (caller loops) and ErrClosed after Close. Caller owns the returned slice; an
// implementation must not retain the slice passed to Write past the call.
type FrameLink interface {
    Read() (Frame, error)
    Write(Frame) error
    Close() error
}

// DatagramLink is a pre-framed DDP datagram transport. Two implementations satisfy it:
// a kernel/native socket (AF_APPLETALK, drivers/net) OR Framing(FrameLink). Callers cannot
// tell which. Same sentinels/ownership rules as FrameLink.
type DatagramLink interface {
    ReadDatagram() (ddp.Datagram, error)
    WriteDatagram(ddp.Datagram) error
    Close() error
}

var (
    ErrTimeout = errors.New("link: read timeout") // loop, not fatal
    ErrClosed  = errors.New("link: closed")       // after Close, terminal
)

// PhysicalMedium is reported by links that can detect it (drives Wi-Fi bridge encap, §2).
type PhysicalMedium uint8
const ( MediumEthernet PhysicalMedium = iota; MediumWiFi )

// --- Optional capabilities (type-asserted by composition; ports never assert). ---

// MediumReporter: a FrameLink that knows its physical medium.
type MediumReporter interface{ Medium() PhysicalMedium }

// FilterableLink: a FrameLink that can push a kernel filter (BPF). Software-fallback
// filtering is the FilterDecorator instead.
type FilterableLink interface{ SetFilter(expr string) error }

// --- Framing: the FrameLink→DatagramLink adapter contract (does DDP encap + AARP/node-claim
// --- for the EtherTalk case). Declared here; implemented per protocol in adapters/ports. ---
type Framer interface {
    Framing(FrameLink) (DatagramLink, error)
}

// --- Frame-altitude decorators. Each WRAPS a FrameLink and returns a FrameLink, so they
// --- compose: Capture(Dedup(Filter(raw))). Signatures fixed; bodies land in adapters (Phase 2). ---

// FilterFunc reports whether a frame passes (software-side filtering).
type FilterFunc func(Frame) bool
func Filter(inner FrameLink, pass FilterFunc) FrameLink         // drop frames failing pass
func Dedup(inner FrameLink, window /*time.Duration*/ int64) FrameLink // suppress kernel loopback dupes
func Capture(inner FrameLink, sink CaptureSink) FrameLink       // tee frames to a sink
func Bridge(inner FrameLink, mode string) FrameLink            // Wi-Fi/bridged MAC rewrite

// CaptureSink consumes tee'd frames (a pcap-file writer is an adapter implementing this).
type CaptureSink interface {
    WriteFrame(tsUnixNano int64, f Frame)
    Close() error
}
```

- **Accept:** compiles; an in-memory loopback `FrameLink` placeholder exists for tests; the
  decorator funcs may return their inner unchanged (no-op) in Phase 1 — only the *signatures*
  are fixed here. `Framer` has at least a no-op identity implementation in tests.
- **Must not:** import pcap, gopacket, or any capture backend (archtest enforces).
- **Deps:** A1, B7 (the `ddp.Datagram` type). If B7 lands after, temporarily alias
  `type Datagram = []byte` in `core/link` and switch to `ddp.Datagram` when B7 merges.

### B3. Bus primitive + telemetry instance (`core/bus`) — §5
- **Defines:** the ONE bus primitive (instantiated per domain, §10c) and the telemetry event
  types. **Implement exactly these signatures:**

```go
// Package bus — typed, topic-scoped, allocation-light pub/sub primitive (§5). One primitive,
// instantiated per domain (telemetry here; FS-mutation in core/fs). NO slog/reflect/json.
package bus

import (
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

// New constructs a bus instance. buffer is the per-subscriber channel depth (0 → default).
func New(buffer int) Bus

// --- Telemetry topic constants + event types (topics are strings; consts avoid typos). ---
const ( TopicState = "state"; TopicStats = "stats"; TopicLog = "log" )

type StateChanged struct{ Component, From, To string }            // Topic()=="state"
type StatSample   struct{ Component string; Stats component.Stats } // Topic()=="stats"

// LogRecord carries TYPED fields — never []slog.Attr / ...any (no reflection, §6).
type LogRecord struct {
    Component string
    Level     uint8     // mirrors core/log.Level
    Msg       string
    Fields    []Field
    Time      time.Time
}
type Field struct {                  // one scalar kind set; rendered by switch, not reflection
    Key  string
    Kind FieldKind
    Str  string
    Int  int64
    Bool bool
}
type FieldKind uint8
const ( KindStr FieldKind = iota; KindInt; KindBool )
```

- **Accept:** unit test: a `Subscribe("state")` channel receives only `state` events; a
  full subscriber drops (publisher never blocks); unsubscribe stops delivery and is idempotent.
- **Must not:** import `slog`/`reflect`/`encoding/json`.
- **Deps:** A1, B1 (for `component.Stats`). (If you prefer `Stats` in `core/bus`, move it there
  and have B1 alias — pick one home and reference it everywhere.)

### B4. FS-mutation bus instance (`core/fs` bus part) — §5/§10c
- **Defines:** the FS-domain bus as a second **instance of the B3 primitive** (reuse `bus.Bus`,
  do NOT fork the primitive) plus its typed event. **Implement exactly:**

```go
// in package fs (core/fs) — the file-mutation bus instance + its event (§5/§10c).
package fs

import (
    "time"
    "github.com/ObsoleteMadness/ClassicStack/core/bus"
)

type Op uint8
const ( OpCreate Op = iota+1; OpRename; OpModify; OpDelete; OpAttrChange )
func (o Op) String() string

const TopicFSMutation = "fs" // single topic for now; sub-topics (per-volume) may be added

// Event is a file-system mutation. OldPath is set only for OpRename. Origin tags the
// publisher ("afp","smb","fsnotify") so subscribers skip their own events (loop avoidance).
type Event struct {
    Op       Op
    HostPath string
    OldPath  string
    Origin   string
    Time     time.Time
}
func (Event) Topic() string { return TopicFSMutation } // satisfies bus.Event

// NewBus returns the FS-domain bus (a bus.New instance). SkipOrigin is a helper a subscriber
// uses to ignore events it published itself.
func NewBus(buffer int) bus.Bus
func SkipOrigin(ev bus.Event, self string) bool
```

- **Accept:** unit test mirroring B3 against an `fs.Event`; `SkipOrigin` filters correctly.
- **Deps:** B3.

### B5. Logging (`core/log`) — §6
- **Defines:** scoped levelled logging with typed fields and a multi-sink fan-out. **Implement
  exactly these signatures:**

```go
// Package log — scoped, levelled, typed-field logging fanning to multiple sinks (§6).
// Zero reflection: fields are typed scalars, never ...any. The bus is just one sink (adapter).
package log

import "time"

type Level uint8
const ( Debug Level = iota; Info; Warn; Error )

// Field is a typed key/value (no interface{} boxing → no reflection, no scalar alloc).
type Field struct {
    Key  string
    Kind Kind
    s    string
    i    int64
    b    bool
}
type Kind uint8
const ( KindStr Kind = iota; KindInt; KindBool )
func Str(k, v string) Field
func Int(k string, v int64) Field
func Bool(k string, v bool) Field

// Logger is the producer API a component is handed, scoped to itself (first bound field).
//
// ALLOCATION CONTRACT: the variadic Log(...Field) is the ergonomic form for cold/setup paths.
// On a hot path (per-frame, per-packet), the variadic slice escapes to the heap unless escape
// analysis proves otherwise — on TinyGo/ESP32 that is heap churn per packet. So the interface
// ALSO provides fixed-arity, non-variadic hot-path methods that are guaranteed zero-alloc.
// Rule for implementors: never call the variadic form in a data-path loop; use Logf*/the
// fixed-arity helpers, and always guard with Enabled() first.
type Logger interface {
    With(fields ...Field) Logger               // child logger; cold path (setup), variadic ok
    Log(lvl Level, msg string, fields ...Field) // ergonomic; NOT for hot paths
    Enabled(lvl Level) bool                     // cheap guard; check before building any field

    // Fixed-arity hot-path methods — NO variadic, NO slice, provably zero-alloc when the
    // level is enabled (and a no-op when not). Cover the common field shapes; add arities
    // only as real call sites demand (keep the set small).
    Log0(lvl Level, msg string)
    Log1(lvl Level, msg string, f Field)
    Log2(lvl Level, msg string, f1, f2 Field)
}

// Record is the finished log entry a Sink receives. Fields = bound (scope) + call fields.
type Record struct {
    Scope  string
    Level  Level
    Msg    string
    Fields []Field
    Time   time.Time
}

// Sink consumes records. Implementations must be safe for concurrent Write.
type Sink interface {
    Write(rec Record)
    Close() error
}

// New builds a root logger writing to sinks at/above min level; scope sets the base tag.
func New(scope string, min Level, sinks ...Sink) Logger

// Stdlib-only sinks live here; heavy sinks (syslog/journald/semihosting) are adapters,
// and the bus sink (publishes bus.LogRecord) is an adapter too.
func NewRingSink(capacity int) Sink   // in-memory tail for the UI
func NewStderrSink() Sink
```

- **Accept:** scoped logger tags records; `Enabled(Debug)==false` fast path has
  `testing.AllocsPerRun == 0`; **the fixed-arity hot-path methods (`Log0/Log1/Log2`) have
  `AllocsPerRun == 0` even when the level is ENABLED** (the variadic `Log` is allowed to
  allocate); fan-out delivers to two sinks; `With` adds fields without mutating the parent.
- **Must not:** import `slog`/`reflect`; call the variadic `Log` from any data-path loop.
- **Deps:** A1, A3 (buffer sizing for formatting).

### B6. Config model + section registry (`core/config`) — §4
- **Defines:** the in-memory model (no serialisation tags), the section registry that lets new
  components add config without editing a central struct, and the Codec/Store adapter seams.
  **Implement exactly these signatures:**

```go
// Package config — pure in-memory model + section registry + codec/store seams (§4).
// NO struct tags, NO reflection, NO koanf/toml (those are adapters).
package config

// Section is one component's typed config (e.g. *EtherTalkSection). Clone returns a deep
// copy so staging never mutates the live section. Validate checks the section in isolation.
type Section interface {
    Key() string          // "EtherTalk", "AFP", … (matches the component/registry name)
    Clone() Section
    Validate() error
}

// Model is the single in-memory source of truth. Well-known sections are typed fields for
// ergonomics; component sections live in Sections keyed by Section.Key().
type Model struct {
    Logging LoggingSection
    Router  RouterSection
    Bridge  InterfaceSection
    Sections map[string]Section // registered component sections
}
func (m *Model) Clone() *Model
func (m *Model) Get(key string) (Section, bool)
func (m *Model) Set(s Section)
// EffectiveInterface resolves a component's interface, folding bridge inheritance +
// per-section override (§4/§9d) — a PURE function, re-runnable on every reconfigure.
func (m *Model) EffectiveInterface(sectionKey string) InterfaceSection

// SectionSchema registers a component's config shape so codecs can round-trip it without
// knowing the type. New returns a zero section; Validate may wrap Section.Validate.
type SectionSchema struct {
    Key      string
    New      func() Section
    Validate func(Section) error
}
func Register(s SectionSchema)        // call from component package init() or explicit wiring
func Schemas() []SectionSchema        // codecs iterate these

// Codec converts the model to/from a byte representation (TOML, UCI, JSON) — ADAPTERS
// implement this; core ships none. Round-trip is the contract: Unmarshal(Marshal(m)) == m.
type Codec interface {
    Marshal(*Model) ([]byte, error)
    Unmarshal([]byte, *Model) error
}

// Store is where config bytes live and how they're versioned (file w/ numbered backups,
// UCI tree, in-mem) — ADAPTERS implement this. Save returns a revision id (backup path / commit).
type Store interface {
    Load() ([]byte, error)
    Save(data []byte) (revision string, err error)
}
```

- **Accept:** register two fake sections; an in-memory test `Codec`+`Store` round-trips the
  model (`Unmarshal(Marshal(m))` deep-equals `m`); `Clone` is independent of the original.
- **Must not:** import koanf/toml/uci (adapters).
- **Deps:** A1.

### B7. Protocol datagram core types (`core/protocol/ddp` + siblings) — §2/§12
- **Defines:** the pure, allocation-light, reflection-free DDP `Datagram` type + encode/decode.
  It is a *codec*, not a service, so it's the one bit of "real" code allowed in Phase 1 (the
  link/bus interfaces reference the type). **Implement at least this surface:**

```go
// Package ddp — Datagram Delivery Protocol datagram type + codec (§2/§12). Pure, no reflection.
package ddp

// Datagram is a decoded DDP packet (long-header form). Fields use fixed-width types; Data is
// the caller-owned payload slice. Keep it a value type to avoid per-packet heap allocation.
type Datagram struct {
    Hops        uint8
    DestNetwork uint16
    SrcNetwork  uint16
    DestNode    uint8
    SrcNode     uint8
    DestSocket  uint8
    SrcSocket   uint8
    DDPType     uint8
    Data        []byte
}

// Encode appends the wire form to dst and returns it (append-style → caller controls alloc).
func (d Datagram) Encode(dst []byte) ([]byte, error)
// Decode parses one datagram from b. The returned Data may alias b (document it); callers
// that retain it must copy.
func Decode(b []byte) (Datagram, error)
```

- **Stub** sibling protocol packages (`core/protocol/{atp,asp,pap,nbp,ipx,netbeui,smb,netbios}`)
  as empty-with-`doc.go` for now — real codecs land in Phase 2 (M2).
- **Accept:** `Decode(Encode(d)) == d` round-trip on a captured datagram (from `/captures` if
  handy, else hand-built); encode of a known datagram is byte-identical to a golden.
- **Deps:** A1.

### B8. FS interface family (`core/fs`) — §9 / §10a / §10a-bis
- **Defines:** the single filesystem seam AFP+SMB consume, plus the per-share-swappable fork
  engine, name engine, and filename codec, and the share-build that assembles + validates them.
  **Implement exactly these signatures** (this is the largest interface set — get it right):

```go
// Package fs — the one FS seam services consume + per-share fork/name/codec engines (§9/§10).
// (This package also hosts the FS-mutation bus from B4.)
package fs

import (
    "io/fs"
    "github.com/ObsoleteMadness/ClassicStack/core/bus"
    "github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// File is a per-open-handle. Implementations must not retain p past Write/WriteAt.
type File interface {
    ReadAt(p []byte, off int64) (int, error)
    WriteAt(p []byte, off int64) (int, error)
    Truncate(size int64) error
    Stat() (fs.FileInfo, error)
    Sync() error
    Close() error
}

// FileSystem is the cross-service backend contract. Names crossing this boundary are in
// STORE form (already run through the share's FilenameCodec). Capabilities advertises optional
// behaviour; ShortName/MediumName delegate to the share's NameEngine.
type FileSystem interface {
    ReadDir(path string) ([]fs.DirEntry, error)
    Stat(path string) (fs.FileInfo, error)
    DiskUsage(path string) (total, free uint64, err error)
    CreateDir(path string) error
    CreateFile(path string) (File, error)
    OpenFile(path string, flag int) (File, error)
    Remove(path string) error
    Rename(old, new string) error
    ShortName(path string) (string, error)
    MediumName(path string) (string, error)
    Capabilities() Capabilities
}
type Capabilities struct {
    CatSearch, ChildCount, ReadDirRange, DirAttributes, ReadOnly bool
}

// --- Fork engine (§9b): composed onto the FS per share; fork ops become FS methods via ForkFS.
type ForkType uint8
const ( DataFork ForkType = iota; ResourceFork )
type ForkEngine interface {
    OpenFork(path string, fork ForkType, flag int) (File, error)
    ForkLen(path string, fork ForkType) (int64, error)
    ReadFinderInfo(path string) (info [32]byte, ok bool, err error)
    WriteFinderInfo(path string, info [32]byte) error
    ReadComment(path string) (c []byte, ok bool)
    WriteComment(path string, c []byte) error
    MoveMetadata(old, new string) error
    DeleteMetadata(path string) error
}
type ForkFS interface { // what file services actually hold
    FileSystem
    ForkEngine
}

// --- Name engine (§10a): short (8.3) / medium (31-char) derivation; per share, pinnable.
type NameKind uint8
const ( ShortName NameKind = iota; MediumName )
type NameEngine interface {
    Bind(dir, long string, kind NameKind) string        // allocate-or-return, collision-suffixed
    ToLong(dir, derived string, kind NameKind) (string, bool)
}

// --- Filename codec (§10a-bis): charset + reserved-char translation at the FS boundary.
//
// CRITICAL: Decode does NOT return a universal Go `string`. The store's legal byte form is
// backend-specific — a POSIX local_fs takes arbitrary non-NUL bytes (often UTF-8 but it does
// not enforce it); an S3/WebDAV backend requires strict URL/XML-safe characters; a FAT image
// has its own charset. Returning a `string` would (a) assume every decoded name is
// representable+valid for the backend, and (b) risk a lossy []byte→string→[]byte round-trip
// through host I/O. So Decode yields the backend's NATIVE representation (StoredName, a byte
// sequence the FileSystem can pass straight to host I/O) and the codec validates it against
// the store's profile. The escape tokens a codec emits MUST themselves be legal in the store
// charset (e.g. the 0xNN tokens are ASCII, valid in UTF-8 and FAT alike).
type StoredName []byte // the backend's on-disk native byte form of one path element

type FilenameCodec interface {
    // Decode: client wire bytes → store-native bytes, validated for THIS backend's profile.
    // Returns ErrUnrepresentable when the wire name cannot be legally stored (caller maps it
    // to the protocol's "illegal name" error rather than corrupting the path).
    Decode(wire []byte) (StoredName, error)
    // Encode: store-native bytes → client wire bytes (the exact inverse of Decode).
    Encode(stored StoredName) (wire []byte, err error)
    Profile() FilenameProfile
}
type FilenameProfile struct {
    WireCharset  string // client side: "macroman", "utf8", …
    StoreCharset string // backend side: "utf8", "posix-bytes", "fat", "url-safe", "macroman", …
    MaxElement   int    // max element length in STORE bytes (0 = unbounded)
    // Validate reports whether a store-native element is legal for this backend. A POSIX
    // backend rejects only NUL and '/'; an S3 backend rejects URL/XML-unsafe bytes; a FAT
    // backend enforces 8.3/charset. The FileSystem MAY call this defensively before host I/O.
    // (Field, not method, so the profile stays a pure value; codecs set it.)
    Validate func(elem StoredName) error
}
var ErrUnrepresentable = errors.New("fs: filename not representable in store charset")

// --- Per-share assembly (§9d). Backend (FileSystem) + the three engines + metastore, chosen
// --- by config; BuildShare validates the combination and rejects incompatible pairings.
type ShareSpec struct {
    Name          string
    FSType        string            // selects the FileSystem Factory
    ForkBackend   string            // "appledouble"|"ads"|"xattr"|"native"|"auto"
    FilenameCodec string            // "macroman-utf8"|"macroman-native"|"utf8"|…
    NameEngine    string            // short/medium engine id
    Metastore     string            // CNID/shortname/desktop store id (default "mem")
    ReadOnly      bool
    Extra         map[string]any    // backend-specific keys (carrier, not reflection)
}
type Factory func(ShareSpec, bus.Bus, metastore.Store) (FileSystem, error)
func RegisterFS(fsType string, f Factory) // build-tagged init() in adapters
func BuildShare(spec ShareSpec, b bus.Bus) (ForkFS, error) // validates fs_type×fork×codec×name
```

- **FilenameCodec rules (the inversion trap, design §10a-bis):**
  - The `FileSystem` operates on **store-native bytes end-to-end** — names crossing the FS
    boundary are already `StoredName` (codec-decoded); the codec is the *only* place wire↔store
    conversion happens. (Open question for implementor: whether `FileSystem` path params become
    `StoredName`/`[]byte` rather than `string` — prefer that for backends like POSIX/S3 where a
    Go `string` is lossy; if kept `string` for ergonomics, the contract is "valid `StoreCharset`
    bytes only", enforced by `Profile.Validate`.)
  - `Encode(Decode(wire)) == wire` for every legal wire name; an unrepresentable name returns
    `ErrUnrepresentable` (→ protocol "illegal name"), **never** a silently mangled path.
  - Escape tokens must be legal in `StoreCharset` so they survive host I/O and Go's path
    routines (the `0xNN` scheme is ASCII for exactly this reason).
- **Also:** create `core/encoding` (pure MacRoman↔UTF-8 tables, reflection-free) for the
  default `FilenameCodec` adapter to reuse in Phase 2. Provide an identity `FilenameCodec`,
  a `nullForkEngine`, and a passthrough `NameEngine` placeholder here.
- **Accept:** a `memfs` `FileSystem` placeholder + the three placeholder engines satisfy the
  interfaces; `BuildShare` accepts a valid triple and **rejects** an invalid one (e.g.
  `hfs-image` × `utf8` codec, read-only `zipfs` × non-`appledouble` fork); a codec round-trip
  test asserts `Encode(Decode(wire))==wire` and that an unrepresentable wire name (e.g. a
  byte illegal in `StoreCharset`) returns `ErrUnrepresentable`.
- **Deps:** A1, B3/B4 (bus), B6 (config/Section), B9 (metastore.Store).

### B9. Metastore interface (`core/metastore`) — §9a
- **Defines:** the one keyed-store interface CNID / shortname / desktop all share, plus the
  default mem implementation. **Implement exactly:**

```go
// Package metastore — one keyed store for cnid/shortname/desktop (§9a). sqlite is just one
// adapter; the default is mem-snapshot-to-file (stdlib only) so embedded/TinyGo drop sqlite.
package metastore

// Store is a small persistent keyed map. Keys/values are opaque bytes; the caller (CNID,
// shortname, desktop) owns the schema. Range visits entries under prefix until fn returns false.
type Store interface {
    Get(key []byte) (val []byte, ok bool)
    Put(key, val []byte) error
    Delete(key []byte) error
    Range(prefix []byte, fn func(k, v []byte) bool) error
    Sync() error
    Close() error
}

// Open returns a store of the named kind at path (kind selects an adapter; "mem" is built-in).
func Open(kind, path string) (Store, error)

// NewMem returns the default in-memory store, snapshotting to path on Sync/Close (path ""
// = volatile). Reopening the same path reloads the snapshot.
func NewMem(path string) (Store, error)
```

- **Accept:** `NewMem` round-trips keys across `Sync` then reopen in a temp dir; `Range`
  respects the prefix and early-exits on `false`.
- **Deps:** A1.

### B10. Control plane contract (`core/control`) — §7
- **Defines:** the single transport-agnostic management contract every front-end (http, ubus,
  cli) drives, shaped as request/response methods + a topic subscription (so it maps onto ubus
  natively). **Implement exactly these signatures:**

```go
// Package control — the one transport-agnostic management contract (§7). Front-ends (http,
// ubus, cli) are ADAPTERS over Plane; none is privileged. NO net/http, NO transport types here.
package control

import (
    "context"
    "github.com/ObsoleteMadness/ClassicStack/core/bus"
    "github.com/ObsoleteMadness/ClassicStack/core/config"
)

// Plane is the management surface. Methods are plain request/response (→ ubus methods, REST
// handlers, CLI subcommands). Subscribe is the live channel (→ ubus events / SSE), carrying
// the telemetry topics (state/stats/log). Reconfigure is the ADDRESSED operation (§11) — no diff.
type Plane interface {
    Config() (*config.Model, error)
    Reconfigure(ctx context.Context, name string, section config.Section) error
    Save(ctx context.Context) (revision string, err error)

    Start(ctx context.Context, name string) error
    Stop(ctx context.Context, name string) error
    Restart(ctx context.Context, name string) error

    Status() []Unit
    ListInterfaces() ([]InterfaceInfo, error)
    ListFSTypes() []string
    Diagnostics() Diagnostics

    Subscribe(topics ...string) (<-chan bus.Event, func())
}

// Unit is one component's status snapshot for the dashboard.
type Unit struct {
    Name      string
    Kind      string   // "port"|"service"|"router"|"transport"
    Enabled   bool
    Running   bool
    Binding   string
    DependsOn []string
    Props     map[string]string
}
type InterfaceInfo struct{ Name, Addr string }

// Supervisor is what Plane drives (implemented in compose/supervisor, C2/C3). Plane is a thin
// façade; the supervisor owns lifecycle + the model.
type Supervisor interface {
    Model() *config.Model
    Reconfigure(ctx context.Context, name string, section config.Section) error
    Start(ctx context.Context, name string) error
    Stop(ctx context.Context, name string) error
    Restart(ctx context.Context, name string) error
    Status() []Unit
    ListInterfaces() ([]InterfaceInfo, error)
    ListFSTypes() []string
}

// Diagnostics is the optional read-only probe set (zones, echo, RTMP table, …). A build may
// return ErrUnavailable for probes it can't run.
type Diagnostics interface {
    ListZones(ctx context.Context) ([]string, error)
    // … further probes added as services land; keep each ctx-first, typed-result.
}

// New builds a Plane over a Supervisor, a config Store/Codec (for Save), and the telemetry bus.
func New(sup Supervisor, codec config.Codec, store config.Store, telemetry bus.Bus) Plane
```

- **Accept:** compiles; a fake `Supervisor` lets a `Plane` answer `Status` and `Reconfigure`;
  `Subscribe("state")` returns the telemetry channel.
- **Must not:** import `net/http` or any transport package.
- **Deps:** A1, B3 (bus), B6 (config.Model/Section/Codec/Store).

---

## Group C — The harness (registry + supervisor + reconfigure), depends on Group B

### C1. Component registry (`compose/registry`) — §8
- **Goal:** name→factory registry populated by build-tagged `init()`; absent tag = not
  registered (the §8 replacement for `*_disabled.go`). **Implement exactly:**

```go
// Package registry — name→factory for components, populated by build-tagged init() (§8).
package registry

import (
    "github.com/ObsoleteMadness/ClassicStack/core/component"
    "github.com/ObsoleteMadness/ClassicStack/core/config"
)

// Factory builds a component from its config section (and whatever deps it resolves from the
// model). Returns the component or an error; a disabled section yields (nil, nil).
type Factory func(m *config.Model) (component.Component, error)

func Register(name string, f Factory)                       // call from build-tagged init()
func Build(name string, m *config.Model) (component.Component, bool, error) // ok=false = not built
func Names() []string                                       // registered names (sorted)
```

- **Accept:** a placeholder factory registered under a build tag appears only with that tag;
  `Build` of an unregistered name returns `ok=false` (clean not-found, no error), and the
  supervisor logs one "requested but not built" line.
- **Deps:** B1, B6.

### C2. Supervisor: lifecycle DAG + start/stop ordering (`compose/supervisor`) — §3/§11
- **Goal:** the supervisor owns the component set + dependency DAG; starts in dependency order,
  stops in reverse; publishes `StateChanged` on every transition. **Implement at least:**

```go
// Package supervisor — owns the component DAG; ordered start/stop; addressed reconfigure (C3).
// Implements control.Supervisor (B10).
package supervisor

import (
    "context"
    "github.com/ObsoleteMadness/ClassicStack/core/bus"
    "github.com/ObsoleteMadness/ClassicStack/core/component"
    "github.com/ObsoleteMadness/ClassicStack/core/config"
)

type Supervisor struct{ /* model, telemetry bus, nodes map, DAG edges, … */ }

func New(m *config.Model, telemetry bus.Bus) *Supervisor

// Add registers a component with its hard dependencies (DAG edges). dependsOn are component
// names that must be running before this one starts (and stop after it). Soft bindings use
// component.Attachable instead (§11d), NOT dependsOn.
func (s *Supervisor) Add(c component.Component, dependsOn []string)

func (s *Supervisor) Start(ctx context.Context) error // topo order; publishes StateChanged
func (s *Supervisor) Stop(ctx context.Context) error  // reverse topo order
```

- **Accept:** ordering test with placeholders asserts start order respects edges, stop is the
  reverse, and a `StateChanged{From,To}` fires on the telemetry bus per transition.
- **Deps:** B1, B3, C1.

### C3. Supervisor: addressed reconfigure + notify (§11)
- **Goal:** implement the addressed `Reconfigure` exactly as §11a — **no model diff.**
  **Implement this method + algorithm:**

```go
// Reconfigure applies a new section to ONE named component and cascades a restart to
// dependents only as far as each cannot absorb it live. Addressed, not diffed (§11a).
func (s *Supervisor) Reconfigure(ctx context.Context, name string, section config.Section) error
```

Algorithm (must match §11a precisely):
```
Reconfigure(name, section):
  1. s.model.Set(section)                       # update shared model section (often by ref)
  2. c := node(name)
  3. if c implements Configurable:
        err := c.ApplyConfig(section)
        if err == nil { publish StateChanged(name, running, reconfigured); return nil }  # live
        if !errors.Is(err, ErrNeedsRestart) { return err }                               # real failure
  4. restart(c):                                 # no Configurable, or ErrNeedsRestart
        Stop(c); rebuild from section; Start(c)   # publishes Stop/Start StateChanged
  5. for each dependent d along DAG out-edges of name:
        Reconfigure-notify(d)                     # d asked the SAME question (step 3–4);
                                                  # cascade stops where a dependent hot-applies
  # Attachable bindings (§11d) are re-run by Stop/Start as side effects — NOT dependents,
  # so they never enter this cascade.
```

- **Accept:** the reconfigure-and-notify test (Verification #8, formalised in E4): reconfigure
  one placeholder; only it + dependents that can't hot-apply emit Stop/Start; hot-applying
  dependents don't restart; unrelated components untouched; **assert no diff pass occurs**
  (e.g. the test fails if a `Model`-comparison hook is invoked).
- **Deps:** C2, B1 (Configurable/Attachable), B6.

### C4. Stats collector / rate subscriber (`compose`) — §5
- **Goal:** a telemetry-bus `stats`-topic subscriber that computes rates from `StatSample`
  deltas (replaces the old metrics hub). Placeholder components emit fake `StatSample`s.
- **Accept:** feeding two samples N seconds apart yields the expected rate.
- **Deps:** B3, C2.

---

## Group D — Placeholders (where real functionality will land; depends on B + C)

Each placeholder is a `Component` that satisfies the right interfaces and capabilities but
does nothing real (logs "not implemented", returns zero values). They make the harness
*runnable* end-to-end and give Phase 2 a concrete target to fill in.

### D1. Placeholder ports (ethertalk / localtalk / ipx / netbeui)
- **Creates:** `core/port/<name>` placeholder `Component`s that take a `FrameLink`/`DatagramLink`,
  implement `Bindable`/`Statful`/`Configurable`, and no-op the data path.
- **Accept:** registry can build them; supervisor can start/stop/reconfigure them.
- **Deps:** B1, B2, C1–C3.

### D2. Placeholder router (`core/router`)
- **Defines** the `RoutedPort` data interface (§3) and the router's membership API; the
  placeholder implements the `Component` lifecycle with no real RTMP/ZIP. **Signatures:**

```go
// Package router — AppleTalk router membership + DDP data interface (§3). Placeholder in Phase 1.
package router

import (
    "github.com/ObsoleteMadness/ClassicStack/core/component"
    "github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// RoutedPort is the data half a routed port exposes to the router (the lifecycle half is
// component.Component). A port is RoutedPort + Component. The router never knows whether the
// port's datagrams came from a kernel socket or a Framing(FrameLink) (§2).
type RoutedPort interface {
    component.Component
    Unicast(network uint16, node uint8, d ddp.Datagram)
    Broadcast(d ddp.Datagram)
    Network() uint16
    Node() uint8
    NetworkMin() uint16
    NetworkMax() uint16
}

// Router is a Component. Attach/Detach are membership events: Detach withdraws the port's
// directly-connected routes IMMEDIATELY (no aging delay, §3). Inbound is the port→router hook.
type Router interface {
    component.Component
    Attach(p RoutedPort) error
    Detach(p RoutedPort) error
    Inbound(d ddp.Datagram, from RoutedPort)
}
```

- **Accept:** placeholder `Router` + a placeholder `RoutedPort` (from D1); Attach then Detach
  fires the membership hooks; supervisor can start/stop it.
- **Deps:** B1, B2, B7, D1.

### D3. Placeholder services (afp / smb / netbios / macip)
- **Creates:** `core/service/<name>` placeholder `Component`s consuming a `DatagramLink`
  (where applicable) and the `core/fs`/`core/metastore` interfaces; no real protocol logic.
- **Deps:** B1, B8, B9.

### D4. Placeholder adapters (so the harness can run for real on one platform)
- **Creates:** the *minimum* real adapters to run an end-to-end no-op stack on desktop:
  `adapter/link/inmem` (loopback FrameLink), `adapter/config/toml`, `adapter/store/file`,
  `adapter/control/inproc`, and a thin `adapter/control/http`. Heavy *data-path* adapters
  (pcap/sqlite/s3) are **declared but stubbed** in Phase 1.
- **Note:** OpenWRT adapters (UCI codec/store, ubus control) are **not** deferred to Phase 2 —
  they get their own first-class harness step (D6) so the contract is proven OpenWRT-shaped
  while the skeleton is empty.
- **Deps:** B2, B6, B10.

### D5. Assembly + a runnable skeleton main (`compose` + `cmd/`)
- **Goal:** wire registry → supervisor → placeholders → in-proc control, so a new
  `cmd/classicstack-ng` (temporary name) boots the empty stack, reports status, accepts a
  `Reconfigure`, and shuts down cleanly. Does not replace the real `cmd/classicstack` yet.
- **Accept:** `go run ./cmd/classicstack-ng` starts placeholders, a control call lists them as
  "running (placeholder)", reconfigure works, clean shutdown.
- **Deps:** all of C, D1–D4.

### D6. OpenWRT compatibility — UCI config + ubus control + procd (§4, §7)
- **Goal:** prove the config seam and the control contract are genuinely OpenWRT-shaped **on
  the empty skeleton**, so Phase 2 finds no impedance mismatch. First-class-on-each-target
  (charter) must be demonstrated in Phase 1, not assumed.
- **Creates:**
  - **`adapter/config/uci`** — a `Codec` that marshals/unmarshals the `core/config.Model`
    to/from UCI syntax (config `'classicstack'`, `option`/`list`, sections per component),
    and **`adapter/store/uci`** — a `Store` that reads/writes via the UCI tree (shelling
    `uci`/`/etc/config` on-target; a file-backed fake off-target for tests). Round-trips the
    same Model the TOML codec does.
  - **`adapter/control/ubus`** — registers a `classicstack` object on **`ubus.sock`**; each
    `Plane` method → a ubus method (typed `blobmsg` policy); `Subscribe(topic…)` → ubus
    notifications. On non-OpenWRT dev hosts it builds against a small ubus-socket shim/fake so
    the parity test (E3) can exercise it without an OpenWRT box.
  - **procd integration sketch:** an `init.d`/procd service file + `service classicstack reload`
    → a `Plane.Reconfigure`, documented (a stub script in `.refactor/` or `contrib/openwrt/`),
    verified by a script-level smoke test where feasible.
- **Accept:** UCI codec round-trips the Model (`Unmarshal(Marshal(m)) == m`) identically to
  TOML (B6); the ubus adapter answers `Status`/`Reconfigure` against the in-proc `Plane` and
  relays `state`/`stats`/`log`; the parity test (E3) includes ubus and passes. All ubus/UCI
  code lives in adapters — archtest (A2) confirms none of it leaks into `core/`.
- **Must not:** pull ubus/UCI deps into core; assume an OpenWRT host for the unit tests (use
  fakes), but keep an on-target `ubus call`/`ubus listen` smoke check for CI-with-OpenWRT.
- **Deps:** B6, B10, D4, D5 (needs a running `Plane`).

---

## Group E — Test harness for the structure itself

### E1. Component conformance harness (Verification #4)
- **Goal:** a shared table-driven harness any component can be run through: Start→Stop→Start
  idempotency, `Stop` after failed `Start`, `Statful` returns without panic, `Configurable`
  hot-apply + `ErrNeedsRestart` paths.
- **Accept:** every placeholder component passes it.
- **Deps:** B1, C2.

### E2. Bus conformance + back-pressure tests
- **Goal:** reusable tests for the bus primitive (B3) reused by both bus instances (B3/B4):
  topic scoping, drop-tolerance, unsubscribe, no-alloc on unrequested topics.
- **Deps:** B3, B4.

### E3. Multi-front-end parity test (Verification #6)
- **Goal:** drive the in-proc, http, **and ubus** control adapters against the same `Plane`;
  assert identical method results + identical relayed topics across all three. This is the
  executable proof that OpenWRT is a first-class peer, not a reduced variant.
- **Accept:** parity holds for `Status`/`Reconfigure`/etc. and for `state`/`stats`/`log`
  relays; runs against the ubus fake off-target, with an on-target `ubus call`/`ubus listen`
  smoke variant when an OpenWRT runner is available.
- **Deps:** B10, D4, D5, D6.

### E4. Reconfigure-and-notify test (Verification #8)
- Already specified in C3's acceptance; E4 formalises it as a standing test in the harness
  suite and adds the "no model-diff occurs" assertion.
- **Deps:** C3.

### E5. Wire the new tests into CI
- **Goal:** the A4 CI matrix runs E1–E4 + archtest (A2) + codec round-trips (B6 TOML **and**
  D6 UCI) + DDP round-trip (B7) on every push, **plus the TinyGo amd64 linux & windows build
  gates (A4)**.
- **Accept:** CI green; a forbidden import, a reflection-using core package (caught by both
  archtest and the TinyGo gate), a broken placeholder, or a UCI/TOML round-trip divergence
  fails CI.
- **Deps:** A4, D6, E1–E4.

---

## Phase 1 exit criteria (Definition of Done)

- [ ] `core/` / `adapter/` / `compose/` rings exist; every core interface from §2–§11 is
      defined and documented.
- [ ] Both bus instances (telemetry + FS) exist on one primitive, with topic scoping + tests.
- [ ] Logging (scoped, levelled, multi-sink, zero-reflection) exists with no-alloc disabled path.
- [ ] Registry + supervisor can start/stop/**reconfigure** placeholder components via the DAG.
- [ ] A runnable `cmd/classicstack-ng` boots the all-placeholder stack and answers control calls.
- [ ] Import-graph gate, component-conformance, bus, reconfigure-and-notify, and parity tests
      are green in CI.
- [ ] **TinyGo amd64 build gates (linux + windows) are green** and demonstrably fail when a
      forbidden/reflection import is added to a core package on the TinyGo path.
- [ ] **OpenWRT seam proven on the skeleton:** UCI codec/store round-trips the Model (parity
      with TOML); the ubus adapter answers Plane methods + relays topics; E3 parity includes
      ubus; no UCI/ubus deps leak into `core/`.
- [ ] **Zero real protocol/service logic ported** (DDP codec excepted, per B7).
- [ ] Existing `internal/app` stack still builds and runs untouched.

When all boxes are ticked, proceed to [02-PHASE-migration.md](02-PHASE-migration.md).
