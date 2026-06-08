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
| B2 | `core/link`: FrameLink/DatagramLink + decorator surface + framing contract (§2) | A1 (soft B7) | | ⬜ |
| B3 | `core/bus`: bus primitive + telemetry events, topic-scoped (§5) | A1 | claude | ✅ |
| B4 | `core/fs` bus: FS-mutation instance of the B3 primitive (§5/§10c) | B3 | | ⬜ |
| B5 | `core/log`: scoped Logger, typed Field, Sink, ring/stderr sinks (§6) | A1,A3,B3 | | ⬜ |
| B6 | `core/config`: Model + SectionSchema registry + Codec/Store ifaces (§4) | A1 | claude | ✅ |
| B7 | `core/protocol/ddp`: real Datagram codec (+ stub siblings) (§2/§12) | A1 | claude | ✅ |
| B8 | `core/fs`: FileSystem/File/ForkEngine/ForkFS/NameEngine/**FilenameCodec** + per-share params; lift `core/encoding` (§9/§10a/§10a-bis) | A1,B4,B6 | | ⬜ |
| B9 | `core/metastore`: Store iface + `mem` snapshot impl (§9a) | A1 | claude | ✅ |
| B10 | `core/control`: Plane contract (methods + Subscribe) + Supervisor/Diagnostics ifaces (§7) | A1,B3,B6 | | ⬜ |

### Group C — Harness (depends on Group B)
| # | Task | Deps | Owner | Status |
|---|------|------|-------|--------|
| C1 | `compose/registry`: name→factory, build-tag `init()` (§8) | B1,B6 | | ⬜ |
| C2 | `compose/supervisor`: DAG, ordered start/stop, StateChanged publish (§3/§11) | B1,B3,C1 | | ⬜ |
| C3 | Supervisor addressed `Reconfigure`+notify (no diff) + Attachable side-effects (§11) | C2,B1,B6 | | ⬜ |
| C4 | `compose` stats/rate subscriber on telemetry bus (§5) | B3,C2 | | ⬜ |

### Group D — Placeholders (depends on B + C)
| # | Task | Deps | Owner | Status |
|---|------|------|-------|--------|
| D1 | Placeholder ports (ethertalk/localtalk/ipx/netbeui) | B1,B2,C1–C3 | | ⬜ |
| D2 | Placeholder router w/ Attach/Detach membership (§3) | B1,B2,D1 | | ⬜ |
| D3 | Placeholder services (afp/smb/netbios/macip) | B1,B8,B9 | | ⬜ |
| D4 | Minimal real adapters: inmem link, toml codec, file store, inproc control | B2,B6,B10 | | ⬜ |
| D5 | Assembly + runnable `cmd/classicstack-ng` (boots all-placeholder stack) | C*,D1–D4 | | ⬜ |
| D6 | **OpenWRT seam: `adapter/config/uci` + `adapter/store/uci` + `adapter/control/ubus` (ubus.sock) + procd/init.d sketch** (§4,§7) | B6,B10,D4,D5 | | ⬜ |

### Group E — Test harness for the structure
| # | Task | Deps | Owner | Status |
|---|------|------|-------|--------|
| E1 | Component conformance harness (Start/Stop idempotency, capabilities) | B1,C2 | | ⬜ |
| E2 | Bus conformance + back-pressure tests (reused by B3/B4) | B3,B4 | | ⬜ |
| E3 | Multi-front-end parity test (inproc vs http **vs ubus** over same Plane) | B10,D4,D5,D6 | | ⬜ |
| E4 | Reconfigure-and-notify test (asserts no model-diff) | C3 | | ⬜ |
| E5 | Wire all tests into CI (incl. TinyGo amd64 gates + UCI/TOML round-trips) | A4,D6,E1–E4 | | ⬜ |

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
| M1 | Link adapters: pcap/tap/ppp/slip/kerneldp/driversnet + decorators | Phase 1 | | ⬜ |
| M2 | Protocol codecs (atp/asp/pap/nbp/ipx/netbeui/smb/netbios) + capture-replay | M1 | | ⬜ |
| M3 | Real ports (ethertalk/localtalk/ipx/netbeui) over real links | M1,M2 | | ⬜ |
| M4 | Router + tables (event membership) + ZIP/RTMP + ipx/netbeui routers | M3 | | ⬜ |
| M5 | DDP services (MacIP/IPXGW/AEP/NBP) + stats publish | M4 | | ⬜ |
| M6 | Storage seam: unified FS, metastore, fork engines (SFM/Netatalk interop), name engines, **filename codecs** (MacRoman/reserved, from path_codec.go) | Phase 1 (B8/B9) | | ⬜ |
| M7 | File services AFP/SMB/NetBIOS over fs/metastore + Attachable transports | M6,M2 | | ⬜ |
| M8 | Logging cutover + control front-ends (http, ubus) + config codecs (toml/uci) | M5,M7 | | ⬜ |
| M9 | Platform integration (Windows svc / launchd / systemd / procd) | M8 | | ⬜ |
| M10 | cmd cutover + delete `internal/app`/`*_disabled.go`; docs | M1–M9 | | ⬜ |
| T1 | Client tools `cmd/csecho`, `cmd/csnetsend` (protocol-reuse proof) | M2 | | ⬜ |

**Phase 2 DoD:** see exit criteria in [02-PHASE-migration.md](02-PHASE-migration.md).

---

## How to claim a task
1. Put your name/handle in **Owner**, set status 🟡.
2. Work only within the step's stated scope; honour its "must not" constraints.
3. Keep `go build ./... && go test ./...` green; add the step's acceptance test.
4. Set ✅ and open a PR referencing the step id (e.g. "refactor: B3 bus primitive").
