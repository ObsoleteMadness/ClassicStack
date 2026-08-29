# Changelog

Notable changes to ClassicStack, particularly anything a downstream importer of
`core/protocol/*`, `core/service/*`, or `client/*` (a `go get`-er of a subpackage,
not just the `classicstack` binary) would need to know about. Loosely follows
[Keep a Changelog](https://keepachangelog.com/); dates are release-tag dates.

ClassicStack is pre-1.0 (`v0.1.0`–`v0.3.0` so far): **no semantic-versioning
compatibility guarantee applies yet** — a public struct field, constant, or
function signature under `core/` or `client/` may still change between minor
tags. This file exists so such changes are at least announced rather than
silently discovered by a downstream build breaking.

## [Unreleased]

Ongoing pre-1.0 architecture/quality pass (see the project's execution plan for
the full list). Landed so far:

### Added
- `core/csnet`: a new shared MAC/IP address package (`ParseMAC`, `FormatMAC`,
  `RandomMAC`, `ParseIPv4`), consolidating five byte-for-byte-duplicated
  `RandomMAC` implementations and four independently-written MAC parsers
  across `core/port`, `adapter/control/finder`, `adapter/macipgw`, and
  `cmd/internal/csconnect`. TinyGo-safe (`!tinygo`/`tinygo` build-tag split,
  matching the `core/buf` / `core/hostinfo` convention).
- `ExampleXxx` functions for `core/csnet`, `core/protocol/ddp`,
  `core/protocol/nbp`, `client/link`, and `client/atalk` — compiler-checked
  usage samples that also render on pkg.go.dev's Example tabs.
- `examples/memfs-afp-server`: a runnable worked example of the server SDK — a
  standalone AFP file server (one AppleTalk node, no router services) serving
  an in-memory volume implemented from scratch. See `docs/manual.md` §6.
- `docs/manual.md` §6, "Extending ClassicStack — the server SDK": the
  server-embedding counterpart to §5's existing client-SDK guide.

### Changed
- **`ddp.Checksum`** (`core/protocol/ddp`) is now exported; it was the
  unexported `checksum`. `adapter/link/framing`'s LocalTalk framer now calls
  it directly instead of keeping its own byte-for-byte copy
  (`ddpChecksum`, deleted).
- `core/protocol/ncp`'s hand-rolled binary encode/decode helpers
  (`appendBE16`/`appendBE32`/`beU16b`/`appendLE16`/`appendLE32`/`be16`/`be32`,
  plus scattered inline `byte(v>>8), byte(v)` header encoding) now go through
  `core/binaryprimitives`. Internal only — no public API change.

## [v0.3.0] — 2026-06-07

## [v0.2.0] — 2026-05-17

## [v0.1.0] — 2026-04-18

No changelog was kept before this file was added (partway through the
pre-1.0 architecture pass above); see `git log` / the GitHub release notes
for each tag's actual contents.
