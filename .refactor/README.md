# ClassicStack / OmniTalk — Refactor Workspace

This folder holds the plan to refactor ClassicStack onto the greenfield hexagonal
architecture. It is structured so individual steps can be farmed out to separate
agents or people working in parallel.

## Documents

| File | What it is |
|---|---|
| [00-DESIGN.md](00-DESIGN.md) | The full target architecture (charter + 14 sections). The "why" and "what". **Read this first.** |
| [01-PHASE-harness.md](01-PHASE-harness.md) | **Phase 1** — stand up the new layout, interfaces, buses, placeholders, and the test harness. *No existing functionality is ported yet.* |
| [02-PHASE-migration.md](02-PHASE-migration.md) | **Phase 2+** — migrate existing functionality onto the harness, one subsystem at a time (strangler). |
| [TODO.md](TODO.md) | The actionable checklist (every step as a tickable task with owner/status), kept in sync with the phase docs. |

## Working agreement

1. **Phase 1 builds an empty-but-compiling skeleton.** Interfaces, message buses, the
   component/registry/supervisor harness, and placeholder components that satisfy the
   interfaces but do nothing real. The whole tree must `go build` and `go test` green at
   the end of every step.
2. **Do not port real protocol/service logic in Phase 1.** Placeholders only. Real logic
   lands in Phase 2, behind the interfaces Phase 1 defines.
3. **The dependency rule is law** (00-DESIGN §1): `core/` imports only stdlib + itself —
   no pcap, gopacket, koanf, net/http, sqlite, `reflect`. CI gate enforces it (step in
   Phase 1). If a step needs a forbidden import, it belongs in an adapter, not core.
4. **Greenfield, not extrapolation.** Build the ideal target; the current code is a
   feasibility reference only. Delete aggressively; fewer lines is better.
5. **Each step is self-contained and reviewable.** A step states its goal, the files it
   creates, its acceptance check, and what it must NOT do. Steps are sized so one
   agent/person can complete one in isolation.
6. **New tree lives alongside the old until Phase 2 migrates each subsystem.** Phase 1 does
   not move or break existing packages; it adds the `core/`, `adapter/`, `compose/` rings
   empty. The old `internal/app` stack keeps running until a subsystem is migrated.

## Status

- Phase 1: in progress — Group A complete (A1–A4); Group B wave 3 complete (B1/B3/B6/B7/B9)
- Phase 2: not started

See [TODO.md](TODO.md) for per-step status.
