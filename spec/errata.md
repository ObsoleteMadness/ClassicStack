# Spec Errata

This document records places where ClassicStack's wire behavior intentionally differs from the published spec, because the spec contradicts what real clients actually require. Each entry cites the spec section we deviate from, the client behavior that drove the change, and the file/function where the deviation lives.

## CIFS / SMB1

### SMB_COM_SEARCH FileName padding ([MS-CIFS] 2.2.4.58.2)

**Spec:** *"The character string MUST be padded with ' ' (space) characters, as necessary, to reach 12 bytes in length. The final byte of the field MUST contain the terminating null character."* — i.e. `MYFILE.TXT  \0`.

**Observed:** Windows for Workgroups 3.11 treats the 13-byte FileName field as a NUL-terminated C string starting at byte 0. With spec-compliant space padding, the File Manager UI shows entries as `FOOD       ` (spaces visible in column, can't be opened by name).

**What we do:** NUL-pad after the name (`FOOD\0\0\0\0\0\0\0\0\0`). This matches Samba and NT4's behavior, and is what every real CIFS client actually expects.

**Where:** `service/smb/command_fs_search.go` — `formatSearchFileName`.

### SMB_COM_READ_MPX ([MS-CIFS] 2.2.4.23)

**Spec:** *"The server returns the requested data in one or more response messages. Each response carries Offset, Count, DataLength, and DataOffset; the client reassembles by file Offset and stops once Count bytes have been delivered."* — i.e. a single well-formed WCT=8 response with `Count == DataLength` and `Remaining = 0xFFFF` should satisfy the read.

**Observed:** Windows for Workgroups 3.11 / Win9x over Direct IPX (NT LM 0.12 dialect) silently rejects spec-compliant single-response replies. The client retransmits the same Read MPX request at file offset 0 forever, never advancing — see `captures/ipx.pcap` frames 365–393 (FID 0x0003, MID 35457, MaxCount 4096) and frames 415+ (FID 0x0004). The response on the wire was structurally correct: WCT=8, Offset=0, Count=4096, Remaining=0xFFFF, DataLength=4096, DataOffset=52, ByteCount=4097, valid Pad+Data. The exact reason Win9x refuses it is unknown; the multi-response streaming form may be required, or some MID/dialect quirk we have not reverse-engineered.

**What we do:** Reject with `ERRSRV/ERRuseSTD` (`STATUS_SMB_USE_STANDARD`), which prompts the client to fall back to `SMB_COM_READ`. This is exactly what Samba's `reply_readbmpx` (source3/smbd/reply.c) has done since the 1990s. Costs one extra round-trip per chunk; in exchange the transfer actually completes.

**Where:** `service/smb/command_file_io.go` — `handleReadMPX`.

### SMB_COM_SEARCH MaxCount ([MS-CIFS] 2.2.4.58.1)

**Spec:** *"This value represents the maximum number of entries across the entirety of the search, not just the initial response."*

**Observed:** WfW 3.11 sends `MaxCount=1` on the initial Search request and `MaxCount=20` on every continuation. A strict session-wide reading would mean the first response exhausts the budget and we should immediately return ERRnofiles to every continuation — but WfW clearly expects the per-response semantics, and so does every other observed CIFS client.

**What we do:** Treat MaxCount as a per-response cap: return up to MaxCount entries from this call, retain the rest under the search handle for the next continuation.

**Where:** `service/smb/command_fs_search.go` — `handleSearch`.

### FS command engine path resolution over the share seam (M7)

**Spec:** [MS-CIFS] file/path commands carry a server-side path that the server resolves against the share root.

**Observed / design:** the legacy `service/smb` resolved wire paths with a DOS-name-mangling fuzzy matcher (`findDOSLikeComponentMatch`: uppercased, punctuation-stripped, prefix-or-truncation matching, e.g. `VOLUME68K` → `Volume 68k`). That conflated two concerns — wire→store charset transcoding and 8.3 short-name resolution — inside the SMB service, which the §9 refactor inverts.

**What we do:** the new `core/service/smb` FS command engine resolves a wire path through the share's `FilenameCodec` only (`Share.ResolvePath`, threading the per-request UTF-16/ANSI charset off the FLAGS2 Unicode bit), then reaches the backend via `sh.FS()`. Exact (codec-decoded) names resolve directly; the FileSystem backend owns case-folding. DOS-mangled-name resolution (the `VOLUME68K` case) is deferred to a `core/fs` NameEngine (`short`/`medium`), not re-implemented in the protocol service. RENAME/DELETE ride the metadata-carrying `FS().Rename`/`Remove`, so SMB never pairs MoveMetadata/DeleteMetadata itself. NT_CREATE_ANDX and the locking/MPX/raw-mode paths answer STATUS_NOT_SUPPORTED in this slice (Win9x/WfW/classic-Mac use OPEN_ANDX, not NT_CREATE_ANDX).

**Where:** `core/service/smb/{resolve,fileio,pathops,trans2}.go`.

## AFP

### Catalog date epoch (Inside Macintosh: Networking, "AFP date and time")

**Spec:** AFP date/time values are signed 32-bit counts of **seconds since 1 January 2000, 00:00 GMT**. This applies uniformly to `ServerTime` (FPGetSrvrParms), the volume create/modify/backup dates (FPOpenVol), and the catalog create/modify/backup dates (FPGetFileDirParms / FPGetFileParms / FPGetDirParms / FPEnumerate).

**Observed / legacy divergence:** The original `service/afp` port (`filedir_pack.go` `toAFPTime`) packed catalog dates as seconds since **1 January 1904, local time** — the classic Mac OS *HFS file system* epoch, which is a different reference point from the AFP *protocol* epoch and also non-UTC. Against a real client that mixes the two (e.g. comparing a volume date from OpenVol with a file date from GetFileDirParms) the two would be ~96 years and one timezone apart.

**What we do:** The refactored `core/service/afp` spine uses the spec epoch consistently — 2000-01-01 UTC — for every AFP timestamp (`handlers.go` `afpEpoch`/`macTime`, used by both `packVolParams` and the catalog packer in `parms.go`). FPGetSrvrParms already emitted the 2000 epoch in the old port, so the new spine is internally consistent where the old one was not.

**Where:** `core/service/afp/handlers.go` — `afpEpoch`, `macTime`; `core/service/afp/parms.go` — `fileDirParams`.

### Desktop database persistence and icon/comment split (Inside Macintosh: Networking, AFP 2.x §C)

**Spec:** The Desktop database is a per-volume store of Finder comments, application icons, and APPL (creator→application) mappings, opened with FPOpenDT and persisted on the volume so the Finder need not rescan.

**What we do (refactored `core/service/afp`):** The spine splits the database to keep the §9 storage seam honest:

- **Comments** (FPGetComment/FPAddComment/FPRemoveComment) ride the fork seam — `v.FS().ReadComment`/`WriteComment` — so a comment lives in the same metadata container (AppleDouble sidecar / NTFS stream / Netatalk EA) as the file it annotates and survives a rename through the FS, exactly like Finder info. RemoveComment writes a zero-length comment. GetComment for a file with no comment returns `kFPItemNotFound` (-5012). Comments are capped at 199 bytes.
- **Icons + APPL mappings** have no per-file home in the seam, so they are held in a **per-volume in-memory** `desktopDB` (built lazily on first FPOpenDT). This mirrors how the `mem` metastore stands in until the sqlite/adapter wiring lands — persistence is an adapter concern, not a spine concern, and the in-memory form keeps core free of database/path knowledge. (The legacy `service/afp` persisted these in `.desktop.db` SQLite; that backend re-homes behind the adapter altitude, not in core.)

**FPAddIcon arrives via ASPUserWrite:** FPAddIcon is command **192**, not a normal ASPCommand — the Mac delivers it over the two-phase ASPWrite path (the icon bitmap is bulk write data). The spine's `writeDataCount`/`appendWriteData` recognise the 20-byte FPAddIcon header (size at bytes 18–19, data at byte 20) alongside the 12-byte FPWrite header, so the same data path serves both.

**Path encoding note:** The catalog commands (FPCreateFile/…/FPAddAPPL/FPRemoveAPPL) resolve their pathname as the rest of the command block (`resolveCatalogPath` → null-separated CNode names). The comment commands carry a trailing field after the path (AddComment's comment), so they read the pathname as a length-prefixed Pascal string (`resolveDTPath` → `pString`) — the form the AFP wire uses for kFPLongName/kFPShortName paths. Both reach the same `Volume.ResolvePath`.

**Where:** `core/service/afp/desktop.go` — `afpOpenDT`/`afpAddComment`/`afpAddIcon`/`afpAddAPPL` et al., `desktopDB`, `dtTable`; `core/service/afp/forkio.go` — `writeDataCount`/`appendWriteData`.

### FPCatSearch over the FileSystem seam (Inside Macintosh: Networking, AFP 2.1 §"FPCatSearch")

**Spec:** FPCatSearch (command **43**) searches a volume's whole catalog for files and directories matching a set of criteria expressed as two parameter blocks — *spec1* (the value / lower bound) and *spec2* (the upper bound of ranged fields, plus the Finder-info mask) — keyed by a `ReqBitMap`. It returns matches a page at a time, the client echoing an opaque 16-byte `CatalogPosition` cursor to resume.

**What we do (refactored `core/service/afp`):** The search *semantics belong to the FileSystem backend*, not to the AFP spine. A plain hierarchical backend walks its tree; a synthetic backend redefines "search" entirely — MacGarden, for instance, turns a CatSearch into an explicit query against its upstream archive and materialises the HTML results as *virtual* folders and files (entries an `Enumerate` of the volume would never surface). So the spine does **not** impose a tree-walk. It:

1. decodes the AFP wire criteria (spec1/spec2 keyed by `ReqBitMap`) into the backend-neutral `fs.CatSearchCriteria` — name (partial/full, decoded store-native through the share codec), parent dir id (resolved to a store path via the CNID store), and a free-text `Query` for synthetic backends;
2. delegates to the bound `fs.ForkFS` through the **optional `fs.CatSearcher` capability**, gated on `Capabilities().CatSearch`;
3. packs whatever store paths the backend returns with the same `fileDirParams` packer the catalog-read commands use.

A volume whose backend does **not** advertise `Capabilities().CatSearch` (or does not implement `fs.CatSearcher`) answers **`kFPCallNotSupported`** — the AFP-correct result for a backend that declines the search — rather than a half-emulated walk. This is the design point the field forced: CatSearch is the filesystem implementor's to define, including the option to not support it.

**Default predicate walk:** `fs.WalkCatSearch` is a *shared default* a plain hierarchical backend (`local_fs`, `memfs`) opts into in one line — it walks depth-first through the backend's own `ReadDir`, honouring the name (case-insensitive substring/exact) and parent predicates, and ignores criteria it does not model (date/length ranges, Finder-info mask) rather than failing the search (lenient, never false-negatives the dominant name match). It lives in `core/fs`, not the AFP spine, so it is reusable and the spine stays storage-agnostic.

**Cursor / paging:** The opaque 16-byte `CatalogPosition` carries the *backend-defined* `fs.CatSearchCursor` (byte 0 = continuation flag, byte 1 = length, bytes 2.. = the cursor, ≤14 bytes); the AFP spine round-trips it verbatim and never interprets it, so any backend pagination scheme survives (MacGarden could carry an upstream page token; `WalkCatSearch` carries a 4-byte flat visit index). A page returns up to `ReqMatches` records capped at ~4 KB (one ASP quantum); more results → `NoErr` + the backend cursor, last page → `kFPEOFErr` + zero cursor (the AFP/Netatalk convention).

**Divergence from the legacy port:** The old `service/afp/catsearch.go` also delegated to a backend `FileSystem.CatSearch`, but flattened the criteria to a single printable-substring `query` string and packed only directories. The refactored seam passes structured criteria (`fs.CatSearchCriteria`) plus the free-text `Query`, returns both files and directories, and round-trips a backend-opaque cursor — so a synthetic backend gets enough to run a real query while a predicate backend gets the structured fields.

**Where:** `core/fs/catsearch.go` — `CatSearcher`, `CatSearchCriteria`/`CatSearchResult`/`CatSearchCursor`, `WalkCatSearch`, `ErrCatSearchUnsupported`; `core/service/afp/catsearch.go` — `afpCatSearch`, `decodeCatSearchCriteria`, `Volume.catSearcher`/`packCatSearchRecord`.
