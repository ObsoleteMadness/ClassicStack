# Phase 2 — Migrate existing functionality onto the harness

**Goal:** fill the Phase 1 placeholders with real, working functionality, one subsystem at a
time (strangler), keeping the build green and behaviour compatible throughout. Each migrated
subsystem **replaces** its old `internal/app` wiring and its `*_disabled.go` stubs.

**Prerequisite:** Phase 1 exit criteria all met ([01-PHASE-harness.md](01-PHASE-harness.md)).

**Guiding rules** carry over: greenfield target (don't port the slop, re-express it cleanly);
core stays stdlib-only + reflection-free; compatibility-over-correctness (capture-replay tests
must pass, document deviations in `spec/errata.md`); delete the old path once the new one is
proven; per-step reviewable.

## Migration order (dependency-driven)

Migrate bottom-up so each layer rests on already-migrated layers:

```
links/adapters → protocols/codecs → ports → router → DDP services → storage seam → file services → control front-ends → cmd cutover
```

Each subsystem follows the same **strangler recipe**:
1. Implement the real adapter/component behind the Phase 1 interface.
2. Port the protocol/service logic from the old package into the new component (re-express, don't copy-paste the wiring).
3. Run the subsystem's existing tests + capture-replay against the new component.
4. Switch `cmd/classicstack-ng` to use the real component instead of the placeholder.
5. Delete the old package + its `*_disabled.go` stub once parity is proven.
6. (Final) cut `cmd/classicstack` over to the new compose path; remove `internal/app`.

---

## M1. Link adapters (real I/O)
- **Migrate:** pcap → `adapter/link/pcap` (FrameLink + the filter/dedup/capture/bridge
  decorators from §2; BPF strings live here, not in ports); TAP → `adapter/link/tap`;
  serial PPP/SLIP → `adapter/link/ppp`,`/slip`; kernel datagram → `adapter/link/kerneldp`
  (`AF_APPLETALK`); TinyGo/ESP → `adapter/link/driversnet`.
- **Capture is pcap-only and always available (§6f):** the `Capture` decorator tees frames to a
  `CaptureSink`; provide **two writer adapters** behind one interface — `adapter/capture/libpcap`
  (libpcap dumper, used with the pcap link) and `adapter/capture/pcapfile` (**pure-Go, stdlib-only,
  TinyGo-safe** .pcap writer) so TAP / ESP32-raw / TashTalk-tty / in-mem backends still emit a
  Wireshark-openable file with no libpcap linked.
- **Source:** today's `port/rawlink/*`, `capture/*`, `port/.../bridge_link.go`.
- **Done when:** a port placeholder fed by `adapter/link/pcap` sees real frames; the
  `framing` adapter turns a pcap FrameLink into a DatagramLink; gopacket confined here
  (archtest stays green); a non-pcap link (e.g. TAP or in-mem) writes a valid pcap via
  `capture/pcapfile` that `tshark -r` reads back.
- **Delete the traffic log (§6f):** there is no separate "traffic"/byte-log mechanism — wire
  content is the pcap capture, throughput is `StatSample` counters on the telemetry bus, and any
  narrated protocol event is a `Trace`/`Debug` line. Remove the old traffic-log plumbing rather
  than porting it (`pkg/metrics` traffic paths, per-port `TrafficMetered` byte-logging wiring).

## M2. Protocol codecs
- **Migrate:** real `core/protocol/{atp,asp,pap,nbp,ipx,netbeui,smb,netbios}` codecs
  (DDP already done in Phase 1/B7). Pure, reflection-free, allocation-light.
- **Source:** `appletalk/`, `protocol/*`, the encode/decode scattered in services today.
- **Done when:** capture-replay round-trip tests pass per protocol (decode→encode byte-identical
  on `/captures`); errata documented for any client-driven deviation.

## M3. Ports (data path)
- **Migrate:** EtherTalk (AARP/node-claim move into the `framing` adapter, §2), LocalTalk
  (LToUDP/TashTalk/Virtual), IPX, NetBEUI — as real `core/port` components consuming a
  `FrameLink`/`DatagramLink`, implementing `Bindable`/`Statful`/`Configurable`/`Metered`.
- **Source:** `port/ethertalk`, `port/localtalk/*`, `port/ipx`, `port/netbeui`.
- **Done when:** a port comes up over a real link, claims its address, meters throughput to
  the telemetry bus, and survives Stop→Start and Reconfigure.
- **Note:** M3 builds ports as singletons; the singleton→**named repeated instance** + interface
  namespace generalisation is M11.

## M4. Router + tables
- **Migrate:** AppleTalk router, RoutingTable (with the event-driven membership: Attach/Detach
  → immediate directly-connected route withdrawal, §3), ZIP/RTMP services, plus the IPX and
  NetBEUI mini-routers.
- **Source:** `router/*`.
- **Done when:** routed ports attach/detach cleanly; aging works; ZIP/RTMP answer; the
  routing-table snapshot tests pass against the new router.
- **Note:** explicit membership-by-name (`[Router].members`, per router type; unlisted enabled
  instances run standalone) lands with the named-instance config in M11.

## M5. DDP services
- **Migrate:** MacIP gateway, IPXGW, AEP/NBP — as real `core/service` components riding the
  router; live counts published as `StatSample` (replacing `refreshMacIPStatus` et al, §5).
- **Source:** `service/macip`, `service/ipxgw` (IPXGW wiring), `service/aep`, `service/zip` NBP.
- **Done when:** MacIP leases/sessions show via the stats topic; diagnostics probes work.

## M6. Storage seam (the §9 inversion) — do before file services
- **Migrate:** the unified FS interface (collapse the AFP-duplicate registry into `core/fs`);
  the `metastore` for CNID/shortname/desktop (mem default; sqlite behind a tag); the
  `ForkEngine` adapters (`fork/appledouble`, `fork/ads`, `fork/xattr`, `fork/native`); the
  name engines (short/medium); the **`FilenameCodec` adapters** (`fncodec/macroman-utf8` as
  default, `macroman-native`, `utf8`) — lifting `service/afp/path_codec.go` out of the service
  and `pkg/encoding` into `core/encoding`; the per-share build that validates
  `fs_type`×`fork_backend`×`filename_codec`.
- **Backend params + schema:** `ShareSpec` carries a typed `Path` plus an `Extra map[string]any`
  param bag; each factory declares its config schema via `RegisterFSWithParams(fsType, Factory,
  Param{Key,Required,Secret,Doc}…)`, readable via `ParamsFor(fsType)`. `BuildShare` validates the
  required params are present (and rejects unknown keys) before constructing the backend — an `ftp`
  share missing `url`, or an `hfs-image` missing `partition`, fails loudly on Apply. `Secret`
  params are redacted in logs/diagnostics. Port `local_fs` from `pkg/vfs` to a real `core/fs`
  factory that reads `spec.Path` (the first real backend in the new registry; `memfs` stays for tests).
- **`ForkFS.Rename`/`Remove` carry the metadata container:** fold `MoveMetadata` into `Rename` and
  `DeleteMetadata` into `Remove` (delete metadata first) on the assembled `ForkFS`, so callers above
  the FS make one correct call. The low-level metadata ops stay on `ForkEngine` for the engines/tests.
- **Source:** `service/afp/fs.go`, `pkg/vfs`, `pkg/cnid`, `service/afp/appledouble_backend.go`,
  `service/afp/desktopdb.go`, `pkg/shortname`, `service/afp/path_codec.go`, `pkg/encoding`.
- **Interop (hard):** `fork/ads` = SFM stream names/encoding; `fork/xattr` = Netatalk EA
  layout (§9b). Document the AfpInfo + Netatalk-EA wire formats in `spec/`.
- **Filename codec must be reversible + store-native:** `Decode` returns `fs.StoredName`
  (backend-native bytes), not a Go `string`; `Encode(Decode(wire, c), c) == wire` for each
  supported wire charset `c` (MacRoman + reserved chars via the `0xNN` token round-trip);
  unrepresentable names return `ErrUnrepresentable` (→ protocol "illegal name"), never a mangled
  path; reserved set is backend-declared (POSIX bytes vs NTFS vs FAT vs S3 url-safe), not
  `runtime.GOOS`.
- **Wire charset is per request, threaded by the service:** the lifted `path_codec.go` no longer
  hard-wires MacRoman — the AFP service maps the request path-type byte to a `fs.WireEncoding`
  (`kFPShortName`/`kFPLongName`→`WireMacRoman`, `kFPUTF8Name`→`WireUTF8`) and passes it on every
  `Decode`/`Encode` call; SMB maps its dialect/Unicode flag (legacy→`WireANSI`, NT→`WireUTF16`).
  This preserves serving multiple client versions on one share. Carry the AFP pathType branch
  that already exists at `service/afp/paths.go` (`resolvePath(..., pathType)`) through to the
  codec call instead of the current fixed `encoding.MacRomanToUTF8`/`UTF8ToMacRoman`.
- **New transcode paths beyond MacRoman↔UTF-8:** `core/encoding` (lifted `pkg/encoding`) today
  only does MacRoman↔UTF-8. The new `WireEncoding` values need transcoders the codec adapters
  must add:
  - **`WireUTF16` (SMB NT):** UTF-16LE↔store. Handle the byte-pair framing, surrogate pairs,
    an optional/leading BOM, and **odd-length input** (a truncated final unit → `ErrUnrepresentable`,
    not a panic or silent drop). Prefer stdlib `unicode/utf16` + `unicode/utf8` (reflection-free,
    TinyGo-safe) over a new dependency.
  - **`WireANSI` (SMB legacy/DOS):** a code-page table (e.g. CP437/CP850/CP1252 — the SMB
    negotiated OEM code page) ↔ store. Add the table(s) to `core/encoding` the same hand-written,
    reflection-free way as the MacRoman table; the codec picks the page from the negotiated
    dialect. Document the chosen default code page and any observed client quirks in `spec/`.
  - Keep each transcoder behind the `FilenameCodec` so `Wire()` advertises only the charsets a
    given codec actually implements — an adapter that hasn't added UTF-16 yet simply omits it
    from `Wire()` and SMB NT requests fail loudly with `ErrWireUnsupported` rather than mangling.
- **Done when:** an AFP/SMB volume reads/writes forks via the chosen engine; metadata round-trips
  through `pkg/appledouble` codec regardless of container; a `ForkFS.Rename`/`Remove` carries the
  metadata container without the caller pairing the calls; `BuildShare` rejects a share missing a
  required backend param (e.g. `ftp` without `url`) with a clear error and `ParamsFor` returns the
  declared schema; `local_fs` builds from `spec.Path`; sqlite is droppable (mem default works);
  MacRoman/reserved-char filename round-trip tests pass (port `path_codec_test.go`/`enumerate_encoding_test.go`).

## M7. File services (AFP, SMB) + NetBIOS
- **Migrate:** AFP, SMB, NetBIOS as real components. They consume **only** the §9 fs/metastore
  interfaces (lose all storage-layout knowledge) and the relevant transport. NetBIOS transports
  (IPX/NetBEUI) become `Attachable` bindings (§11d), not hard deps.
- **Shared share seam (`core/share`):** introduce a protocol-neutral, thin `share.Share` descriptor
  (`Name`/`FS() fs.ForkFS`/`Config`/`ReadOnly`/`Description`/`Permissions`-stub/`Codec`) — it
  *exposes* the FS, it does not mirror catalog ops (callers do `share.FS().Stat(p)`). AFP `Volume`
  and SMB `Share` each HOLD a `*share.Share` and add only protocol concerns: wire path parsing
  (`ResolvePath`/`EncodeName` via the codec), and for AFP the `metastore.CNIDStore` + a CNID rebind
  *after* the metadata-carrying `FS().Rename`/`Remove`. `core/share` imports `core/fs` only.
- **Dynamic share management (`share.Manager`, §11):** both services implement
  `Shares()`/`AddShare(ShareSpec)`/`UpdateShare(name,ShareSpec)`/`RemoveShare(name)`, guarding their
  share/volume slice with the service mutex. `AddShare` validates the spec via `BuildShare` (bad
  triple/missing param fails before binding) and AFP allocates the volume id internally.
  `RemoveShare` unpublishes the share (no new `FPOpenVol`/TreeConnect) but does **not** tear down
  in-flight sessions — they ride their copied `*Volume` handle until the client closes it.
  `UpdateShare` builds the new stack first, then swaps under the lock, preserving the AFP id.
- **Command core vs. session transport (§3-bis):** each file service is a **pure command core**
  (`dispatch(sess, block) → (reply, result)`, imports no `net`) plus session transports that
  wrap it. SMB's transports come in two families — **NetBIOS-based** and **direct
  (NetBIOS-less)** — and SMB drives all of them through ONE transport-agnostic seam (`conn.go`'s
  `SessionConsumer`), so it does not distinguish them. The **in-core** transports stay in `core/`:
  - AFP/ASP (DDP/ATP) — `core/service/afp` (the M7 spine: `asp.go` over `dispatchAFP`). Done.
  - SMB-over-NetBIOS — NBF (NetBEUI) + NBIPX (IPX socket `0x0455`) — `core/service/netbios` engines
    feeding the SMB `SessionConsumer`. Done.
  - **SMB direct-hosted over IPX** (socket `0x0550`, Microsoft "NWLink direct host") — a core
    transport with NO NetBIOS layer: its own connection-id framing on the IPX mini-router, then the
    SAME `NewConn`/`ServeMessage`/`Close` seam. Re-home from legacy `service/smb/over_ipx_direct`.
    No `net`, so it stays in core. ⬜ not yet ported.
- **TCP/stream transports are build-tagged ADAPTERS, not core (§1/§3-bis):** because `net` is
  forbidden in core (a netless Pico must still serve DDP), the TCP front-ends move out of
  `core/`:
  - `adapter/dsi` (`//go:build dsi || all`) — re-home `service/dsi/dsi.go`'s listener/accept
    loop + 16-byte DSI framing onto the AFP command core's `CommandHandler` seam (replacing the
    old `afp.CommandHandler`). Registers via `init()` (§8).
  - `adapter/smbtcp` (`//go:build smbtcp || all`) — **direct-TCP `:445`** framing (4-byte length
    prefix) over `net.Conn` onto the SMB command core. Win2000+ clients.
  - `adapter/netbios-tcp` (`//go:build nbt || all`) — **NBT (RFC 1001/1002)**, the TCP sibling of
    the NBF/NBIPX transports: name service (UDP 137), datagram service (UDP 138), session service
    (TCP 139). Its session half feeds the SAME NetBIOS `SessionConsumer` seam SMB rides over
    NetBEUI/IPX, and its datagram half feeds the SAME `DatagramConsumer` seam the browser rides —
    so NBT adds NO SMB or browser code, only the wire transport (§3-ter). It needs `net`, hence an
    adapter. This is what most vintage TCP clients (Win9x/NT) actually use; `:445` direct-TCP is
    Win2000+. Decide per deployment which (or both) to link.
  - Each owns its `net.Listener`; binding is `host:port` via `component.Bindable`, default all
    interfaces, restart-grade reconfigure (§11b). An `//go:build esp32` sibling does WiFi/`netdev`
    bring-up before `net.Listen`. **Do NOT create `core/service/dsi`** — that would pull `net`
    into core.
- **Source:** `service/afp/*`, `service/smb/*`, `service/netbios/*`, `service/asp` → `core/`;
  `service/dsi` → `adapter/dsi`.
- **Done when:** a real client (Classic Mac / DOS / early Windows, or recorded exchange)
  connects and transfers files over **both** AFP/ASP (DDP) **and** AFP/DSI (TCP); same-FS
  AFP+SMB coordinate via the FS bus (§10d); AFP `Volume` and SMB `Share` both hold a
  `core/share.Share` and implement `share.Manager` (add/update/remove a share on a running
  server, with `RemoveShare` leaving in-flight sessions intact); bug-for-bug capture-replay
  tests pass; a netless TinyGo build (no `dsi`/`smbtcp` tag) still compiles and serves DDP.

## M7d. NetBIOS browser service (optional, datagram-layer; §3-ter)
- **Migrate:** the browser out of `service/smb` into a standalone **`core/service/browser`** — a
  datagram-layer NetBIOS service, common to ALL NetBIOS transports (NetBEUI/IPX/NBT), NOT bound to
  SMB. It plugs into the NetBIOS service as the `DatagramConsumer` (the seam landed in M7), parses
  the `\MAILSLOT\BROWSE` opcodes (HostAnnounce 0x01 / AnnouncementReq 0x02 / RequestElection 0x08 /
  GetBackupList 0x09/0x0A / DomainAnnounce 0x0C / LocalMasterAnnounce 0x0F), maintains the browse
  list + election role (potential/backup/local-master), and emits its own announcements out through
  the NetBIOS datagram egress. One command core, three transports, zero per-transport browser code.
- **The RAP/LANMAN `NetServerEnum2` seam:** the "get server list" call arrives over the SMB IPC$
  named pipe (`\PIPE\LANMAN`) — the *session* path. SMB asks the browser for the current list via a
  small read-only `BrowseList()` query interface the browser exposes; SMB holds no browser logic and
  the browser holds no SMB logic. This is the one read-only meeting point of the two services.
- **Source:** `service/smb/browser_frames.go`, `service/smb/command_rap_lanman.go`, the `browserRole`
  /announcement/election machine in `service/smb/server.go`.
- **Optional (§8):** registry `init()`; a build/deployment that wants only file serving never links
  it (the `DatagramConsumer` stays unset, datagrams drop after decode). Announcements/elections are
  configurable off.
- **Done when:** a Windows client populates Network Neighborhood from our HostAnnounce; a
  `NetServerEnum2` over IPC$ returns the browse list; the browser serves the same list over NetBEUI,
  IPX and (once `adapter/netbios-tcp` lands) NBT with no transport-specific browser code; SMB carries
  no browser logic.

## M8. Logging + control front-ends
- **Migrate:** route all services' logging through `core/log` scoped loggers (drop ad-hoc
  `netlog`); real `adapter/control/http` (port the web UI/SPA over the Plane), then
  `adapter/control/ubus` (OpenWRT, §7); config codecs/stores: `adapter/config/toml`,
  `adapter/config/uci`, `adapter/store/file`, `adapter/store/uci`.
- **Share config + Manager wiring (the M7c follow-on):** define the AFP/SMB volume
  `core/config` sections (name/path/fs_type/fork_backend/filename_codec/read_only/description
  + an `options` sub-map folded into `ShareSpec.Extra`), add the single
  `config → []fs.ShareSpec` mapper both services' registry factories use to build their
  initial shares, and drive the supervisor's addressed `Reconfigure` for an AFP/SMB section
  through `share.Manager.AddShare/UpdateShare/RemoveShare` so a web-UI/UCI share edit takes
  effect on Apply without a service restart. A `secret` param (password) is masked in the
  rendered form (from `fs.ParamsFor`) and redacted in diagnostics. This consumes the
  `share.Manager` contract M7c already shipped.
- **Server identity wiring (§4-bis):** add the top-level `config.Identity{Hostname, Workgroup}`
  section (owned by NO service — NOT a field on the SMB or NetBIOS section). The hostname is a
  *server* property SMB needs even with NetBIOS absent (direct-TCP `:445`, or AFP-only / NetBIOS
  off). The registry reads it **once** and hands the same `Hostname` to whichever consumers are
  enabled — SMB (add `SetServerName`, advertised in NEGOTIATE — today SMB only has `SetWorkgroup`),
  `netbios.NewService` *if NetBIOS is enabled*, the browser *if linked*; flow `Workgroup` likewise.
  No per-service hostname field, so consumers cannot diverge; the model `Validate` rejects any
  externally-surfaced second name that disagrees (the "error if they vary" backstop). Validation is
  layered: a baseline hostname check always applies, but the **NetBIOS ≤15-byte/upper-case rule is a
  consumer constraint applied only when NetBIOS is enabled** (a 20-char name is fine for an
  SMB-`:445` / AFP-only server, rejected once NetBIOS turns on). `Hostname` change is restart-grade
  for NetBIOS (re-claim per transport) and for direct-TCP SMB's advertised name.
- **Source:** `pkg/logbuf`, `pkg/metrics`, `service/webui/*`, `pkg/control/*`, `config/*`,
  `internal/app/smb_shares.go` (+ AFP equivalent), `compose/registry/reg_afp.go`/`reg_smb.go`.
- **Done when:** web UI drives the new Plane; ubus parity test passes on an OpenWRT target;
  UCI round-trips the model; a share added/updated/removed in the UI (or via `Reconfigure`)
  binds/unbinds on a running AFP & SMB server through `share.Manager`, with the
  `ParamsFor`-generated per-`fs_type` form supplying backend params and masking secrets.

## M9. Platform integration
- **Migrate:** Windows service / launchd / systemd / procd wrappers to drive the new compose
  supervisor + Plane.
- **Source:** `cmd/classicstack-svc`, `cmd/classicstackd`.
- **Done when:** each platform starts/stops/reloads ClassicStack natively; `reload` → a Plane
  `Reconfigure`.

## M10. cmd cutover + teardown
- **Do last:** point `cmd/classicstack` at the compose path; delete `internal/app` (supervisor,
  `wireXxx`, all `*_disabled.go`, `appConfig`, config glue); remove the temporary
  `cmd/classicstack-ng`. Update README/CLAUDE.md for the new layout.
- **Done when:** one binary, new architecture, full test suite + capture-replay green;
  `internal/app` is gone; line count is **down**.

## M11. Named port instances + interface namespace
Full design: [03-DESIGN-named-ports-and-interfaces.md](03-DESIGN-named-ports-and-interfaces.md).
M3–M10 build ports as **singletons** (one `[EtherTalk]`/`[LToUDP]`/`[TashTalk]`/`[IPX]` per key);
M11 generalises them to **named, repeated instances** bound to a **named interface namespace**.
Implement bottom-up so each layer rests on the new config shape:
- **Config layer first:** port sections become `NamedSection` (gain `name`, move to `Model.Lists`);
  add the `[[Interface]]` namespace (kinds `nic`/`serial`/`bridge`) and fold the single
  `Model.Bridge` into it (`EffectiveInterface` resolves by name); add `[Router].members`
  (per router type; empty = none join, opt-in). TOML/UCI round-trip via the existing
  array-of-tables / repeated-block machinery.
- **Opener dispatch:** interface-kind → opener table (pcap / `adapter/serial` / rawsock); the
  shared `adapter/serial` opener lands here and tashtalk/ppp/slip reduce to byte-stream framers.
- **Registry/supervisor:** one factory → **N components**, one per instance, addressed by
  instance name; the supervisor enumerates `Model.Lists[key]`.
- **Router membership:** Attach/Detach driven only for instances named in `[Router].members`
  (builds on the event-driven membership from M4/§3).
- **Do the IPX/NetBEUI device-link injection here**, against the named-instance shape, not the
  singleton one.
- **Done when:** several EtherTalk/TashTalk/IPX instances run at once on distinct interfaces,
  each its own segment; `[Router].members` controls attachment; standalone (unlisted) instances
  receive but don't route; TOML/UCI round-trip; conformance harness exercises multi-instance.

## Client tools (any time after M2)
- **Add:** `cmd/csecho` (AEP over a link, §12) and `cmd/csnetsend` (NetBIOS) as proofs of the
  protocol-reuse claim — each links core + one adapter only (small binary).

---

## Phase 2 exit criteria

- [ ] Every placeholder replaced by real, tested functionality.
- [ ] `internal/app` and all `*_disabled.go` deleted; `cmd/classicstack` runs on compose.
- [ ] Capture-replay / bug-for-bug compatibility suite green; deviations in `spec/errata.md`.
- [ ] Web UI + ubus both drive the same Plane (parity test green); UCI + TOML both round-trip.
- [ ] TinyGo build of a minimal embedded target links and runs; sqlite-free build works.
- [ ] Per-protocol security notes present (Verification #9); intentional-weakness paths annotated.
- [ ] Net lines of code reduced vs. the pre-refactor tree.
- [ ] (M11) Ports are named repeated instances over a named interface namespace; `[Router].members`
      governs attachment; multiple instances per transport run at once on distinct interfaces.
