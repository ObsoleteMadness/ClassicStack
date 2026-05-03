# ClassicStack Architecture

ClassicStack is a Go AppleTalk Phase 2 router and AFP file server. It bridges
legacy Apple networking protocols to modern environments — EtherTalk
(raw Ethernet), LToUDP (multicast UDP), TashTalk (serial), and
virtual LocalTalk transports — and serves AFP volumes over both the
classic ASP/ATP/DDP stack and modern DSI/TCP.

This document is the entry point for contributors. Read it once and
you should know where any piece of code lives, why, and what it can
import.

## Module map

```
cmd/classicstack/   wiring only — flag/INI parsing, service registration
config/         single typed config tree; INI loader, validation
protocol/       wire format only (codec + constants, zero I/O)
  ddp/            DDP datagram + MacRoman codec
  (atp, asp, zip, rtmp, aep, llap, nbp to follow)
port/           link-layer transports (Port + RawLink)
  ethertalk/    raw Ethernet via libpcap/Npcap, AARP
  localtalk/    LocalTalk + LToUDP/TashTalk/Virtual backends
  rawlink/      generic raw L2 link abstraction
  nat/          OS-stack NAT helper (used by macip)
router/         Router, RoutingTable, ZoneInformationTable
service/        stateful services; compose protocol + port
  afp/          Apple Filing Protocol server
  asp/ dsi/     AFP transports (classic and modern)
  atp/          AppleTalk Transaction Protocol
  zip/          Zone Information Protocol
  rtmp/         Routing Table Maintenance Protocol
  aep/          AppleTalk Echo Protocol
  llap/         LocalTalk Link Access Protocol
  macip/        IP-over-AppleTalk gateway with NAT and DHCP relay
  macgarden/    Macintosh Garden HTTP client (used by macgarden VFS)
pkg/            reusable, AppleTalk-agnostic
  binutil/      allocation-free wire codec helpers, Wire interface
  appledouble/  AppleDouble v2 sidecar format (parse/build)
  cnid/         AFP Catalog Node IDs (memory + SQLite stores)
  logging/      slog factory: handler config, level parsing
  telemetry/    Counter/Gauge/Histogram via expvar (otel build tag)
netlog/         project logging API — Debug/Info/Warn facade over slog
spec/           Apple protocol references (read this when touching wire code)
```

## Layering rules

```
cmd  →  service  →  (protocol | port | pkg)
                            ↓        ↓
                          (no I/O)  (port-side)
```

- `protocol/*` has zero I/O, zero goroutines, zero state. Pure
  encode/decode and constants. Cite the relevant `spec/` document in
  the package doc comment.
- `port/*` owns the link layer. It knows about frames and addresses,
  not about higher protocols.
- `service/*` owns sockets, sessions, and state machines. It composes
  `protocol` codecs over `port` transports.
- `pkg/*` is reusable outside ClassicStack. It must not import anything
  under `service/`, `port/`, `cmd/`, or `router/`.
- `internal/*` is private to ClassicStack. Mocks and shared test harness
  live here.
- `cmd/classicstack/` does no business logic. It parses configuration
  and wires services together.

## Core interfaces

| Interface | Where | Purpose |
|---|---|---|
| `port.Port` | [port/port.go](port/port.go) | Unicast/Broadcast/Multicast frame transport |
| `port.BridgeConfigurable` | [port/port.go](port/port.go) | Optional bridge-mode and host-MAC knobs |
| `port/localtalk.FrameSender` | [port/localtalk/localtalk.go](port/localtalk/localtalk.go) | Backend hook for LocalTalk variants |
| `port/rawlink.RawLink` | [port/rawlink/](port/rawlink/) | Raw L2 read/write — used by EtherTalk and MacIP |
| `service.Service` | [service/service.go](service/service.go) | Object plugged into the router by socket |
| `service.Router` | [service/service.go](service/service.go) | What services see of the router |
| `afp.FileSystem` | [service/afp/fs.go](service/afp/fs.go) | Pluggable AFP volume backend |
| `cnid.Store` | [pkg/cnid/cnid.go](pkg/cnid/cnid.go) | Catalog Node ID persistence |
| `binutil.Wire` (canonical shape) | [pkg/binutil/binutil.go](pkg/binutil/binutil.go) | `MarshalWire`/`UnmarshalWire`/`WireSize` |

## Configuration

Single typed tree in `config/`. Two loaders feed it:

1. TOML — `config.Load(path)` parses `server.toml` via `knadh/koanf`
   with the `pelletier/go-toml` v2 parser.
2. Flags — `cmd/classicstack/main.go` overlays CLI flags on top of the
   file defaults.

`config.Root.Validate()` runs once before services start. Services
receive typed subtrees at construction time. Construction options
are immutable: ports do not mutate themselves after `Start()`.

## Logging and telemetry

ClassicStack has two logging packages with distinct jobs:

- **`netlog/`** is the call-site API. Services and ports use
  `netlog.Debug`, `netlog.Info`, `netlog.Warn`. The facade keeps call
  sites short (no per-package `*slog.Logger` plumbing) while still
  routing through whatever structured handler `cmd/classicstack` installs.
- **`pkg/logging/`** is the slog factory used once at startup.
  `cmd/classicstack` calls `logging.New("ClassicStack", ...)` to build a
  `*slog.Logger` with the configured handler (console, JSON, or both)
  and installs it via `netlog.SetLogger`. Use this directly only when
  you need a `*slog.Logger` value — e.g. attaching structured fields
  with `.With` for the lifetime of an object.

Sources are tagged in two complementary ways: messages carry a
`[AFP]` / `[ASP]` / `[EtherTalk]` prefix that grep finds in either
format, and the slog handler stamps every record with a `source`
attribute that JSON consumers can filter on.

Stdlib `log.Printf` and `log.Fatal` are not used inside library code.
`cmd/classicstack/main.go` uses `log.Fatal*` only for unrecoverable startup
errors before any logger is wired.

Telemetry is `pkg/telemetry`, separate from logs. Default backend is
`expvar` (stdlib, zero deps). Initial counters:
- `classicstack_router_frames_in_total`
- `classicstack_afp_commands_total`
- `classicstack_aarp_probe_retries_total`

A future `//go:build otel` file will swap in an OpenTelemetry backend
without touching call sites.

## Wire codec convention

The canonical shape lives in [pkg/binutil/binutil.go](pkg/binutil/binutil.go):

```go
type Wire interface {
    MarshalWire(b []byte) (n int, err error)   // append-style, no alloc
    UnmarshalWire(b []byte) (n int, err error)
    WireSize() int
}
```

Implementations live alongside their model types. `pkg/binutil` provides
allocation-free `PutU8/16/32/64`, `GetU8/16/32/64`, and Pascal-string
helpers. Errors:
- `binutil.ErrShortBuffer` for buffer-too-small.
- `binutil.ErrMalformed` for invalid prefixes / enum values.

Migrated so far: ASP `WriteContinuePacket`, ATP `ATPHeader`, DSI `Header`.
Other wire models still use raw `binary.BigEndian` calls; migration
proceeds one type per commit with golden hex round-trip tests.

## Timer and retry patterns

ClassicStack does not use exponential backoff. The protocols predate it.
Three canonical shapes:

1. **Reliable-delivery retransmits** (ATP-style). Per-transaction
   `retryTimeout` + `retriesLeft` counter, an injectable `Clock.AfterFunc`
   so tests control time. Exemplar: `service/atp/transaction.go`.
2. **Periodic polling** (AARP probe, AMT aging, routing-table aging).
   `time.NewTicker` from a goroutine that selects on `<-ctx.Done()`
   (or `<-stop`). The tick cadence *is* the policy. Exemplar:
   `port/ethertalk/ethertalk.go:acquireAddressRun`.
3. **One-shot waits** (LocalTalk CTS response, DSI request/reply).
   `time.NewTimer` + `select { case <-timer.C: ...; case <-resp: ... }`.

If a future consumer genuinely needs exponential backoff, extract it
then. Don't speculate.

## AFP architecture

AFP supports two transport stacks simultaneously:
- **Classic**: DDP → ATP → ASP → AFP
- **Modern**: TCP → DSI → AFP

Both deliver into a shared `afp.CommandHandler`. Today that handler is
the 525-line switch in [service/afp/server.go](service/afp/server.go).
A future commit decomposes it into a registry of per-command handlers
under `service/afp/commands/`.

AppleDouble metadata is stored either as `._filename` sidecars or in
`.appledouble/` folders (Netatalk-compatible). The sidecar **format**
lives in [pkg/appledouble](pkg/appledouble/); the AFP-specific
`ForkMetadataBackend` (which talks to the host filesystem) stays in
`service/afp/`.

CNID tracking goes through [pkg/cnid](pkg/cnid/) with two backends:
in-memory (default for tests) and SQLite (`modernc.org/sqlite`,
default for production). Each volume gets its own `cnid.Store`.

## File system backends

`service/afp` defines `FileSystem` (see [service/afp/fs.go](service/afp/fs.go)).
The shipped backend is `LocalFileSystem`. A `macgarden_fs.go` backend
exists alongside it; a future commit relocates it to
`service/afpfs/macgarden/` behind `//go:build macgarden`, registered
through a factory map in `afp` so adding new backends does not modify
the core package.

## Spec references

The `spec/` directory contains 14 markdown documents describing the
protocols this codebase implements. Start with `spec/00-overview.md`
for DDP socket assignments and service interface contracts before
modifying router or service code. PRs touching protocol semantics
must cite the relevant section.

## Glossary

- **DDP**: Datagram Delivery Protocol. AppleTalk's network layer.
- **ATP**: AppleTalk Transaction Protocol. Reliable request/response.
- **ASP**: AppleTalk Session Protocol. Sessions over ATP.
- **DSI**: Data Stream Interface. AFP transport over TCP.
- **ZIP**: Zone Information Protocol.
- **RTMP**: Routing Table Maintenance Protocol.
- **AEP**: AppleTalk Echo Protocol.
- **NBP**: Name Binding Protocol.
- **AFP**: Apple Filing Protocol.
- **CNID**: Catalog Node ID. AFP's persistent file/directory identifier.
- **AppleDouble**: Sidecar format for storing resource forks and Finder
  metadata on non-HFS filesystems.
- **AARP**: AppleTalk Address Resolution Protocol (Ethernet-side).
- **LLAP**: LocalTalk Link Access Protocol.
- **MacIP**: IP-over-AppleTalk gateway protocol.
