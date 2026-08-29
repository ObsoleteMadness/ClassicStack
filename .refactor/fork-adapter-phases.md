# Fork-adapter redesign — phase prompts

Self-contained task prompts for the fork-adapter refactor agreed 2026-06-25. Each is
executable cold (a fresh agent could pick one up). Decisions already locked:

- **Mandatory adapter ABOVE the fs** (the base FS stays fork-unaware; one adapter always
  present; `nofork` makes "no forks" explicit — no silent null fallback).
- **Registry-driven** (replace the `core/fs/fork.go` `forkEngineByName` switch with a
  `RegisterForkAdapter` registry, mirroring `RegisterFS`).
- **`appledouble` parameterized by a sidecar LAYOUT** (netatalk `._name`, osx-zip
  `__MACOSX/…`, appledouble-dir `.AppleDouble/name`) — OS-X-zip is a layout, not a new
  adapter.
- **Renaming adapter moves its OWN containers atomically**; the §10d bus event just
  notifies the peer to re-stat / re-derive shortnames (no service touches another's
  sidecars).
- **Placement:** pure adapters (nofork/appledouble/applesingle) stay in `core/fs`
  (TinyGo-clean); genuinely host-native adapters go in `adapter/fork/` under build tags.
  NOTE (verified): the existing `ads`/`xattr` engines are pure Go that write through the
  base FS using stream-suffixed paths (`name:AFP_Resource`, EA pseudo-paths) — they do
  NOT call host syscalls themselves, so they can stay in core. Only a future `native`
  (true HFS+/hfs-image host fork via syscalls) needs `adapter/fork/`.

Project rules that apply to every phase: confirm against `/spec/16-storage-seam.md`; use
consts not literals; gofmt + `go vet`; keep `core/internal/archtest` green (no
reflect/net/cgo into core); DTOs self-(de)serialise; attribute 3rd-party code. Verify
with `go build -tags all ./...` AND headless `go build ./...`, plus
`go test -tags all ./core/... ./adapter/...`.

---

## Phase 1 — Fork-adapter registry + mandatory `nofork` (no behaviour change)

**Goal:** replace the hardcoded `forkEngineByName` switch with a self-registration
registry mirroring `RegisterFS`, and make a fork adapter MANDATORY for every share —
with `nofork` as the explicit "no forks" choice (today's `null`/`none`). Pure
refactor: every currently-valid share must build identically and all existing tests
pass unchanged.

**Files:**
- `core/fs/fork.go` — `forkEngineByName` switch (lines ~13-43); the appledouble engine.
- `core/fs/fs.go` — the `forkEngineByName` callsite in `BuildShare` (line ~434);
  `NewNullForkEngine`/`nullForkEngine` (lines ~628-677); `withDefaults` ForkBackend
  default (`"appledouble"`).
- `core/fs/fork_ads.go`, `core/fs/fork_xattr.go` — register themselves.

**Do:**
1. Add a registry to `core/fs` (new `fork_registry.go` or in `fork.go`):
   ```go
   type ForkAdapterFactory func(base FileSystem) (ForkEngine, error)
   func RegisterForkAdapter(name string, f ForkAdapterFactory)   // lower-cases name
   func forkAdapterByName(name string, base FileSystem) (ForkEngine, error) // looks up; err "fs: unknown fork backend" if absent
   ```
   Guard with a `sync.RWMutex` like `fsFactories`.
2. Register the built-in adapters from `init()` in their own files (NOT a switch):
   - `appledouble` (+ aliases `auto`, `native` for now — they currently fall through to
     AppleDouble; keep that behaviour, add a TODO that `native` becomes a real host-fork
     adapter in Phase 4) → `core/fs/fork.go`.
   - `ads` → `core/fs/fork_ads.go`; `xattr` → `core/fs/fork_xattr.go`.
   - `nofork` (aliases `null`, `none`) → wherever `nullForkEngine` lives; rename the
     doc/comments so `nofork` is the primary name. Keep `NewNullForkEngine` exported
     (it has external callers — grep first) but have it back `nofork`.
3. Replace the `forkEngineByName(...)` call in `BuildShare` with `forkAdapterByName(...)`.
4. Make the adapter mandatory: `BuildShare` already always builds one (default
   `appledouble`); ensure there is NO code path that yields a nil/absent adapter, and
   that an unknown name is a hard error (it already is). Document in the `ForkFS` /
   `BuildShare` doc comment that a fork adapter is always present (nofork when none
   wanted).
5. Delete the now-dead `forkEngineByName` switch.

**Don't:** change any container layout, the `ForkEngine` interface, or `shareFS`
orchestration. No behaviour change.

**Verify:** `go test -tags all ./core/...`; existing fork tests
(`fork_test.go`, `fork_ads_test.go`, `fork_xattr_test.go`, `fork_ads_test.go`) pass
unchanged; `BuildShare` with `ForkBackend:"null"`, `"none"`, `"nofork"` all yield the
no-op adapter; unknown name still errors. archtest green. Add a test that
`RegisterForkAdapter` + `forkAdapterByName` round-trips and that every built-in name
(`appledouble`/`auto`/`native`/`ads`/`xattr`/`nofork`/`null`/`none`) resolves.

---

## Phase 2 — `appledouble` parameterized by a `SidecarLayout`

**Goal:** the AppleDouble adapter hardcodes the Netatalk `._name` sidecar location
(`sidecarPath` in `core/fs/fork.go`, lines ~57-65). Extract that into a swappable
`SidecarLayout` strategy and add the real-world variants, so an OS-X-created zip
(`__MACOSX/dir/._name`) is readable and a `.AppleDouble/`-dir volume works. Directly
enables zipfs/macgarden to read OS-X archives.

**Files:** `core/fs/fork.go` (`sidecarPath`, `splitPath`, `appleDoubleForkEngine`,
`readSidecar`/`writeSidecar`/`MoveMetadata`/`DeleteMetadata` all call `sidecarPath`).

**Do:**
1. Define `type SidecarLayout interface { SidecarPath(storePath string) string }` (a
   store-relative '/'-path for the AppleDouble container of a data path).
2. Implement the layouts:
   - `netatalkLayout` — current behaviour: `dir + "/._" + base` (and `._base` at root).
   - `osxZipLayout` — `__MACOSX/` + dir + `/._` + base (the OS-X archive convention).
   - `appleDoubleDirLayout` — `dir + "/.AppleDouble/" + base` (Netatalk `.AppleDouble/`
     folder form; confirm exact name vs spec/16).
3. Give `appleDoubleForkEngine` a `layout SidecarLayout` field; replace every
   `sidecarPath(x)` call with `e.layout.SidecarPath(x)`. Default to `netatalkLayout`.
4. Select the layout from config: add an optional `ShareSpec.Extra` key (e.g.
   `"appledouble_layout" = "netatalk|osx-zip|appledouble-dir"`) read in the appledouble
   factory (Phase 1's `RegisterForkAdapter` factory takes `base`; thread the spec in —
   either widen the factory signature to `(spec ShareSpec, base FileSystem)` or read a
   package-level resolved layout). Prefer widening the factory signature to take the
   `ShareSpec` so other adapters can read their own config too. Empty = netatalk.
5. Confirm the AppleDouble payload codec (`core/appledouble`) is unchanged — only the
   container LOCATION varies, not the byte format.

**Verify:** round-trip a resource fork + FinderInfo through each layout; a sidecar
written with `osx-zip` lands under `__MACOSX/`; `MoveMetadata`/`DeleteMetadata` follow
the layout. Add `core/fs/fork_layout_test.go`. Cross-check the `__MACOSX` convention
against a real OS-X zip in `/captures` if one exists. archtest green.

---

## Phase 3 — Adapter owns Rename/Remove atomically + `ForkContainers` capability

**Goal:** today `shareFS.Rename` does `FileSystem.Rename` then
`ForkEngine.MoveMetadata` (two steps, documented failure order — `core/fs/fs.go`
~480-494). Move that orchestration INTO the adapter so one `Rename` moves data + its
own containers, and expose the container paths so the §10d reactor can coordinate
same-host-path shares.

**Files:** `core/fs/fs.go` (`shareFS.Rename`/`Remove` ~480-494); `core/fs/fork.go`
(appledouble `MoveMetadata`/`DeleteMetadata`); `core/share/reactor.go` (the §10d
consumer); `/spec/16-storage-seam.md` (§9 rename/remove contract).

**Do:**
1. Add an optional capability:
   ```go
   // MetadataPaths returns the store-relative paths whose rename/remove must accompany
   // the data fork's (sidecars / AppleDouble dirs). Empty for adapters whose metadata
   // rides with the file (ads/xattr/native streams). The §10d coordination uses this so
   // a same-host-path peer knows which containers a foreign rename touched.
   type ForkContainers interface { MetadataPaths(storePath string) []string }
   ```
   Implement on `appleDoubleForkEngine` (returns `[]string{layout.SidecarPath(p)}`);
   `nofork`/`ads`/`xattr` either omit it or return nil.
2. Keep `shareFS.Rename`/`Remove` as the single entry, but ensure the data + metadata
   move is atomic-as-possible and the metadata-first/last ordering matches the §9
   contract (metadata-first on Remove so a failure leaves data to retry; data-first on
   Rename then metadata — preserve current ordering unless spec says otherwise). The
   net change is that the ADAPTER, not `shareFS`, decides what its containers are.
3. Wire coordination: when a rename/remove is published to the §10d bus
   (`core/fs/bus.go` `Event`), the peer service's `share.Reactor`
   (`core/share/reactor.go`) already resolves "which of my shares owns this HostPath".
   Have the reactor, on an `OpRename`/`OpDelete` under a shared root, consult that
   share's adapter `MetadataPaths` to know the sidecars moved too, and re-derive
   shortnames via the share's `NameEngine` (`shareFS.Names()`). Keep the wire-push
   DEFERRED (the reactor sink is still count/notify only — see fs-bus-coordination
   memory) but make the metadata + shortname state consistent.

**Don't:** implement AFP-attention / SMB-CHANGE_NOTIFY wire push (still deferred).

**Verify:** an EtherDFS-style rename on a host path shared with an AFP share moves the
data + sidecars; the AFP reactor observes the rename and its shortname mapping for the
new name resolves. Two same-path shares stay metadata-consistent across a rename. Test
in `core/share/reactor_test.go` + `core/fs/fork_test.go`. archtest green.

---

## Phase 4 — `applesingle` + true host-`native` adapter (adapter/fork/)

**Goal:** add the remaining adapters. `applesingle` (single-stream container, no
sidecar) is pure → `core/fs`. `native` (real host resource fork via OS syscalls —
HFS+ `..namedfork/rsrc`, a raw HFS disk image) is host-native → new `adapter/fork/`
ring, self-registering under build tags like the fs backends.

**Files:** new `core/fs/fork_applesingle.go`; new `adapter/fork/native/` package; a
`compose/registry/reg_fork_native.go` blank-import (mirror `reg_zipfs.go`); update the
`native` alias added in Phase 1 to resolve to the real adapter when built.

**Do:**
1. `applesingle` in `core/fs`: implement `ForkEngine` over a single AppleSingle
   container file (data + resource + FinderInfo in one stream, per `core/appledouble`
   codec — AppleSingle shares the entry format). Register `applesingle` via
   `RegisterForkAdapter`. No sidecar → `MetadataPaths` returns the container path (it's
   not a `._` sidecar but it IS a separate file, so coordination still needs it).
2. `native` in `adapter/fork/native/`: real host resource-fork access —
   `..namedfork/rsrc` on Darwin/HFS+, the resource fork of an `hfs-image` backend.
   Per-OS build-tagged (`_darwin.go` etc.), self-`RegisterForkAdapter("native", …)`
   from `init()`. Blank-imported by a build-tagged `compose/registry/reg_fork_native.go`.
   Remove the Phase-1 `native`→appledouble alias (or keep it as the fallback when the
   real adapter isn't linked, mirroring the macgarden/zipfs disabled-stub pattern —
   a `native` stub in core that errors "rebuild with -tags forknative" is the
   consistent choice).
3. Revisit the `macroman-native × xattr` cross-component rule in `validateShareSpec`
   (`core/fs/fs.go` ~496) and any new native×codec constraints — express them as the
   per-backend/cross-component rules from the earlier validator work.

**Verify:** AppleSingle round-trips a fork + FinderInfo; `native` (where buildable)
reads/writes a real HFS+ resource fork; archtest stays green (native code is in
`adapter/`, never core); headless `go build ./...` excludes the native ring cleanly.
Confirm the AppleSingle entry format against `/spec/16-storage-seam.md`.

---

## Sequencing

1 → 2 → 3 → 4. Phase 1 is the zero-behaviour-change foundation; land it first as its own
commit. Each phase is independently shippable and independently testable. Commit per
phase (milestone-commit style).
