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

| Backend | Container | Status |
|---|---|---|
| `appledouble` | `._name` AppleDouble v2 sidecar next to the file | **implemented** (`core/fs/fork.go`) |
| `native` / `auto` | host-native fork (HFS+/APFS) | delegates to `appledouble` until per-platform support lands |
| `ads` | NTFS alternate data stream (`name:AFP_Resource`, `name:AFP_AfpInfo`) — SFM layout | **implemented** (`core/fs/fork_ads.go`) |
| `xattr` | Netatalk extended-attribute layout | delegates to `appledouble` (M7 interop) |
| `null` / `none` | discards metadata (placeholder shares) | implemented |

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

The resource fork is the `AFP_Resource` stream. `core/fs/fork_ads.go` emits this
record so a name written by ClassicStack is readable by Windows SFM/SMB and
vice-versa; the FinderInfo bytes are identical to the AppleDouble FinderInfo
entry, so only the container differs. Stream paths are addressed through the base
`FileSystem` using the host `path:stream` syntax (`name:AFP_Resource`,
`name:AFP_AfpInfo`); on a non-NTFS `FileSystem` these degrade to ordinary sidecar
paths, which keeps the record handling testable without NTFS but means the
on-disk *container* is host-native streams only when the base FileSystem maps
`path:stream` to real ADS. The engine preserves `backupTime` and `prodosInfo` on
a FinderInfo round-trip so a record written by Windows SFM is not clobbered, and
treats a missing or wrong-signature `AFP_AfpInfo` stream as "no FinderInfo"
rather than an error (SFM tolerance). The SFM ADS layout has no comment stream,
so AFP comments are not persisted on `ads` shares.

### 1c. Netatalk EA layout (for the future `xattr` backend)

Netatalk's `ea` volume option stores each fork/metadata item as a host extended
attribute (`user.org.netatalk.Metadata`, `user.org.netatalk.ResourceFork`). The
metadata EA holds the same FinderInfo bytes plus Netatalk's `ad_entry` table.
The `xattr` backend MUST read/write that EA layout so a ClassicStack share over
an existing Netatalk volume sees the same forks.

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
