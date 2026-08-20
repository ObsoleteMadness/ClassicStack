# Finder catalog — addressing, capabilities, and chrome

This document specifies the operator Finder catalog contract shared by the
ClassicStack HTTP adapter (`adapter/control/finder`) and ClassicStack-web
(`Catalog` / `FinderAPI`). It is the file-browser surface, not a file-service
wire protocol.

## 1. One addressing scheme per catalog

A catalog node is **either** CNID-addressed **or** path-addressed. Parent and
child use the same pair. Mixing `id`/`parentId` with `path`/`parentPath` on one
node is forbidden.

| Scheme | `addressBy` | Identity fields | Volumes |
|---|---|---|---|
| CNID | `cnid` | `id`, `parentId` (uint32) | AFP (remote, local AFP, IndexedDB VirtualFS) |
| Path | `path` | `path`, `parentPath` (store-relative, `'/'`-separated; `''` = volume root) | SMB, NCP, EtherDFS (remote and local) |

The node JSON carries a discriminant `addr` equal to the volume’s
`capabilities.addressBy`:

```
{ "addr": "cnid", "id": 14, "parentId": 2, "name": "BAR", ... }
{ "addr": "path", "path": "FOO/BAR", "parentPath": "FOO", "name": "BAR", ... }
```

AFP root CNID is `2` (AFP Catalog Node ID root). Path-volume root is `""`.

**Not catalog keys:** SMB FID, NCP directory/file handles, EtherDFS FileID/DirID.
Those are session-local open/find slots. Finder does **not** allocate synthetic
CNIDs (`EnsureCNID`) for path volumes.

Addressing follows the **session protocol**, not “does MetaEngine exist”:
`local:afp:…` → CNID (real share CNIDs); `local:smb:…` / NCP / EtherDFS → path.

## 2. Native ops vs translation

`get` / `children` / `lookup` / `mkdir` / `create` / `rename` / `move` / `remove`
are **scheme-pure**: a CNID catalog takes numeric CNIDs; a path catalog takes
store paths. HTTP native routes take `id` **xor** `path` matching the session
scheme. Sending the wrong kind is `400`.

Path ↔ CNID translation is a **separate** API. It does not put `path` on AFP
nodes:

| Call | HTTP | Behaviour |
|---|---|---|
| `resolvePath(path)` | `GET /finder/resolve?session=&path=` | Store path → native node. CNID catalog returns `{addr:cnid,…}`; path catalog is `get(path)`. |
| `pathOf(ref)` | `GET /finder/path?session=&id=` (CNID) or the path itself | Native ref → store path for display, bookmarks, paste. |

Local AFP implements `resolvePath` / `pathOf` with `MetaEngine.CNID` /
`PathForCNID` (one lookup). Remote AFP / IndexedDB may walk `lookup` from root
(`''` or CNID `2`). `lookup(parent, name)` remains one pathname component.

URL restore: AFP may pass a store path through `resolvePath`, then cwd is the
CNID. That is not `get(path)` on a CNID catalog.

## 3. Dates

Finder DTO / `VNode` timestamps are **Unix milliseconds** (JavaScript `Date`
native). AFP Mac time and DOS create time convert at the catalog edge. Finder
does not call `fromMacTime` on catalog fields.

## 4. Capabilities

`Catalog.capabilities()` (and `SessionInfo.capabilities` on open/connect) is the
volume’s declared feature set plus identity. FinderWindow does **not** branch on
`shareKind` for Get Info field sets, create/rename rules, or catalog I/O.

### 4a. Identity (chrome only)

| Field | Meaning |
|---|---|
| `shareKind` | `local` \| `afp` \| `smb` \| `ncp` \| `etherdfs` |
| `protocol` | For `shareKind: local`, the live service (`afp` / `smb` / `ncp` / `etherdfs`) |
| `filesystem` | Store backend (`local_fs`, `memfs`, `zipfs`, client scheme) |
| `transport` | `tcp` / `ddp` / `ipx` / `nbf` / `etherdfs` |
| `forkBackend` | `appledouble` / `nofork` / `passthrough` / `hfs` / `ads` / … |
| `dialect`, `os` | Optional Get Info chrome |

Unknown `shareKind` / `filesystem` values fall back to a generic disk glyph.
Finder must still list the volume.

Chrome table (decoration/formatting only): SMB → Windows glyph, NCP → Novell,
AFP → AppleShare, EtherDFS → DOS drive. Local shares use `protocol` so
`local`+`smb` still looks like a Windows share. Status-bar path rendering uses
`pathFormat` (`posix` / `mac` / `dos` / `ncp`); store paths stay `'/'`-separated.

### 4b. Features

| Field | Meaning |
|---|---|
| `addressBy` | `cnid` \| `path` — identity scheme for every node on this volume |
| `readOnly` | Mutations rejected |
| `resourceFork`, `finderInfo`, `desktopIcons`, `resourceIcons` | Dual-fork / FinderInfo / icon sources |
| `names` | `long` / `medium` / `short` present |
| `maxNameBytes` | Per name kind |
| `nameCase` | `preserve` \| `upper` \| `insensitive` |
| `dates` | `created` / `modified` / `accessed` / `backup` |
| `attributes` | `{id, label, type:'bool', editable?}` file flags |
| `hideAttribute` | Attr id that hides listing rows (`invisible` or `hidden`) |
| `pathFormat` | Display path punctuation only |

File-flag ids: `readonly`, `hidden`, `system`, `archive`, `invisible`, `locked`.
Per-file values live in `node.attrs` (not mixed with identity). `writeAttrs`
patches those ids onto `MetaEngine.SetAttrs` and/or FinderInfo flag bits.

Feature flags start from the **protocol** preset (AFP has Macintosh metadata;
SMB/NCP/EtherDFS do not). The open `ForkFS` can only turn Mac flags **off**
(`nofork`); AppleDouble / passthrough never promote `resourceFork` /
`finderInfo` onto a protocol that does not store them. `VolumeView` on the base
FS fills names/dates/attributes. A local share without `VolumeView` uses the
engine union for those fields, still gated by the protocol preset for forks.
Identity is copied from the session. Do not switch on `sess.Kind` to decide
which **feature** fields exist.

Connect may still layer AppleDouble over remote SMB/NCP/EtherDFS so `._`
sidecars stay hidden from listings; `identity.shareKind` remains `smb` /
`ncp` / `etherdfs` for the volume glyph, and Get Info does not show type/creator
or Macintosh resource-fork UI.

## 5. Cross-volume copy

`CrossTransferRequest` uses the **native** `NodeRef` of each session (`srcId` is
a CNID number or a path string according to the source catalog). A path pasted
onto an AFP destination goes through `resolvePath` first.
