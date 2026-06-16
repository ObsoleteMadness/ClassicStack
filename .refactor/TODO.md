# Refactor TODO

Tickable checklist mirroring [01-PHASE-harness.md](01-PHASE-harness.md) and
[02-PHASE-migration.md](02-PHASE-migration.md). Each task is sized for one agent/person.

**Status legend:** ⬜ todo · 🟡 in progress · ✅ done · ⛔ blocked
Fill **Owner** when claimed. **Deps** must be ✅ before starting (✋ = can parallelise once deps met).

---

## Phase 1 — Harness (no real logic ported)

### Group A — Skeleton & guardrails (sequential-ish; everything depends on A1)
| # | Task | Deps | Owner | Status |
|---|------|------|-------|--------|
| A1 | Create ring layout (`core/`,`adapter/`,`compose/`) as empty packages w/ `doc.go` | — | claude | ✅ |
| A2 | Import-graph CI gate (forbidden imports incl. reflect/json/slog) | A1 | claude | ✅ |
| A3 | `core/buf` per-target buffer-size consts (default + `tinygo`) | A1 | claude | ✅ |
| A4 | CI matrix (build default/all, vet, archtest) + **TinyGo amd64 GATES: linux & windows** (must fail on forbidden/reflect import) | A1,A2,A3 | claude | ✅ |

### Group B — Core interfaces (✋ parallel once A1 done)
| # | Task | Deps | Owner | Status |
|---|------|------|-------|--------|
| B1 | `core/component`: Component + capability ifaces (§3) | A1 | claude | ✅ |
| B2 | `core/link`: FrameLink/DatagramLink + decorator surface + framing contract (§2) | A1 (soft B7) | copilot | ✅ |
| B3 | `core/bus`: bus primitive + telemetry events, topic-scoped (§5) | A1 | claude | ✅ |
| B4 | `core/fs` bus: FS-mutation instance of the B3 primitive (§5/§10c) | B3 | copilot | ✅ |
| B5 | `core/log`: scoped Logger, typed Field, Sink, ring/stderr sinks (§6) | A1,A3,B3 | copilot | ✅ |
| B6 | `core/config`: Model + SectionSchema registry + Codec/Store ifaces (§4) | A1 | claude | ✅ |
| B7 | `core/protocol/ddp`: real Datagram codec (+ stub siblings) (§2/§12) | A1 | claude | ✅ |
| B8 | `core/fs`: FileSystem/File/ForkEngine/ForkFS/NameEngine/**FilenameCodec** + per-share params; lift `core/encoding` (§9/§10a/§10a-bis) | A1,B4,B6 | copilot | ✅ |
| B9 | `core/metastore`: Store iface + `mem` snapshot impl (§9a) | A1 | claude | ✅ |
| B10 | `core/control`: Plane contract (methods + Subscribe) + Supervisor/Diagnostics ifaces (§7) | A1,B3,B6 | copilot | ✅ |

### Group C — Harness (depends on Group B)
| # | Task | Deps | Owner | Status |
|---|------|------|-------|--------|
| C1 | `compose/registry`: name→factory, build-tag `init()` (§8) | B1,B6 | claude | ✅ |
| C2 | `compose/supervisor`: DAG, ordered start/stop, StateChanged publish (§3/§11) | B1,B3,C1 | claude | ✅ |
| C3 | Supervisor addressed `Reconfigure`+notify (no diff) + Attachable side-effects (§11) | C2,B1,B6 | claude | ✅ |
| C4 | `compose` stats/rate subscriber on telemetry bus (§5) | B3,C2 | claude | ✅ |

### Group D — Placeholders (depends on B + C)
| # | Task | Deps | Owner | Status |
|---|------|------|-------|--------|
| D1 | Placeholder ports (ethertalk/localtalk/ipx/netbeui) | B1,B2,C1–C3 | claude | ✅ |
| D2 | Placeholder router w/ Attach/Detach membership (§3) | B1,B2,D1 | claude | ✅ |
| D3 | Placeholder services (afp/smb/netbios/macip) | B1,B8,B9 | claude | ✅ |
| D4 | Minimal real adapters: inmem link, toml codec, file store, inproc control | B2,B6,B10 | claude | ✅ |
| D5 | Assembly + runnable `cmd/classicstack-ng` (boots all-placeholder stack) | C*,D1–D4 | claude | ✅ |
| D6 | **OpenWRT seam: `adapter/config/uci` + `adapter/store/uci` + `adapter/control/ubus` (ubus.sock) + procd/init.d sketch** (§4,§7) | B6,B10,D4,D5 | claude | ✅ |

### Group E — Test harness for the structure
| # | Task | Deps | Owner | Status |
|---|------|------|-------|--------|
| E1 | Component conformance harness (Start/Stop idempotency, capabilities) | B1,C2 | claude | ✅ |
| E2 | Bus conformance + back-pressure tests (reused by B3/B4) | B3,B4 | claude | ✅ |
| E3 | Multi-front-end parity test (inproc vs http **vs ubus** over same Plane) | B10,D4,D5,D6 | claude | ✅ |
| E4 | Reconfigure-and-notify test (asserts no model-diff) | C3 | claude | ✅ |
| E5 | Wire all tests into CI (incl. TinyGo amd64 gates + UCI/TOML round-trips) | A4,D6,E1–E4 | claude | ✅ |

**Phase 1 DoD:** see exit criteria in [01-PHASE-harness.md](01-PHASE-harness.md).

> **core/ errata — `encoding/binary` is forbidden:** it transitively imports
> `reflect`, so the archtest gate rejects any core/ package that imports it. Use the shared
> **`core/binaryprimitives`** codecs (`BE16/…/LE64` readers, `PutBE16/…` in-place, `AppendBE16/…`
> append) — do NOT re-hand-roll `be16`/`putLE32`/etc. per package (that duplication was consolidated
> in `c5de757`). `encoding/binary` is an explicit entry in the archtest forbidden list. Note `fmt`
> also pulls `reflect` transitively (even `fmt.Fprintf("%v"/"%X")`), so format small fixed things by
> hand in core. B2/B5/B8 do byte work — they import `binaryprimitives`.
>
> **core/ errata — `net` is forbidden:** TCP listeners are not available on every embedded
> target (a netless RP2040/Pico still serves DDP over raw Ethernet), so `net` must not enter
> `core/`. Add `net` to the archtest forbidden list **when M7a/M7b land** (not before — no core
> package imports it today). TCP/stream services live in `adapter/dsi` + `adapter/smbtcp` behind
> the `dsi`/`smbtcp` tags, importing `net` at the adapter altitude over a pure core command core
> (§1/§3-bis). `net`/`net/http` are equally permitted in `adapter/control/http` (web UI) — the
> rule is "net in adapters, never in core," not "net only for TCP services."
>
> **A4 errata:** on modern TinyGo (verified 0.41.1), the stdlib coverage is broad
> enough that `net/http`/`reflect` imports do **not** fail the TinyGo build. The
> forbidden-import / no-reflection allowlist is therefore enforced by the **archtest
> gate (A2)** — verified failing on a `net/http` probe — while the **TinyGo amd64
> gates** enforce real embedded-compilability (cgo/unsupported runtime features).
> The two gates are complementary, not redundant. (Local verify: both amd64 builds
> green; archtest fails-then-passes on probe insert/revert.)

---

## Phase 2 — Migration (strangler; starts only after Phase 1 DoD)

| # | Task | Deps | Owner | Status |
|---|------|------|-------|--------|
| M1 | Link adapters: pcap/tap/ppp/slip/kerneldp/driversnet + decorators | Phase 1 | claude | ✅ |
| M2 | Protocol codecs (atp/asp/pap/nbp/ipx/netbeui/smb/netbios) + capture-replay | M1 | claude | ✅ |
| M3 | Real ports (ethertalk/localtalk/ipx/netbeui) over real links | M1,M2 | claude | ✅ |
| M4 | Router + tables (event membership) + ZIP/RTMP + ipx/netbeui routers | M3 | claude | ✅ |
| M5 | DDP services (MacIP/IPXGW/AEP/NBP) + stats publish | M4 | claude | ✅ |
| M6 | Storage seam: unified FS, metastore, fork engines (SFM/Netatalk interop), name engines, **filename codecs** (MacRoman/reserved, from path_codec.go) | Phase 1 (B8/B9) | claude | ✅ |
| M6a | `core/fs` `ShareSpec.Path`+`Extra` param bag + per-fs_type `Param` schema (`RegisterFSWithParams`/`ParamsFor`, `BuildShare` required-param validation); real `local_fs` factory from `spec.Path`; metadata-carrying `ForkFS.Rename`/`Remove` (§9/§9d) | M6 | claude | ✅ |
| M7 | File services AFP/SMB/NetBIOS over fs/metastore + Attachable transports (pure command cores; in-core ASP/NetBIOS transports) | M6,M2 | claude | 🟡 |
| M7a | `adapter/dsi` (AFP-over-TCP `:548`): re-home `service/dsi` onto AFP command core's CommandHandler; `//go:build dsi`; net only here (§1/§3-bis). **Prereq DONE:** the transport-agnostic AFP command-core seam now exists (`core/service/afp/conn.go` — `Conn`/`Command`/`Close` + `CommandHandler`/`CommandCircuit`, the AFP analogue of SMB's conn.go); ASP now drives it, so the adapter binds to the seam not the ASP spine. | M7 | | ⬜ |
| M7b | `adapter/smbtcp` (SMB **direct-TCP `:445`** framing, 4-byte length prefix) onto SMB command core; `//go:build smbtcp`; net only in adapter (§3-bis) | M7 | | ⬜ |
| M7b2 | `adapter/netbios-tcp` (**NBT**, RFC 1001/1002: name udp137 / datagram udp138 / session tcp139) — TCP sibling of NBF/NBIPX; session half → NetBIOS `SessionConsumer`, datagram half → `DatagramConsumer`; adds NO SMB/browser code, only the wire transport; `//go:build nbt`; net only in adapter (§3-ter). Most vintage TCP clients use `:139`, not `:445`. | M7 | | ⬜ |
| M7e | **SMB direct-hosted over IPX** (socket `0x0550`, "NWLink direct host") — a CORE transport (no `net`, no NetBIOS layer): connection-id framing on the IPX mini-router driving the SAME SMB `SessionConsumer` seam as NBF/NBIPX. Re-home from legacy `service/smb/over_ipx_direct`. Proves SMB runs over IPX both with NetBIOS (NBIPX, 0x0455) and without (direct, 0x0550). | M7 | claude | ✅ |
| M7c | `core/share`: thin Share descriptor (Name/FS/Config/ReadOnly/Description/Permissions-stub) + `Manager` CRUD; AFP `Volume` & SMB `Share` hold the shared Share; both services implement `share.Manager` (add/update/remove; RemoveShare keeps in-flight sessions) — contract + tests, supervisor wiring is M8a (§9d/§11) | M6a,M7 | claude | ✅ |
| M7d | `core/service/browser` (optional, datagram-layer; §3-ter): NetBIOS browser broken out of `service/smb` into a standalone service common to NetBEUI/IPX/NBT. Plugs into the NetBIOS `DatagramConsumer` seam; parses `\MAILSLOT\BROWSE` (HostAnnounce/Election/GetBackupList/DomainAnnounce/LocalMaster); maintains browse list + election role; SMB asks for the list via a read-only `BrowseList()` seam over IPC$ `\PIPE\LANMAN`. Optional (registry `init()`); SMB carries no browser logic. **The RAP `NetServerEnum2` IPC$ consumer that calls `BrowseList()` is the SMB side (a follow-on in `core/service/smb`).** | M7 | claude | ✅ |
| M7f | **Mailslot seam (§3-quater)** — lift the `\MAILSLOT\*` SMB_COM_TRANSACTION envelope out of `core/protocol/browser` into `core/protocol/mailslot`; add a mailslot dispatch layer (`Consumer` registered by mailslot name + `SendMailslot`) that plugs into the NetBIOS `DatagramConsumer`/`SendDatagram` seams; **rework `core/service/browser` to register for `\MAILSLOT\BROWSE` and handle ONLY browser frames** (no mailslot-envelope code). The browser sits entirely on top of NetBIOS via the mailslot layer; per-protocol framing stays in the NBF/NBIPX transports. Reshape of the M7d slice. | M7d | claude | ✅ |
| M7g | **Messenger service `\MAILSLOT\MESSNGR`** (`net send` / WinPopup receive) — a second mailslot consumer, proving the seam is multi-consumer. Receive first; send/UI future. Optional. Depends on the M7f mailslot seam. | M7f | claude | ✅ |
| M8 | Control front-ends (http, ubus, inproc — full Plane surface) + config codecs/stores (toml/uci) + bus log sink + http Basic-auth/first-run. **DONE except the two structurally-deferred pieces:** the **web UI/SPA** is split out as **M8-spa** (held by user), and the **logging cutover** (retire `netlog`/`pkg/logbuf`/`pkg/metrics` in the live runtime) is **moved to M10** — it cannot run while `internal/app` is the live runtime, so it executes AS PART OF the cmd cutover. All new-ring code already logs via `core/log`; only legacy `internal/app` still uses `netlog`. | M5,M7 | claude | 🟡 |
| M8a | Share config + Manager wiring: AFP/SMB volume `core/config` sections + `config → []fs.ShareSpec` mapper (options→Extra) in the registry factories; supervisor `Reconfigure` for an AFP/SMB section drives `share.Manager` Add/Update/Remove; `ParamsFor`-generated per-fs_type form masks `secret` params (§9d/§11). **Plus server identity (§4-bis):** top-level `config.Identity{Hostname,Workgroup}` (one source of truth, owned by NO service — SMB needs the hostname even with NetBIOS absent, e.g. direct-TCP `:445` / AFP-only); registry hands one `Hostname` to whichever consumers are enabled — SMB (`SetServerName`, advertised in NEGOTIATE), NetBIOS *if enabled*, browser *if linked*; no per-service hostname field so they cannot diverge; `Validate` backstops any externally-surfaced second name; the NetBIOS ≤15-byte/upper-case rule applies only when NetBIOS is enabled; `Hostname` change is restart-grade. | M7c,M8 | | ⬜ |
| M8-spa | **New-ring SPA** (web UI in `adapter/control/http/spa`, `//go:build webui\|\|all`) over the already-built http control adapter contract (`/setup`+409/401 gate, masked `/config`, `/reconfigure`+`/save`, `/status`+start/stop/restart, users CRUD, `/subscribe` SSE for status/stats/log). Renders a `Secret` param as a password field (the server already unmasks a blind round-trip). Needs a small contract add: a Plane method surfacing per-fs_type `fs.ParamsFor` (today `ListFSTypes` returns names only). **DEFERRED — held by user 2026-06-16.** Build it **fresh/minimal**, and **use native Web Components, NOT jQuery.** | M8,M8a | | ⬜ |
| **M-ng** | **MINIMAL TESTABLE `classicstack-ng` (loopback, no hardware) — pull the front half of the M10 cutover forward into its own target.** Today `cmd/classicstack-ng` is the D5 skeleton: it registers components and starts/stops them in dependency order but **moves zero packets** — every port comes up inert (`reg_*` injects `nil` link/framer/router) and no service is wired to the router or to a transport. The compose root that cross-wires the runtime data path has never been built (registry factories build everything inert pending exactly this). Goal: a `classicstack-ng` whose **whole protocol stack runs over `inmem` links** so integration tests can inject DDP/SMB/IPX frames and assert protocol replies across the **real** router + services — no pcap, no NICs. Broken into the sub-steps below; each is a standalone commit. **Real device links (pcap), TOML-driven config, and the logging cutover are explicitly NOT in M-ng** — they stay in M8/M9/M10. | M7,M8a | | ⬜ |
| M-ng1 | **Service ↔ router wiring** (highest value, lowest risk — do first). A `compose` root step that, after the registry builds each component, calls the setters that already exist: AFP/SMB/MacIP/NBP/IPXGW `SetRouter` + `router.RegisterService` (by `Socket()`). This alone makes DDP services reachable end-to-end over a loopback router. The supervisor stays lifecycle-only (no wiring role); the wiring lives in a new `compose` cross-wire function the ng main calls between Build and StartAll. | M-ng | | ⬜ |
| M-ng2 | **Transport ↔ service seams.** Wire the M7 seams the registry never connected: IPX/NetBEUI mini-routers ↔ their frame ports; SMB `SessionConsumer`/`DatagramConsumer` onto the NBF (`NewNBFEngine`)/NBIPX (`NewIPXEngine`)/direct-IPX engines; IPXGW `SetIPXRouter`; mailslot `Router` + browser/messenger consumers; AFP over its ASP spine on the router (the new `afp.HandlerAdapter`/ASP path). All the `*Adapter`/`New*Engine` constructors exist — M-ng2 is the call-site that connects them. | M-ng1 | | ⬜ |
| M-ng3 | **inmem link assembly + integration tests.** Drive every port from `adapter/link/inmem` pairs (M3 port tests already do this per-port) so the assembled ng stack has a loopback wire. Add `cmd/classicstack-ng` integration tests (or a `compose/integration` package): inject an AFP login→OpenVol→Enumerate, an SMB NEGOTIATE→TREE_CONNECT, an IPX/NetBEUI frame; assert the protocol replies come back through the real router+services. This is the **M-ng exit criterion** — "can test classicstack-ng" = these pass. | M-ng1,M-ng2 | | ⬜ |
| M9 | Platform integration (Windows svc / launchd / systemd / procd) | M8 | | ⬜ |
| M10 | cmd cutover + delete `internal/app`/`*_disabled.go`; docs. **Builds on M-ng** (which already did the service↔router / transport↔service cross-wiring over inmem): M10 adds the REAL device-link injection (pcap/framing + `LinkFactory`), TOML/UCI-driven config into the ng main, makes `classicstack-ng` the shipped binary, and deletes the legacy runtime. **Includes the logging cutover moved from M8** (retire `netlog`/`pkg/logbuf`/`pkg/metrics` — they die with `internal/app`). This is the step that unlocks **real-client end-to-end testing** (a Mac/PC connects over a real NIC). | M1–M9,M-ng | | ⬜ |
| T1 | Client tools `cmd/csecho`, `cmd/csnetsend` (protocol-reuse proof) | M2 | | ⬜ |

**Phase 2 DoD:** see exit criteria in [02-PHASE-migration.md](02-PHASE-migration.md).

> **M1 notes (what landed / deferred):**
> - **Landed:** real `core/link` decorators (`Filter`/`Dedup`/`Bridge`+`BridgeWiFi`, ported
>   from `port/rawlink/bridge_link.go`, stdlib-only/reflection-free, archtest-clean);
>   `adapter/link/pcap` (libpcap FrameLink, gated behind `-tags pcap`; no-pcap stub otherwise,
>   so default + TinyGo builds carry no cgo); `adapter/link/framing` (Ethernet/SNAP DDP
>   FrameLink→DatagramLink seam); both capture writers behind `core/link.CaptureSink` —
>   `adapter/capture/pcapfile` (**pure-Go, stdlib-only, TinyGo-gated**) and
>   `adapter/capture/libpcap` (gopacket/pcapgo). The TinyGo amd64 gates now import `core/link`
>   + `pcapfile` so their TinyGo-safety is verified, not assumed.
> - **Stubs (clearly marked, return `ErrNotImplemented`):** `adapter/link/{tap,ppp,slip,kerneldp,driversnet}`.
>   `kerneldp` returns a `DatagramLink` (AF_APPLETALK); the rest return `FrameLink`.
> - **Deferred within M1 → M3:** AARP address-resolution / node-claim in the framing adapter
>   (encode/decode are real; outbound goes to the AppleTalk broadcast MAC, inbound AARP frames
>   are skipped — marked `TODO(M3)`). Real TAP/PPP/SLIP/kerneldp/driversnet I/O lands with the
>   ports (M3).
> - **Deferred → cmd cutover (M10):** the §6f "delete the traffic-log plumbing" cannot run while
>   `internal/app`/`pkg/metrics` are still live; it's done as those are removed at cutover.
> - **Drive-by fix:** `adapter/link/inmem` `Pair` shared one `sync.Once` so closing both ends no
>   longer double-closes the shared done channel (panic).

> **M2 notes (what landed / deferred):**
> - **Landed:** real codecs in `core/protocol/{atp,asp,nbp,ipx,netbeui,netbios,smb,pap}`, all
>   stdlib-only/reflection-free (hand-rolled BE/LE helpers — no `encoding/binary`/`binutil` in
>   core). DDP was already done in B7. Each replaces its `doc.go` stub. Append-style `Encode(dst)`
>   where it suited (atp/ipx/smb); each protocol's natural API kept where the legacy one was
>   already clean (nbp/netbeui/netbios).
> - **Capture-replay (decode→re-encode byte-identical against `/captures`):** `ipx` (ipx.pcap
>   frame 1, RIP broadcast), `netbeui` (netbeui.pcap frame 1, ADD_NAME_QUERY "CLASSICSTACK"),
>   `smb` (ipx.pcap frame 14, SMB_COM_TRANSACTION header). `atp`/`asp`/`nbp`/`netbios` use
>   golden-vector + round-trip tests (no standalone capture; they ride inside DDP/IPX frames).
> - **SMB scope:** M2 delivers the 32-byte SMB1 **header** codec + command/dialect/status consts
>   (incl. the [MS-CIFS] Reserved field at offset 22). Per-command param/data blocks stay in
>   `service/smb` until the file-services rebuild (M7), which will sit on this header.
> - **NetBIOS scope:** wire codecs only (name/datagram/session-packet + NBIPX/NMPI). The generic
>   `SessionTable[Remote]` (sync/atomic/generics service state) stays in legacy `protocol/netbios`
>   until M7 — it is session state, not a wire codec.
> - **PAP:** no legacy source and no current consumer; written fresh from Inside AppleTalk Ch. 10
>   (ATP-UserData header codec). Spec-derived, not capture-observed — flagged for `spec/errata.md`
>   if a real client deviates. Enables a future print service without inventing wire format later.
> - **TinyGo gates** now blank-import all 8 codecs — embedded wire encode/decode is verified clean.
> - **Not deleted yet:** legacy `protocol/*` packages stay until their consumers (services/router)
>   migrate (M3–M7); deletion happens per-subsystem as parity is proven, per the strangler recipe.

> **M3 notes (what landed / deferred):**
> - **Design check first:** §3 says "a port = Component + `router.RoutedPort`" and AppleTalk ports
>   "speak DDP to `router.Inbound`". An earlier draft used a generic `core/port.Sink` — wrong
>   shape; deleted. The port now delivers via `router.Inbound(d, self)`. `core/router` imports
>   only component/log/ddp, so `core/port → core/router` is cycle-free. Added the missing
>   `Multicast(zoneName, d)` to `router.RoutedPort` (§3 lists it).
> - **Two real port bases (core, stdlib-only, archtest- + TinyGo-clean):**
>   - `core/port/internal/runport` — AppleTalk ports: a `link.DatagramLink` read loop delivering
>     to `router.Inbound`, real frame/byte counters, `Metered` observer, `Unicast/Broadcast/
>     Multicast`, `SetAddress` (network/node for M4 routes), Stop→Start (link reopened per Start
>     via a `LinkFactory`), and `Configurable` (iface change → `ErrNeedsRestart`).
>   - `core/port/internal/frameport` — IPX/NetBEUI ports (§3: "speak frames to their own
>     mini-routers"): a `link.FrameLink` read loop with inbound dedup (FNV-1a, 25 ms window /
>     100 ms TTL, matching legacy), metering, counters, `Send`, same lifecycle/Configurable.
> - **Real ports:** `ethertalk` + `localtalk` (embed runport; take an injected `FrameLink` +
>   `link.Framer` since core can't import the `adapter/link/framing` seam — compose injects it).
>   `ipx` + `netbeui` (embed frameport; own `DeliveryCallback` + `Send`, decode the Ethernet/LLC
>   encapsulation inline). IPX accepts Ethernet II 0x8137 + raw 802.3 + 802.2 LLC (0xE0); NetBEUI
>   does the UI-frame path (0xF0F003).
> - **Tested (the M3 "done when"):** inbound decode→deliver, outbound encapsulation + metering,
>   dedup, Stop→Start restartability, and Reconfigure for each port, via in-test fake links/router
>   (core tests stay core-only — no adapter import). Both TinyGo amd64 gates now blank-import the
>   four ports + `core/router`.
> - **Deferred:** AARP/node-claim (EtherTalk) and LLAP ENQ/ACK claim (LocalTalk) stay in the
>   framing/link adapters — `SetAddress` records a completed claim; zone→multicast-MAC mapping is
>   M4 (router/ZIP). NetBEUI LLC **Type-2** connection state machine (SABME/UA/I-frame/DISC) is
>   session-layer → M7 (NetBIOS service); the M3 port is UI-frame only. The IPX/NetBEUI
>   **mini-routers** themselves are M4. Registry factories still build ports inert (nil link) —
>   real device-link injection is the cmd/compose cutover (M8/M10).
> - **Removed:** `core/port/internal/portbase` (the Phase-1 inert placeholder base) — fully
>   replaced by runport/frameport; its four port `doc.go` stubs deleted (package docs now live in
>   each port's main file). Legacy `port/*` packages stay until M4 wires their mini-routers.

> **M4 notes (what landed / deferred):**
> - **Real AppleTalk router** (`core/router`, replacing the Phase-1 placeholder `RouterImpl`):
>   owns the routing + zone tables, does `Inbound` (source/dest-network fill-in, local delivery by
>   dest socket, forward via `Route`), `Route` (next-hop unicast/broadcast + 15-hop limit), and
>   `Reply` (mirror src/dst, broadcast for non-local/startup-range sources). **Event-driven
>   membership (§3):** `Attach` installs the port's directly-connected route; `Detach` withdraws
>   every route + zone reachable through it **immediately** (no aging delay) via
>   `RemoveEntriesForPort`. Service dispatch is a `Socket()→Service` map; `RegisterService`/
>   `UnregisterService` mutate it. A `ServiceRouter` interface is the surface RTMP/ZIP/AEP consume
>   (testable against a fake).
> - **Routing table + ZIT** ported from legacy `router/{routing_table,zone_information_table}.go`,
>   re-expressed for core: RTMP aging machine Good→Suspect→Bad→Worst→removed (directly-connected
>   Distance-0 entries never age); `Consider`/`MarkBad`/`SetPortRange`/`Age`/`Snapshot`/`Entries`.
>   Hand-built entry key (no `fmt.Sprintf` → reflection-free, §1). ZIT uses `core/encoding` for
>   MacRoman case-folding — **added `MacRomanToUpper`/`MacRomanToLower` + the AppleTalk case tables
>   to `core/encoding`** (the M6 `pkg/encoding` lift starts here).
> - **DDP services** as `core/service` components riding the router (router injected at
>   construction, not at Start — fits the `Component` lifecycle): `aep` (echo), `rtmp`
>   (responding socket-1 service + sending timer + aging timer), `zip` (responding socket-6 service
>   incl. ATP GetMyZone/GetZoneList/GetLocalZones + sending timer). `encoding/binary` replaced with
>   hand-rolled `be16`; `netlog` replaced with `core/log` scoped warnings.
> - **IPX + NetBEUI mini-routers** (§3: peers of the DDP router, not members — own address spaces):
>   `core/router/ipx` ports the legacy `router/ipx` socket/node/broadcast dispatch (node-handler
>   precedence, broadcast fan-out, addressed-to-us filter; on Ethernet the IPX node *is* the MAC, so
>   `Send` resolves dst MAC from `DstNode`). `core/router/netbeui` is the new parallel: NBF UI-frame
>   dispatch by destination NetBIOS name + broadcast handler, with session-command (0x14–0x1F)
>   frames routed to a registered **session handler** — the LLC Type-2 connection machine stays M7.
>   Both fed by the M3 frame ports via the ports' named callback types (so the concrete ports
>   satisfy the mini-router `Port` interfaces exactly; compile-time asserted).
> - **Tested (the M4 "done when"):** routing-table install/replace/aging/snapshot/withdraw; ZIT
>   add/query/remove/overlap-reject; router Attach/Detach (immediate connected-route withdrawal),
>   socket dispatch, Reply routing, network fill-in; RTMP range-request reply + RTMP-data route
>   learning; ZIP GetMyZone reply + ZIP-reply zone commit; IPX socket/node/broadcast dispatch +
>   foreign-drop + Send src/MAC fill; NetBEUI name/broadcast/session dispatch. All via in-test fakes
>   (core stays core-only). Both TinyGo amd64 gates now blank-import `core/router/{ipx,netbeui}` +
>   `core/service/{aep,rtmp,zip}`; archtest + full tagged harness green.
> - **Deferred:** wiring the real router/services into the registry + supervisor (the cmd/compose
>   cutover, M8/M10 — registry `reg_router.go` still builds the router but no services are attached
>   yet); the EtherTalk port's `MulticastAddress` capability that ZIP GetNetInfo consults (lands
>   with the EtherTalk multicast/AARP work). Legacy `router/*` + `service/{rtmp,zip,aep}` stay until
>   the cutover proves parity, per the strangler recipe.

> **M5 notes (what landed / deferred):**
> - **NBP name-information service** (`core/service/nbp`): the stateful service riding the router
>   on the NIS socket (2/DDP-2). Owns the registered-name table (`RegisterName`/`UnregisterName`,
>   case-insensitive dedup) and answers BrRq / LkUp / Fwd — replying for local matches and
>   resolving/multicasting/forwarding zone lookups via `ServiceRouter` (`RoutingTable().GetByNetwork`,
>   `Zones().NetworksInZone`/`ZonesInNetworkRange`). Re-expressed from legacy
>   `service/zip/name_information.go` against core surfaces (no `port.Port`/`netlog`). This is the
>   shared dependency MacIP + IPXGW register their advertised names through.
> - **MacIPX codec** (`core/protocol/macipx`): the M2-deferred gateway codec, lifted from legacy
>   `protocol/macipx` and made reflection-free (sentinel errors, no `fmt`). Opcodes (Data/Listen/
>   Register req+rsp), `AssignedNodeForDDP`, listen/register decode. Golden-vector tests use the
>   spec example (req "00 02 00 00 00 01" → node 7a:00:00:00:01:01 → wire `23 00 02 …`).
> - **IPX gateway** (`core/service/ipxgw`): the AppleTalk-side MACIPXGW counterpart, a real
>   `router.Service` on socket 78. Handles register (0x20→0x23 via `rtr.Reply`), encapsulated IPX
>   (0x00 → decode → inject into the M4 `core/router/ipx` mini-router), and listen (0x10). Inbound
>   IPX addressed to an assigned node (or broadcast fan-out by listen-socket) is re-encapsulated and
>   routed back over DDP. `SetIPXRouter` wires the mini-router (broadcast-handler + per-node claims).
> - **MacIP gateway** (`core/service/macip`, replacing the D3 placeholder): the AppleTalk-facing
>   transport — ATP config (TReq→TResp IP assignment, socket 72) + DDP-22 IP data. The IP-side
>   network (raw Ethernet, NAT, DHCP relay, proxy ARP) is an **injected `IPEgress` adapter seam**, so
>   core stays stdlib-only/reflection-free — **IPv4 is `[4]byte`, no `net` package**. Pool/lease
>   tracking, pool↔pool direct delivery, and `RegisterExternalLease` (for adapter DHCP) live in core.
> - **Stats (§5):** every service implements `component.Statful` — NBP (brrq/lkup/fwd/replies +
>   registered_names gauge), IPXGW (registers/data_frames/listens/tunneled_in + clients gauge),
>   MacIP (assigns/data_out/data_in/dropped + active_leases gauge). AEP was already done in M4. The
>   compose stats subscriber (C4) publishes these as `bus.StatSample`, replacing `refreshMacIPStatus`
>   et al.
> - **Tested (the M5 "done when"):** NBP LkUp reply for a registered name + no-match-no-reply +
>   register/unregister dedup; macipx golden vectors + multi-entry listen + misalignment; IPXGW
>   register-reply node assignment, encapsulated-IPX forwarding into the mini-router, inbound-IPX
>   tunnel back to a client; MacIP pool assign/reuse/range, ATP config assign reply, DDP-22→egress,
>   egress→AppleTalk route. All via in-test fakes (core stays core-only). cs-tinygo blank-imports
>   nbp/ipxgw/macip + protocol/macipx; archtest + harness + linux/windows amd64 cross-build green
>   (TinyGo toolchain not installed locally — archtest enforces the reflection-free rule the TinyGo
>   gate complements).
> - **Deferred:** wiring the real services into the registry + supervisor is the cmd/compose cutover
>   (M8/M10) — `reg_macip.go` keeps an inert placeholder (mirrors M4's unattached DDP services), and
>   no `reg_nbp`/`reg_ipxgw` registry entries exist yet. The **IP-side egress adapter** (pcap raw
>   Ethernet + OS-NAT + DHCP-relay + proxy-ARP + ICMP-to-gateway + IP fragmentation), and the ASP
>   session lease-pinning hooks, land with the adapter/cutover work, not in core. Legacy
>   `service/{macip,ipxgw}` + `service/zip/name_information.go` stay until cutover proves parity.

> **M6 / M6a notes (what landed / deferred):**
> - **Storage seam (`core/fs`, commit `4301eee`):** the unified `FileSystem`/`File`/`ForkEngine`/
>   `ForkFS`/`NameEngine`/`FilenameCodec` contract; `BuildShare` assembles the per-share stack and
>   validates the `fs_type`×`fork_backend`×`filename_codec` triple; fork engines (`appledouble`,
>   `ads`, `xattr`, `native`); `core/encoding` lifted from `pkg/encoding` (MacRoman↔UTF-8 +
>   case tables added in M4); `core/metastore` CNID/shortname store (mem default, sqlite behind a
>   tag). The metadata-carrying `ForkFS.Rename`/`Remove` (MoveMetadata/DeleteMetadata folded in) are
>   on the assembled `shareFS` so callers make one correct call.
> - **M6a param bag (commit `4301eee` + `local_fs` follow-on):** `ShareSpec.Path`+`Extra`;
>   `RegisterFSWithParams`/`ParamsFor` per-fs_type `Param` schema; `BuildShare` validates required
>   params (a share missing its declared `path`/`url` fails on Apply) and the codec/fork triple.
>   The first **real** backend — `core/fs/local.go` `local_fs` — reads `spec.Path` as a host
>   directory root, maps '/'-joined share-relative paths onto `os` with traversal protection
>   (`ErrPathEscape`), and registers a required `path` Param. `memfs` stays for tests.
> - **Deferred:** real `DiskUsage` (platform statfs/GetDiskFreeSpaceEx) on `local_fs` returns 0/0
>   (unknown) for now — the OS adapter refines it later. Other backends (hfs-image, zipfs, ftp,
>   macgarden) keep their declared schemas but real factories land as needed; only `memfs` + the new
>   `local_fs` are wired today.

> **M7 notes (in progress — slices landed):** the in-core file-services command engines (AFP, SMB,
> NetBIOS NBF + NBIPX session transports) are now **functionally complete** — all AFP commands, the
> SMB session + FS command set incl. NT_CREATE_ANDX, and both NetBIOS session transports plus the
> connectionless datagram/node-status paths. The M7 row stays 🟡 because two scoped items remain
> gated on later milestones: §10d same-host-path coordination needs the **shared event bus** (each
> service keeps its OWN `shareFS` instance — own fork engine/codec — but both are built with one
> `bus.Bus` so a mutation by one reaches the other) that **M8a** wires once it recognises two specs
> exporting the same host path, and legacy `service/{afp,smb,netbios}` deletion is blocked on the
> **M8/M8a→M10** compose cutover
> (still imported by the live `internal/app`). The byte-range locking/MPX/raw SMB paths are left at
> STATUS_NOT_SUPPORTED until a target client needs them.
> - **Slice 1 (`47da010`):** service shape — AFP `Volume`/SMB `Share`/NetBIOS over the fs/metastore
>   seam (no storage-layout knowledge); per-request wire charset threading; real `ads` fork backend.
> - **Slice 2 (`1a03dc4`):** real `xattr` fork backend (Netatalk EA layout, spec/16 §1c).
> - **Slice 3 (`e1b0d97`):** AFP protocol-dispatch spine — ATP multi-packet responder, ASP session
>   table (GetStatus/OpenSession/Tickle/Command), AFP command demux + starter set (GetSrvrInfo,
>   Login guest/cleartext, GetSrvrParms, OpenVol/CloseVol, GetFileDirParms, Enumerate).
> - **M7c (`2f26eb8`):** `core/share` thin Share descriptor + `Manager` CRUD; AFP `Volume` & SMB
>   `Share` hold a `*share.Share`; both implement `share.Manager` (RemoveShare keeps in-flight
>   sessions). Supervisor/config wiring is M8a.
> - **Slice 4 (`e01e6f1`):** AFP fork I/O — per-session fork table + FPOpenFork/FPRead/FPWrite/
>   FPCloseFork/FPFlush/FPFlushFork/FPGetForkParms over `v.FS().OpenFork` + positional I/O (no
>   AppleDouble/stream/EA knowledge in the spine). Short/at-EOF read → bytes+kFPEOFErr; R/O-handle
>   write → kFPAccessDenied; from-end write appends at live `ForkLen`; forks drained on CloseSession.
>   ENOSPC left as an OS-adapter refinement (core stays syscall-free).
> - **Slice 5 (`8922b9b`):** AFP catalog mutation — FPCreateFile (soft/hard), FPCreateDir (returns
>   new dirID), FPDelete (file/empty dir; refuses root), FPRename (in-place leaf, CNID preserved),
>   FPOpenDir/FPCloseDir (dirID = CNID). `resolveCatalogPath` resolves dirID + relative pathname
>   through the volume CNID store + FilenameCodec; storage reached only via `v.FS().CreateFile/
>   CreateDir/Remove` and CNID-aware `v.renamePath/removePath`. `catalog_test.go` covers all five.
> - **Slice 6 (`5a7e828`):** AFP full file/dir parameter bitmaps — `parms.go` packs the complete
>   AFP 2.x parameter block (attributes, parent DID, create/mod/backup dates, 32-byte Finder info,
>   long/short names as offset pointers into a trailing variable area, file-number/dir-id CNID,
>   data/resource fork lengths, offspring count, owner/group, access rights) from the §9 seam.
>   Volume gains FinderInfo/ShortName/ParentCNID; GetFileDirParms/Enumerate/OpenFork/GetForkParms all
>   pack via `vol.fileDirParams`. Dates fixed onto the spec 2000-GMT epoch (legacy used 1904-local) —
>   `spec/errata.md` "AFP catalog date epoch". `parms_test.go` checks every field at its bit offset.
> - **Slice 7 (`d595bd7`):** AFP two-phase ASPWrite data path — the server-initiated
>   aspWrite→aspDataWrite→TResp→reply exchange (spec/10) so a large FPWrite carries its data over its
>   own ATP transaction. `write.go` `pendingWriteTable` keyed by the tid the server stamps into the
>   aspDataWrite TReq (WS echoes it in its TResp); `asp.go` `handleWrite` (phase 1: parse FPWrite
>   reqCount, send aspDataWrite via the originating port's `Unicast`) + `handleDataResponse`
>   (phase 2b→3: accumulate TResp data, run FPWrite on EOM, reply to the original aspWrite); `atp.go`
>   `parseATPResponse` decodes the inbound TResp the spine previously dropped. Zero-reqCount writes
>   complete inline. `write_test.go` drives single-/multi-packet/zero-length writes over a recording
>   port. Pure ASP/ATP transport — storage still touched only via the fork engine.
> - **Slice 8 (`14ed254`):** AFP Desktop database — FPOpenDT/FPCloseDT (per-session DTRefNum→volume
>   table), FPGetComment/FPAddComment/FPRemoveComment (ride the fork seam via `v.FS().ReadComment`/
>   `WriteComment`, so comments travel with the file's metadata container), FPAddIcon/FPGetIcon/
>   FPGetIconInfo + FPAddAPPL/FPRemoveAPPL/FPGetAPPL (per-volume in-memory `desktopDB` for icons +
>   APPL mappings — persistence is an adapter concern, like the mem metastore). FPAddIcon (cmd 192)
>   arrives over the two-phase ASPWrite path (bitmap is bulk data): `writeDataCount`/`appendWriteData`
>   now recognise the 20-byte FPAddIcon header alongside FPWrite's 12-byte one. `desktop_test.go`
>   covers OpenDT/CloseDT, comment round-trip (+ item-not-found), the FPAddIcon two-phase path →
>   GetIcon/GetIconInfo, and APPL round-trip. spec/errata "Desktop database persistence" documents the
>   comment/icon split + path-encoding convention.
> - **Slice 9 (`7515477`) + reshape (`1cb4c08`):** AFP **FPCatSearch** (cmd 43) — the last AFP command.
>   First cut walked the catalog in the spine; corrected (field feedback) so **search semantics belong
>   to the FileSystem backend, which may decline**. Added an OPTIONAL `fs.CatSearcher` capability
>   (`core/fs/catsearch.go`): `CatSearchCriteria` (name partial/full, parent path, free-text `Query`
>   for synthetic backends, Max), `CatSearchResult`, opaque `CatSearchCursor`, `ErrCatSearchUnsupported`,
>   and a default `WalkCatSearch` (depth-first predicate walk plain backends opt into — memfs/local_fs do).
>   `afpCatSearch` decodes the AFP spec1/spec2 wire → `fs.CatSearchCriteria`, delegates via the capability
>   (gated on `Capabilities().CatSearch`), returns **kFPCallNotSupported** when declined, packs returned
>   paths with `fileDirParams`, round-trips the backend's opaque cursor through the 16-byte CatalogPosition.
>   MacGarden et al. can turn CatSearch into an explicit query → virtual files. spec/errata "FPCatSearch
>   over the FileSystem seam". **AFP command set now complete.**
> - **Endian consolidation (`c5de757`):** created **`core/binaryprimitives`** — the one home for fixed-width
>   BE/LE integer codecs (readers `BE16/…/LE64`, in-place `PutBE16/…`, append `AppendBE16/…`),
>   dependency-free + reflection-free. Migrated ~14 hand-rolling packages to it and deleted the locals.
>   **Restored archtest GREEN**: the red was a PRE-EXISTING `encoding/binary` import in
>   `core/appledouble` + `core/fs/fork_{ads,xattr}` (cascading to fs/share/afp/smb), masked by a cached
>   result, PLUS a stray `fmt.Fprintf` in `core/fs/codec.go` (fmt also pulls reflect). 00-DESIGN.md
>   §"No reflection in core" documents the package + the don't-re-hand-roll rule + the fmt caveat.
> - **SMB session-establishment spine (`e593271`):** `core/service/smb` — transport-independent
>   `Service.Dispatch(sess, req)` over the `core/protocol/smb` header codec: NEGOTIATE (accept NT LM 0.12,
>   WCT=17, Win9x-tuned caps), SESSION_SETUP_ANDX (guest UID=1, no credential check), TREE_CONNECT[_ANDX]
>   (bind TID → `*Share` or IPC$; unknown → STATUS_BAD_NETWORK_NAME), TREE_DISCONNECT/LOGOFF_ANDX/ECHO;
>   FS commands → STATUS_NOT_SUPPORTED until the FS-engine slice. `session.go` per-conn `smbSession`
>   (uid, TID→`treeConnect{share,ipc}`) binds `*Share` directly. Unit-tested over raw SMB frames
>   (`dispatch_test.go`). The NetBIOS→SMB session-data delivery seam is NOT wired yet (netbios.Transport
>   has Open/Close/Announce but no inbound-frame callback) — a separate slice.
> - **SMB FS command engine (this slice):** `core/service/smb` now serves the file/path/find commands
>   over the bound `*Share`'s FS, not just session establishment. `session.go` gains per-conn FID +
>   search tables (TID-disconnect and conn-end close any leaked handles). New files: `body.go`
>   (uniform WCT/words/BCC slicing + `reply`/`successNoData`/`errResponse` assembly), `resolve.go`
>   (`treeFor` TID→`*Share`, `extractWirePath` strips the 0x04 buffer-format + UTF-16 alignment pad,
>   `resolvePath` via the share codec, `mapFSErr`), `attrs.go` (DOS attr bits, the FS NTSTATUS set +
>   their DOS-form mapping in `toWireStatus`, FILETIME/allocSize), `fileio.go`
>   (OPEN[_ANDX]/CREATE/READ[_ANDX]/WRITE[_ANDX]/CLOSE/FLUSH — zero-len write truncates, read-only
>   handle → ACCESS_DENIED, READ_ANDX even-aligned DataOffset), `pathops.go`
>   (DELETE/RENAME/CREATE_DIR/DELETE_DIR/CHECK_DIR/QUERY_INFORMATION[_DISK] — idempotent mkdir,
>   non-empty rmdir → DIRECTORY_NOT_EMPTY, read-only share refuses mutation), `trans2.go`
>   (FIND_FIRST2/FIND_NEXT2 with a snapshotted per-session searchHandle + FILE_BOTH_DIR_INFO packing
>   in the request wire charset, FIND_CLOSE2, QUERY_PATH/FILE_INFO basic/std/ea/all levels),
>   `match.go` (case-insensitive DOS `*`/`?` wildcard). Every path reaches storage only through
>   `sh.FS()`; RENAME/DELETE ride the metadata-carrying `FS().Rename`/`Remove`. The legacy
>   DOS-name-mangling fuzzy resolver is **dropped** (deferred to a `core/fs` NameEngine) — see
>   spec/errata "FS command engine path resolution over the share seam". `dispatch_test.go`'s
>   not-supported probe now uses NT_CREATE_ANDX (genuinely unimplemented this slice); new
>   `fileio_test.go`/`pathops_test.go`/`trans2_test.go` drive create→write→read→close, read-only
>   denial, bad-TID, UTF-16 round-trip, mkdir/checkdir/rmdir, delete/rename, query-info, and
>   find-first2/next2 pagination over raw SMB frames. archtest + full tagged harness green.
> - **NetBIOS→SMB session-data seam (this slice):** the missing inbound-frame delivery is wired.
>   `core/service/netbios` gains the NBF (NetBEUI) session engine (`nbf.go`): `Service.NewNBFEngine`
>   builds the responder-side virtual-circuit state machine, which compose registers on the
>   `core/router/netbeui` mini-router as both its `NameHandler` (session-establishment NAME_QUERY)
>   and `SessionHandler` (SESSION_*/DATA_* frames). It answers a CALL (NAME_QUERY→NAME_RECOGNIZED),
>   completes establishment (SESSION_INITIALIZE→SESSION_CONFIRM, advertising the 1464-byte Ethernet
>   I-field), reassembles the DATA_FIRST_MIDDLE/DATA_ONLY_LAST segments of each SMB message,
>   DATA_ACKs it, and routes the whole message to the installed `SessionConsumer` — sending the
>   response back fragmented over DATA frames. SESSION_END and `Service.Stop` close the upper-layer
>   circuits so no handles leak. The seam is two small interfaces (`session.go`): `SessionConsumer`
>   (open a circuit) + `SessionCircuit` (serve a message / close); SMB satisfies them via `conn.go`
>   (`*smb.Service.NewConn` → `*Conn`, one `smbSession` per circuit) + `ConsumerAdapter`. The engine
>   reaches the wire only through a `FrameSender` seam (the mini-router's Send/SendBroadcast) and the
>   upper layer only through `SessionConsumer` — no link or SMB knowledge in either direction
>   (§3-bis command-core / session-transport split). It is the core re-home of the legacy
>   `service/netbios/over_netbeui` transport's session half, stripped of netlog + the port import.
>   `nbf_test.go` drives CALL-establishment, foreign-name ignore, data→consumer→reply over the real
>   mini-router with a recording port, segment reassembly, SESSION_END + Stop teardown; `conn_test.go`
>   proves the SMB circuit shares one session across messages and Close drains handles. cs-tinygo now
>   blank-imports `core/service/{afp,smb,netbios}` so the file services' embedded-compilability is
>   verified. Caller (CALL-out) side and the NO_RECEIVE/RECEIVE_CONTINUE flow-control + I-frame
>   retransmit machinery are an adapter-altitude reliability concern, not needed by a listening file
>   server; the responder path SMB-over-NBF depends on lands in core.
> - **NetBIOS-over-IPX (NBIPX) session transport (this slice):** the second session transport feeding
>   the same `SessionConsumer`/`SessionCircuit` seam. `core/service/netbios/nbipx.go` is the IPX
>   parallel of the NBF engine: `Service.NewIPXEngine` builds the responder-side NB-IPX session state
>   machine that compose registers on the `core/router/ipx` mini-router as the `SocketHandler` for the
>   NB-IPX session socket (0x0455, `NBIPXSessionSocket`). It accepts SESSION_INIT (→ SESSION_CONFIRM
>   carrying our connection ID, circuit keyed by peer IPX address + the remote's SourceConnID),
>   reassembles the DATA_FIRST_MIDDLE/DATA_ONLY_LAST(EOM) segments of each SMB message off the 16-byte
>   `NBIPXSessionHeader`, routes the whole message to the installed consumer, and sends the response
>   back as one EOM-flagged DATA_ONLY_LAST; SESSION_END closes the upper-layer conn + SESSION_END_ACKs.
>   It reaches the wire only through the `DatagramSender` seam (the mini-router's `Send`) and the upper
>   layer only through `SessionConsumer` — no link/router/SAP or SMB import (the legacy
>   `over_ipx` transport's session half, stripped of netlog + the router/SAP coupling). The NetBIOS
>   `Service` now tracks engines as a `circuitCloser` set (both `*Engine` and `*IPXEngine`) so `Stop`
>   tears down circuits of either transport. `nbipx_test.go` drives INIT-establishment, non-PEP ignore,
>   data→consumer→reply, segment reassembly, and SESSION_END + Stop teardown over the **real**
>   `core/router/ipx` mini-router with a recording port (compile-asserting `*IPXEngine` satisfies
>   `ipxrouter.SocketHandler`); `go list -deps ./core/router/ipx` carries no `service/netbios`, so the
>   assertion is acyclic. NB-IPX name-query/NMPI/mailslot-datagram paths stay out of this engine (they
>   are name/datagram-layer, not the session data path SMB rides). cs-tinygo already blank-imports
>   `core/service/netbios`, so the embedded-compilability of the new engine is covered.
> - **NT_CREATE_ANDX (this slice):** `core/service/smb/ntcreate.go` — the NT/2000/XP open-or-create
>   path, the one modern-Windows open a real client uses. Over the bound `*Share`'s FS it honours
>   CreateDisposition (SUPERSEDE/OPEN/CREATE/OPEN_IF/OVERWRITE/OVERWRITE_IF, gated against existence)
>   and the FILE_DIRECTORY_FILE / FILE_NON_DIRECTORY_FILE CreateOptions (opens files AND directories;
>   a directory FID carries no open fork.File). DesiredAccess maps to a read-only/read-write handle
>   the WRITE path then enforces; the WCT=34 reply packs the four NT timestamps, ext-attrs,
>   alloc/EOF sizes and the Directory flag. Storage is reached only via `sh.FS()` — no storage-layout
>   knowledge. `ntcreate_test.go` covers create/collision, open/missing, read-only-handle write
>   denial, directory create + the dir/file mismatch statuses, and bad-TID. The not-supported probe
>   now uses LOCKING_ANDX (genuinely unimplemented).
> - **NetBIOS datagram + node-status paths (this slice):** the NBF engine's `HandleFrame` now answers
>   the two connectionless responder paths alongside the session machine (`nbf_datagram.go`):
>   STATUS_QUERY → STATUS_RESPONSE (the node-status name table, built from the engine's own name set,
>   truncated to the requester's advertised buffer with the more/too-big flags — how nbtstat / browser
>   elections probe a node), and DATAGRAM / DATAGRAM_BROADCAST decoded to names+payload and routed to
>   a new optional `DatagramConsumer` seam (`SetDatagramConsumer`, the datagram analogue of
>   SessionConsumer — a browser/mailslot service plugs in there without touching the transport; until
>   one does, datagrams drop after decode). `nbf_test.go` covers status-query answer/foreign-ignore/
>   truncation and datagram deliver/drop. The NBIPX engine deliberately leaves its NMPI name-query /
>   mailslot-datagram paths to the name/datagram layer (consistent with nbipx.go scope).
> - **Capture-replay (this slice):** `core/protocol/netbios/nbipx_capture_test.go` — three real frames
>   from `captures/ipx.pcap` decode→re-encode byte-identical: frame #2 (IPX type-20 NB-IPX
>   name-service FIND.NAME for CLASSICSTACK), frame #3 (NMPI NAME_CLAIM 0xF1), frame #14 (NMPI
>   MAILSLOT_SEND 0xFC carrying the `\MAILSLOT\BROWSE` browser announcement + embedded SMB — proves the
>   header/payload split). These exercise the codec the M7 NBIPX session transport rides on. The
>   `captures/afp-*.pcap` files are **link-layer** (LLAP/DDP/AARP over LocalTalk/EtherTalk), not clean
>   AFP-command frames — the AFP request layer rides too deep (LLAP→DDP→ATP→ASP) for a standalone codec
>   golden-vector, and a DDP-layer round-trip is non-identical because the wire frame carries a DDP
>   checksum the core codec emits as zero (checksum-disabled, the legacy `AsLongHeaderBytes(false)`
>   behaviour). AFP parity stays the golden-vector + round-trip tests the command engine already
>   carries, per the M2 "atp/asp/afp ride inside DDP frames" note.
> - **§10d same-host-path AFP+SMB coordination — DEFERRED to M8a:** NOTE the model — this is NOT one
>   shared FS object. Each service keeps its **own** `shareFS` instance (it must: AFP wants the
>   AppleDouble fork engine, SMB wants the bare data fork, and each has its own filename codec), even
>   when an AFP volume and an SMB share export the **same host directory**. What they share is the
>   **event bus**: §10d (00-DESIGN.md) is "each service subscribes to the FS bus, filters by Origin to
>   skip its own events, and translates the rest into its protocol's change-notify" — one `Publish`
>   per mutation, many reactors. The mechanism (`core/fs/bus.go` `Event`/`SkipOrigin`) exists, but
>   today AFP `NewVolume` and SMB `NewShare` each call `share.Build(spec, nil)` with a **nil** bus, so
>   there is no shared bus to publish on, no second subscriber, and `shareFS` does not yet publish on
>   mutation. Recognising that two specs name the same host path and handing both `share.Build` calls
>   one common `bus.Bus` is **M8a**'s job (the config→ShareSpec mapper + supervisor). Until then,
>   publish-on-mutation + per-service Origin filtering would be unreachable code; recorded here so M8a
>   picks it up. (The separate-host-path case — the common one, e.g. AFP `Music` and SMB `Docs` on
>   different directories — needs no coordination at all: the shares cannot affect each other.)
> - **M7e — SMB direct-hosted over IPX (this slice):** `core/service/smb/directipx.go` — the
>   Microsoft "NWLink direct host" transport: SMB framed straight onto IPX socket `0x0550` (type-4
>   PEP) with NO NetBIOS layer (contrast NBIPX on `0x0455`, which rides the NetBIOS session engine).
>   It is **connectionless** — each IPX datagram carries one whole SMB message, so no reassembly — and
>   drives the SAME transport-agnostic SMB `SessionConsumer` seam (`conn.go` `NewConn`/`ServeMessage`/
>   `Close`) that NBF/NBIPX use; `*Service.NewDirectIPX(sender)` builds it. It keeps one `Conn`
>   (smbSession) per remote IPX endpoint and a server-assigned **CID** ([MS-CIFS] §2.2.1.6.4) allocated
>   on NEGOTIATE, stamped into the SMB header SecurityFeatures field of every response with the
>   request's SequenceNumber mirrored; SMB_COM_ECHO multi-response (N datagrams, incrementing seq) is
>   honoured. It reaches the IPX wire only through a local `DirectIPXSender` seam (the `core/router/ipx`
>   mini-router's `Send` satisfies it structurally), so SMB never imports the mini-router — the same
>   acyclicity discipline as the NetBIOS engines (`go list -deps ./core/router/ipx` carries no
>   `service/smb`). The SMB `Service` now tracks transports it owns directly as a `circuitCloser` set,
>   torn down on `Stop`. Re-home of legacy `service/smb/over_ipx_direct`, stripped of the netbios
>   `SessionContext` coupling + `encoding/binary`. `directipx_test.go` drives NEGOTIATE→CID-allocation,
>   circuit-shared-across-messages, ECHO multi-response, response-ingress-drop, non-SMB-drop, and
>   Stop-closes-circuits over the **real** IPX mini-router with a recording port (compile-asserting
>   `*DirectIPX` satisfies `ipxrouter.SocketHandler`). **This proves SMB-over-IPX both ways:** with
>   NetBIOS (NBIPX `0x0455`) and without (direct `0x0550`). Compose registration on the mini-router is
>   M8a (mirrors NBF/NBIPX — the engine is done; the wiring lands with the config layer).
> - **M7d — NetBIOS browser service (this slice):** the browser is broken out of legacy
>   `service/smb` into a standalone datagram-layer service (§3-ter), common to all NetBIOS
>   transports. Two new core packages: **`core/protocol/browser`** — the [MS-BRWS] wire codec as
>   self-serialising DTOs (rule #10): `MailslotTransaction` (the SMB_COM_TRANSACTION `\MAILSLOT\BROWSE`
>   envelope), `Announcement` (host/local-master), `DomainAnnouncement`, `Election` (+ `Compare`, the
>   criteria→uptime→lower-name ordering), `GetBackupListRequest`/`Response`, `AnnouncementRequest`,
>   plus `UnwrapPayload` (tolerates the Win9x 2-byte preamble) — reflection-free, `bp`-based,
>   round-trip tested. **`core/service/browser`** — the command core: a `component.Component` that
>   IS the NetBIOS `DatagramConsumer`; `HandleDatagram` unwraps the mailslot, drops self-sourced
>   loop-backs (the storm guard), records observed servers (browse list) + machine-group masters,
>   answers AnnouncementRequest, runs the election (lose→potential+silent; win→transmit loop→after
>   3 uncontested retransmits become local master + emit a local-master announcement), and answers
>   GetBackupList ONLY as local master (token echoed, sourced from our `<1D>` name). Exposes the
>   read-only `BrowseList()` / `BackupList()` query API SMB's IPC$ `\PIPE\LANMAN` `NetServerEnum2`
>   consumes. Election timers are injectable (`electionDelay`/`now`) so the machine is race-tested
>   without real-time sleeps. **Outbound seam added to `core/service/netbios`:** `Service.SendDatagram`
>   fans a `Datagram` to every transport's `datagramEgress`; the NBF engine emits a
>   `CmdDatagram[Broadcast]` UI frame — the outbound mirror of `DatagramConsumer`. The browser imports
>   `core/service/netbios` only for the two seam types; `go list -deps ./core/service/netbios` carries
>   no `service/browser` (acyclic). cs-tinygo blank-imports both new packages. archtest green (the new
>   core packages are reflection/net/binary-clean).
> - **M7d-d — NBIPX datagram-egress (this slice):** the browser now also broadcasts over IPX, not just
>   NetBEUI. `*IPXEngine` gains `emitDatagram` and registers as a `datagramEgress`, so
>   `Service.SendDatagram` fans the browser's HostAnnounce/election/backup-list to NBF AND NBIPX. The
>   NBIPX egress wraps the browser's SMB mailslot payload in an NMPI MailslotSend (opcode 0xFC), IPX
>   type-20 broadcast on the datagram socket (0x0553), with the source/destination NetBIOS names in the
>   NMPI header (group dest → workgroup name-type). Like NBF it fans to the IPX broadcast node (no
>   name→node binding for an out-of-band send). Re-home of the legacy `over_ipx` `sendNMPIDatagram`.
>   `nbipx_test.go` proves `SendDatagram` emits the NMPI MailslotSend with names + payload round-tripped.
>   The browser is now transport-complete: it serves over both NetBEUI and IPX.
> - **Layering correction (→ M7f, §3-quater):** review flagged that the M7d browser marshals/unmarshals
>   the `\MAILSLOT\*` SMB_COM_TRANSACTION envelope itself (`core/protocol/browser` MailslotTransaction,
>   used in `service/browser/handle.go`). That envelope is a SHARED mailslot framing, not browser-
>   protocol — other consumers want it too (`\MAILSLOT\MESSNGR` net-send, future DirectPlay). M7f lifts
>   it into `core/protocol/mailslot` + a mailslot dispatch layer (Consumer-by-name + SendMailslot over
>   the NetBIOS DatagramConsumer/SendDatagram seams); the browser is reworked to handle ONLY browser
>   frames. Per-NetBIOS-transport framing (NBF UI-frame / NBIPX NMPI-MailslotSend) ALREADY lives
>   correctly in `core/service/netbios` — that part of M7d/M7d-d stands; only the mailslot-envelope
>   layer moves out of the browser.
> - **M7g — messenger service landed (this slice):** the §3-quater seam is proven multi-consumer.
>   New `core/protocol/messenger` is the [MS-MSRP] single-block "net send"/WinPopup frame codec (a
>   self-serialising `Message{From,To,Text}` DTO: type byte `0x01` + three NUL-terminated OEM strings);
>   no live capture exists (`/captures` has none), so per CLAUDE.md rule 6 the wire layout is documented
>   from [MS-MSRP] + the long-stable WinPopup form and the parser tolerates a missing trailing NUL. New
>   `core/service/messenger` registers for `\MAILSLOT\MESSNGR` on the mailslot router as a second
>   `mailslot.Consumer` alongside the browser — it holds **zero** mailslot-envelope and zero transport
>   code (mirrors the browser's `MailslotSink`). On receive it decodes, **logs at Info** ("net send
>   received", typed from/to/text), and **publishes `bus.MessageReceived` on the new `bus.TopicMessage`**
>   so a UI can display net-send events (the user's ask). The send half (`Service.SendMessage` → a
>   directed `\MAILSLOT\MESSNGR` write) is the core a future `cmd/csnetsend` (T1) wraps; the standalone
>   binary + transport Link is deferred to T1 (user chose "core send half only"). `core/protocol/netbios`
>   gains `NameTypeMessenger` (`<03>`). cs-tinygo blank-imports both new packages; archtest green;
>   `go list -deps ./core/service/netbios` still carries neither messenger package (acyclic).
> - **M7f — mailslot seam landed (this slice):** the layering correction is done. New
>   `core/protocol/mailslot` holds the `\MAILSLOT\*` SMB_COM_TRANSACTION envelope codec (a
>   self-serialising `Write` DTO + the well-known `NameBrowse`/`NameLANMAN`/`NameMessenger` consts),
>   lifted verbatim out of `core/protocol/browser` — and the lift surfaced + fixed a latent bug: the
>   data offset was a fixed 86, which overran for any mailslot name longer than `\MAILSLOT\BROWSE`
>   (e.g. `\MAILSLOT\MESSNGR`); it now tracks the name length. New `core/service/mailslot` is the
>   dispatch layer: a `Router` that IS the NetBIOS `DatagramConsumer` (unwraps the envelope, routes the
>   bare body by mailslot name, case-insensitive, to the registered `Consumer`) and exposes
>   `SendMailslot(name, src, dest, body, broadcast)` (wraps + `SendDatagram`). `core/service/browser`
>   is reworked: it is now a `mailslot.Consumer` (`HandleMailslot`, registered for `\MAILSLOT\BROWSE`)
>   and sends through a `MailslotSink` — it holds **zero** mailslot-envelope code and zero transport
>   code; `MailslotTransaction` is deleted from `protocol/browser`. The browser imports
>   `core/service/netbios` only for the seam types via the mailslot layer; `go list -deps
>   ./core/service/netbios` carries neither `service/mailslot` nor `service/browser` (acyclic). All
>   four packages (protocol/service × mailslot/browser) race-tested green; cs-tinygo blank-imports both
>   new packages; archtest green. A future `\MAILSLOT\MESSNGR` messenger (M7g) plugs into the same
>   Router as a second consumer with no browser/SMB coupling.
> - **M7d-b — SMB IPC$ NetServerEnum2 consumer (this slice):** the SMB side of the browser query.
>   `core/service/smb/lanman.go` adds the `SMB_COM_TRANSACTION` dispatch case: a TRANSACTION on the
>   IPC$ pipe whose byte area names `\PIPE\LANMAN` + RAP function `NetServerEnum2` (0x0068) is answered
>   from the browse list. SMB asks the browser through a `BrowseProvider` seam (`Available()` +
>   `ServerEntries() []BrowseServer`, `SetBrowseProvider`) — a small local interface the browser
>   satisfies structurally (`browser.Available()`/`ServerEntries()`), so SMB imports no browser package
>   (a one-line `[]browser.ServerEntry`→`[]smb.BrowseServer` adapter is M8a compose wiring, alongside
>   `SetDatagramConsumer`/`SetSessionConsumer`). A potential browser → ERROR_REQ_NOT_ACCEP (71);
>   DOMAIN_ENUM mixed with other type bits → ERROR_INVALID_FUNCTION (1); the RAP reply packs
>   SERVER_INFO_1 records + comment heap in the TRANSACTION param/data blocks. A TRANSACTION on a
>   non-IPC$ tree, or with no browser wired, answers STATUS_NOT_SUPPORTED / empty-success rather than
>   dropping. `lanman_test.go` covers the browse-list reply, the potential-browser + domain-enum gates,
>   no-provider empty success, and the non-IPC$ refusal.
> - **M7d-c — SMB IPC$ NetShareEnum (this slice):** the share-list RAP call (function 0x0000) over the
>   same `\PIPE\LANMAN` pipe, answered straight from SMB's own state — no browser involved. The
>   TRANSACTION dispatch now switches on the RAP function; NetShareEnum packs a SHARE_INFO_1 record
>   (Name(13)+Pad(1)+Type(2)+RemarkOff(4)=20) per bound disk share (STYPE_DISKTREE, remark =
>   `Share.Description()`, a new accessor over the held `*share.Share`) plus the always-present virtual
>   IPC$ pipe (STYPE_IPC). `lanman_test.go` proves both records (PUBLIC + IPC$) with their names/types
>   in the data block. This is what a client's "browse this server's shares" actually queries, so the
>   IPC$ RAP layer now answers both the inter-server browse list and the per-server share list.
> - **Remaining M7 (deferred, not blocking M7 close):** the byte-range LOCKING_ANDX / MPX / raw-read-
>   write SMB paths answer STATUS_NOT_SUPPORTED — left until a target client needs them (no identified
>   client does). Legacy `service/{afp,smb,netbios}` deletion is strangler step 5 but is **blocked**:
>   those packages are still imported by the live `internal/app` runtime (afp_enabled / smb_enabled /
>   netbios_enabled hooks + asp/dsi), so deletion happens at the **M8/M8a compose cutover → M10**, not
>   in M7. TCP transports are M7a (`adapter/dsi`) / M7b (`adapter/smbtcp`) / M7b2 (`adapter/netbios-tcp`).

> **M8a notes (auth slice landed — partial M8a):** the authentication seam the design lacked is
> in. **`core/auth`** (always-compiled, reflection-free — archtest- + TinyGo-gate-clean): the
> `Authenticator`/`UserStore` contract (`User` DTO carries no secrets) + a hand-rolled
> PBKDF2-HMAC-SHA256 credential codec (`DeriveCredential`/`Verify`/`SaltHex`/`ParseCredential`) over
> `crypto/hmac`+`sha256`+`subtle` only. **Salt generation (`crypto/rand`) and the file store moved to
> the ADAPTER ring** — `adapter/auth/local` (`Open(path)` smbpasswd-style `name:saltHex:hashHex:flags`
> users file, atomic temp+rename writes, case-insensitive names) — because `crypto/rand` AND
> `encoding/hex` both transitively pull `reflect` (banned in core); core hand-rolls hex and takes the
> salt as a parameter. **`core/share`**: the `Permissions` stub became real (`AllowedUsers` +
> `Allows`/`AllowsGuest`; empty list = guest/world default), lifted from a new
> `fs.ShareSpec.AllowedUsers` in `share.New`, surfaced on `share.Info`. **Gate is at LOGIN** (per the
> client reality: legacy AFP/SMB log in once under one identity, then bind shares — no per-share
> re-auth): AFP `FPLogin` parses the cleartext user/pass (previously dropped) and validates via a
> local `Authenticator` seam (nil = guest, the old behaviour), then the identity filters
> `FPGetSrvrParms` + gates `FPOpenVol`; SMB `SESSION_SETUP_ANDX` parses the AccountName (NT WCT=13 /
> LM WCT=10), validates cleartext (hashed LM/NTLM → accept-as-guest, errata noted), then the identity
> filters `NetShareEnum`/`NetServerEnum2` + gates `TREE_CONNECT`. **Control plane**: `Plane` gained
> `Users()`/`SetUser`/`SetUserDisabled`/`RemoveUser` (the web UI's user CRUD), backed by an optional
> `control.UserAdmin` the supervisor satisfies via a wired `auth.UserStore` (`SetUserStore`; nil →
> `ErrUnavailable`, the Diagnostics "not in this build" shape). Share allow-lists ride the existing
> `Config()`/`Reconfigure` path (no new Plane method). **`config.AuthSection`** (`Backend`/`Path`, no
> secret fields — secrets live in the dedicated users file) + `compose/registry/reg_auth.go`
> (`//go:build afp||smb||all`: `BuildUserStore(m)` + section registration). **Still M8a, NOT done by
> this slice:** the AFP/SMB volume/share config sections + `config→ShareSpec` mapper (`allowed_users`
> → `ShareSpec.AllowedUsers`), the supervisor assembly that actually calls `SetAuthenticator`/
> `SetUserStore`/`Manager.Add` (no compose root wires services into each other yet — the registry
> factories are still zero-config stubs), server identity (§4-bis), and the §10d shared-bus
> coordination. The HTTP/ubus `/api/users` front-ends + SPA Users panel are M8/webui (this slice
> delivers the Plane methods they bind to). **Deferred (tagged adapters, future):** PAM / Windows-SSPI
> / sqlite user stores under `adapter/auth/*`; file-level ACLs / per-user read-only; AFP DHX & SMB
> NTLM challenge UAMs.

> **M8a notes (AFP volume config sections — partial M8a):** the `config→[]fs.ShareSpec` mapper
> for AFP is in, as **repeated named sections** (the operator writes one block per volume, the
> idiomatic UCI/TOML form). **`core/config` gained a MultiSection concept**: a `SectionSchema`
> may set `Repeated: true`; repeated instances live in a new `Model.Lists[key][]Section` (parallel
> to singleton `Sections`), each distinguished by a `NamedSection.InstanceName()`; Model gained
> `List`/`SetList`/`AddInstance` (replace-by-name)/`Instance`/`RemoveInstance`, and `Clone`
> deep-copies the lists. Pure stdlib — archtest + TinyGo gates stay green. **Both codecs round-trip
> repeated sections**: TOML as an array-of-tables under the lowercased key (`[[afpvolumes]]`), UCI
> as repeated `config <type> '<name>'` blocks (the UCI block name is authoritative on read — a
> divergent inner `option name` is reconciled to it). **`core/service/afp`**: `VolumeSection` (a
> flat, codec-friendly NamedSection view of `fs.ShareSpec` — typed `path`/`fs_type`/`fork_backend`/
> `filename_codec`/`name_engine`/`metastore`/`read_only`/`allowed_users`, plus an `options` list of
> `key=value` entries → `ShareSpec.Extra` for backend-specific params) + `Spec()`/`SpecsFromModel`
> mapper; `RegisterVolumes()` (called from `reg_afp.go`, like `auth.Register`, so the section
> exists exactly when AFP is built). **`reg_afp.go`** now builds one Volume per configured section
> via `NewWithVolumes` (allocating ids 1..N), failing loudly on a bad spec; a model with no volumes
> yields the historical zero-volume service. The AFP service already carried the full `share.Manager`
> surface (`AddShare`/`UpdateShare`/`RemoveShare`/`Shares`) from M7c, so dynamic add/update/remove
> is in place. **Still M8a, NOT done by this slice:** the SMB **share** config sections (same
> mapper, SMB-side — landed in the follow-on below), the supervisor `Reconfigure` path that drives
> `share.Manager` Add/Update/Remove from a changed volume section (the registry builds the full set
> at boot/rebuild today; per-section hot-apply of one volume is the next step),
> `ParamsFor`-generated per-fs_type form masking of `secret` params, server identity (§4-bis), and
> the §10d shared-bus coordination.

> **M8a notes (SMB share config sections — partial M8a):** the SMB-side mirror of the AFP-volume
> slice, reusing the `core/config` repeated-section machinery. **`core/service/smb`**: `ShareSection`
> (the same flat NamedSection field shape as `afp.VolumeSection` — typed `path`/`fs_type`/
> `fork_backend`/`filename_codec`/`name_engine`/`metastore`/`read_only`/`allowed_users` + an
> `options` `key=value` list → `ShareSpec.Extra`, plus one SMB-specific field: `description`, the
> NetShareEnum remark, which AFP volumes have no equivalent for) + `Spec()`/`SpecsFromModel` +
> `RegisterShares()` (called from `reg_smb.go`). `smb.ShareSpec` gained a `Description` field and
> `NewShare` applies it via `built.SetDescription` (description is SMB-specific, NOT carried on
> `fs.ShareSpec`). **`reg_smb.go`** now builds one Share per configured section via `NewWithShares`,
> failing loudly on a bad spec; a model with no shares yields the historical zero-share service. The
> SMB `share.Manager` surface (Add/Update/RemoveShare) was already in from M7c. Both file services
> are now config-driven through the same repeated-section mechanism. **Still M8a:** the supervisor
> `Reconfigure`→`share.Manager` hot-apply path (landed in the follow-on below), server identity
> (§4-bis), §10d shared-bus.

> **M8a notes (supervisor share hot-apply — partial M8a):** the `Reconfigure`→`share.Manager`
> hot-apply path the two config-section slices enabled. Both file services now implement
> `component.Configurable`: **`ApplyConfig` ignores the passed section** (the file-service "config"
> is the *set* of repeated volume/share sections in `config.Model.Lists`, not a singleton section)
> and instead **re-resolves the whole desired set from the model and reconciles** it against the
> live shares via `share.Manager` — `afp.Service.ReconcileVolumes` / `smb.Service.ReconcileShares`,
> keyed by name (case-insensitively for SMB, as tree-connect matches): add new, update changed
> (rebuild that one share's stack — AFP preserves the volume's protocol-assigned id across an
> update), remove dropped. Reconcile is **all-or-nothing**: it builds the full desired set before
> swapping, so a bad triple/param in one section aborts the reconcile leaving the live shares
> untouched. The model→spec closure is wired by the registry (`reg_{afp,smb}.go` `SetVolumeResolver`/
> `SetShareResolver`, closing over the model and `SpecsFromModel`); **no resolver wired (a unit-level
> service) → `ApplyConfig` returns `ErrNeedsRestart`** so the supervisor falls back to its rebuild
> path. So editing one share in the UI now reconciles live (no AFP/SMB restart, in-flight sessions
> undisturbed) instead of rebuilding the whole service. **Still M8a:** server identity (§4-bis,
> landed in the follow-on below), §10d shared-bus; `secret`-param form masking (mostly UI/M8).

> **M8a notes (server identity §4-bis — partial M8a):** the one-source-of-truth server identity.
> **`core/config/identity.go`**: `config.Identity{Hostname, Workgroup, Description}` is a well-known
> top-level `Model` field (alongside Logging/Router/Bridge — NOT on the SMB/NetBIOS section), value
> type, rides `Model.Clone`. **Description** (user-requested) is the free-text server comment a
> Windows browse list shows next to the name — NOT NetBIOS-constrained. `Validate()` = baseline
> (no path/control chars); `ValidateForNetBIOS()` = the ≤15-byte rule as a CONSUMER constraint
> (run only when NetBIOS is enabled — a 20-char name is legal for SMB-:445 / AFP-only); `NetBIOSName()`
> = upper-cased+trimmed (over-length is a validate failure, not silent truncation). **Codecs:** both
> TOML and UCI round-trip an `identity` well-known section (`adapter/config/{toml,uci}`), covered in
> the existing well-known round-trip tests. **Consumers wired by the registry (one read, no
> divergence):** `reg_smb.go` → `SetServerName`/`SetWorkgroup`/`SetDescription` (SMB now self-reports
> name+comment in NetServerEnum2 even with NO browser/NetBIOS — the no-provider branch returns the
> self entry, covering direct-TCP :445); `reg_netbios.go` → `netbios.NewService(logger, NetBIOSName())`
> when the hostname is non-empty (else the nameless `New`); the browser carries `Description` on its
> self `ServerEntry` via a new `SetDescription` (browser isn't registry-wired yet — M8/M10 compose —
> but the setter + self-entry comment are in). **No per-service hostname field exists**, so SMB and
> NetBIOS cannot diverge. **NOTE (now resolved — see the Model.Validate note below):** when this
> slice landed there was no central Apply-time hook, so `ValidateForNetBIOS` was defined but uncalled;
> that gap is now closed. **Still M8a:** §10d same-host-path AFP+SMB shared-bus (landed in the
> follow-on below); `secret`-param form masking (UI/M8).

> **M8a notes (§10d same-host-path AFP+SMB coordination — coordination seam landed, wire push
> deferred):** when an AFP volume and an SMB share back the SAME host path, a mutation by one now
> reaches the other through one shared FS-mutation bus. **(A) Shared bus per host path:** the registry
> holds an `fsBusBroker` (`compose/registry/fsbus.go`) handing one `fs.Bus` per distinct host path
> (normalised case-fold/trailing-slash); both file-service factories resolve through it via
> `SetBusResolver(fsBus.busFor)`. Threaded through `share.Build` by new `afp.NewVolumeWithBus` /
> `smb.NewShareWithBus` (bus-less `NewVolume`/`NewShare` kept for tests/zero-config); the registry now
> builds the initial set through the reconcile path so the shared bus applies from boot. **Origin
> stamping:** `fs.OriginBus(b, origin)` wraps the bus to stamp "afp"/"smb" (`afp.OriginAFP`/
> `smb.OriginSMB`) onto each `fs.Event`, forwarding to the same underlying bus. **(B) FS publishes:**
> `local_fs` publishes OpCreate (CreateDir/CreateFile), OpModify (write-then-Close, coalesced — a
> read-only open stays silent), OpRename (+OldPath), OpDelete, with the absolute host path; `memfs`
> doesn't publish (no shared store). **(C) Reactor:** `share.Reactor` (`core/share/reactor.go`)
> subscribes per distinct bus, drops its own Origin (`fs.SkipOrigin`), resolves the affected share(s)
> by host-path prefix (rename matches either end), and delivers `(share, event)` to a notify sink;
> each service builds one in `New`, subscribes in `Start`, stops in `Stop`; `ReactorDelivered()` is the
> observable. Tests cover OriginBus stamping/shared-underlying, local_fs publish-on-mutation, the
> reactor filter+resolve+stop, broker dedup, and an **end-to-end** `compose/registry` test (AFP+SMB on
> one path, AFP creates → SMB notified, AFP's own reactor not). **DEFERRED to its own slice — the wire
> push:** the notify sink is a no-op counter; it does NOT emit AFP attention or SMB CHANGE_NOTIFY
> frames. SMB's `conn.go` `ServeMessage(req)→reply` seam has no server-initiated channel (real
> CHANGE_NOTIFY needs a new async-push contract across NBF/NBIPX/NBT/direct), and classic AFP has no
> per-dir change-notify (only volume-mod-date polling + server-message attention). The coordination
> plumbing is complete; turning the resolved notification into wire frames is the follow-on. **Still
> M8a:** `secret`-param form masking (UI/M8). **Also pending:** §10e host-watcher (fsnotify) inbound
> edge — an adapter that publishes external mutations onto the same bus; the reactors fire for free.

> **M8a notes (§10d wire push — SMB CHANGE_NOTIFY landed; AFP excluded):** the deferred wire-push half
> of §10d, **SMB only**. The session seam gained a server-initiated push channel: `Conn.SetPushWriter`
> (on both `smb` and `netbios` `SessionCircuit` interfaces); each transport installs a push closure
> after `NewConn` — NBF via `sendSessionData`, NB-IPX via a new `pushData` over the circuit's retained
> net/node/sock+conn-ids, direct-IPX via a new `pushResponse` stamping the circuit CID. SMB now serves
> `NT_TRANSACT (0xA0)` `NOTIFY_CHANGE (0x0004)` (`core/service/smb/notify.go`): parse the Setup,
> register a held `pendingNotify` on the session (ids + bound share), return **nil** (held open, not
> answered). The reactor sink `notifyFSChange` (now wired into `share.Reactor` in place of the no-op)
> completes every held watch for the changed share by pushing one `FILE_NOTIFY_INFORMATION` record
> (FILE_ACTION_* from the fs.Op + the changed leaf in UTF-16LE) over the circuit; one-shot per
> [MS-CIFS], share-coarse granularity (client re-reads). NOTIFY_CHANGE on IPC$/unbound tree is refused
> (not held). The SMB service tracks live sessions (`NewConn`/`Close` register/unregister) so the
> reactor fans completions to every watching circuit. **AFP is EXCLUDED by protocol** — classic AFP
> has no per-directory change-notify push (clients poll the volume mod-date; the only ASP attention
> codes are shutdown/crash/message), so its reactor sink stays nil (ReactorDelivered is the observable,
> no wire frame). Tests: `notify_test.go` (NT_TRANSACT parse, held-then-completed, one-shot,
> no-watch-no-push, IPC$-refused) + `nbf_test.go` server-push delivery. **Still pending:** §10e
> host-watcher (fsnotify), then the SMB push fires for external edits too. **Still M8a:** `secret`-param
> masking (UI/M8).

> **M8a notes (§10e host-watcher — landed):** the inbound edge of the FS bus. `adapter/fswatch`
> (build-tagged `fswatch || all` + a no-tag stub so a tag-less build links no fsnotify). `fswatch.Watcher`
> is a `component.Component`: Start opens an `fsnotify.Watcher`, walks each host root adding every
> subdir (fsnotify watches dirs not trees; a new dir is added on its OpCreate), and the loop maps each
> fsnotify op → `fs.Op` (Remove>Rename>Create>Write>Chmod) and publishes `fs.Event{Origin:"fsnotify"}`
> (new const `fs.OriginFSNotify`) on the path's bus. Origin is neither afp nor smb, so BOTH reactors
> fire (no SkipOrigin match) — an external edit notifies every client; SMB completes held NOTIFY_CHANGE,
> AFP observes. Wiring: `config.HostPathProvider` + `Model.HostPaths()` (impl by afp.VolumeSection /
> smb.ShareSection — decoupled, untagged) collect distinct roots; `registry.BuildHostWatcher(m, logger)`
> builds over `fsBus.busForPath` (same per-host-path bus, keyed identically to busFor). `fsbus.go` tag
> widened to `afp || smb || fswatch || all` so the broker exists for a fswatch-only build. cs-tinygo
> confirmed to NOT pull fsnotify (build-tag isolation). Tests: `adapter/fswatch` (mapOp precedence,
> real-fsnotify publish with Origin/HostPath/Op, idempotent Start/Stop, missing-root-skipped) +
> `core/config` HostPaths dedup. The §10d/§10e pair is now complete. **Still M8a:** `secret`-param
> masking (UI/M8); wire a central `Model.Validate()` Apply hook (calls `Identity.ValidateForNetBIOS`).

> **M8a notes (Model.Validate Apply hook — landed; closes the §4-bis caveat):** the whole-model
> validation the commit path runs. `config.Model.Validate(config.ValidateOptions)` (core/config):
> runs `Identity.Validate` (baseline), then every registered section's `Validate` (singletons in
> `Sections` + each repeated instance in `Lists`, via the schema's `Validate` when registered else the
> section's own — codecs do NOT call schema.Validate, so this is the real validation entry point),
> then `Identity.ValidateForNetBIOS` **only when `opts.NetBIOSEnabled`**. `ValidateOptions` carries the
> cross-cutting facts the model can't infer (NetBIOS has no config section — it's enabled by being
> built/wired); the zero value = no consumer constraints (right for SMB-:445 / AFP-only).
> `control.Plane.Save` calls Validate before `codec.Marshal`, deriving `NetBIOSEnabled` from the
> supervisor `Status()` (a `NetBIOS` unit's `Enabled`; matched by the string `"NetBIOS"` so core/control
> imports no service pkg). An invalid section / over-length-under-NetBIOS hostname is now rejected
> before it reaches the store. Tests: `core/config` (Validate happy/bad-identity/bad-section/bad-repeated,
> NetBIOS-gated) + `core/control` (Save rejects bad hostname; NetBIOS rule gated on enabled/disabled/absent).
> **Remaining M8a:** `secret`-param form masking (UI/M8) — the last core M8a item.

> **M8 notes (config-codec round-trip + UCI fix — partial M8):** the TOML/UCI codecs and the
> file/UCI stores were already built (B6/D4/D6); this slice adds the missing **real-section**
> round-trip coverage and fixes a latent codec bug it surfaced. The new M8a `Auth` section now has
> explicit round-trip tests through **both** codecs (`adapter/config/{toml,uci}/auth_roundtrip_test.go`)
> plus an end-to-end **codec→file.Store→codec** persistence test (the path the control plane's
> config-apply drives), proving the store selector a user writes is what `auth.SectionFromModel`
> reads back. **Bug fixed:** the UCI tokenizer dropped an empty quoted value (`option key ''`), so a
> default `config.Model` — whose well-known `Logging.Level` is `""` — could not be reloaded through
> UCI (the whole `Unmarshal` failed on the short option line); the tokenizer now emits an empty
> token when a quote was opened. Documented in `spec/errata.md` "UCI empty-quoted-value tokenizer".
> **Still M8, NOT done by this slice:** the logging cutover (live `internal/app`/`pkg/logging`/
> `netlog` onto `core/log` + bus sink) is blocked on the M8/M10 compose cutover; the HTTP/ubus
> control front-ends + SPA; and the AFP/SMB **volume** config sections / multi-share UCI named
> sections (M8a, the `config→[]fs.ShareSpec` mapper).

> **M8 notes (bus log sink — partial M8):** the `log` telemetry-topic SOURCE is in:
> **`adapter/log/bus`** is a `core/log.Sink` that republishes each log `Record` as a
> `bus.LogRecord` on `bus.TopicLog`, translating `core/log.Field` → `bus.Field` (typed, no
> reflection). This is what the control plane's `Subscribe("log")` → SSE/ubus log viewer consumes —
> the new-ring equivalent of the legacy `pkg/logbuf` broadcaster. It lives in the **adapter ring by
> design** (§6c: "the bus sink is just one sink — the logger does not depend on the bus"), so
> `core/log` stays bus-free (verified: `go list -deps ./core/log` carries no `core/bus`) and a CLI /
> embedded build can log to stderr/UART with no bus linked. Handles the logger's scratch-buffer
> field aliasing (copies fields into the published event), retunes its threshold live via a
> `*LevelVar`, and no-ops on a nil bus. Race-tested. **The actual logging CUTOVER** — pointing the
> live runtime's loggers at this sink + a stderr/file sink, retiring `netlog`/`pkg/logging`/
> `pkg/logbuf` — is still gated on the M8/M10 compose cutover (can't run while `internal/app` is the
> live runtime); this slice delivers the sink that cutover will install.

> **M8 notes (control front-ends catch-up — partial M8):** the http/ubus/inproc control adapters had
> drifted behind the `control.Plane` contract (they covered only status/start/stop/restart/reconfigure/
> list_fs_types/subscribe). This slice brings all three up to the full Plane surface: **`Config`,
> `Save`, `ListInterfaces`, `ListZones` (the Diagnostics probe), and the Users CRUD (`Users`/`SetUser`/
> `SetUserDisabled`/`RemoveUser`)**. The shared `inproc.Client` interface — the contract the E3 parity
> test drives all three through — gained those methods; inproc forwards straight to the Plane, http adds
> routes+handlers+client methods, ubus adds JSON-RPC method cases+client calls. **`Save` now runs
> `Model.Validate` server-side** (the M8a hook), so an invalid config is rejected at the front-end.
> **`control.ErrUnavailable` round-trips** as a recognisable sentinel: http maps it to HTTP **501** (client
> reconstitutes it via `errForStatus`), ubus matches the error string (`errFromUbus`), so a UI can
> `errors.Is(err, control.ErrUnavailable)` the same way over every transport — the "not in this build /
> no store wired" shape the Users/Diagnostics methods carry. Tests: `parity_test.go` gained
> `TestMultiFrontEndParity_NewMethods` (Config/ListFSTypes/ListZones/Users ErrUnavailable parity across
> all three) and `TestMultiFrontEndParity_UserCRUD` (full add→list→disable→remove cycle round-tripped
> across http+ubus+inproc against a user-store-bearing supervisor). **Still M8:** the SPA / web UI in the
> new ring (none exists in `adapter/` yet; legacy `service/webui` is old-ring); the logging cutover
> (blocked on M10). **Still M8a:** `secret`-param form masking (UI/M8).

> **M8a notes (secret-param masking — landed; the last core M8a item):** the `fs.Param.Secret` flag
> now actually redacts on the management boundary. **`config.SecretMasker`** (core/config) is the
> optional capability a Section implements when it carries secret-valued fields — `MaskedClone()`
> (clone with secrets → `config.RedactedSecret` `"********"`) and `Unmask(prev)` (clone restoring any
> still-sentinel field from the live stored section). `Model.MaskSecrets()` clones the model and masks
> every SecretMasker section. **`control.Plane.Config()`** now returns `MaskSecrets()` — a secret never
> leaves the process in clear — and **`Reconfigure`** unmasks the inbound section against the live one
> (resolved as a singleton by Key, or a repeated instance by `InstanceName`) **before** delegating, so a
> blind UI round-trip (resubmitting the placeholder) restores the stored secret instead of overwriting it;
> a genuine edit (any non-sentinel value) is kept. The secret knowledge stays in the sections that own
> their `fs_type`: **`afp.VolumeSection`** + **`smb.ShareSection`** implement SecretMasker via two
> `core/fs` helpers, **`fs.MaskSecretOptions`/`fs.UnmaskSecretOptions`**, which consult `fs.ParamsFor`
> for which `Options` keys are `Secret` (an empty value stays empty — "unset" vs "hidden" — case-
> insensitive key match). core/config and core/control carry **no** fs-type knowledge (structural
> interface, like `HostPathProvider`); reflection-free, archtest + both TinyGo amd64 gates green. Tests:
> `core/fs` (mask/unmask/round-trip/no-secrets/no-prior), `core/service/{afp,smb}` (section
> MaskedClone/Unmask + edit-kept), `core/control` (Config masks + live model untouched; Reconfigure
> unmasks a blind round-trip and passes an edit through). **This closes the core M8a slice list.** The
> SPA's secret-input form hint (render a password field for a `Secret` param) is the only remaining piece
> and is M8/webui (front-end markup over the already-exposed `ListFSTypes`/`ParamsFor` schema).

> **M8 notes (web-admin Basic auth + first-run setup — landed):** the new-ring HTTP control adapter
> ([adapter/control/http](adapter/control/http)) was world-open; it is now gated by a single web-admin
> credential over **HTTP Basic auth** (no sessions/JWT — honest-security posture). **`config.AdminAuth`**
> (§4-ter) is a new well-known typed `Model` field (peer of `Identity`): `User` + salted PBKDF2-SHA256
> `SaltHex`/`HashHex`, round-tripping through TOML+UCI into `server.toml` (`[adminauth]` emitted only once
> `Configured()`). It stores a **hash, never plaintext**, and is deliberately NOT a `SecretMasker` field
> (a hash is not a reversible secret, and masking would break Verify on a `Config()` round-trip).
> `AdminAuth.Verify` uses pure `core/auth` helpers (no `crypto/rand`) so `core/config` stays
> reflection-free. **Ring split:** salt generation lives in `adapter/control/http` (`/setup` derives the
> hash); `control.Plane.SetAdmin` (+ `Supervisor.SetAdminAuth`) stamps the hash-only DTO into the model
> and auto-saves via the existing Save path. **This required moving the file-service `Auth` section out of
> `core/auth` → new `core/auth/authsection`** to break a `config→auth→config` cycle (core/auth's
> contract+PBKDF2 stay config-free/TinyGo-clean). **Gate (`authGate`):** first-run → every route but
> `POST /setup` returns 409 `{"setup_required":true}`; post-setup → `/setup` sealed (409), all routes
> require Basic creds (401 + `WWW-Authenticate` on miss, constant-time verify). HTTP client gained
> `NewClientWithAuth` (Basic-auth RoundTripper covering SSE too) + `Setup`/`SetupRequired`. **Caveat
> documented:** Basic auth is base64 not encrypted + the adapter has no TLS → loopback/TLS-terminated
> only. Tests: `core/config` (AdminAuth Verify/Validate/Configured/Clone), TOML+UCI `[adminauth]`
> round-trip, `core/control` (SetAdmin stamps+persists, AdminConfigured, rejects-invalid),
> `adapter/control/http` (first-run 409, /setup persists `server.toml` with hash & no plaintext,
> post-setup 401/200, /setup-refused-once-set, authed client round-trip), parity tests seed an admin +
> use the authed client. **Still M8:** legacy `service/webui` stays unauthed (old ring, M10); TLS for the
> new-ring HTTP adapter; the SPA (which binds to this `/setup` + 409/401 contract).

---

## How to claim a task
1. Put your name/handle in **Owner**, set status 🟡.
2. Work only within the step's stated scope; honour its "must not" constraints.
3. Keep `go build ./... && go test ./...` green; add the step's acceptance test.
4. Set ✅ and open a PR referencing the step id (e.g. "refactor: B3 bus primitive").
