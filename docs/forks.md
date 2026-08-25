---
title: "Forks & Metadata"
weight: 10
---

# Forks & metadata

How classic Mac and DOS file metadata — resource forks, Finder type/creator, HFS attribute
bytes, and DOS attributes (read-only/hidden/system/archive) — is represented and stored, both
server-side (serving a host directory as an AFP/SMB/NCP/EtherDFS share) and client-side
(`csfs`/`csmount`'s `-fork` flag). See [cli.md](cli.md) for the flag syntax and
[`spec/16-storage-seam.md`](../spec/16-storage-seam.md) for the underlying design doc this page
summarizes.

---

## 1. One fork engine per share

Every share — an `[[afpvolumes]]` entry, `[[smbshares]]` share, `[[ncpvolumes]]` volume, or
`[[etherdfsdrives]]` drive — resolves to **exactly one** fork engine when it's built; there is no
"none" state by accident, only the explicit `nofork` choice. The engine is responsible for:

- **The resource fork** — an arbitrary byte stream alongside the data fork.
- **Finder info** — the 32-byte record classic Mac clients read/write per file, containing the
  4-byte type code, 4-byte creator code, Finder flags, and window position.
- **The Finder comment** — a short text field ("Get Info" comments), capped at 199 bytes.

DOS attributes (read-only/hidden/system/archive) are a related but **separate** concern, handled
by the share's `MetaEngine` rather than its fork engine — see [§5](#5-dos-attributes) below.

Configure the backend per-share with `fork_backend` in `server.toml` (see
[config.md](config.md#afp--afpvolumes)); leaving it blank picks a per-platform default. The
registered backend names, and where each one puts its bytes:

| `fork_backend` | Storage location | Platform |
|---|---|---|
| `appledouble` (also `auto`) | `._name` sidecar beside the file | any |
| `appledouble-osxzip` | `__MACOSX/dir/._name` | any (matches what macOS puts in a zip) |
| `appledouble-dir` | `dir/.AppleDouble/name` | any (legacy Netatalk folder layout) |
| `applesingle` | one self-contained file replacing the data file | any |
| `macbinary` | one self-contained MacBinary II file | any |
| `derez` | `name.rdump` (text) + `name.idump` (8 bytes) | any |
| `ads` | NTFS alternate data streams | Windows (NTFS volumes) |
| `xattr` | Netatalk-compatible extended attributes | Linux |
| `hfs` | real HFS+/APFS resource fork + `com.apple.FinderInfo` | macOS only |
| `native` | alias: `ads` on Windows, `hfs` on macOS, `xattr` on Linux | any |
| `passthrough` | forwards to a base filesystem that is itself fork-aware (e.g. a client mount of an AFP share) | any |
| `nofork` (also `null`, `none`) | discards all resource fork / Finder info / comment writes | any |

An unregistered name fails share construction outright rather than silently falling back to
`nofork`.

---

## 2. AppleDouble (`appledouble`, `appledouble-osxzip`, `appledouble-dir`)

The default backend, and the one Netatalk, macOS, and Samba all understand. All three variants
share one binary layout (AppleDouble v2) and differ only in where the sidecar file lives.

**File layout** (`core/appledouble`):

```
offset 0   uint32  magic    0x00051607
offset 4   uint32  version  0x00020000
offset 8   [16]byte filler
offset 24  uint16  entry count
offset 26  entry table: N × { uint32 id, uint32 offset, uint32 length }
...        entry data
```

A file this project writes always contains, in order: a Finder-info entry (32 bytes), an optional
comment entry, then the resource-fork entry. Reading is lenient — an entry whose recorded
offset/length doesn't fit inside the file is skipped rather than aborting the whole parse, so a
sidecar written by another AppleDouble implementation with entries this project doesn't model
(icons, ProDOS info, ...) still parses cleanly.

**Sidecar path, per variant:**

| Backend | Sidecar for `dir/name` |
|---|---|
| `appledouble` / `auto` | `dir/._name` |
| `appledouble-osxzip` | `__MACOSX/dir/._name` |
| `appledouble-dir` | `dir/.AppleDouble/name` |

Sidecar names/directories (`._*`, `.AppleDouble`, `__MACOSX`) are hidden from directory listings
shown to clients.

---

## 3. AppleSingle (`applesingle`) and MacBinary (`macbinary`)

Unlike AppleDouble, these two replace the data file with **one self-contained container** —
there's no separate `._name`, so a rename of the file already carries all its metadata with it.

**AppleSingle** (magic `0x00051600`, version `0x00020000`): Finder info first (always present, 32
bytes), then an optional comment, then the resource fork (padded to 4 KiB chunks so it can grow
in place), then the data fork last — the data fork sits at the end because it's the piece most
likely to be appended to, so growing it doesn't disturb the other entries' offsets.

**MacBinary II** (used by classic download tools and BBS/FTP transfers of the era): a fixed
128-byte header — filename, 4-byte type, 4-byte creator, Finder flags, data-fork length,
resource-fork length, dates, and a version byte that must read `129` for this to be trusted as a
real MacBinary file — followed by the data fork padded to a 128-byte boundary, then the resource
fork likewise padded. A file that doesn't look like well-formed MacBinary (wrong version byte,
non-zero reserved bytes, or exceeding the 63-byte MacBinary filename limit) is rejected rather than
silently overwritten.

Neither backend can represent a Finder comment as a separate entity from what it stores; consult
the relevant format's own comment-entry support instead of expecting a third file.

---

## 4. `derez` — text resource forks for version control

Built for keeping a classic-Mac resource fork (e.g. a CodeWarrior project) readably diffable in
git, using the same textual "DeRez" dump format Apple's own `Rez`/`DeRez` tools produce. Storage:

- `name.rdump` — the resource fork rendered as Rez/DeRez text, one resource per block:
  ```
  data 'TYPE' (128, "example", purgeable) {
  	$"0011 2233 4455 6677 8899 AABB CCDD EEFF"  /* ........ */
  };
  ```
  16 bytes per line in hex with an ASCII-art comment column. Known attribute flags
  (`sysheap`/`purgeable`/`locked`/`protected`/`preload`) are named; anything else appears as a
  raw `$HH` literal. A `'` or non-printable byte inside a resource type, or a non-printable byte
  inside a name, is escaped as `\0xHH`.
- `name.idump` — exactly 8 bytes: the 4-byte Finder type followed by the 4-byte creator.

This backend cannot store a Finder comment (reads return "no comment"; writes are silently
dropped) — it only round-trips the resource fork and type/creator. It's inspired by, and
attributed in [`NOTICE`](../NOTICE) to, Elliot Nunn's `macresources`/`rdump`.

---

## 5. Host-native backends

### `ads` — Windows NTFS alternate data streams

Forks ride real NTFS alternate data streams, using the exact stream names NT Services for
Macintosh used, so a volume this project writes is also readable by legacy SFM tooling:

| Stream | Contents |
|---|---|
| `name:AFP_Resource` | the resource fork, raw bytes |
| `name:AFP_AfpInfo` | a 60-byte record: signature `AFP\0`, version, backup time, the 32-byte Finder info, and 6 bytes of ProDOS info |
| `name:Comments` | the Finder comment; writing an empty comment **removes** the stream rather than leaving a zero-length one, matching SFM's own `RemoveComment` behavior |

Requires an actual NTFS volume for a plain host directory (checked at share-build time; a
non-NTFS host path fails to build with a clear error) — this restriction doesn't apply when the
base filesystem is itself a fork-aware client mount (an AFP connection, for instance), where `ads`
instead just relabels that connection's native forks under the SFM stream names.

### `xattr` — Linux, Netatalk-compatible

Two extended attributes per file, matching Netatalk's own on-disk layout so a share migrated from
or to Netatalk keeps working:

- `user.org.netatalk.Metadata` — a fixed 402-byte AppleDouble-v2 *header* (reusing the same format
  as an `appledouble` sidecar, but with a `"Netatalk        "` filler instead of zero bytes) that
  records the Finder info and the resource fork's *length*.
- `user.org.netatalk.ResourceFork` — the resource fork's actual bytes, stored separately from
  the length that describes them.

Because `xattr` serves wire-native UTF-8/Unicode names rather than raw MacRoman bytes, it cannot
be paired with the `macroman-native` filename codec (see [filename-encoding.md](filename-encoding.md)) — share construction rejects that combination.

### `hfs` — macOS, real HFS+/APFS forks

Only available on a macOS build. The resource fork is opened through the classic macOS "named
fork" pseudo-path (`file/..namedfork/rsrc`) rather than an extended attribute; Finder info is the
32-byte `com.apple.FinderInfo` extended attribute. HFS+/APFS has no per-file comment field of its
own (comments there are historically an AFP Desktop-DB concept), so comment reads/writes on this
backend are no-ops.

### `native`

An alias, resolved per host OS at share-build time: `ads` on Windows, `hfs` on macOS, `xattr` on
Linux. Use this when you want "whatever this host does best" without hard-coding a backend name
that would fail to build on a different platform.

---

## 6. Client-side: `csfs`/`csmount` `-fork`

The file client (see [cli.md §2](cli.md#2-file-client)) accepts the same backend names for its
`-fork` flag, projecting a remote share's forks into host storage instead of a local share's:

- **Windows:** `appledouble | applesingle | macbinary | derez | passthrough | native | ads | nofork`
- **macOS/Linux** (built with `-tags fuse`): `appledouble | applesingle | macbinary | derez | passthrough | native | hfs | xattr | ads | nofork`

Two behaviors specific to the client side:

- **`native` is resolved by the *host* OS you're mounting/copying onto**, not by the remote
  protocol — `""`, `passthrough`, `native`, `hfs`, `ads`, and `xattr` are all treated as "expose
  forks as this host's own native attributes/streams"; any other value (`appledouble`,
  `applesingle`, `macbinary`, `derez`) instead projects sidecar files into the mount/copy
  namespace.
- **Reverse sidecar projection:** when the remote volume already carries forks natively on the
  wire (an AFP share, for instance) but you asked for a sidecar layout anyway (`-fork derez` or
  any `appledouble*` variant), `csfs`/`csmount` can't literally write a `._name` file to the
  remote server — instead it *synthesizes* the `._name`/`.rdump`/`.idump` paths on the fly from
  the wire fork/Finder-info calls, so directory listings and `cp`/`get` still see the sidecar
  files you asked for. This synthesis is only defined for the AppleDouble family and `derez`;
  `applesingle`/`macbinary` (whole-file containers) aren't projected this way.

---

## 7. Finder type/creator defaults — `extmap.conf`

A file with **no stored Finder info at all** (freshly created from a non-Mac client, or copied in
from a plain host filesystem) is given a default type/creator by extension, read from
`extmap.conf` — a Netatalk-style text file, one mapping per line:

```text
.txt "TEXT" "ttxt"
.jpg "JPEG" "ogle"
```

Each line is an extension, then the 4-character type and 4-character creator each in double
quotes. Blank lines and `#`-comments are ignored. This is purely a **fallback**: a file that
already has real stored Finder info (written by an actual Mac client, or projected in by a
`-fork` backend) never consults the extension map.

Configure per-volume with `extmap_path` in `server.toml` (see [config.md](config.md)); leaving it
blank uses the server-wide default map.

---

## 8. DOS attributes

Read-only, hidden, system, and archive are tracked independently of the fork engine, through the
share's `MetaEngine` (`meta_backend` in `server.toml` — see [config.md](config.md)). The bit
values are the standard FAT/NTFS ones and are identical across every protocol this project serves:

| Bit | Attribute |
|---|---|
| `0x01` | Read-only |
| `0x02` | Hidden |
| `0x04` | System |
| `0x08` | Volume label (structural, never stored) |
| `0x10` | Directory (structural, derived from the entry, never stored) |
| `0x20` | Archive |

Only read-only/hidden/system/archive are ever persisted; directory and volume-label are always
derived fresh from the filesystem entry itself.

**Where the bits actually live** depends on `meta_backend` (blank picks a per-platform default —
`ads` on an NTFS-backed Windows share, `xattr` on Linux, else the universal `metastore`
fallback):

- **`ads` (Windows):** the DOS attribute bits **are** the file's real NTFS attributes —
  read/written directly via the Win32 file-attribute APIs, so they show up to Explorer and every
  other Windows tool exactly as you'd expect, and any other attribute bit the OS sets
  (e.g. compressed) is preserved untouched.
- **`xattr` (Linux, when built with the `xattr` tag):** stored in the `user.DOSATTRIB` extended
  attribute — the same attribute name Samba uses, so a share migrated to/from Samba keeps its
  attributes.
- **`metastore` (universal fallback):** cached in the share's own metadata store, keyed by path.
  Used automatically wherever the host doesn't support the platform-native option (e.g. a non-NTFS
  volume on Windows, or Linux without xattr support), and always available as an explicit choice.

Wherever the value is persisted, it's encoded in Samba's own `XATTR_DOSINFO` version-3 wire
format (version, a valid-fields bitmask, the attribute bitmask, and an NT creation-time field),
so a value written by one backend, by Samba itself, or by another ClassicStack share on the same
host, is read back correctly by any of them.

---

## 9. See also

- [cli.md](cli.md) — the `-fork` flag on `csfs`/`csmount`.
- [config.md](config.md) — `fork_backend`, `filename_codec`, `meta_backend`, `extmap_path`
  server.toml keys.
- [filename-encoding.md](filename-encoding.md) — how filenames themselves are transcoded; a
  related but separate concern from the forks/attributes covered here.
- [`spec/16-storage-seam.md`](../spec/16-storage-seam.md) — the underlying design document.
