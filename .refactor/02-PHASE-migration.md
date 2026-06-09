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

## M4. Router + tables
- **Migrate:** AppleTalk router, RoutingTable (with the event-driven membership: Attach/Detach
  → immediate directly-connected route withdrawal, §3), ZIP/RTMP services, plus the IPX and
  NetBEUI mini-routers.
- **Source:** `router/*`.
- **Done when:** routed ports attach/detach cleanly; aging works; ZIP/RTMP answer; the
  routing-table snapshot tests pass against the new router.

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
  through `pkg/appledouble` codec regardless of container; sqlite is droppable (mem default works);
  MacRoman/reserved-char filename round-trip tests pass (port `path_codec_test.go`/`enumerate_encoding_test.go`).

## M7. File services (AFP, SMB) + NetBIOS
- **Migrate:** AFP, SMB, NetBIOS as real components. They consume **only** the §9 fs/metastore
  interfaces (lose all storage-layout knowledge) and the relevant transport. NetBIOS transports
  (IPX/NetBEUI) become `Attachable` bindings (§11d), not hard deps.
- **Source:** `service/afp/*`, `service/smb/*`, `service/netbios/*`, `service/asp`, `service/dsi`.
- **Done when:** a real client (Classic Mac / DOS / early Windows, or recorded exchange)
  connects and transfers files; same-FS AFP+SMB coordinate via the FS bus (§10d); bug-for-bug
  capture-replay tests pass.

## M8. Logging + control front-ends
- **Migrate:** route all services' logging through `core/log` scoped loggers (drop ad-hoc
  `netlog`); real `adapter/control/http` (port the web UI/SPA over the Plane), then
  `adapter/control/ubus` (OpenWRT, §7); config codecs/stores: `adapter/config/toml`,
  `adapter/config/uci`, `adapter/store/file`, `adapter/store/uci`.
- **Source:** `pkg/logbuf`, `pkg/metrics`, `service/webui/*`, `pkg/control/*`, `config/*`.
- **Done when:** web UI drives the new Plane; ubus parity test passes on an OpenWRT target;
  UCI round-trips the model.

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
