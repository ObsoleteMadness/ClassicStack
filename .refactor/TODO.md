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
| M7a | `adapter/dsi` (AFP-over-TCP `:548`): re-home `service/dsi` onto AFP command core's CommandHandler; `//go:build dsi`; net only here (§1/§3-bis) | M7 | | ⬜ |
| M7b | `adapter/smbtcp` (SMB-over-TCP `:139`/`:445`) onto SMB command core; `//go:build smbtcp`; net only in adapter | M7 | | ⬜ |
| M7c | `core/share`: thin Share descriptor (Name/FS/Config/ReadOnly/Description/Permissions-stub) + `Manager` CRUD; AFP `Volume` & SMB `Share` hold the shared Share; both services implement `share.Manager` (add/update/remove; RemoveShare keeps in-flight sessions) — contract + tests, supervisor wiring is M8a (§9d/§11) | M6a,M7 | claude | ✅ |
| M8 | Logging cutover + control front-ends (http, ubus) + config codecs (toml/uci) | M5,M7 | | ⬜ |
| M8a | Share config + Manager wiring: AFP/SMB volume `core/config` sections + `config → []fs.ShareSpec` mapper (options→Extra) in the registry factories; supervisor `Reconfigure` for an AFP/SMB section drives `share.Manager` Add/Update/Remove; `ParamsFor`-generated per-fs_type form masks `secret` params (§9d/§11) | M7c,M8 | | ⬜ |
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
> gated on later milestones: §10d same-FS coordination needs the shared bus/FS that **M8a** builds,
> and legacy `service/{afp,smb,netbios}` deletion is blocked on the **M8/M8a→M10** compose cutover
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
> - **§10d same-FS AFP+SMB coordination — DEFERRED to M8a:** the FS-bus mechanism (`core/fs/bus.go`
>   `Event`/`SkipOrigin`) exists, but today AFP `NewVolume` and SMB `NewShare` each call
>   `share.Build(spec, nil)` with a **nil** bus and build **separate** FS stacks — there is no shared
>   FS instance for two services to coordinate over, and `shareFS` does not yet publish on mutation.
>   Wiring publish-on-mutation + per-service Origin filtering is only exercisable once **M8a** builds
>   AFP+SMB over one shared bus/FS (the config→ShareSpec mapper + supervisor Reconfigure). Doing it
>   now would add unreachable code; it is correctly M8a's, recorded here so M8a picks it up.
> - **Remaining M7 (deferred, not blocking M7 close):** the byte-range LOCKING_ANDX / MPX / raw-read-
>   write SMB paths answer STATUS_NOT_SUPPORTED — left until a target client needs them (no identified
>   client does). Legacy `service/{afp,smb,netbios}` deletion is strangler step 5 but is **blocked**:
>   those packages are still imported by the live `internal/app` runtime (afp_enabled / smb_enabled /
>   netbios_enabled hooks + asp/dsi), so deletion happens at the **M8/M8a compose cutover → M10**, not
>   in M7. TCP transports are M7a (`adapter/dsi`) / M7b (`adapter/smbtcp`).

---

## How to claim a task
1. Put your name/handle in **Owner**, set status 🟡.
2. Work only within the step's stated scope; honour its "must not" constraints.
3. Keep `go build ./... && go test ./...` green; add the step's acceptance test.
4. Set ✅ and open a PR referencing the step id (e.g. "refactor: B3 bus primitive").
