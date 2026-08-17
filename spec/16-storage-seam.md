# Storage seam — forks, metastore, and filename codecs (M6)

This document specifies the on-disk and on-the-wire formats that the storage
seam (`core/fs`, `core/metastore`, `core/encoding`, `core/appledouble`) must
reproduce so AFP/SMB clients interoperate with Netatalk- and SFM-written volumes.
It is the reference for the §9 inversion: file services hold no storage-layout
knowledge and call only these interfaces.

## 1. Fork engines

A share's resource fork and Finder metadata are stored by one **fork backend**
(`fork_backend` in the share config), all implementing `fs.ForkEngine`. They
share the same logical payload — a 32-byte FinderInfo, an optional comment, and
the resource-fork bytes — and differ only in the container:

Each backend self-registers into the fork-adapter registry (`fork_registry.go`,
`RegisterForkAdapter`); a fork adapter is **mandatory** for every share (resolved by
name in `BuildShare`, default `appledouble`), so a fork-less share uses the explicit
`nofork` adapter, never a silent fallback.

| Backend | Container | Status |
|---|---|---|
| `appledouble-default` (aliases `appledouble`, `auto`) | `._name` AppleDouble v2 sidecar beside the file (Netatalk) | **implemented** (`core/fs/fork.go`) |
| `appledouble-osxzip` | `__MACOSX/dir/._name` sidecar (OS-X-created archives) | **implemented** — one base engine, sidecar-layout variant |
| `appledouble-dir` | `dir/.AppleDouble/name` sidecar (Netatalk folder form) | **implemented** — sidecar-layout variant |
| `applesingle` | TRUE AppleSingle: data + resource + FinderInfo in one container file (magic `0x00051600`); resource fork 4K-allocated, data fork last | **implemented** (`core/fs/fork_applesingle.go`) |
| `macbinary` | MacBinary II: 128-byte header + data fork + (128-padded) resource fork in one file | **implemented** (`core/fs/fork_macbinary.go`) |
| `ads` | NTFS alternate data stream (`name:AFP_Resource`, `name:AFP_AfpInfo`, `name:Comments`) — SFM layout | **implemented** (`core/fs/fork_ads.go`) |
| `xattr` | Netatalk extended-attribute layout (`org.netatalk.Metadata`, `org.netatalk.ResourceFork`) | **implemented** (`core/fs/fork_xattr.go`) |
| `hfs` | real HFS+/APFS host fork: resource fork via `..namedfork/rsrc`, FinderInfo via `com.apple.FinderInfo` xattr (macOS) | **implemented** (`adapter/fork/hfs`, darwin-only, no build tag) |
| `native` | per-OS ALIAS for the host's own layout: `ads` on Windows, `hfs` on darwin, `xattr` on Linux (`core/fs/fork_native*.go`) | **implemented** — resolves at build time; no tag |
| `derez` | DeRez/`rdump` TEXT sidecar for the resource fork + `idump` sidecar for type/creator — the resource fork is checked into git as diffable text (Elliot Nunn's macresources format) | **implemented** (`core/fs/fork_derez.go` + `core/macresources`) |
| `nofork` / `null` / `none` | discards metadata (explicit no-forks / placeholder shares) | implemented |

The `derez` backend deserves a note: it serialises the binary resource fork to Rez/DeRez
**text** (`<name>.rdump`) on write and re-serialises that text back to the binary fork on
read, with the Finder type/creator in a companion `<name>.idump`. It exists so a developer
working on a classic codebase (e.g. a CodeWarrior project) can keep resources diffable in
version control. The format and the reference implementation are Elliot Nunn's
([macresources](https://github.com/elliotnunn/macresources)); `core/macresources` is a Go
port credited in that package.

### 1a. AppleDouble v2 sidecar (`core/appledouble`)

The sidecar file is named `._<original>` in the same directory. Format (all
fields big-endian):

```
magic    uint32 = 0x00051607
version  uint32 = 0x00020000
filler   [16]byte
numEntries uint16
entries[numEntries]:
  id     uint32   (1=DataFork 2=ResourceFork 4=Comment 5=IconBW 9=FinderInfo)
  offset uint32   (from start of file)
  length uint32
... entry payloads ...
```

ClassicStack writes a canonical sidecar: a FinderInfo entry (32 bytes), an
optional Comment entry, then the ResourceFork entry. Round-trips through
`appledouble.Parse`/`appledouble.Build` are byte-stable for the canonical form.

### 1b. SFM ADS / `AfpInfo` (for the future `ads` backend)

Services for Macintosh (SFM) and modern SMB store the FinderInfo in an
`AFP_AfpInfo` named stream — a 60-byte record:

```
signature uint32 = 'AFP\0'  (0x41465000)
version   uint32 = 0x00010000
reserved1 uint32
backupTime uint32
finderInfo [32]byte
prodosInfo [6]byte
reserved2  [6]byte
```

The resource fork is the `AFP_Resource` stream and the Finder comment is the
`Comments` stream (the SFM `AFP_COMM_STREAM`). `core/fs/fork_ads.go` emits these
so a name written by ClassicStack is readable by Windows SFM/SMB and vice-versa;
the FinderInfo bytes are identical to the AppleDouble FinderInfo entry, so only
the container differs. Stream paths are addressed through the base `FileSystem`
using the host `path:stream` syntax (`name:AFP_Resource`, `name:AFP_AfpInfo`,
`name:Comments`); on a non-NTFS `FileSystem` these degrade to ordinary sidecar
paths, which keeps the record handling testable without NTFS but means the
on-disk *container* is host-native streams only when the base FileSystem maps
`path:stream` to real ADS. On `local_fs` over a real NTFS host they resolve to
genuine alternate data streams — verified end-to-end by `TestADSOverLocalFS_RealNTFS`
(the streams are real ADS, invisible to `ReadDir`, and ride along on move/delete).

The engine preserves `backupTime` and `prodosInfo` on a FinderInfo round-trip so
a record written by Windows SFM is not clobbered, and treats a missing or
wrong-signature `AFP_AfpInfo` stream as "no FinderInfo" rather than an error (SFM
tolerance). An empty `WriteComment` removes the `Comments` stream (SFM
RemoveComment semantics). The stream names MUST match NT SFM (`macfile.h`
`AFP_*_STREAM`).

The **volume-level** SFM streams — `AFP_IdIndex` (the CNID database) and
`AFP_DeskTop` (the desktop DB) — are NOT part of the per-file fork engine.
ClassicStack tracks CNIDs in the range-scannable metastore (`meta_ads.go`),
because CNID's subtree-rebind needs range scanning that SFM's single opaque
`AFP_IdIndex` stream cannot provide; the AFP desktop DB is a separate service
concern. Reproducing those two streams byte-for-byte would not improve
interoperability of an individual file's forks, so they are deliberately omitted.

The `AfpInfo` record type and its codec are the exported `fs.AfpInfo` DTO
(`core/fs/afpinfo.go`, `Marshal`/`UnmarshalAfpInfo`), the single source of truth
shared by `fork_ads.go` and the WinFsp mount client below.

### 1b-i. WinFsp mount client: forks as SFM streams

The `csmount` client (`client/winfsp`) can present a remote share's forks the
same way — as NTFS named streams using the NT Services-for-Macintosh names —
when native forks are enabled (`-fork native` → `Options.NativeForks`). Beside
the unnamed data stream it surfaces:

| stream | source (`fs.ForkEngine`) |
|---|---|
| `:AFP_Resource` | `OpenFork(ResourceFork)` |
| `:AFP_AfpInfo` | `Read`/`WriteFinderInfo` wrapped in the 60-byte `fs.AfpInfo` record |
| `:Comments` | `Read`/`WriteComment` |

`GetStreamInfo` enumerates only the streams that currently carry content, so a
plain file advertises just the data stream. Because go-winfsp exposes the stream
name to the open/create delegates as the `path:stream` suffix, the adapter peels
the suffix (only when `NativeForks` is on) and routes the handle to the fork; the
record streams (AfpInfo/Comments) are buffered in the handle and flushed back
through the `ForkEngine` on close. With native forks off the mount has no streams
and a `:stream` path is rejected, matching the pre-stream behaviour. This mirrors
the server-side `ads` layout above so a fork is addressed by the same stream name
whether ClassicStack is serving or mounting it. SFM's server-internal volume
streams (`AFP_DeskTop`, `AFP_IdIndex`) are not surfaced.

### 1b-ii. FUSE mount client: forks as xattrs

The same `csmount` client on macOS (macFUSE) and Linux (libfuse) presents forks
through `client/fuse` via cgofuse. Native forks (`-fork` empty / `passthrough` /
`native` / `hfs` / `ads` / `xattr`) map `fs.ForkEngine` onto host xattrs; sidecar
layouts still project `._name` / `.rdump` via `fork_export.go`. Names and blobs
are per-GOOS so a file copied off the mount is readable by that platform's usual
clients:

| platform | xattr | source |
|---|---|---|
| Darwin | `com.apple.FinderInfo` | 32-byte `ReadFinderInfo` / `WriteFinderInfo` |
| Darwin | `com.apple.ResourceFork` | `OpenFork(ResourceFork)`; Darwin `position` is a byte offset |
| Darwin | `file/..namedfork/rsrc` | same resource fork as a virtual path (not listed in `Readdir`) |
| Linux | `user.org.netatalk.Metadata` | 402-byte Netatalk Metadata EA (§1c); comments live in this header |
| Linux | `user.org.netatalk.ResourceFork` | raw resource-fork bytes |

Linux also accepts the unprefixed `org.netatalk.*` names if FUSE strips the
`user.` namespace. `Listxattr` advertises an attribute only when it has content.
The Metadata EA codec is the exported `fs.EncodeNetatalkMetadataEA` /
`ParseNetatalkMetadataEA` pair shared with `fork_xattr.go`. Linux FUSE support
is experimental.

### 1c. Netatalk EA layout (`core/fs/fork_xattr.go`)

Netatalk's `ea = sys` volume option stores each fork/metadata item as a host
extended attribute (`user.org.netatalk.Metadata`, `user.org.netatalk.ResourceFork`).
`core/fs/fork_xattr.go` reads/writes that layout so a ClassicStack share over an
existing Netatalk 3.x/4.x volume sees the same forks:

- **`user.org.netatalk.Metadata`** — a fixed **402-byte** (`AD_DATASZ_EA`)
  AppleDouble v2 *header*: the same magic/version/entry-table layout as a `._name`
  sidecar (so FinderInfo and the comment round-trip byte-for-byte through the
  `core/appledouble` codec), but with two differences: the 16-byte filler is
  `"Netatalk        "` (space-padded) instead of zeros, and the resource-fork
  `ad_entry` records the fork **length only** — the bytes are *out-of-line* in the
  separate ResourceFork EA, so the blob is a pure metadata header padded to the
  fixed size. Because the recorded length exceeds the blob, a generic AppleDouble
  parser's bounds check skips that entry; the engine reads the length straight
  from the entry table.
- **`user.org.netatalk.ResourceFork`** — the raw resource-fork bytes.

The engine addresses both EAs through the base `FileSystem` using a
`"path\x00ea\x00<name>"` key (analogous to the `ads` engine's `path:stream`); on
a host FileSystem that maps that key to a real extended attribute the container
is a true xattr, and on any other FileSystem it degrades to an ordinary path key,
keeping the record handling testable without an xattr-capable host. Writing the
resource fork refreshes the Metadata EA's recorded length on Sync/Close so the
two EAs stay in step (Netatalk's invariant). A missing or wrong-magic Metadata EA
is treated as "no metadata" rather than an error (Netatalk tolerance). The
`macroman-native` filename codec is rejected with `xattr` (validated in
`BuildShare`), since a Netatalk EA volume serves UTF-8/Unicode wire names.

## 2. Filename codecs (`core/fs/codec.go`, `core/encoding`)

A `fs.FilenameCodec` converts one path element between a **client wire charset**
and the share's **store-native bytes**, reversibly:
`Encode(Decode(wire, c), c) == wire` for every `c` in `Wire()`.

### 2a. Wire charset is per request

The service threads the wire charset from its protocol, not from `runtime.GOOS`:

| Wire | Source | Notes |
|---|---|---|
| `WireMacRoman` | AFP `kFPShortName` / `kFPLongName` path type | MacRoman ↔ store |
| `WireUTF8` | AFP `kFPUTF8Name` path type | UTF-8 ↔ store |
| `WireANSI` | SMB legacy/DOS (OEM code page) | single-byte ↔ store |
| `WireUTF16` | SMB NT (Unicode flag) | UTF-16LE ↔ store |

A codec advertises only the charsets it implements via `Wire()`; an unsupported
request fails with `ErrWireUnsupported` rather than mangling the name.

### 2b. Reserved-character escaping (`0xNN` tokens)

The store charset declares a backend `ReservedSet` (`posix`, `ntfs`, …). A wire
rune that the backend cannot hold in a path element — `/` on POSIX, the Win32
reserved set on NTFS, plus all control chars `< 0x20` — is escaped reversibly as
the ASCII token `0xNN` (uppercase, two hex digits of the code point). Decoding
reverses tokens whose code point is reserved for that backend. This is the
lifted `service/afp/path_codec.go` behaviour, now backend-declared instead of
`runtime.GOOS`-switched. A name that is genuinely unrepresentable in the store
charset returns `ErrUnrepresentable` (→ protocol "illegal name"); it is never
written as a mangled path.

### 2c. Transcoders

`core/encoding` provides hand-written, reflection-free, TinyGo-safe tables:

- **MacRoman ↔ UTF-8** — the full 256-entry MacRoman table.
- **UTF-16LE ↔ UTF-8** (`WireUTF16`, SMB NT) — via stdlib `unicode/utf16`.
  Strips an optional leading BOM, resolves surrogate pairs, and rejects
  **odd-length input** (a truncated final unit) with `ErrTruncatedUTF16`
  (surfaced to the codec as `ErrUnrepresentable`) — never a panic or silent drop.
- **ANSI (OEM code page) ↔ UTF-8** (`WireANSI`, SMB legacy/DOS).

#### Chosen ANSI code page: CP437

The default OEM code page is **CP437** (the original IBM PC / DOS code page),
because the legacy SMB clients ClassicStack targets — Windows for Workgroups
3.11, DOS LAN Manager — negotiate CP437 as their OEM character set. The low 7
bits are ASCII-identity; the upper half (0x80–0xFF) uses the canonical IBM CP437
table. Additional pages (CP850, CP1252) can be added the same hand-written way
and selected from the SMB-negotiated dialect; until then a non-CP437 request
fails with `ErrUnmappableANSI` rather than guessing.

**Observed client quirk:** WfW 3.11 sends filenames in the *negotiated* OEM page,
not necessarily the host's — a share serving both DOS and NT clients therefore
relies on the per-request wire charset (§2a), never a fixed server-side charset.

## 3. Metastore (`core/metastore`)

CNID/shortname/desktop state rides on a keyed `metastore.Store` (opaque
key/value bytes; the caller owns the schema). The default kind is `mem`
(in-memory, snapshotting to a file); `sqlite` is a build-tagged adapter
(`adapter/metastore/sqlite`, tag `sqlite`/`all`) registering the `sqlite` kind.
SQLite is therefore **droppable**: the default build links no SQLite and the mem
store works. `core/metastore.CNIDStore` is the AFP CNID registry re-expressed
over this seam — its key layout (`c/p/<cnid>`, `c/i/<path>`, `c/seq`) is the same
regardless of which store kind backs it, so a `mem`-snapshotted volume and a
`sqlite` volume preserve CNIDs identically across restarts.

The metastore is the **definitive per-share metadata store**: every typed facade
(CNID, short/medium name binding, DOS attributes) rides the one `Store` a share
opens, so the `sqlite` kind is the single durable home for all of them and `mem`
is the embedded/TinyGo fallback. The facades and their key prefixes:

| facade | keys | purpose |
|--------|------|---------|
| `CNIDStore` | `c/p/`, `c/i/`, `c/seq` | AFP catalog node IDs |
| `derivedNameEngine` | `n/f/<kind>/`, `n/r/<kind>/` | 8.3 short / 31-char medium name bindings |
| `DOSAttrStore` | `d/a/` | DOS attributes (RO/HID/SYS/ARCH + create-time) |

### 3a. Name casing (`core/fs/name.go`)

The `short` and `medium` name engines are **case-insensitive for lookup but
preserve the stored case** — Windows-FS semantics, identical on Windows, macOS,
and Linux (the engine never consults the host's own case rules). Both the forward
(`long → derived`) and reverse (`derived → long`) keys are upper-cased, so:

- A request for `Report.txt` and one for `REPORT.TXT` resolve to the **same**
  binding (the first-stored case is kept as the value); they do not produce two
  bindings.
- Two genuinely different long names that fold to the same key **collide** and the
  second gets a fresh `~N` (8.3) / `-N` (medium) suffix.
- A `medium` (31-char) name round-trips in its **original** case (`MyMixedCase`
  stays mixed), but is found case-insensitively.

8.3 short names are upper-cased on the wire (FAT convention); the 31-char medium
name (classic AFP "long" name; NetWare and DOS-redirector long names) keeps case.
AFP serves its wire long name through `Volume.MediumName`, NCP its 8.3 field
through `Volume.appendFileName`, SMB its short name through the share
`ShortName`, and EtherDFS reverses a wire 8.3 name to the host name through
`NameEngine.ToLong` — one generator, four services.

### 3b. DOS attributes (`core/fs/dosattr.go`, `core/metastore/dosattr.go`)

DOS/FAT file attributes (read-only, hidden, system, archive) and the DOS
create-time have no home on a POSIX host (and even on Windows are unavailable to
the OS 8.3-name service on non-system drives), so a share persists them through a
`DOSAttrStore` selected per share by `dos_attr_backend`:

| backend | storage | availability |
|---------|---------|--------------|
| `metastore` | the share's `Store` (`d/a/<path>`) | always; definitive + cache |
| `sidecar` | a `.dosattr/<name>` companion holding the XATTR_DOSINFO blob | every filesystem |
| `xattr` | the host file's `user.DOSATTRIB` extended attribute | `xattr` tag, linux/darwin |
| `native` | the Windows host file attributes (`Get/SetFileAttributes`) | `windows` GOOS |
| `auto` (default) | native → xattr → sidecar, whichever the host supports, always caching in the metastore | — |

The on-disk value is the **Samba `XATTR_DOSINFO` version-3 record**
(`metastore.EncodeDOSInfo`/`DecodeDOSInfo`): version(2) + valid_flags(4) +
attrib(4) + ext_attrib(4) + reserved(4) + create_time(8, NTTIME). Because the
metastore, sidecar, and xattr backends share this one wire format, a value written
by any of them is readable by the others **and by Samba** — a ClassicStack SMB
share over a directory Samba also serves sees the same hidden/system bits.

A backend needing a real host path resolves it through the optional
`fs.HostPather` (implemented by `local_fs`); a synthetic backend (`memfs`,
`zipfs`) that is not a `HostPather` falls back to the metastore, which needs no
host path. The built share exposes its store through the optional `fs.DOSAttred`
interface (`DOSAttrs() DOSAttrStore`); a file service type-asserts the `ForkFS` to
reach it, OR-ing the stored RO/HID/SYS/ARCH bits onto the structural
Directory/Archive bits it derives from the entry. SMB persists them via
`TRANS2_SET_PATH/FILE_INFORMATION`; EtherDFS via `AL_SETATTR`.
