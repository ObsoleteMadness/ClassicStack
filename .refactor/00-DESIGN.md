# Greenfield Architecture: ClassicStack / OmniTalk

## Charter — what ClassicStack is for

ClassicStack **bridges contemporary systems and legacy systems** by implementing legacy
network protocols and file/print services in **modern, self-contained code** — with **no
dependency on legacy kernel extensions or OS services** (no `AF_APPLETALK` stack, no Windows
Services-for-Macintosh, no kernel IPX). We meet users where they are — Windows, macOS, Linux,
or embedded — and serve as many legacy clients as possible (Classic Mac OS, DOS, early
Windows). The four pillars below drive every design decision in this document.

**Cross-platform — first-class on each target.**
- *Desktop* (Windows / macOS / Linux): behaves natively — Windows Service, launchd, systemd.
- *OpenWRT*: lives in the ecosystem — UCI configuration, ubus control, procd integration.
- *Embedded* (primarily ESP32 via **TinyGo**): the same core runs on microcontrollers.
  TinyGo is also a **size lever on desktop** — e.g. a TinyGo build inside an Alpine image to
  keep a Linux container tiny.

**Flexible.**
- Components are **selectable at build time** so binaries stay small (only what you ship is
  linked).
- Ports and services **start, stop, and reconfigure at runtime** (§11).
- **No leaky abstractions** — adapters absorb each environment's capabilities (§1).

**Small.** Clear separation of concerns; prefer the smaller implementation; delete code.

**Adaptable.** **No hard-coded assumptions about the physical interface.** A port works over
pcap, TAP, PPP/SLIP, a kernel datagram socket, or an in-memory pipe **without rewriting the
port** — the link is an adapter (§2).

### Compatibility over correctness (a deliberate stance)

We target long-obsolete systems that predate modern best practice (no/weak encryption, no
bounds discipline, quirky clients). Our goal is **compatibility — including bug-for-bug
compatibility with real clients — not abstract spec correctness.** Concretely:

- **Observed client behaviour outranks the spec.** Where a real Classic Mac / DOS client
  disagrees with the written protocol, we match the client and **document the deviation**
  (per CLAUDE.md: code comment + `spec/errata.md`).
- **Quirk handling is a feature, not debt.** Working around known client bugs is expected.
- **Where we have no spec, the wire is the spec** — observed framing/commands/responses are
  documented and become the contract (e.g. MacIPX from captures).

### Security posture — host-side modern, wire-side faithful

The governing split: **apply modern good practice to everything on *our* side of the bridge,
and faithfully speak the legacy client's insecure dialect on the *wire* side.** Insecurity is
scoped to the wire-facing edge by necessity — it is never leaked inward to the host.

**Host side (we hold ourselves to current practice):**
- **Robust, safe Go**: no memory-unsafety, careful parsing of hostile input, fail closed on
  our own bugs, sanitise everything that crosses into the host (paths, names, sizes).
- **Credentials at rest are protected with modern primitives** even when the protocol that
  *uses* them is weak — e.g. a user store keeps salted-hashed passwords, regardless of the
  fact that the legacy auth handshake will compare them against a cleartext or
  weakly-hashed value off the wire.

**Wire side (compatibility is the requirement, weakness is intentional):**
- We **implement the legacy protocol's auth and crypto exactly as the client expects** —
  cleartext passwords, obsolete ciphers, broken hashes — because that is the only thing the
  client can speak. Refusing defeats the project's purpose.
- **Deliberately-obsolete cryptography is a feature, not a defect.** Example: a future HTTP
  proxy may MITM and **re-encrypt with SSL 3.0 and dead ciphers** so a Netscape 2.0 client
  can connect over "SSL." A modern browser will (correctly) reject it; the legacy client
  works. A security scanner flagging the SSL 3.0 code path **must not "fix" it** — it is
  there on purpose, behind a feature/build flag, and annotated as intentional.

**Our duty in exchange:** **identify, document, and surface** every exposure so operators
deploy with eyes open — a per-protocol security note in docs and, where relevant, a warning
in the UI. CI verifies the note exists (§ verification); it never tries to harden a protocol
beyond what its clients support.

## Context

This document specifies the **target greenfield architecture**, designed from first
principles around the charter above — not by extrapolating the current code. The existing
stack is treated only as a feasibility reference; where it already proves a pattern works,
good, but it is **not** the starting point and its structure does not constrain the design.

The whole design reduces to one principle in service of the charter: **a pure,
dependency-free core (protocols, ports, services, router, config model, control contract)
with every platform concern — pcap, file I/O, config format, control transport, fork/metadata
storage — pushed out to adapters at the edges.** This hexagonal (ports-and-adapters) shape is
what makes the four pillars achievable at once: TinyGo/embedded portability, OpenWRT-native
operation, build-time component selection for small binaries, and physical-interface
independence.

A later pass maps current code onto this target and sequences incremental (strangler)
refactors. Bias throughout: **delete code, fewer lines, fewer deps, no leaks into the core.**

---

## 1. Layering (the dependency rule)

Dependencies point inward only. Nothing in an inner ring may import an outer ring.

```
            ┌─────────────────────────────────────────────┐
   adapters │ pcap · tuntap · serial · file · http · ubus  │   (build-tagged, heavy deps)
            │ uci · toml · pcapfile-writer                 │
            ├─────────────────────────────────────────────┤
   compose  │ assembly: registry-driven wiring, supervisor │   (knows adapters + core)
            ├─────────────────────────────────────────────┤
   core     │ router · services (afp/smb/netbios/macip…)   │   (pure Go, no OS/net deps
            │ ports (ethertalk/ipx/netbeui/localtalk)      │    beyond stdlib; TinyGo-safe)
            │ protocols (ddp/atp/asp/ipx/netbeui/smb…)     │
            │ config model · control contract · stats      │
            └─────────────────────────────────────────────┘
```

**Core rule (enforceable):** packages under `core/` (or equivalent) may import only the
standard library and each other. No `pcap`, no `koanf`, no `net/http`, no `gopacket`, **and
no `net`** (see the note below). A CI check greps core import graphs for forbidden packages
(cheap, catches regressions).

**Why `net` is forbidden in core (the TCP-services boundary).** `net` is *not* available on
every embedded target: an ESP32-C3/S3 has WiFi + the `tinygo-org/net`+`netdev` stack and can
run a TCP listener, but an RP2040 / Raspberry Pi Pico has **no net stack at all** — yet it can
still drive a raw-Ethernet `FrameLink` and therefore speak DDP. If `net` types lived in core,
core would stop compiling for the Pico-class target and break the embedded pillar. So TCP is a
**capability some builds have and some don't** — exactly what the §8 build-tagged registry
exists for. Stream/TCP services (AFP-over-DSI, SMB-over-TCP, a future HTTP proxy) are therefore
**opt-in adapters** (`adapter/dsi` behind a `dsi` tag, `adapter/smbtcp` behind `smbtcp`) that
import `net` at the *adapter* altitude, over a **pure command core that imports no `net`**
(§3). Absent the tag the adapter is not compiled, the component is not registered, and the
AppleTalk/DDP path is unaffected.

`net` is not unique to TCP services — **any adapter may import it**: the web-UI front-end
(`adapter/control/http`, §7) uses `net`/`net/http`, a future `net`-backed link adapter would
too. The rule is precisely *"`net` lives in adapters, never in core,"* so a build that links
none of those adapters (a netless embedded DDP-only build) carries no `net` at all, while a
desktop build that wants the web UI and DSI links `net` through those adapters without core
ever depending on it.

This is what makes TinyGo viable: a TinyGo build links the core + only the adapters that
compile under TinyGo (e.g. an in-memory or fd-based link, no libpcap, and — on a netless
target — no DSI/SMB-TCP).

### Two further core-wide rules (embedded discipline)

These apply everywhere in core, alongside the dependency rule, and are driven by the
embedded/TinyGo pillar of the charter:

- **No reflection in core.** Reflection (`reflect`, and the `...any`/`interface{}`-value
  patterns that pull it in transitively — `encoding/json`, `slog` attrs, `fmt` with `%v` on
  arbitrary types) inflates TinyGo binaries and adds runtime cost. Core uses **typed code**
  instead: typed config sections (§4), typed log fields (§6), hand-written or
  generated-without-reflection codecs. Marshalling that *needs* reflection lives in an
  **adapter** (e.g. the JSON control encoding), never in core. The import-graph gate also
  flags `reflect` in core.
  - *Corollary — one home for byte-order codecs.* Because `encoding/binary` transitively
    imports `reflect` (its `Read`/`Write` reflection paths), it is **banned in core** (the gate
    flags it). The fixed-width big-/little-endian integer codecs every protocol and service
    needs therefore live in **one** package, **`core/binaryprimitives`** — readers (`BE16`,
    `LE32`, …), in-place writers (`PutBE16`, `PutLE32`, …), and append writers (`AppendBE16`,
    `AppendLE32`, …). Do **not** re-hand-roll `be16`/`putLE32`/etc. per package (the pattern
    the migration found duplicated a dozen times); import `binaryprimitives` and call it. The
    package is dependency-free and reflection-free, so it is safe for every ring (adapters use
    it too, rather than re-deriving the same shifts). Note `fmt` also pulls `reflect`
    transitively — even a lone `fmt.Fprintf("%02X")` in a core package trips the gate — so
    format small fixed things by hand in core (see `core/fs/codec.go`).
- **Allocation discipline, with per-target buffer sizing.** Hot paths (link read/write loops,
  framing, log formatting) avoid per-call allocation: reuse slices, use fixed sensibly-sized
  buffers, and pool where a buffer outlives a single call. Buffer sizes are **constants chosen
  per build target** — small on TinyGo/ESP32 (tight RAM), larger on desktop (throughput) —
  expressed as build-tagged constants in a `core/buf` package so a port/service reads
  `buf.FrameMax` rather than hard-coding a number. This keeps one code path across targets
  with target-appropriate memory behaviour.

---

## 2. The Link edge — ports process byte slices only

**Problem today:** `port/ipx/port.go` imports `rawlink`, `capture`, `netlog`; hard-codes
`IPXBPFFilter`; knows libpcap handle lifecycle via `LinkFactory`; does kernel-loopback
dedup. EtherTalk/MacIP are similar. The filter, the pcap reopen-on-restart dance, and
capture all leak into protocol code.

**Target:** a port is a pure frame codec. It receives `[]byte` frames and emits `[]byte`
frames through one narrow interface, and knows nothing about *where* bytes come from.

```go
// core/link — the ONLY thing a port talks to for I/O.
type Frame = []byte

type Link interface {
    Read() (Frame, error)   // ErrTimeout / ErrClosed sentinels; caller owns slice
    Write(Frame) error
    Close() error
}
```

Everything currently bolted onto the link or the port becomes a **decorator** in the
adapter layer, composed outside the core:

- **Filtering** — today a port pushes a BPF string into the link. Instead the *adapter*
  applies the kernel BPF (pcap can), and a **software fallback filter** is a `Link`
  decorator (`filterLink{inner, predicate}`) for backends without kernel filtering.
  The BPF string stays with the pcap adapter; the *predicate* (a pure func on bytes)
  can live near the protocol if software-side matching is needed. The port itself only
  sees already-filtered frames.
- **Capture** — a `captureLink{inner, sink}` decorator tees frames to a `capture.Sink`.
  Ports stop importing `capture` entirely.
- **Loopback dedup** — a `dedupLink` decorator (the IPX `recentFrames`/`frameHash` logic
  generalised), so every Ethernet-shared port gets it for free and no port reimplements it.
- **Bridge MAC rewrite** — a `Link` decorator that rewrites MACs for Wi-Fi/bridged segments.
- **Restart/reopen** — reopening a backend handle across Stop/Start lives in the adapter; the
  core port just holds a `Link` and calls `Close`. A "reopenable" wrapper in the adapter
  layer hands back a fresh inner link per Start.

Result: a port imports none of pcap, capture, filter, or reopen-lifecycle code; it is a frame
codec tested against a trivial in-memory `Link`.

**Capability discovery** is interface-based: adapters that can report the physical medium or
do kernel-level filtering expose extra optional interfaces (`MediumReporter`,
`FilterableLink`); composition code type-asserts to discover them. Ports never do — a port
that needs no capability sees only the minimal `Link`.

### Two link altitudes, but one is a decorator over the other

There are two link shapes, and they **compose** — `DatagramLink` is not a parallel,
independent interface but a layer that can sit *on top of* a `FrameLink`:

```go
// core/link
type FrameLink interface {              // raw L2 frames: pcap, TAP, PPP/SLIP, esp32 raw
    Read() (Frame, error); Write(Frame) error; Close() error
}
type DatagramLink interface {           // pre-framed DDP datagrams
    ReadDatagram() (ddp.Datagram, error)
    WriteDatagram(ddp.Datagram) error
    Close() error
}
```

A `DatagramLink` is obtained in one of two ways, and **the layers above cannot tell which**:

1. **`framing(FrameLink) DatagramLink`** — an adapter that does DDP encap/decap (and, for
   EtherTalk, AARP/node-claim) over a raw frame link. The full chain is
   `pcap → framing → DatagramLink → service/router`. This is the path on any platform where
   we own the wire.
2. **a kernel/native datagram socket** — e.g. Linux `AF_APPLETALK`, or a TinyGo/ESP-IDF
   socket stack — implements `DatagramLink` directly because the OS already did the framing.
   Used as an *interop convenience*, never a dependency (charter).

The decorators earlier in §2 (filter / dedup / capture / bridge-MAC) operate at the
**FrameLink** altitude. The `framing` adapter is just one more frame-level consumer; capture
at the datagram altitude, if wanted, is a `DatagramLink` decorator.

Why this factoring is better than two independent interfaces:

- **Services consume `DatagramLink`; ports/framers consume `FrameLink`.** A file/print or
  router service is written once against datagrams and **runs unchanged** whether those
  datagrams come from the Linux kernel DDP socket **or** from our own `pcap → framing` stack.
  That is exactly the "native kernel datagram OR our raw frame links" goal.
- **No duplicated surface.** There is no second hierarchy to keep in sync; the kernel socket
  and `framing(frameLink)` are two implementations of the *same* `DatagramLink`.
- **AARP / node-claim live in the `framing` adapter** (EtherTalk's), not in the router or the
  service — so the kernel-socket implementation legitimately omits them, and the service layer
  never knows the difference.

The router only ever sees `RoutedPort` (§3) fed by a `DatagramLink`; it does not care whether
that datagram link is kernel-backed or frame-backed. Composition picks per deployment; nothing
in core changes. PPP/SLIP, pcap, and TAP differ only in which `FrameLink` adapter sits at the
bottom of the `→ framing → DatagramLink` chain — no port or service rewrite, per the charter's
adaptable pillar.

---

## 3. Unified component model (ports AND services AND protocols-as-transports)

The deep inconsistency is that AppleTalk ports, IPX/NetBEUI transports, and services
(NetBIOS, SMB) all have bespoke lifecycle + capability surfaces. Unify on **one small
lifecycle contract** plus **optional capability interfaces** — never a fat interface.

```go
// core/component
type Component interface {
    Name() string
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
}

// Optional capabilities (type-asserted by the supervisor / UI; never forced):
type Enableable  interface { Enabled() bool }
type Bindable    interface { Binding() string }                 // "eth0", ":548", "ipx:0550"
type Statful     interface { Stats() Stats }                    // see §5
type Configurable interface { ApplyConfig(Section) error }      // hot-apply, see §4
type Bridged     interface { SetBridgeMode(mode string) error } // existing BridgeConfigurable
type Metered     interface { SetTrafficObserver(TrafficObserver) }
```

This replaces today's `hook`, `portHook`, `routerHook`, `ddpServiceHook`, and the parallel
`IPXHook`/`NetBEUIHook`/`NetBIOSHook`/`SMBHook` interfaces with **one** `Component` plus
capabilities. The supervisor stores `map[string]Component` and a dependency DAG; it does
not need to know whether a component is a port, a service, or a bridged transport.

**Data-path interfaces stay separate and narrow** (this is the user's point #1 generalised):
- AppleTalk ports speak DDP datagrams to the router (`router.Inbound`).
- IPX/NetBEUI transports speak frames to their own mini-routers.
- A port is `Component` (lifecycle) + its protocol-specific data interface.

We do **not** force DDP semantics onto IPX, nor frames onto AFP. The unification is at
**lifecycle / config / stats / capability**, where the inconsistency actually hurts, not
at the data path, where forcing uniformity creates awkward fits.

There is a **third data-path shape** the datagram model doesn't cover: **stream/TCP services**
(AFP-over-DSI on `:548`, SMB-over-TCP on `:139`/`:445`). These don't read datagrams from the
router — they *listen* on a TCP port, *accept* connections, and serve a framed byte stream per
connection. They are not `RoutedPort`, have **no `Socket()`, and are never router-`Attach`ed**;
they are `Component` + `Bindable` (the bind address, §3-bis). Crucially, the listener and all
`net` use live in an **adapter**, never in core (the §1 `net`-forbidden rule).

### 3-bis. Command core vs. session transport (where TCP services sit)

A file/print service is split into a **pure command core** and one-or-more **session
transports** that wrap it:

```
 core/  (pure, no net — compiles on a netless Pico)   adapter/  (//go:build dsi)
 ┌────────────────────────────────┐                   ┌─────────────────────────────────┐
 │ afp command core               │◀── consumes ──────│ DSI server: net.Listener, accept │
 │   dispatch(sess, block)        │   a small         │ loop, 16-byte DSI framing over   │
 │     → (reply, result)          │   CommandHandler  │ net.Conn → core's CommandHandler │
 │ in-core ASP transport (DDP/ATP)│   the core exposes│ init() registers it (§8 registry)│
 └────────────────────────────────┘                   └─────────────────────────────────┘
```

- The **command core** (`dispatch(sess, block) → (reply, result)`) is transport-free and lives
  in `core/service/afp` (and `core/service/smb`). It imports no `net`. *This already exists for
  AFP*: the M7 spine's `dispatchAFP` is exactly this, with `asp.go` as one transport (DDP/ATP,
  in core).
- **Session transports** wrap the core for a specific wire. **AFP**'s:
  - **ASP** (in core) — DDP/ATP datagram transport; needs no `net`, so it stays in `core/`.
  - **DSI** (`adapter/dsi`, `//go:build dsi || all`) — owns the `net.Listener`, accept loop,
    per-conn goroutines, and 16-byte DSI framing; maps DSI
    `GetStatus/OpenSession/Command/Write/Tickle/CloseSession` onto the core `CommandHandler`.

  **SMB**'s session transports come in **two families**, and crucially **SMB itself does not
  distinguish them** — every one drives the same transport-agnostic seam (`SessionConsumer`:
  open a circuit, serve each reassembled message, close on teardown — `conn.go`). So SMB rides
  *with or without* NetBIOS:
  - **NetBIOS-based** (the session is a NetBIOS session; SMB plugs into the NetBIOS service as
    its `SessionConsumer`): **NBF** over NetBEUI, **NBIPX** over IPX (socket `0x0455`), **NBT**
    over TCP (`adapter/netbios-tcp`, session service on TCP 139). NBF + NBIPX are in core (no
    `net`); NBT is an adapter.
  - **Direct (NetBIOS-less)** — SMB framed straight onto the lower transport with no NetBIOS
    name/session layer, driving the **same** seam directly:
    - **Direct-hosted SMB over IPX** (socket `0x0550`) — Microsoft "NWLink direct host." A core
      transport (no `net`): its own connection-id framing on the IPX mini-router, then
      `NewConn`/`ServeMessage`/`Close`. *(Legacy `service/smb/over_ipx_direct` is exactly this.)*
    - **Direct-TCP SMB** (`adapter/smbtcp`, `//go:build smbtcp || all`, `:445`) — 4-byte
      length-prefixed framing over `net.Conn`. Needs `net`, hence an adapter.
  This is why server identity is NOT a NetBIOS-owned name (§4-bis): SMB has live transports
  (direct-IPX `0x0550`, direct-TCP `:445`) that never touch NetBIOS yet still advertise the
  hostname.

The core exposes a small `CommandHandler`-style seam (mirroring today's
`afp.CommandHandler.HandleCommand(block) → (reply, errCode)`) that the transport adapters
consume, so the dependency points **adapter → core**, never the reverse. The decoupling needs
**no new core interface and no `net` in core** — the §8 registry is the seam: the adapter
`init()` registers the component; build without the tag → no adapter → core untouched.

**Same code, three deployments** (the portability payoff):
- *Netless embedded (Pico-class):* no `dsi`/`smbtcp` tag, no `net`; raw-Ethernet `FrameLink` →
  DDP still serves AppleTalk.
- *WiFi embedded (ESP32-C3/S3):* build with `dsi`/`smbtcp`; the adapter (or an `//go:build esp32`
  sibling file in it) brings up WiFi/`netdev` (`espradio` + `net.UseNetdev(…)`) then `net.Listen`s
  — the same `net.Listener` interface as desktop.
- *Desktop/server:* the tag + stdlib `net.Listen`.
The AFP/SMB command-core source is **identical** across all three; only which adapters are
linked changes.

### 3-ter. The NetBIOS browser is a datagram-layer service, common to all transports

SMB is a *session* service; the **browser** (host/domain announcements, master-browser
elections, the `GetBackupList` exchange, the browse list the RAP/`NetServerEnum2` LANMAN call
serves) is a *datagram* service. The two are independent: a file server works fine with no
browser at all (clients still connect by name/IP), and a browser can run for hosts that serve
no files. Today the legacy code buries the browser inside `service/smb`
(`browser_frames.go`, `command_rap_lanman.go`, the `browserRole` machine in `server.go`),
coupling a transport-and-protocol-neutral concern to one session protocol. The greenfield
design breaks that out.

The browser is the **datagram analogue of the §3-bis command-core/session-transport split**.
NetBIOS runs over three transports — NetBEUI (NBF), IPX (NBIPX/NMPI), and TCP (NBT, UDP 138) —
and each carries *both* a session path (SMB rides it) *and* a connectionless datagram path. But
the browser does **not** sit directly on the raw datagram path: a **mailslot layer** (§3-quater)
sits between, because the browser, the messenger (`net send`), and the LANMAN RAP calls are all
**mailslot** consumers, and the `\MAILSLOT\*` SMB_COM_TRANSACTION envelope is a shared framing
none of them should re-implement. So:

```
 NBF datagram   ─┐                  ┌─ \MAILSLOT\BROWSE ─► browser  (HostAnnounce/Election/…)
 NBIPX mailslot  ├─ DatagramConsumer ┤─ \MAILSLOT\MESSNGR ─► messenger (net send; landed M7g)
 NBT  UDP-138   ─┘  (netbios.Datagram)│  mailslot router   └─ \MAILSLOT\LANMAN ─► (RAP datagram form)
                                      └─ unwraps the SMB_COM_TRANSACTION \MAILSLOT\* envelope,
                                         routes the INNER frame by mailslot name to the consumer
```

- The browser is a **`core/service/browser`** command core that holds **neither transport nor
  mailslot-envelope knowledge**. It registers with the mailslot layer for `\MAILSLOT\BROWSE` and
  only ever sees/sends a bare **browser frame** (HostAnnounce 0x01 / AnnouncementReq 0x02 /
  RequestElection 0x08 / GetBackupList 0x09/0x0A / DomainAnnounce 0x0C / LocalMasterAnnounce
  0x0F); the mailslot layer wraps/unwraps the SMB_COM_TRANSACTION envelope, and the NetBIOS
  transports do the per-protocol wire framing (NBF UI-frame / NBIPX NMPI-MailslotSend / NBT
  UDP-138). The browser maintains the browse list + election role (potential/backup/local-master)
  and sends its announcements through the mailslot layer, so **one browser serves NetBEUI, IPX
  and TCP with no per-transport AND no mailslot-framing code** — those differences live one and
  two layers below it respectively.
- The **RAP/LANMAN `NetServerEnum2` ("get server list")** that returns the browse list to a
  client arrives over the SMB **IPC$ named pipe** (`\PIPE\LANMAN`), i.e. on the *session* path.
  SMB therefore needs a thin seam to *ask the browser* for the current list — a small
  `BrowseList()` query interface the browser exposes and the SMB IPC$ handler consumes, with
  SMB still holding no browser logic. (This is the one place the session and datagram services
  meet; it is a read-only query, not a dependency that reorders lifecycle.)
- It is **optional** (`§8` registry, no `*_disabled.go`): a build or deployment that wants only
  file serving never links it, and the `DatagramConsumer` simply stays unset (datagrams drop
  after decode, as today). Elections/announcements are also configurable off (be a non-browser
  host that still announces itself, or announce nothing).

The payoff is the same as everywhere else: **one browser command core, three transports, one
mailslot layer, zero duplication** — and SMB stops carrying browser code it never should have
owned.

### 3-quater. The mailslot seam — a shared datagram-delivery layer, not SMB's

Mailslots (`\MAILSLOT\*`) are a general **second-class NetBIOS datagram delivery** mechanism
(connectionless, unreliable, one-way): a write to a named mailslot is carried in an
SMB_COM_TRANSACTION over a NetBIOS group/unique-name datagram. They are **not** owned by SMB and
**not** owned by any single consumer — several services receive on different mailslot names:

- `\MAILSLOT\BROWSE` — the browser (host/domain announcements, elections, GetBackupList).
- `\MAILSLOT\LANMAN` — the RAP datagram form (older browse traffic).
- `\MAILSLOT\MESSNGR` — the **messenger** service (`net send` / WinPopup); **landed (M7g)** as the
  second consumer, proving the seam is multi-consumer. It receives a single-block [MS-MSRP] message,
  logs it, and publishes `bus.MessageReceived` on the telemetry `message` topic (§5) for the UI; the
  send half (`Service.SendMessage`) is the core a future `cmd/csnetsend` (T1) wraps.
- (room for more — e.g. a DirectPlay-emulation consumer later.)

So the mailslot envelope is its **own seam**, sitting between the consumers and the NetBIOS
datagram path — exactly the "no per-protocol code in the consumer" rule applied one layer up:

```go
// core/protocol/mailslot — the SMB_COM_TRANSACTION \MAILSLOT\* envelope codec (the wrapper
// the browser used to marshal itself, lifted out). Self-serialising (DTO rule):
type Write struct { Name string; Body []byte; ... }   // Marshal / Unmarshal

// core/service/mailslot (or a router on the NetBIOS service) — the dispatch layer:
type Consumer interface { HandleMailslot(name string, src, dest Name, body []byte) }
//   Register(name string, c Consumer)                  // "\MAILSLOT\BROWSE" → browser
//   SendMailslot(name string, src, dest Name, body []byte, broadcast bool) error
```

- The mailslot layer is the thing that plugs into NetBIOS as the **`DatagramConsumer`** /
  `SendDatagram` user. It unwraps the `\MAILSLOT\*` envelope from an inbound `netbios.Datagram`,
  routes the **inner frame** to the consumer registered for that mailslot name, and on send wraps
  a consumer's frame back into the envelope and hands it to NetBIOS (which does the per-transport
  framing). Consumers (browser, messenger) never touch the envelope or any transport.
- **Layering, top to bottom:** consumer frame (browser/messenger) → mailslot envelope
  (`\MAILSLOT\*` SMB_COM_TRANSACTION) → NetBIOS datagram (names + payload) → per-transport wire
  framing (NBF UI-frame / NBIPX NMPI-MailslotSend / NBT UDP-138). Each layer owns exactly one
  concern; nothing reaches around another.
- It is **optional and lazy**: with no mailslot consumers registered, inbound mailslot datagrams
  drop after decode. A build that wants only file serving links neither the mailslot layer nor
  the browser.

This is the corrected home for the SMB_COM_TRANSACTION mailslot wrapper that an earlier browser
slice put inside `core/protocol/browser` — it is lifted into `core/protocol/mailslot` and the
browser is reworked to handle only browser frames. The RAP NetServerEnum2/NetShareEnum calls that
arrive over the **SMB IPC$ session pipe** (`\PIPE\LANMAN`, not a mailslot) stay where they are
(§3-ter); they are the session-path query, distinct from the datagram-path mailslot announcements.

### Router membership becomes event-driven (user point on dynamism)

The router exposes `Attach(p RoutedPort)` / `Detach(p RoutedPort)`. `Detach` *immediately*
withdraws all routes for that port (this already exists: `RemoveEntriesForPort`) and the
zone associations. Make this a first-class **membership event** rather than a side effect:

```go
type RoutedPort interface {
    Component
    DDPPort // Unicast/Broadcast/Multicast/Network/Node/range — today's port.Port data half
}
```

The periodic RTMP ager stays for *learned* routes (that's the protocol), but
directly-connected routes live and die with port membership — no aging delay. The
supervisor drives Attach/Detach as ports start/stop, which is close to today's
`routerHook` adopt/detach but expressed as one explicit contract instead of three.

---

## 4. Configuration — model is pure, formats are adapters

**Problem today:** `config.Load` uses koanf+go-toml; `config.Model` has TOML *and* JSON
struct tags; `Save` writes numbered-backup files. The serialised format (TOML) and the
storage (files) are baked into the core. OpenWRT/UCI and ubus can't reuse it.

**Target:** split into three rings.

1. **Core config model** — plain Go structs, *no* serialisation tags, no I/O. The single
   in-memory source of truth. Validation lives here (pure functions). This is roughly
   today's `config.Model` with the `toml:`/`json:` tags **removed**.

2. **Codec adapters** — convert between the model and a byte representation:
   `TOMLCodec`, `UCICodec`, `JSONCodec`, each in its own build-tagged adapter package.
   ```go
   type Codec interface {
       Marshal(*Model) ([]byte, error)
       Unmarshal([]byte, *Model) error
   }
   ```
   TinyGo/OpenWRT builds pull in only the codec they need; the server binary omits the rest.

3. **Store adapters** — where config lives and how it's versioned:
   `FileStore` (numbered backups, today's `save.go`), `UCIStore`, `MemStore` (tests).
   ```go
   type Store interface {
       Load() ([]byte, error)
       Save(data []byte) (revision string, err error)  // revision = backup path / UCI commit id
   }
   ```

`Plane.Save()` becomes `codec.Marshal(model)` → `store.Save(bytes)`. Swapping TOML-file
for UCI-store is composition, not a code change in core.

### 4-bis. Server identity is one top-level value, not per-service config

The server's **hostname** (e.g. `CLASSICSTACK`) is a *server-level* property, **not** a
NetBIOS-owned name — SMB needs it even when NetBIOS is absent. SMB runs without NetBIOS in real
deployments: **direct-TCP SMB (`:445`, §3-bis M7b)** has no NetBIOS layer at all (the client
connects by IP/DNS and SMB still advertises a server name in NEGOTIATE), and a deployment can
disable NetBIOS entirely (AFP-only, or SMB-over-`:445`-only) while SMB keeps serving. So the
ownership is: **the hostname is the server's, and each of NetBIOS / SMB / browser is a
*consumer*** — NetBIOS claims it as its workstation/file-server name *when running*, SMB
advertises it in NEGOTIATE, the browser announces it. It must have **exactly one source of
truth** so the consumers cannot disagree. The trap today: NetBIOS takes a `serverName` in its
constructor while SMB carries an independent `workgroup` and has no server-name field at all;
nothing connects them. The fix is **single ownership, not divergence detection** — make it
impossible to set two values, rather than validating two values agree.

So server identity is a **well-known top-level section of the config `Model`** (alongside
Logging/Router/Bridge, §4), owned by no single service:

```go
// core/config — top-level, cross-cutting; consumed by NetBIOS/SMB/browser, owned by none
type Identity struct {
    Hostname  string   // the server name. SMB advertises it (even over direct-TCP :445 with
                       // NO NetBIOS); NetBIOS claims it when running; browser announces it.
                       // Empty → derive from OS hostname. See validation note re: the
                       // NetBIOS ≤15-byte/upper-case constraint (a CONSUMER constraint, not
                       // intrinsic to the field).
    Workgroup string   // SMB NEGOTIATE domain + browser DomainAnnounce. Default WORKGROUP.
                       // Also NetBIOS-flavoured but, like Hostname, used by SMB without NetBIOS.
    Description string // free-text server comment (the remark a Windows browse list shows next
                       // to the server name). SMB packs it in its NetServerEnum2 self record;
                       // the browser carries it on its self announcement. Optional, empty = none.
                       // NOT NetBIOS-constrained (a comment, not a name).
}
```

Wiring rule (compose, M8a): the registry reads `Model.Identity` **once** and hands the same
`Hostname` to whichever consumers are linked/enabled — SMB (which gains a `SetServerName` it
advertises in NEGOTIATE — today it only has `SetWorkgroup`), `netbios.NewService(...)` *if
NetBIOS is enabled*, and the browser *if linked*. There is no per-service hostname field to
disagree with: `Hostname` lives only on `Identity`; the SMB/NetBIOS sections do **not** carry
one. A NetBIOS-less server (SMB on `:445` only, or AFP-only) simply has no NetBIOS consumer —
the field still drives SMB's advertised name. `Workgroup` flows the same way.

- **Validation is layered, on the single value** (§4 `Validate`): a baseline hostname check
  always applies (non-empty after default, no path/control chars). The **NetBIOS-specific**
  rules (≤15 bytes, upper-cased, no NetBIOS-reserved chars) are a **consumer constraint applied
  only when the NetBIOS service is enabled** — a 20-char hostname is legal for an SMB-over-`:445`
  / AFP-only server but rejected at Apply once NetBIOS is turned on (with a message naming
  NetBIOS as the constraint source). This keeps the limit where it belongs (NetBIOS) instead of
  baking a NetBIOS rule into a field SMB-without-NetBIOS also uses.
- **Defence-in-depth fallback:** *if* a config format or legacy import path ever surfaces a
  second name (e.g. a UCI `smb.@server.name` a user hand-edits), the model's `Validate` rejects
  a non-empty per-service name that disagrees with `Identity.Hostname` with a clear error,
  rather than silently picking one. This is the "error if they vary" guard — a backstop for
  external inputs, not the primary mechanism (the primary mechanism is that the model has one
  field).
- **Reconfigure (§11):** changing `Hostname` is a restart-grade change for NetBIOS (it must
  re-claim the name on every transport) and for direct-TCP SMB's advertised name — a
  `RestartRequired` from the affected services' `Reconfigure`, not a hot-apply.

**Landed (M8a, 2026-06-15):** `config.Identity{Hostname, Workgroup, Description}` is a well-known
top-level `Model` field (round-trips through both codecs). The registry reads it once and hands
the values to the enabled consumers: `reg_smb.go` calls `SetServerName`/`SetWorkgroup`/
`SetDescription` (a NetBIOS-less :445 SMB still self-reports name+comment in NetServerEnum2);
`reg_netbios.go` builds `netbios.NewService(logger, Identity.NetBIOSName())` when the hostname is
non-empty; the browser carries `Description` on its self `ServerEntry` (wired via `SetDescription`
once the browser is registry-wired — M8/M10 compose). `Identity.Validate` is the baseline
(no path/control chars); `Identity.ValidateForNetBIOS` is the ≤15-byte consumer constraint applied
only when NetBIOS is enabled. No per-service hostname field exists, so consumers cannot diverge.

### Schema registration (so new transports don't edit a central struct)

Today adding a transport means editing `config.Model`, `appConfig`, `appConfigFromModel`,
`modelFromAppConfig`, `buildPorts`/`buildHooks`. Replace the flat struct's *open-coded*
sections with a **section registry**: each component package registers a typed section
schema + a factory.

```go
// core/config
type SectionSchema struct {
    Key      string                       // "EtherTalk", "IPX", ...
    New      func() Section               // zero value of the typed section
    Validate func(Section) error
}
func Register(SectionSchema)             // called from component package init or explicit wiring
```

`Model` holds well-known top-level sections (Logging, Router, Bridge) as typed fields for
ergonomics, plus a `map[string]Section` of registered component sections. Codecs iterate
registered schemas — so TOML/UCI round-trip works for any registered transport without the
codec knowing it exists. This kills the `appConfig` ↔ `Model` double-conversion glue
(`config_model.go`), which exists only because the two representations drifted.

**Eliminate `appConfig`.** Components consume their own typed section straight from the
model at wiring time. The `resolveProtocolInterface` / bridge-inheritance logic becomes a
small pure helper on the model (`(*Model).EffectiveInterface(section)`), not a glue layer.

---

## 5. Event bus — one primitive, topic-scoped, instantiated per domain

Everything that changes over time flows on an **event bus** rather than being polled — a
poller samples a *level* and misses *transitions*. The bus replaces the scattered
notification mechanisms a grown system accumulates (bolt-on traffic meters, a metrics hub, a
status registry re-published by hand, a separate log broadcaster).

There is **one bus *primitive*** — a typed, topic-scoped, allocation-light pub/sub — and we
**instantiate it per domain** (see §10c): a control/telemetry bus here, a separate FS-mutation
bus in `core/fs`. "Multiple buses," "multiple channels," and "multiple topics" are the same
idea at different granularities: a **topic** is the selector; a **channel** is how one
subscriber receives a topic's events; a **separate bus** is the coarsest boundary (a whole
domain, kept apart for layering and binary size, §10c). The primitive supports the finer
grains; composition chooses the coarse ones.

```go
// core/bus — typed, in-process, allocation-light pub/sub. No reflection.
type Event interface{ Topic() string }

type StateChanged struct{ Component, From, To string }     // topic "state"
type StatSample   struct{ Component string; Stats Stats }  // topic "stats" (Stats typed, §5/§6)
type LogRecord    struct{ Component string; Level Level; Msg string; Fields []Field } // topic "log"; typed fields, not any

type Bus interface {
    Publish(Event)
    // Subscribe returns a channel carrying ONLY the named topics. An event whose
    // topic the subscriber didn't request is never enqueued onto its channel —
    // no per-event allocation or wake-up for events it would discard (§1 discipline).
    Subscribe(topics ...string) (<-chan Event, func())   // func() unsubscribes
}
```

So a UI log viewer does `Subscribe("log")`, the dashboard does `Subscribe("state","stats")`,
and neither pays for the other's traffic. (`LogRecord` carries typed `Field`s, not
`[]slog.Attr` / `...any` — see §6 for why reflection is banned.)

How each producer participates:

- **State changes** — the supervisor publishes `StateChanged` on every Start/Stop/attach/
  detach transition. This **deletes** the hand-rolled `refreshNetBIOSStatus`,
  `refreshSMBStatus`, `refreshMacIPStatus`, `promoteUnitToHook`-with-running juggling: the
  status view becomes a subscriber that folds events into a snapshot.
- **Stats** — components still *own* their counters (no hot-path observer plumbing through
  every layer), but instead of a poller pulling them, each component **publishes a
  `StatSample`** when meaningful — either on its own cadence (a 1 Hz self-tick for
  throughput-style counters) or on change (a new lease, a route added). Hot paths just
  bump local atomics; sampling/publish happens off the data path. This keeps the
  no-hot-path-allocation property while making transitions observable.
- **Logs** — services emit through a scoped `Logger` (§6); its records flow to the bus as one
  sink among several (file, syslog, …). The UI log viewer is just a bus subscriber. See §6
  for the producer side, the level/scope model, and the multi-sink fan-out.

Consumers are adapters subscribing to the topics they need on the telemetry bus: the HTTP/SSE
stream and ubus relay whatever their client asked for, the dashboard takes `state`+`stats`,
the log viewer takes `log`. The metrics/rate computation is "a `stats`-topic subscriber that
computes rates from `StatSample` deltas." The bus lives in core (stdlib channels only —
TinyGo-safe); the *transports* that expose it (SSE, ubus) are adapters.

Optional `Statful` capability is still useful for a one-shot pull (e.g. a CLI `stats` tool
that wants a snapshot without subscribing), so keep it — but the live path is the bus.

---

## 6. Logging — scoped loggers, levels, multiple abstracted sinks, zero reflection

Logging is an abstraction in core with three properties: it is **scoped per component**,
**levelled**, and **fans out to multiple swappable sinks** — and it does all of this
**without reflection** so it stays small on embedded targets.

### 6a. Scoped logger as the producer API

A component is handed a `Logger` scoped to itself at construction; AFP's logger is tagged
`afp`, EtherTalk's `ethertalk`, and so on. The scope is attached once, not repeated at every
call site, and it propagates to every record automatically.

```go
// core/log
type Level uint8
const ( Trace Level = iota; Debug; Info; Warn; Error ) // Trace = per-request protocol narration

// Field is a typed key/value — NO reflection, NO interface{} value boxing on the hot path.
type Field struct {
    Key string
    // exactly one of these is set, picked by the constructor (Str/Int/Bool/…)
    kind  fieldKind
    s     string
    i     int64
    b     bool
}
func Str(k, v string) Field  { ... }
func Int(k string, v int64) Field { ... }
func Bool(k string, v bool) Field { ... }

type Logger interface {
    With(fields ...Field) Logger          // returns a child logger with bound fields
    Log(lvl Level, msg string, fields ...Field) // level is PER CALL, not fixed at construction
    Enabled(lvl Level) bool               // cheap guard so disabled levels allocate nothing
}

// The level THRESHOLD lives at the sink boundary, not at logger construction — so it is
// runtime-settable (§6b) and per-sink (a debug ring + an info-only stderr off one logger).
func New(scope string, sinks ...Sink) Logger        // no level arg
type LevelVar struct{ /* atomic */ }                // a threshold a sink holds, retuned live
func NewStderrSink(min *LevelVar) Sink              // nil min ⇒ emit all
func NewRingSink(capacity int, min *LevelVar) Sink
```

`Logger.With(Str("volume","Media"))` gives AFP a per-volume child without re-tagging. Scope
is just the first bound field (`Str("scope","afp")`), set when the component's logger is
created — so filtering by service is a field match, uniform with everything else.

### 6b. Levels, and a cheap disabled-path

`Trace/Debug/Info/Warn/Error`. **`Trace`** is per-request protocol/service narration — e.g.
"AFP `FPOpenFork` path=…", "DDP datagram dest=…" — the human-readable *event*, never the raw
wire bytes (those go to a pcap capture, §6f). The **level is chosen per call** on `Log`; the
**threshold lives at the sink** as a `*LevelVar`, so a UI can set "AFP=debug, everything
else=info" *at runtime* (the control plane calls `LevelVar.Set`) without rebuilding any logger,
and one logger can feed several sinks at different thresholds. `Enabled(lvl)` folds across the
sinks (true iff some sink would emit at `lvl`) so a hot path skips building fields entirely when
no sink wants the level — important on embedded, where an unguarded format in a read loop is
pure waste.

### 6c. Multiple sinks behind one interface

A `Sink` consumes finished records; the logger fans each record to every registered sink.
Sinks are **adapters**, selected at build/runtime per target:

```go
type Sink interface {
    Write(rec Record)   // Record = scope + level + msg + typed fields + time
    Close() error
}
```

Sinks named in the charter/targets: a **bus sink** that publishes the `log` topic on the
telemetry bus (§5), which SSE/ubus relay to the UI log viewer; **file** (rotating); **syslog**
(desktop/OpenWRT); **udev/kmsg or stderr** (embedded); **stdout** (containers/`journald`); a
**ring buffer** (in-memory tail). A debugger sink (gdb/semihosting) fits the same interface on
bare-metal. Heavy sinks are build-tagged; a TinyGo/ESP32 build might link only a small ring +
a semihosting/UART sink.

The bus sink is **just one sink** — the logger does not depend on the bus. This keeps a CLI
tool or an embedded build able to log to a file/UART with **no bus, no SSE, no control plane**
linked at all.

### 6d. Why typed fields, not `slog`/`any`

`slog`-style `...any` attributes box every value into an `interface{}` and lean on reflection
to render them — which inflates TinyGo binaries and allocates on every log call. The typed
`Field` (one of a small set of scalar kinds) renders with a `switch`, allocates nothing for
scalars, and produces no reflection metadata. This is the logging instance of the global
no-reflection rule (§ cross-cutting constraints).

### 6e. Variadic vs. fixed-arity — the hot-path allocation rule

Typed fields remove the *boxing* allocation, but a **`Log(lvl, msg, fields ...Field)`** call
still allocates the **`...Field` slice** on the heap unless escape analysis proves it doesn't
escape — which it often can't across an interface method. On desktop that's a cheap, ignorable
allocation; on a microcontroller logging every frame cycle it is heap churn and fragmentation.

So the `Logger` interface provides **fixed-arity, non-variadic hot-path methods** that take no
slice and are provably zero-alloc when the level is enabled (and a no-op when not):

```go
Log0(lvl Level, msg string)
Log1(lvl Level, msg string, f Field)
Log2(lvl Level, msg string, f1, f2 Field)
```

Rule: the variadic `Log(...Field)` is for cold/setup paths; **data-path loops use the
fixed-arity form, always behind an `Enabled(lvl)` guard** so a disabled level builds no fields
at all. Keep the arity set small (0–2 covers the packet paths); add more only when a real call
site needs it. Verified by an `AllocsPerRun == 0` test on the hot-path methods (Verification).

### 6f. Wire visibility is pcap, not a log — and the traffic log is deleted

There are **two distinct concerns**, and only the first is "logging":

1. **Application/protocol logging** (this section) — `scope` (afp/ddp/smb…), `level`
   (trace…error), `time`, and a text message with typed fields. A protocol/service *event*
   worth narrating — "AFP `FPOpenFork` path=…", "ZIP zone reply", an auth failure — is just a
   `Trace`/`Debug` log line. It never carries raw frame bytes.
2. **Wire capture** — the actual decoded packet / command / request / response stream. This is
   **not** a logging problem and we will **not** reinvent a structured "protocol log" for it.
   The right primitive already exists — a **pcap file** — and the whole ecosystem
   (Wireshark/tshark) already decodes pcap better than we ever would. So wire visibility is
   **pcap-only**: capture raw frames + timestamps to a pcap file and decode offline. No
   second decode path, no structured-protocol-log sink, no embedded dissectors in core.

**Capture is always available, independent of the link backend.** It is the frame-altitude
`CaptureSink` decorator (§2): `Capture(inner, sink)` tees frames to a `CaptureSink`, and a
pcap-file writer is that sink. Two writer adapters implement one `CaptureSink` interface:

- `adapter/capture/libpcap` — when the pcap link adapter is in use, tee via libpcap's own
  dumper (native, nanosecond timestamps).
- `adapter/capture/pcapfile` — a **pure-Go, stdlib-only, TinyGo-safe** pcap-file writer for
  every non-pcap backend (TAP, raw Ethernet on ESP32, the TashTalk tty, the in-mem link). We
  frame the bytes and write the pcap record header ourselves, so an embedded or container
  build with no libpcap **still produces a Wireshark-openable capture.**

Core ships only the `CaptureSink` interface and the `Capture` decorator; both writers are
adapters (libpcap is a heavy dep behind its tag; the pure-Go writer is light enough to link
anywhere). The capture toggle and rolling/size policy are per-port config and control-plane
operations (start/stop capture, download the file).

**The old "traffic log" is removed, not redesigned.** It was redundant the moment capture is
always-on: the *bytes* live in the pcap, and *throughput/volume* is already `StatSample`
counters on the telemetry bus (§5, rates computed by the stats subscriber). There is no third
"traffic" mechanism — `pcap` for content, `StatSample` for rate, `Trace` log for narrated
events. (Migration: delete the existing traffic-log plumbing; see [02](02-PHASE-migration.md).)

## 7. Control plane — contract in core, transport in adapters

The management surface is **one transport-agnostic contract** in core — the `Plane` — and
every front-end is an adapter over it. The contract is deliberately shaped as the two things
*every* control transport provides natively, so no front-end is privileged:

1. **A set of request/response methods** — `Config` / `Reconfigure(name, section)` /
   `Save`, `Start`/`Stop`/`Restart(name)`, `Status`, `ListInterfaces`/`ListFSTypes`,
   `Diagnostics.*`. All are plain "call with typed args, get a typed result-or-error" — no
   streaming, no long-poll, no HTTP verbs baked in.
2. **A topic subscription** for live updates — `Subscribe(topic…)` onto the telemetry bus
   (§5): `state`, `stats`, `log`.

Front-ends are adapters; none imports another:

- `adapter/control/http` — REST + SSE + embedded SPA (desktop browser UI), build-tagged.
- `adapter/control/ubus` — OpenWRT: registers a `classicstack` object on **`ubus.sock`**.
- `adapter/control/cli` / in-process — tools call `Plane` methods directly, no socket at all.

### First-class on each platform — the ubus mapping is exact

The contract maps onto ubus with no impedance because ubus *is* "typed methods on an object +
an async notification channel," which is exactly the two-part shape above:

- **Methods → ubus methods.** Each `Plane` method becomes a ubus method on the
  `classicstack` object with a typed `blobmsg` policy; the adapter marshals args/results.
  `ubus call classicstack reconfigure '{"name":"AFP","section":{…}}'` invokes the same
  addressed `Reconfigure` (§11) the web UI calls — `luci`/`rpcd`, shell scripts, and other
  OpenWRT services drive it identically.
- **`Subscribe` → ubus notifications.** A bus topic is relayed as ubus events, so `ubus
  listen` / `ubus subscribe` receives `state`/`stats`/`log` updates — the same stream SSE
  serves the browser. SSE and ubus are two *encodings* of one event source, not two pipelines.
- **Procd/UCI fit (§4):** config is read/written through the UCI codec + store, and the ubus
  object is registered the way procd-managed daemons expose theirs, so ClassicStack behaves
  like a native OpenWRT service (init script, `service classicstack reload` → a `Reconfigure`,
  UCI as the config source of truth).

Equivalent first-class integration on the other targets is the same pattern with a different
adapter: a Windows-service / launchd / systemd wrapper drives the same `Plane` (and a local
named-pipe / unix-socket control adapter if a CLI needs to reach a running daemon). The core
contract never changes; only which control adapter(s) a build links does.

---

## 8. Optional components without `*_disabled.go` no-op files

**Problem today:** ten `*_disabled.go` files exist purely to satisfy `wireXxx` symbols when
a build tag is absent (`ipx_disabled.go` returns `ipxHookDisabled{}`). Messy, doubles the
surface, easy to drift from the real impl.

**Target:** a **component registry** populated by `init()` in build-tagged adapter
packages. Absent build tag → package not compiled → component simply not registered. No
stub needed.

```go
// compose/registry
type Factory func(*config.Model) (Component, error)
var registry = map[string]Factory{}
func Register(name string, f Factory)   // called from build-tagged init()
func Build(name string, m *Model) (Component, bool)
```

```go
// adapter/ipx/register.go    //go:build ipx || all
func init() { compose.Register("IPX", buildIPX) }
```

The supervisor asks the registry for each enabled component; unregistered = not in this
build, log once if config requested it, move on. **All ten `*_disabled.go` files are
deleted.** The "requested but not built" warning becomes one generic line in the
supervisor, not a per-component stub.

This also fixes the layering: the heavy IPX router/SAP code lives behind the `ipx` tag in
an *adapter*, while the pure IPX *protocol* codec stays in the always-compiled core for
tools to use.

---

## 9. Filesystem, metadata stores, and forks — one FS interface, everything else an adapter

This is the largest core/adapter seam and the one with the most existing-but-partial
structure. The principle: **AFP and SMB talk only to a filesystem interface; where forks
live, where metadata lives, and what backs the store are all adapter choices, invisible to
the protocol services.**

### What exists today (reuse, don't reinvent)

- `pkg/vfs` already has `FileSystem`, `File`, `Capabilities`, a `Factory` registry, and even
  a nascent `vfs.Event`/`vfs.Subscriber`/`DefaultBus`. Good seam.
- `service/afp/fs.go` carries a **duplicate** `FileSystem` + `RegisterFS` registry that
  shadows `pkg/vfs`. This is drift to eliminate — AFP (and SMB) should consume `pkg/vfs`.
- `pkg/cnid` has `Store` with `MemoryStore` + `SQLiteStore`; AFP wraps it as a per-volume
  `CNIDBackend.Open(volume)`. Good shape, just AFP-aliased.
- Forks/metadata (`AppleDoubleBackend`, `ForkMetadataBackend`, `CommentBackend`) are AFP
  interfaces that take an `fs FileSystem` *beside* it. The inversion below makes the fork
  engine register *into* the FS instead.

### 9a. Three metadata stores behind one tiny interface

CNID, shortname, and desktop databases are all the same shape: a keyed map that must
survive restart. sqlite is one *adapter*, not the contract. Define a single store
interface (CNID's `Store` is the template) and provide swappable backends:

```go
// core/metastore — one contract, three named stores (cnid / shortname / desktop)
type Store interface {
    Get(key []byte) (val []byte, ok bool)
    Put(key, val []byte) error
    Delete(key []byte) error
    Range(prefix []byte, fn func(k, v []byte) bool) error
    Sync() error
    Close() error
}
```

Adapters (build-tagged, chosen by config/platform):
- `mem` — in-memory map snapshotted to a file on `Sync`/`Close`, reloaded on open. The
  default; TinyGo/embedded-safe; **lets us drop sqlite entirely on small builds.**
- `sqlite` — today's `modernc.org/sqlite`, behind a build tag for full builds only.
- `ntfs-ads` — store the blob in an NTFS Alternate Data Stream on the volume root.
- `xattr` — store the blob in a Unix extended attribute.

CNID/shortname/desktop each ask `metastore.Open(name, params)`; the per-volume
`CNIDBackend.Open` pattern generalises to all three. This kills the AFP-local CNID aliases
and the `desktopdb.go` sqlite coupling.

### 9b. Fork engine registers *into* the filesystem (the inversion)

Rather than the protocol service picking a fork backend and threading it everywhere, invert
it: a **ForkEngine** is composed onto the FileSystem (per share — see below), and **fork
operations become FS methods**. The protocol service calls `fs.OpenFork(path, RESOURCE, flag)`
and never knows whether the bytes came from a `._` sidecar, an NTFS ADS, a Unix xattr, or an
HFS native fork.

```go
// core/fs — fork-aware extension composed onto the base FileSystem
type ForkType int
const ( DataFork ForkType = iota; ResourceFork )

type ForkEngine interface {
    OpenFork(path string, fork ForkType, flag int) (File, error)
    ForkLen(path string, fork ForkType) (int64, error)
    ReadFinderInfo(path string) ([32]byte, bool, error)
    WriteFinderInfo(path string, fi [32]byte) error
    ReadComment(path string) ([]byte, bool)
    WriteComment(path string, c []byte) error
    MoveMetadata(old, new string) error
    DeleteMetadata(path string) error
}

type ForkFS interface {
    FileSystem
    ForkEngine
}
```

**`ForkFS.Rename`/`Remove` carry the metadata container.** The low-level
`MoveMetadata`/`DeleteMetadata` stay on `ForkEngine` (the engines and their tests use
them directly), but the assembled `ForkFS` folds them into its `Rename`/`Remove` so a
single FS call moves/deletes the data fork *and* its sidecar/ADS/xattr together
(`Remove` deletes metadata first, then the data). Callers above the FS — AFP, SMB —
therefore never pair the two by hand; a protocol that needs an extra step on top (AFP's
CNID rebind) layers it after the one FS call. This removes the identical
`fsys.Rename`+`MoveMetadata` / `DeleteMetadata`+`fsys.Remove` pairing both file services
used to duplicate.

**The fork engine is a per-share config choice, not a backend-internal decision.** A share
already declares its `fs_type` (§9c); it declares its **`fork_backend`** the same way, and
the share-build wiring composes the chosen `ForkEngine` onto the `FileSystem` to produce a
`ForkFS`. The engine is supplied to the FS at construction (e.g. `vfs.New(params)` where
`params` carries the resolved fork backend), **not** chosen inside the backend at runtime.
Why per-share and explicit:

- **It is an operator-visible, inspectable choice** — the same status as `fs_type`. The UI
  offers it per share/volume; the config records it; two servers over the same tree can be
  made to agree by configuration rather than by hoping their auto-detection matches.
- **On-disk layout is stable and portable.** A volume that says `fork_backend = "appledouble"`
  keeps sidecars whether it lives on NTFS, ext4, or FAT — moving the tree doesn't silently
  switch where forks live.
- **`auto` is offered but is *not* the default.** `auto` resolves to the platform-natural
  engine at build time (NTFS→ads, Unix→xattr, HFS image→native, FAT/unknown→appledouble),
  which is convenient for a single-host setup but is exactly the cross-system footgun to warn
  about: the resolved layout depends on *where the server runs*, so a tree shared between
  hosts, or later moved, can change fork storage under it. The UI flags `auto` with this
  caveat; an explicit backend is recommended for any tree that might be shared or relocated.
- Some `fs_type`s constrain the choice: an `hfs-image` backend implies `native` forks; a
  read-only `zipfs` may only support `appledouble`. The share-build validates the
  `fs_type` × `fork_backend` pair and rejects incompatible combinations at config time.

`AppleDoubleBackend` becomes **one ForkEngine adapter** (`fork/appledouble`), joined by
`fork/ads`, `fork/xattr`, `fork/native` (HFS). **AFP holds no AppleDouble knowledge** — the
resource-fork *parsing* needed for Desktop DB icon ingest stays in the AFP protocol layer,
but the fork *storage* backend is entirely behind the `ForkEngine` interface AFP calls.

This is also what lets **SMB** gain Mac-fork support for free: same `ForkFS`, same per-share
`fork_backend`, same engine.

**Wire-format compatibility (interop is a hard requirement, not a free choice):** the four
engines differ only in *container*; the Finder-info / resource-fork *encoding* is shared and
already exists in `pkg/appledouble`. Two of them must match existing servers byte-for-byte:

- **`fork/ads` (NTFS) must reuse the legacy Windows NT "Services for Macintosh" (SFM)
  stream names and encoding.** SFM stored Mac forks/metadata in NTFS named streams with
  fixed names — `AFP_AfpInfo` (a fixed-layout `AfpInfo` struct carrying the magic, version,
  backup time, and 32-byte Finder info) and `AFP_Resource` (raw resource-fork bytes) — plus
  the SFM comment stream. The adapter reads/writes *those exact stream names and the AfpInfo
  binary layout* so SFM-authored volumes remain readable and our volumes stay
  SFM-compatible. (New constants — none exist in the tree yet; document the AfpInfo layout
  per CLAUDE.md spec rules.)
- **`fork/xattr` (Unix) must follow Netatalk's EA backend.** Use Netatalk's attribute names
  — `org.netatalk.Metadata` (the AppleDouble-v2 header blob: Finder info, comment, and fork
  bookkeeping, which `pkg/appledouble` already produces) and `org.netatalk.ResourceFork`
  (resource-fork bytes) — and Netatalk's blob layout, so volumes interoperate with a
  Netatalk install. This extends the project's existing "Netatalk-compatible" stance (the
  AppleDouble sidecar modes already follow it).

`fork/appledouble` (sidecar, FAT/embedded) and `fork/native` (HFS image) round out the set.
All four serialise Finder info / comments through the **same** `pkg/appledouble` codec, so
the Mac-visible metadata is identical regardless of where it physically lands.

### 9c. Filesystem backends — the FS interface is the only thing services see

**One** FS interface and registry, consumed by both AFP and SMB. Backends are pure adapters
registered by name and selected **per share/volume in config** via `fs_type`:

- `local_fs` — host-path backend (the common case).
- `macgarden` — a synthetic/curated backend.
- **`hfs-image`** — a raw HFS/HFS+ disk image as a backend (implies `native` forks). Makes
  emulator disk images directly serveable over AFP.
- **`fat-image`** — FAT16/32 image backend; pairs with SMB for emulator images.
- **`ftp`** — an FTP server exposed as a filesystem, so an FTP target is reachable via AFP/SMB.
- **`zipfs`** — an entire read-only FS inside a zip (read-only distributions; tiny).
- **`s3`** — S3 object storage wrapped as a filesystem.
- **`webdav`** — cloud/WebDAV storage as a filesystem.

Each is build-tagged so a minimal build links only `local_fs` + `mem` metastore (no sqlite,
no S3 SDK, no zip). `s3`/`webdav`/`ftp` are heavy deps and **must** stay behind tags and the
import-graph gate so they never reach the core or a TinyGo build.

### 9d. The per-share storage contract (fs_type + fork_backend + name engine + metastore)

A share/volume is fully described by an explicit, inspectable set of config fields, and the
share-build wiring assembles the `ForkFS` from them. None of these are decided inside a
backend at runtime; all are config the operator (and the UI) can see and pin:

- **`fs_type`** — which `FileSystem` backend (§9c).
- **`fork_backend`** — which `ForkEngine` (§9b): `appledouble` / `ads` / `xattr` / `native`,
  or `auto` (resolves to platform-natural; carries the portability caveat).
- **`filename_codec`** — charset + reserved-char translation (§10a-bis): e.g. `macroman-utf8`
  (host fs), `macroman-native` (HFS image), `utf8` (SMB-native), defaulting per `fs_type`.
- **name engine** — short/medium derivation (§10a), defaulting per `fs_type`, pinnable.
- **metastore** — CNID/shortname/desktop backing (§9a), defaulting to `mem`-snapshot.
- **backend params** — the connection/location config a given `fs_type` needs. The near-
  universal one is a typed **`Path`** on `ShareSpec` (host root / image file / archive);
  everything else rides an **`Extra map[string]any`** carrier (never reflection-marshalled in
  core): `ftp`/`webdav` need `url` + `username` + `password`; `hfs-image` needs `Path` +
  `partition`; `s3` needs `bucket`/`region`/credentials; `local_fs` needs only `Path`;
  `memfs`/`macgarden` need nothing. Each factory **declares a param schema** at registration —
  `Param{Key, Required, Secret, Doc}` via `RegisterFSWithParams`, readable back via
  `ParamsFor(fsType)` — so `BuildShare` validates required params are present (and rejects
  unknown keys) *before* constructing the backend, and the UI generates a per-share form
  (Path field + the backend's extras) from the schema. `Secret` params (passwords) are
  redacted in logs/diagnostics and masked in the UI. The protocol services and `core/share`
  never see these keys — backend config stays behind `core/fs`.

The share-build **validates the combination** (e.g. `hfs-image` ⇒ `native` forks; read-only
`zipfs` ⇒ `appledouble` only) and **validates each backend's required params** (e.g. an `ftp`
share missing `url`), rejecting incompatible or under-specified shares at config time, so a
bad combo fails loudly on Apply rather than silently misbehaving at runtime. Because the whole
contract is per-share config, a volume's on-disk behaviour is portable and reproducible
across hosts (charter: adaptable + compatibility).

### Lift-out work this implies

The work is: (1) one FS interface + registry consumed by AFP and SMB (no per-service
duplicate); (2) `metastore` as a standalone seam so CNID/shortname/desktop
share it; (3) invert forks into the FS via `ForkEngine`/`ForkFS` and move AppleDouble to an
adapter; (4) add the new backends as independent build-tagged adapters. Net: AFP and SMB
shrink (they lose storage-layout code), and embedded/cloud targets become composition.

### 9e. Authentication and share access gating (the user store)

The same "one interface, swappable adapters, only one wired" shape the metastore (§9a) and FS
backends (§9c) use applies to **who may use the server**. The charter stance ("compatibility over
correctness": modern at rest, faithful to the weak dialect on the wire) decides the whole design —
**credentials are salted-hashed at rest even though the legacy auth handshake compares them against
a cleartext or weakly-hashed value off the wire**.

- **`core/auth` — the contract (always compiled, reflection-free).** `Authenticator{Authenticate
  (user, pass) (ok, err)}` is the minimal seam the file services consult; `UserStore` extends it
  with the management surface the web UI drives (`Users`/`SetUser`/`SetDisabled`/`RemoveUser`). The
  PBKDF2-HMAC-SHA256 credential codec lives here too (`DeriveCredential`/`Verify`/`SaltHex`/
  `ParseCredential`) — over `crypto/hmac`+`sha256`+`subtle` only, with **hand-rolled hex**, because
  both `encoding/hex` and `crypto/rand` transitively import `reflect` (banned in core, §1). The
  contract therefore takes the salt as a *parameter*; it never generates randomness.
- **`adapter/auth/local` — the built-in store (always available, adapter ring).** An smbpasswd-style
  line file (`name:saltHex:hashHex:flags`, separate from `server.toml` so secrets never ride config
  backups), loaded into memory, rewritten atomically on mutation. It uses `crypto/rand` (salt) and
  `os` — both fine in the adapter ring, neither allowed in core. It is the default the way
  `local_fs`/`mem` are defaults: pure stdlib, no build tag. A future PAM / Windows-SSPI / sqlite
  store is an additional `adapter/auth/*` behind its own tag (those would carry the hash-format
  differences between PAM crypt, NTLM, and our PBKDF2).
- **The gate is at LOGIN, not per share.** Legacy AFP/SMB clients log in **once** with a single
  identity, then enumerate and bind shares under it — they do not re-authenticate per share. So AFP
  `FPLogin` / SMB `SESSION_SETUP_ANDX` validate the credential (or admit guest), and the resolved
  identity then **filters which shares are enumerable** (AFP `FPGetSrvrParms`, SMB `NetShareEnum`/
  `NetServerEnum2`) and **gates binding** (AFP `FPOpenVol`, SMB `TREE_CONNECT`). A restricted share
  the identity may not use is reported as non-existent (`kFPObjectNotFound` / `STATUS_BAD_NETWORK_
  NAME`), not access-denied, so naming it directly leaks nothing. With no store wired, every login is
  guest and every share world-readable — exactly the pre-auth behaviour.
- **Access policy is share-level, not file ACLs.** `share.Permissions{AllowedUsers}` (empty = guest/
  world) is the whole policy: a coarse "who may see/bind this share." `ReadOnly` stays share-wide,
  not per-user. This is a compatibility server for vintage Macs/DOS, not an enterprise file server —
  deliberately not in the per-file-ACL space. The allow-list is **not** a backend `Extra` param (it
  is protocol-layer policy, not storage config), so it rides on `ShareSpec.AllowedUsers` →
  `share.Permissions`, visible/editable in the UI, never behind `core/fs`.
- **Management surface.** Users live in the store's file, **not** the config model, so the control
  plane exposes user CRUD as its own surface (`control.Plane.Users/SetUser/SetUserDisabled/
  RemoveUser`, backed by the optional `control.UserAdmin` the supervisor satisfies from the wired
  store; absent → `ErrUnavailable`). Share allow-lists, by contrast, ARE config and ride the existing
  `Config()`/`Reconfigure` path. The hashed-credential-can't-be-reversed compromise (a client sending
  an LM/NTLM response we accept as guest rather than refuse) is documented in `spec/errata.md`.

---

## 10. Naming, filename codecs, and the filesystem event bus

Three concerns live here: translating filename **charset/encoding** between client and store
(§10a-bis), deriving **alternate names** (DOS 8.3 "short", Mac 31-char "medium", §10a), and
**propagating filesystem mutations** to everything that must react (§10c–e). All are FS-layer
responsibilities exposed through the FS seam; protocol services stay unaware of how names are
encoded/derived or how changes are detected.

### 10a. Naming is an FS responsibility, exposed through the FS interface

Short and medium names are **derived by the filesystem**, because only the FS knows the
on-disk truth and the per-directory collision state needed to keep names unique and stable.
They are `NameEngine`s **composed onto the FS at share-build time** — the same per-share,
config-driven model as the fork backend (§9b): the chosen engine is supplied to the FS via
its construction params, not selected inside the backend at runtime. Surfaced as methods on
the `FileSystem` interface:

```go
// core/fs — a name engine composed onto the FS
type NameEngine interface {
    Bind(dir, long string) string       // allocate-or-return; applies collision suffixes
    ToLong(dir, short string) (string, bool)
}
// FileSystem methods:  ShortName(path) (string, error)
//                      MediumName(path) (string, error)
```

Defaults are sensible per `fs_type` (a FAT-image share needs real 8.3; an HFS-image share has
its own name limits) but, like the fork backend, an operator can pin the engine explicitly
when a tree must behave identically across hosts. `auto` carries the same portability caveat.

### 10a-bis. Filename codec — charset/encoding translation (distinct from naming)

Naming (above) decides *which* name and handles collisions. A separate concern is the
**charset/encoding** of a name as it crosses between the client and the backing store — and
the current code bakes it into the AFP service (`afpPathElementToHost` / `hostNameToAFPBytes`,
hard-coding `runtime.GOOS == "windows"` for reserved chars). That is a leak: the right
charset and reserved-character policy depend on the **FS backend**, not on the host OS, and an
FS implementor must be able to swap it. So filename translation is its own per-share-swappable
interface, composed onto the FS like the fork and name engines:

```go
// core/fs — translate a single path element between client wire form and STORE-NATIVE form.
// StoredName is the backend's on-disk byte sequence (NOT a universal Go string) — see the
// inversion-trap note below.
type StoredName []byte

// WireEncoding is the client wire charset for ONE request — a PER-CALL argument, because one
// share serves several client versions at once and the charset is negotiated per request
// (AFP pathType: ShortName/LongName=MacRoman, UTF8Name=UTF8; SMB: legacy=ANSI, NT=UTF16LE).
// Typed enum (not a string) to stay reflection-free (§1/§6); extend for new charsets.
type WireEncoding uint8
const (
    WireMacRoman WireEncoding = iota // AFP kFPShortName / kFPLongName
    WireUTF8                         // AFP kFPUTF8Name; SMB POSIX extension
    WireANSI                         // SMB legacy / DOS code page (OEM)
    WireUTF16                        // SMB NT Unicode (UTF-16LE)
)

type FilenameCodec interface {
    // Decode: client wire bytes in `src` charset → store-native bytes, validated for this
    //   backend's profile. e.g. MacRoman → UTF-8 NFC (POSIX host); MacRoman→MacRoman (HFS
    //   image, no transcode); escape store-reserved chars into reversible ASCII tokens.
    //   ErrUnrepresentable when the name cannot be legally stored (→ protocol "illegal name",
    //   never a mangled path); ErrWireUnsupported when `src` is not in Wire().
    Decode(wire []byte, src WireEncoding) (StoredName, error)
    // Encode: store-native bytes → client wire bytes in `dst` charset (inverse of Decode).
    Encode(stored StoredName, dst WireEncoding) (wire []byte, err error)
    Wire() []WireEncoding                    // wire charsets this codec can transcode
    Profile() FilenameProfile
}
type FilenameProfile struct {
    Wire []WireEncoding                      // client wire charsets accepted (per-call src/dst)
    StoreCharset string                      // backend side: "utf8" | "posix-bytes" | "fat" | "macroman"
    MaxElement int                          // max element length in STORE bytes (0 = unbounded)
    Validate func(elem StoredName) error    // backend's legality check (POSIX: NUL+'/'; S3: url-safe; …)
}
```

**The inversion trap (why `Decode` returns bytes, not `string`).** A `string` return would
bake in two false assumptions: that every decoded name is representable in the backend, and
that a `[]byte→string→[]byte` round-trip through host I/O is lossless. Neither holds — a POSIX
`local_fs` takes arbitrary non-NUL bytes and does **not** enforce clean UTF-8; an S3/WebDAV
backend demands strict URL/XML-safe characters; a FAT image has its own charset. So `Decode`
yields the backend's **native byte form** (`StoredName`) that the `FileSystem` passes straight
to host I/O, the codec **validates** it against the store profile, and unrepresentable names
fail loudly with `ErrUnrepresentable` rather than corrupting a path. Escape tokens must be
legal in `StoreCharset` (the `0xNN` scheme is ASCII so it survives UTF-8/FAT/host-path routines
unchanged). The `FileSystem` therefore operates on store-native names end-to-end; the codec is
the single wire↔store boundary.

The translation has three axes — two are backend-dependent (fixed per codec instance), the
third is **client-version-dependent and varies per request**:

1. **Wire charset (per request, not per share).** A single share serves multiple client
   protocol versions at once, and each request names its own wire charset, so the source
   encoding is a **per-call argument** (`Decode(wire, src)` / `Encode(stored, dst)`), not a
   property baked into the codec. **AFP** selects it with the path-type byte —
   `kFPShortName`/`kFPLongName` carry **MacRoman**, `kFPUTF8Name` carries **UTF-8** — so the
   same volume answers a System 7 client and an OS X client correctly. **SMB** selects it by
   the negotiated dialect / per-request Unicode flag — legacy/DOS clients send an **ANSI/OEM
   code page**, NT clients send **UTF-16LE**. The codec advertises the set it accepts via
   `Wire()`; a request whose negotiated charset is outside that set fails with
   `ErrWireUnsupported` rather than being mangled.
2. **Store charset + reserved-character policy (per codec).** The store's charset and illegal
   set differ by backend: NTFS/Windows host forbids `< > : " / \ | ? *` and control chars;
   POSIX only `/` and NUL; FAT adds its own; an HFS/zip/S3 backend has yet another set; an
   `hfs-image` backend wants **MacRoman bytes natively** (no transcode). The codec escapes
   reserved chars reversibly (the `0xNN` token scheme already in `path_codec.go`, generalised)
   so the round-trip is lossless and the Mac sees its original name back. Built on the existing
   `pkg/encoding` MacRoman↔UTF-8 tables — *reused*, wrapped behind this interface, not
   reinvented; a future client charset (e.g. Shift-JIS for KanjiTalk) is one more
   `WireEncoding` constant the codec learns to transcode.

Crucially, one codec instance owns the store-side policy (charset, reserved set, `Validate`)
**once** and transcodes from/to whichever wire charset the service negotiated for that request.
The earlier rejected alternatives — a codec instance per client charset, or a `runtime.GOOS`
switch — would either duplicate the store policy across instances or fail to express a single
host serving several charsets at once.

Why per-share and codec-owned, not service-owned or `runtime.GOOS`-driven:

- The same physical host can serve an `hfs-image` volume (MacRoman, HFS reserved set) **and** a
  `local_fs` volume (UTF-8, host reserved set) simultaneously — one global GOOS switch can't
  express that. The backend declares its codec.
- It composes with the others in the per-share contract (§9d): `fs_type` picks a default codec,
  `filename_codec` can pin/override it, and the share-build validates the pairing (an
  `hfs-image` with a UTF-8 codec is rejected).
- **AFP and SMB both** call `fs`-level name methods that run the codec; neither hard-codes
  encoding. The AFP `path_codec.go` logic moves out of the service and becomes the default
  `FilenameCodec` adapter.

Ordering note: codec (charset/reserved) runs at the FS boundary on every element; the name
engines (short/medium) operate on the *decoded/stored* names, since collision suffixing and
8.3 truncation are defined over the stored charset. CNID caches store-form names (§10b).

1. **Short names (DOS 8.3)** for legacy SMB/DOS clients. On **Windows**, an adapter uses the
   native `GetShortPathName`. On other OSes, use a native API if one exists, else our own
   deterministic 8.3 derivation (FAT-legal sanitise + `~N` collision suffix). One engine
   interface, per-platform adapters.
2. **Medium names (Mac 31-char)** for AFP clients at the classic name-length limit. A
   deterministic truncation of the MacRoman/UTF-8 long name to 31 chars with collision
   suffixes, following Netatalk's scheme, with a **persisted binding** so a given long name
   always maps to the same medium name across reconnects.

Both are engines under the FS seam, not bound to any one backend — any FS composes them, so
AFP and SMB share the same naming behaviour. Services only ever call `fs.ShortName` /
`fs.MediumName`; they never touch an engine.

### 10b. CNID carries names; it does not source them (ownership fix)

The overlap the user flagged: CNID already tracks per-file identity. Rule — **the FS sources
short/medium names; CNID may *store* them as part of a file's registration record, but is not
responsible for deriving them.** So a CNID record can include the short and medium name as
*denormalised cached columns* (cheap reverse lookup for enumeration), populated by the FS at
registration time. CNID never computes a name; if the cache is missing it asks the FS. This
keeps the single source of truth in the FS and removes any temptation to grow naming logic
inside the metastore.

### 10c. Buses are scoped by domain; subscribers take only the topics they want

Two independent design choices, settled here:

**1. Separate buses per domain (the strong boundary).** The FS-mutation bus and the
control/telemetry bus are *distinct packages*, not one merged firehose:

- **FS-domain bus** (`core/fs`): file mutations flowing *inward* between FS backends,
  fork/name engines, metastores, and the file-serving services. Carries `Op`
  (Create/Rename/Modify/Delete/AttrChange), host path (+ old path on rename), `Time`, and an
  `Origin` tag.
- **Control/telemetry bus** (`core/bus`, §5): `StateChanged`/`StatSample`/`LogRecord` flowing
  *outward* to UIs/ubus/SSE.

Why separate, not one bus with a type filter:
- **Layering + binary size.** A build (or a client tool) that only needs FS events never
  imports the telemetry package, and vice-versa — the unused bus and its event types are not
  linked. A single shared bus would force every consumer to link every event type.
- **Boundary by construction.** Host paths cannot leak into the control plane and UI concerns
  cannot reach the storage substrate, because the types simply aren't on the other bus — it's
  enforced by the import graph, not by a runtime filter someone might forget.

A file event may *cause* a telemetry event (a service translating an FS change into a
`StateChanged`), but that is an explicit republish across the boundary, not a shared channel.

**2. Within a bus, subscribe by topic/channel — don't hand everyone the firehose.** Even
inside one domain, subscribers differ: a file server cares about FS mutations under *its*
volume root, a CNID store cares about renames/deletes, a UI log viewer wants logs but not
stat samples. So a bus is **topic-scoped**: `Subscribe(topics…)` returns a channel carrying
only matching events, and a non-matching event is never enqueued onto that subscriber's
channel at all (no per-event allocation or wake-up for events it would just discard — this is
the §1 allocation discipline applied to fan-out). `Origin`-filtering (skip your own events)
still applies on top, for loop avoidance.

Implementation note: "multiple buses" and "multiple channels within a bus" are the same idea
at two granularities — **domain → separate bus; sub-interest within a domain → separate topic
channel.** We use both: the domain split for the hard layering/size boundary above, topic
channels for selectivity inside a domain. Both are stdlib channels only (TinyGo-safe); a bus
is just a small typed registry of topic → subscriber channels.

### 10d. Multiple services on one FS coordinate through the FS bus

When an SMB share and an AFP volume back onto the **same** FS, a mutation by one must reach
the other so each can notify its own clients (AFP `FPservermessage`/dir-change, SMB
change-notify). Each service subscribes to the FS bus, filters by `Origin` to skip the events
it published itself (avoiding feedback loops), and translates the rest into its protocol's
client-notification. CNID/name/fork updates ride this same publish, so "hook everything
internally" is one `Publish` per mutation, many reactors.

**Landed (M8a, 2026-06-15) — the coordination seam; wire push deferred:**

- **Shared bus per host path.** The compose registry holds an `fsBusBroker` (`compose/registry/fsbus.go`) that hands out ONE `fs.Bus` per distinct host path (normalised case-folded, trailing-slash-trimmed). Both file-service factories resolve their shares' buses through it, so a same-path AFP volume and SMB share get the SAME bus; unrelated paths get independent buses. The bus is threaded through `share.Build` via new `afp.NewVolumeWithBus` / `smb.NewShareWithBus` (the bus-less `NewVolume`/`NewShare` remain for tests/zero-config). Each service installs the resolver with `SetBusResolver(fsBus.busFor)` and builds its initial set through the reconcile path so the shared bus applies from boot.
- **Origin stamping.** A service tags its own mutations via `fs.OriginBus(b, origin)` — a `bus.Bus` wrapper that stamps `Origin` ("afp"/"smb", consts `afp.OriginAFP`/`smb.OriginSMB`) onto every `fs.Event` lacking one, forwarding to the same underlying shared bus. So two services wrapping one bus each see the other's stamped events on one fan-out.
- **FS publishes.** `local_fs` publishes `fs.Event` on `CreateDir`/`CreateFile` (OpCreate), write-then-`Close` (OpModify, coalesced to close — not per-WriteAt), `Rename` (OpRename + OldPath), `Remove` (OpDelete), carrying the absolute host path. A read-only open that never writes stays silent. `memfs` (single-process, no shared store) does not publish.
- **Reactor (the consuming half).** `share.Reactor` (`core/share/reactor.go`) subscribes to each distinct bus among a service's shares, drops its own `Origin` (`fs.SkipOrigin`), resolves which share(s) the event's host path falls under (prefix match; rename matches either end), and delivers `(share, event)` to a notify sink. Each service builds one in `New`, subscribes in `Start` (one goroutine per distinct bus), stops in `Stop`; `ReactorDelivered()` is the observable.
- **Wire push (landed M8a, 2026-06-15 — SMB only; AFP excluded by protocol):**
  - **SMB CHANGE_NOTIFY.** The session seam gained a server-initiated push channel: `Conn.SetPushWriter(func([]byte))` (on both the `smb` and `netbios` `SessionCircuit` interfaces). Each transport — NBF (`sendSessionData`), NB-IPX (new `pushData` over retained circuit addressing), direct-IPX (new `pushResponse`) — installs a push closure after `NewConn`, so a transport that retains per-circuit addressing can deliver an unsolicited frame. SMB now handles `NT_TRANSACT (0xA0)` `NOTIFY_CHANGE (Function 0x0004)`: it parses the Setup, registers a held `pendingNotify` on the session (tid/uid/mid/pid/flags2 + bound share), and returns **nil** (the request is held open, not answered). The SMB reactor sink (`notifyFSChange`, wired into `share.Reactor` in place of the old no-op) completes every held watch bound to the changed share by framing one `FILE_NOTIFY_INFORMATION` record (FILE_ACTION_* from the `fs.Op`, the changed leaf in UTF-16LE) and pushing it over the circuit. One-shot per [MS-CIFS] (a fired watch is consumed; the client re-arms). Watch granularity is share-coarse (any change under the tree completes it; the client re-reads) — a faithful, safe superset for a compatibility server. A NOTIFY_CHANGE on IPC$ / an unbound tree is refused (not held), so a client never waits forever. The SMB service now tracks live sessions (`NewConn`/`Close` register/unregister) so the reactor can fan a completion to every watching circuit. A transport with no push channel simply never completes a held watch (benign timeout).
  - **AFP is excluded by protocol.** Classic AFP has **no per-directory change-notify push** — a client discovers changes by polling the volume modification date / re-enumerating, and the only server→workstation ASP attention codes are shutdown/crash/message (none mean "catalog changed"). So the AFP reactor sink stays nil: it tracks `ReactorDelivered()` as the coordination observable but emits no wire frame. Fabricating an attention semantics would violate the compatibility charter.
  - **Still deferred: §10e** host-watcher (fsnotify) — the inbound edge that publishes external mutations onto the same bus; the SMB push then fires for those too, for free.

### 10e. Host filesystem watchers — the inbound edge

Out-of-band changes (something edits a file outside ClassicStack) must also reach the FS bus.
Add a **host-watcher adapter** (e.g. `fsnotify`, build-tagged, OS-specific) that watches a
volume's host path and publishes `vfs.Event`s with `Origin:"fsnotify"`. Then the existing
reactors fire for free:

1. fork/name engines rename/relocate sidecars or refresh bindings as needed,
2. CNID updates its path↔id mapping,
3. SMB/AFP issue change-notify to their connected clients.

The watcher is strictly an **adapter** (heavy dep, not in core, absent on platforms without
it — embedded FS-image backends simply have no external mutator and need none). It is the
inbound mirror of the services-as-publishers path: `fsnotify` in, protocol change-notify out,
the FS bus in the middle. This is what lets a server "be informed when its state changed
outside the application scope," per the user's requirement.

---

## 11. Dynamic per-component reconfiguration

A core goal of the dynamic design: an operator changes **one** component's config in a UI
and that component — plus only the components that depend on it — restarts with the new
config, while everything else keeps running. No whole-stack rebuild, no dropped sessions on
unrelated services.

This is the payoff of three earlier pieces composed together: the `Component` DAG (§3), the
typed config sections + registry (§4), and the staged-config `Plane` (§7).

### 11a. Reconfigure a named component (no diff)

The operation is **addressed**, not computed: the UI is reconfiguring a *specific* component,
so that component's name is the input. There is no model-diffing or blast-radius derivation —
we already know who changed.

```
Reconfigure(name, newSection):
  1. update model.Section[name] = newSection         # shared model, often by ref already
  2. ask the component to apply it:
        - implements Configurable && ApplyConfig(newSection) == nil  → done, live, no restart
        - otherwise (no Configurable, or returns ErrNeedsRestart)     → restart it
  3. on restart: stop the component, rebuild it from its section, start it —
     then notify its dependents that an upstream restarted; each dependent
     decides whether *it* must restart too (it is asked the same way).
```

So the flow is just **update the section → tell the component to apply → restart-and-notify if
needed.** The component being changed is the subject; dependents are reached by *notification
following the DAG edges*, not by precomputing a set. A dependent that can ride an upstream
restart live says so; one that can't restarts in turn and notifies *its* dependents. The
cascade falls out of the dependency edges naturally and stops where components can absorb it.

`Plane.Stage`/`Apply` still exist for the "edit several things then commit" case, but the unit
of action is per-component reconfigure, and even a multi-field apply is just that operation run
for each component the operator touched — never a whole-model diff.

### 11b. Two grades of change: hot-apply vs. restart

Not every change needs a restart. A component declares which it can absorb live:

```go
// optional capability (from §3)
type Configurable interface {
    // ApplyConfig hot-applies a new section without restart when possible.
    // Returns ErrNeedsRestart when the change cannot be applied live, so the
    // supervisor falls back to the stop/start path for this component + dependents.
    ApplyConfig(Section) error
}
```

- **Hot-apply** — cheap, non-structural knobs (log level, a NAT nameserver, a toggle of
  traffic logging) call `ApplyConfig` in place; no dependents disturbed.
- **Restart** — structural changes (bind address, the pcap device a port opens, a volume's
  fs-type/backend) return `ErrNeedsRestart`; the supervisor runs the §11a restart-and-notify
  for that component, which cascades to dependents only as far as each cannot absorb it live.

A component that doesn't implement `Configurable` is always treated as restart-on-change.
This keeps the simple case simple and lets each component opt into liveness where it's safe.

### 11c. What this requires from the design

- **Per-section config ownership (§4)** is the precondition: a component must be
  reconstructable from *its* section alone, with no hidden cross-section coupling resolved at
  startup. Bridge/interface inheritance is a pure `Model` helper (§4) so it re-resolves on
  apply rather than being baked in once at build time.
- **The DAG (§3)** carries the upstream→dependent edges the restart notification follows; no
  separate "blast radius" structure is computed — a restart just walks its own out-edges and
  asks each dependent the same question.
- **`StateChanged` events (§5)** narrate the restart to every UI live — a component going down
  and back up is visible without polling.
- Reconfigure is **addressed and local, not a whole-stack rebuild**: only the named component
  (and dependents that genuinely cannot ride it) ever stop, so nothing needs special "survive
  the rebuild" handling — including the UI serving the request.

### 11d. Transport bindings (the NetBIOS/SMB case) generalise cleanly

Some couplings are softer than a hard dependency: a transport (IPX/NetBEUI) is an *attachable
binding* into NetBIOS, not a parent it must restart. The component model expresses this as a
component implementing an optional `Attachable` capability rather than a DAG edge, so
reconfiguring or restarting IPX detaches+reattaches just that binding and NetBIOS/SMB keep
serving their other transports. An attach point is a re-runnable side effect of a component's
start/stop, not a dependent that gets the restart notification — so it never cascades.

---

## 12. Client tooling — protocols/services usable standalone

**Problem today:** services are reachable only through the full supervisor wiring. There's
no clean way to build an `echo` tool or a `net send`.

**Target:** because the core is pure and a port is just `Component` + data interface + a
`Link`, a tool is: open a `Link` (via whatever adapter), construct the protocol/service
client, go. Provide thin **client constructors** alongside each protocol:

- `aep.NewClient(link)` → `Echo(net, node)` for an echo tool.
- `nbns`/`netbios` already has a `NameService`; add `netbios.NewClient(transport)` with
  `Send(name, msg)` for `net send`.
- `ddp.Dial`-style helpers so a tool can send/receive datagrams without the router.

These ship as `cmd/csecho`, `cmd/csnetsend`, etc., each importing only core + the one
adapter it needs (small binaries). This is the payoff of the dependency rule: the same
protocol code serves the server and the tools.

---

## 13. Binary size / dependency discipline

Concrete rules baked into the design (serving the charter's *small* and *embedded* pillars):

- **gopacket lives behind the pcap link adapter only.** Core never imports it.
- **TOML/UCI parsers live behind their codec adapters.** Core config has zero serialisation
  deps; a UCI build links no TOML parser and vice versa.
- **sqlite is fully optional** (CNID/shortname/desktop) — it's large. The `metastore`
  interface (§9a) has a snapshot-to-file `mem` default, so TinyGo/embedded builds drop sqlite
  entirely; a sqlite metastore is one build-tagged adapter for full builds. Same for
  `s3`/`webdav`/`ftp`/`zipfs` FS backends — heavy, tag-gated, never in core.
- **Prefer stdlib + vendored subsets.** Where we use one function from a big dep, copy that
  function (attributed, per CLAUDE.md rule 7) rather than import the module. Add a
  `go.mod`-size check or `goweight`/binary-size CI gate so regressions are visible.
- **No reflection-dependent libs in core** (TinyGo constraint) — rules out struct-tag
  reflection marshalling in core, which is why config tags move to codec adapters.

---

## 13b. Interface hierarchy & relationships (the contract map)

How the core interfaces relate — what each implements, consumes, and is implemented-by. The
authoritative Go signatures live in [.refactor/01-PHASE-harness.md](01-PHASE-harness.md)
(Group B/C); this is the relationship overview so no implementor has to infer the wiring.

### Lifecycle spine (`core/component`)
```
Component  (Name/Start/Stop)                     ← the universal lifecycle
  ├─ optional caps (type-asserted, never widened into Component):
  │    Enableable, Bindable, Statful, Configurable, Bridged, Metered, Attachable
  └─ implemented by: every port, router, service, transport (real + placeholder)

A port        = Component + router.RoutedPort      (lifecycle + DDP data half)
A service      = Component + (consumes DatagramLink and/or fs.ForkFS + metastore.Store)
A file service = the above + share.Manager  (AFP/SMB: own share.Share descriptors; add/remove/update §11)
A transport    = Component + component.Attachable    (soft-bound into NetBIOS, §11d)
A TCP service  = Component + Bindable (host:port); ADAPTER RING (§3-bis) — owns a net.Listener,
                 wraps a pure core CommandHandler; build-tagged (dsi/smbtcp); NOT a RoutedPort,
                 no Socket(), never router-Attach'd. net lives here, never in core.
```

### Link altitudes (`core/link`) — composition, not parallel hierarchies
```
FrameLink ──Filter/Dedup/Capture/Bridge (decorators: FrameLink→FrameLink)──▶ FrameLink
FrameLink ──Framer.Framing()──▶ DatagramLink        (DDP encap + AARP/node-claim)
kernel socket / drivers-net ──implements──▶ DatagramLink   (no Framing needed)

consumed by:  ports/framers → FrameLink ;  router/services → DatagramLink
capabilities: MediumReporter, FilterableLink  (optional, type-asserted on a FrameLink)
CaptureSink ← Capture(FrameLink) tees frames; writers (adapters): capture/libpcap, capture/pcapfile(pure-Go)
              wire visibility is pcap-only (§6f) — always available, even without libpcap
```

### Event buses (`core/bus` primitive, two instances)
```
bus.Bus (Publish/Subscribe(topics…))          ← ONE primitive (§5)
  ├─ telemetry instance (core/bus): topics state/stats/log/message
  │     events: StateChanged, StatSample{component.Stats}, LogRecord{[]Field}, MessageReceived
  │     producers: supervisor (state), components (stats), log bus-sink (log), messenger (message)
  │     consumers: control.Plane.Subscribe → http/ubus/cli adapters; stats collector; UI net-send view
  └─ FS-mutation instance (core/fs): topic "fs"
        events: fs.Event{Op,HostPath,Origin,…}
        producers: file services, fork/name engines, fswatch adapter
        consumers: CNID/name/fork reactors, the other file service (same-FS coord, §10d)
```

### Logging (`core/log`)
```
Logger (With/Log/Enabled) ──fans Record to──▶ Sink…   (typed Field, no reflection)
levels: Trace/Debug/Info/Warn/Error    level is PER-CALL; threshold is a *LevelVar on each SINK
        (runtime-settable per scope §6b; Enabled folds across sinks)
sinks (core): ring, stderr        sinks (adapter): syslog, journald, semihosting, bus-sink
the bus-sink publishes bus.LogRecord onto the telemetry "log" topic — bus is ONE sink, not the mechanism
wire bytes are NOT logged — they go to a pcap CaptureSink (§6f); the traffic log is deleted
```

### Config (`core/config`)
```
Model{ typed well-known sections + map[string]Section }
Section (Key/Clone/Validate)  ← each component owns one; registered via SectionSchema
Codec (Marshal/Unmarshal)     ← ADAPTERS: toml, uci, json     (round-trip is the contract)
Store (Load/Save)             ← ADAPTERS: file(numbered-backup), uci, mem
consumed by: components (their Section at build/ApplyConfig); Plane (Config/Save)
```

### Storage seam (`core/fs` + `core/metastore`) — per-share assembly (§9d)
```
ForkFS = FileSystem + ForkEngine            ← what a file service actually holds
                                              (Rename/Remove carry the metadata container; §9)
FileSystem            ← RegisterFSWithParams(fsType, Factory, Param…): local/macgarden/hfs-image/fat-image/ftp/zip/s3/webdav
Param{Key,Required,Secret,Doc}  ← per-fs_type config schema; ParamsFor(fsType) renders the UI form;
                                  BuildShare validates required params (Path + Extra) before constructing
ForkEngine            ← appledouble / ads(SFM) / xattr(Netatalk) / native(HFS)
NameEngine            ← short(win/derive) / medium(netatalk)
FilenameCodec         ← macroman-utf8 / macroman-native / utf8   (per-call WireEncoding ↔ StoredName bytes; §10a-bis)
metastore.Store       ← mem(default) / sqlite / ntfs-ads / xattr   (cnid/shortname/desktop)
ShareSpec{ …, Path, Extra }  ← Path = host/image/archive root; Extra = backend params (url/user/pass/partition…)
BuildShare(ShareSpec) validates fs_type × fork_backend × filename_codec × name × metastore × required-params
```

### Share seam (`core/share`) — protocol-neutral share descriptor + CRUD (§9d/§11)
```
share.Share  ← a THIN descriptor, NOT a catalog façade: Name / FS() fs.ForkFS / Config (the ShareSpec
               that built it) / ReadOnly / Description / Permissions(stub) / Codec().
               Exposes the FS — callers do share.FS().Stat(p); it re-wraps no fs ops.
share.Manager ← Shares() / AddShare(ShareSpec) / UpdateShare(name,ShareSpec) / RemoveShare(name)
               the dynamic-reconfigure contract both AFP & SMB implement (§11);
               RemoveShare unpublishes (no new open) but lets in-flight sessions ride their handle.
imports: core/fs ONLY (no metastore/net/reflect/sqlite). AFP Volume / SMB Share HOLD a *share.Share
         and add only protocol concerns (wire path parse; AFP CNID rebind after FS Rename/Remove).
```

### Control (`core/control`) — one contract, many front-ends
```
Plane (methods + Subscribe)  ← façade over Supervisor + Codec/Store + telemetry bus
Supervisor                   ← implemented by compose/supervisor (owns DAG + model + reconfigure)
Diagnostics                  ← optional read-only probes
front-ends (ADAPTERS, none privileged): control/http (REST+SSE), control/ubus (ubus.sock), control/cli
```

### Compose ring (`compose/*`) — wiring, not interfaces
```
registry.Factory  → builds Component from Model (build-tagged init(), §8)
supervisor        → owns Component DAG; Start/Stop ordered; Reconfigure addressed+notify (§11)
                    implements control.Supervisor; publishes StateChanged; drives Attachable
stats collector   → telemetry "stats" subscriber → rates
```

**Import-direction invariant (enforced by the A2 archtest):** `core/component` is imported by
nearly everything and imports nothing but stdlib; `core/config` and `core/bus` are imported by
`core/control` and components but never import them back; adapters import core, never vice
versa; `compose` imports both. No core package imports
pcap/gopacket/koanf/net-http/**net**/sqlite/reflect — `net`/`net/http` are permitted only in
adapters (TCP services §3-bis: dsi/smbtcp; the web-UI front-end §7: control/http; any
net-backed link), never in core.

---

## 14. Proposed package layout (target)

```
core/
  protocol/   ddp atp asp pap nbp  ipx  netbeui  smb  netbios  mailslot  browser  (pure codecs)
              (mailslot = the \MAILSLOT\* SMB_COM_TRANSACTION envelope, §3-quater; browser = the
              [MS-BRWS] frames, NO mailslot envelope)
  link/       FrameLink + DatagramLink (two altitudes), sentinels, in-memory link,
              decorators (filter/dedup/capture/bridge — frame altitude only)
  link/       FrameLink + DatagramLink interfaces; `framing` (FrameLink→DatagramLink,
              does DDP encap/AARP); frame decorators (filter/dedup/capture/bridge); in-mem link
  log/        Logger + Level + typed Field + Sink interface (no reflection); ring sink
  buf/        per-target buffer-size constants + pooled buffers (small on tinygo, large desktop)
  port/       ethertalk ipx netbeui localtalk  (Component + frame codec; AARP/node-claim
              live in the framing adapter, so a kernel DatagramLink omits them)
  router/     appletalk router + tables + ZIP; ipx mini-router; netbeui
  service/    afp(+asp transport) smb netbios(+nbf/nbipx session transports) mailslot browser
              messenger(future) macip
              (Component + protocol logic; consume DatagramLink; talk ONLY to fs/share/metastore
              for storage. Each file service is a PURE command core + a CommandHandler seam — NO
              net here. DSI/SMB-TCP transports are adapters, §3-bis. mailslot is the shared
              \MAILSLOT\* dispatch layer over the NetBIOS DatagramConsumer seam (§3-quater);
              browser + messenger are mailslot consumers that hold NO transport AND NO mailslot-
              envelope code, common to NBF/IPX/NBT, §3-ter)
  fs/         FileSystem + ForkFS/ForkEngine + NameEngine + FilenameCodec interfaces,
              Factory registry + per-fs_type Param schema (the one seam AFP+SMB consume) + FS-domain event bus
  share/      thin share descriptor (Name/FS/Config/ReadOnly/Description/Permissions) + Manager
              CRUD contract (add/update/remove/list) both AFP & SMB implement; imports core/fs only
  encoding/   MacRoman↔UTF-8 tables etc. (reused by FilenameCodec adapters; pure, no reflection)
  metastore/  Store interface for cnid/shortname/desktop (mem snapshot default)
  config/     Model (no tags), SectionSchema registry, validation, EffectiveInterface
  control/    Plane contract (methods incl. Reconfigure(name,section) + Subscribe), Diagnostics
  component/  Component + capability interfaces
  bus/        the bus primitive (typed, topic-scoped pub/sub) + the telemetry instance
              (topics: state/stats/log → UIs); the FS-mutation instance lives in core/fs
adapter/
  link/pcap   link/tap   link/ppp   link/slip       (build-tagged FrameLink backends)
  link/kerneldp  link/driversnet                     (DatagramLink: AF_APPLETALK, TinyGo/ESP-IDF)
  dsi   smbtcp   netbios-tcp                          (TCP stream transports, §3-bis; build-tagged
                                                     dsi/smbtcp/nbt; own net.Listener + framing over
                                                     a pure core CommandHandler/seam. net lives in
                                                     these adapters, not core; esp32 sibling does
                                                     netdev/WiFi bring-up. netbios-tcp = NBT
                                                     RFC1001/1002: name(udp137)/datagram(udp138)/
                                                     session(tcp139) feeding the SAME NetBIOS
                                                     Session+Datagram seams as NBF/NBIPX, §3-ter.
                                                     smbtcp = direct-TCP :445 framing only)
  capture/libpcap  capture/pcapfile                  (CaptureSink writers; pcapfile = pure-Go,
                                                     TinyGo-safe — wire capture even w/o libpcap, §6f)
  log/syslog  log/file  log/journald  log/semihosting  (log sinks; SSE sink = bus → UI)
  config/toml config/uci                          (codecs)
  store/file  store/uci                            (config stores)
  control/http  control/ubus  control/cli           (front-ends; ubus = classicstack obj on
                                                     ubus.sock; methods→ubus methods, Subscribe→ubus events)
  metastore/sqlite  metastore/ntfs-ads  metastore/xattr   (cnid/shortname/desktop backends)
  fork/appledouble  fork/ads  fork/xattr  fork/native     (resource-fork engines)
  name/win-short  name/derive-short  name/medium          (short/medium name engines)
  fncodec/macroman-utf8  fncodec/macroman-native  fncodec/utf8  (filename charset/reserved codecs)
  fswatch/fsnotify                                          (host-FS watcher → core/fs bus)
  fs/local  fs/macgarden  fs/hfs-image  fs/fat-image      (filesystem backends)
  fs/ftp  fs/zipfs  fs/s3  fs/webdav                       (heavy FS backends, tag-gated)
compose/
  registry, supervisor (DAG lifecycle; publishes StateChanged), bus-rate subscriber, assembly
cmd/
  classicstack (server)  classicstack-svc  classicstackd
  csecho  csnetsend  ...                            (tools)
```

(Names indicative; the point is the three rings + the registry, not the exact paths.)

---

## Critical current files this design replaces or guts

- `internal/app/supervisor*.go` (1100+ lines) → `compose/supervisor` driving `Component`s
  via a DAG + registry. The bespoke `hook`/`portHook`/`routerHook`/`ddpServiceHook` and the
  parallel `*Hook` interfaces collapse into `Component` + capabilities.
- `internal/app/config_ini.go`, `config_model.go` → deleted; `appConfig` and the
  Model↔appConfig conversions go away. Components read typed sections from `config.Model`.
- `port/ipx/port.go`, EtherTalk, MacIP ports → strip `rawlink`/`capture`/`netlog`/filter/
  lifecycle; keep only frame codec + data interface; take a `Link`.
- `config/model.go` → drop `toml:`/`json:` tags; add section registry. `config/fromsource.go`,
  `marshal.go`, `save.go` → move into TOML codec + file store adapters.
- All ten `internal/app/*_disabled.go` → **deleted** (replaced by registry absence).
- `service/webui/*` → becomes `adapter/control/http`; the `ControlPlane` interface is the
  kept seam.
- `internal/app` `refreshNetBIOSStatus` / `refreshSMBStatus` / `refreshMacIPStatus` /
  `registerXxxStatus` re-publish dance and the statusTicker → **deleted**; the status view
  is a `core/bus` subscriber folding `StateChanged` + `StatSample` events. `pkg/metrics`
  collapses into a bus-rate subscriber; `pkg/logbuf` broadcaster becomes a bus producer.
- `port.TrafficMetered` / per-port meter plumbing → ports publish `StatSample` to the bus;
  the parallel observer wiring through the supervisor is removed.
- `service/afp/fs.go` duplicate `FileSystem`/`RegisterFS` → **deleted**; AFP (and SMB)
  consume `pkg/vfs` (renamed `core/fs`). `service/afp/appledouble_backend.go` → moves to
  `adapter/fork/appledouble`; AFP calls the `ForkEngine`/`ForkFS` interface, losing all
  direct AppleDouble knowledge (`resource_fork.go` parsing stays — it's protocol).
- `service/afp/cnid.go` AFP-local aliases + `desktopdb.go` sqlite coupling → replaced by the
  shared `metastore.Store`; CNID/shortname/desktop all open named metastores.
- `pkg/cnid/sqlite.go` → becomes `adapter/metastore/sqlite` (build-tagged); `mem` snapshot
  store is the default so sqlite is droppable.
- `service/afp/path_codec.go` (`afpPathElementToHost`/`hostNameToAFPBytes`, `runtime.GOOS`
  reserved-char switch) → moves out of the service into the default `FilenameCodec` adapter
  (`fncodec/macroman-utf8`); reserved-set becomes backend-declared, not GOOS-driven.
  `pkg/encoding` (MacRoman tables) is reused by the codec, lifted to `core/encoding`.

---

## Verification strategy (for the eventual refactor)

The design is validated incrementally; each refactor step keeps the build green and tests
passing. Specific checks:

1. **Import-graph CI gate**: a test that walks `core/...` imports and fails on any
   forbidden package (pcap, gopacket, koanf, net/http, sqlite, **`reflect`**, and the
   reflection-pulling `encoding/json`/`slog`). This *is* the architecture (dependency rule +
   no-reflection rule), made executable.
2. **TinyGo smoke build**: `tinygo build` of a minimal target (core + in-memory link +
   echo tool) in CI proves the core is actually portable.
3. **Round-trip tests per codec**: `Unmarshal(Marshal(model)) == model` for TOML and UCI,
   reusing the existing `config/model_test.go` style.
4. **Component conformance tests**: a shared test harness asserts every registered
   `Component` honours Start→Stop→Start idempotency and reports stats — generalising
   today's `port_hook_test.go` / `router_attach_test.go`.
5. **Existing suite stays green**: `go test ./...` and `go build -tags all` throughout;
   the linting/vet/gosec gates already in CI must keep passing.
6. **Tool acceptance**: `csecho` against a virtual link round-trips an AEP echo; proves the
   protocol-reuse claim end-to-end.
   - **Multi-front-end parity**: the HTTP adapter and the ubus adapter, driven against the
     same in-process `Plane`, produce identical results for the same method calls (e.g.
     `Reconfigure`, `Status`) and relay the same bus topics — proving the contract is genuinely
     transport-agnostic and OpenWRT is first-class, not a reduced variant. On an OpenWRT
     target this includes a `ubus call classicstack …` / `ubus listen` smoke test.
7. **Capture-replay compatibility tests** (the *compatibility-over-correctness* charter made
   executable): decode→re-encode real captures from `/captures` and assert byte-identical
   output; replay recorded client exchanges (including known buggy clients) against the
   service and assert we answer the way the client expects. A divergence from spec that a
   real client depends on is a passing test plus an `spec/errata.md` entry — not a bug.
8. **Reconfigure-and-notify test** (§11): reconfigure one named component; assert it (and only
   the dependents that genuinely can't ride the restart) emit Stop/Start `StateChanged`
   events, dependents that can hot-apply do not restart, and unrelated components keep a stable
   session. No model-diff is involved.
9. **Security-surface audit is a deliverable, not a gate**: each implemented legacy protocol
   ships a documented note of its inherent exposure (cleartext auth, spoofable identity,
   obsolete ciphers). CI checks the note exists for each enabled protocol; it does **not** try
   to harden the wire protocol. **Intentional-weakness code paths** (e.g. SSL 3.0
   re-encryption) carry an explicit annotation/allowlist so gosec/CodeQL-style scanners treat
   them as accepted-by-design, not findings — while the **host-side** code (credential
   storage, input sanitisation) stays under the full, un-suppressed security gate.

---

## Open decisions to confirm before sequencing the refactor

These shape the work but not the target design; flag them when we move from design → plan:

- **Big move vs. strangler**: introduce `core/`/`adapter/`/`compose/` and migrate package
  by package (strangler), or restructure in place by directory moves first? Strangler keeps
  green longer; in-place is fewer net lines.
- **How far to push TinyGo now**: make the *whole* core TinyGo-clean immediately, or just
  the protocol + link layer first (enough for tools) and tighten the rest later?
- **CNID/sqlite**: replace with a lighter embedded store, or keep sqlite behind an adapter
  and accept its size for full builds?
