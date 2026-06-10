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
> `reflect`, so the archtest gate rejects any core/ package that imports it. Hand-roll
> big-endian helpers instead (see `core/protocol/ddp` `appendBE16`/`be16` and
> `core/metastore` `putBE32`/`be32`). `encoding/binary` is now an explicit entry in the
> archtest forbidden list. B2/B5/B8 do byte work — they must follow the same pattern.
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
| M5 | DDP services (MacIP/IPXGW/AEP/NBP) + stats publish | M4 | | ⬜ |
| M6 | Storage seam: unified FS, metastore, fork engines (SFM/Netatalk interop), name engines, **filename codecs** (MacRoman/reserved, from path_codec.go) | Phase 1 (B8/B9) | | ⬜ |
| M7 | File services AFP/SMB/NetBIOS over fs/metastore + Attachable transports | M6,M2 | | ⬜ |
| M8 | Logging cutover + control front-ends (http, ubus) + config codecs (toml/uci) | M5,M7 | | ⬜ |
| M9 | Platform integration (Windows svc / launchd / systemd / procd) | M8 | | ⬜ |
| M10 | cmd cutover + delete `internal/app`/`*_disabled.go`; docs | M1–M9 | | ⬜ |
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

---

## How to claim a task
1. Put your name/handle in **Owner**, set status 🟡.
2. Work only within the step's stated scope; honour its "must not" constraints.
3. Keep `go build ./... && go test ./...` green; add the step's acceptance test.
4. Set ✅ and open a PR referencing the step id (e.g. "refactor: B3 bus primitive").
