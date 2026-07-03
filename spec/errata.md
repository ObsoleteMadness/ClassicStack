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

### SMB hashed-credential accept-as-guest (M8a auth)

**Spec:** [MS-CIFS] SESSION_SETUP_ANDX carries a CaseInsensitivePassword (LM) and CaseSensitivePassword (NTLM) the server validates against the account's stored hash.

**Observed / design:** ClassicStack stores passwords as salted PBKDF2-SHA256 (modern at rest, see the charter "compatibility over correctness" stance) — there is no LM/NTLM hash to compare a wire response against, and an LM/NTLM response cannot be reversed to the cleartext we *can* validate. A legacy client that sends a hashed response (CaseSensitivePasswordLength > 0, or a 24-byte case-insensitive response) therefore cannot be authenticated as a named user.

**What we do:** with a user store wired, SESSION_SETUP validates only a **cleartext** case-insensitive password (the form Win9x/WfW send when the negotiated security mode does not demand a challenge response) against the store; a hashed response is accepted **as guest** (UID granted, Action=guest) rather than refused, so the client still connects and sees guest-open shares. A wrong cleartext password for a named account is refused with STATUS_LOGON_FAILURE. With no store wired, every session is guest (the historical world-readable default). The gate is at login (legacy clients log in once and bind shares under one identity); a per-share allow-list then filters which shares the resulting identity may enumerate (NetShareEnum/NetServerEnum2) and bind (TREE_CONNECT).

**Where:** `core/service/smb/{negotiate.go,lanman.go}`; the store + PBKDF2 in `core/auth` + `adapter/auth/local`.

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

### FPLogin credential validation and per-volume access gating (M8a auth)

**Spec:** AFP authentication is a UAM handshake; "Cleartxt Passwrd" carries the user name (pstring) and an 8-byte password field. "No User Authent" is the guest UAM.

**Observed / design:** ClassicStack is a compatibility server keeping modern primitives at rest (salted PBKDF2-SHA256). The single-step UAMs it accepts (no DHX/2-way-randnum challenge) are the intentional concession that lets vintage clients connect — the weakness is on the *wire*, not at rest.

**What we do:** "No User Authent" is always a guest login. "Cleartxt Passwrd" is a guest login when no user store is wired (the historical world-readable default); with a store wired, a non-empty user name is validated against it (wrong password → `kFPUserNotAuth`), and an empty name is admitted as guest. The resolved identity is recorded on the session and gates which volumes it may **enumerate** (FPGetSrvrParms omits volumes the identity may not access) and **open** (FPOpenVol returns `kFPObjectNotFound` for a restricted volume, not leaking its existence). The gate is at login because a client logs in once and opens volumes under one identity; the allow-list is share-level (`share.Permissions.AllowedUsers`), not file-level ACLs, and `ReadOnly` stays share-wide.

**Where:** `core/service/afp/{handlers.go,afp.go}` (`afpLogin`, `SetAuthenticator`, `afpGetSrvrParms`, `afpOpenVol`); the store + PBKDF2 in `core/auth` + `adapter/auth/local`; the allow-list in `core/share/permissions.go`.

## Config codecs (§4)

### UCI empty-quoted-value tokenizer (M8)

**Spec:** OpenWRT UCI renders a string option as `option <key> '<value>'`; an unset string is `option <key> ''` — an empty single-quoted value is a valid, present value (the empty string), distinct from an absent option.

**Observed:** the `adapter/config/uci` tokenizer dropped any token whose accumulated text was empty, so `''` produced zero tokens. An `option key ''` line then had only two tokens, tripped the `len(tokens) < 3` arity check, and failed the **whole** `Unmarshal`. Because a default `config.Model` has empty well-known string fields (e.g. `Logging.Level == ""`), a freshly-marshalled default model could not be reloaded through UCI — only models that happened to set every string field round-tripped.

**What we do:** the tokenizer now tracks whether the current token was opened with a quote (`quoted`) and emits an empty token at the next separator/EOL when it was, so `''` yields one empty-string token. An unquoted run of whitespace still collapses to nothing. TOML was never affected (go-toml represents empty strings natively).

**Where:** `adapter/config/uci/uci.go` (`tokenize`); regression in `adapter/config/uci/auth_roundtrip_test.go` (`TestEmptyStringOptionRoundTrips`).

## IPX

### IPX Diagnostic Responder (socket 0x0456) — observation-based

**No spec:** ClassicStack ships no formal document for Novell's IPX/SPX Diagnostic protocol (the wire behind the `IPXPING` reachability tool). The layout is from observation of NetWare diagnostic traffic and Novell's published Diagnostic Responder description; `core/protocol/macipx` already noted socket 0x0456 as "the NetWare diagnostic responder" in its opcode-0x10 listen registration.

**Observed:** a Diagnostic *request* on socket 0x0456 carries a 1-byte exclusion-address count followed by that many 6-byte node IDs — nodes that should stay silent (the sender's own node and any already-known responders), so a broadcast diagnostic does not re-collect hosts. A directed reachability ping sends an empty list (count 0). A *response* carries a 1-byte component count followed by per-component records (a type byte plus a type-specific, length-free body); the reachability tool treats any well-formed response as "host alive". Real NetWare responders enumerate IPX/SPX/SAP/NetBIOS components; component bodies are type-implied, not length-prefixed.

**What we do:** `core/service/ipxdiag` registers a `SocketHandler` on socket 0x0456 of the IPX mini-router that answers any request not naming our own node with the minimal response — a single IPX-component record (`diag.SimpleResponse`). `cmd/csipxping` is the client: it sends a directed (or broadcast) request and reports the round-trip time of each reply. The decoder treats component bodies as opaque (the last component absorbs the remainder), enough for reachability without modelling every NetWare component layout.

**Where:** `core/protocol/ipx/diag/diag.go` (codec); `core/service/ipxdiag/ipxdiag.go` (responder); `cmd/csipxping/main.go` (client).

### NCP keyed (encrypted) login accept-as-guest — observation-based

**No spec:** ClassicStack ships no formal document for Novell NCP/bindery; the NetWare 3.x login is implemented from the openly documented protocol and the mars_nwe / ncpfs references (attributed in [17-ncp.md](17-ncp.md)).

**Observed:** NetWare clients (NETx/VLM) prefer the keyed (encrypted) login (function 0x17 subfunction 0x18): they fetch an 8-byte challenge (0x17/0x17), then send a hash shuffled from the challenge and the user's NetWare-hashed password. The server cannot reverse that shuffle to a cleartext password, and ClassicStack stores credentials as PBKDF2-SHA256 (core/auth), not as the NetWare hash, so it cannot recompute the expected response.

**What we do:** consistent with the compatibility-server posture (modern at rest, faithful to the weak client dialect on the wire) and mirroring the SMB "hashed-credential accept-as-guest" entry, a keyed login is accepted as a guest-equivalent login bound to the supplied object name (no credential check). A cleartext login (0x17/0x14) IS validated against the wired user store. A future slice that stores the NetWare-hashed credential alongside the PBKDF2 hash can validate the keyed shuffle exactly.

**Where:** `core/service/ncp/handlers.go` (`loginEncrypted`, `getLoginKey`, `grantLogin`); the login posture in [17-ncp.md](17-ncp.md).

## EtherDFS

### AL_SETATTR / FAT attributes on a non-FAT backend — observation-based

**No spec:** ClassicStack ships no formal document for EtherDFS; the protocol is implemented from the EtherDFS protocol description (`spec/etherdfs.txt`) and the reference servers (attributed in [18-etherdfs.md](18-etherdfs.md)).

**Observed:** the reference EtherDFS server warns that it is "HIGHLY recommended to run ethersrv over a FAT filesystem. Other file systems might work, too, but FAT attributes will be unavailable." The DOS redirector sets/reads the FAT attribute byte (`1=RO 2=HID 4=SYS 32=ARCH`), but a POSIX/host backend behind ClassicStack's shared `core/fs` seam does not model HID/SYS/ARCH, and the seam exposes no attribute-mutation call.

**What we do:** `AL_GETATTR` synthesises the attribute byte from FileInfo (`DIR` for directories, `ARCH` for files, `RO` from the drive's read-only flag or the host file's write permission). `AL_SETATTR` is accepted as a no-op when the target exists (and rejected file-not-found when it does not), rather than failing — matching the reference server's best-effort behaviour on non-FAT hosts. A future slice could persist FAT attributes in the share metastore.

**Where:** `core/service/etherdfs/dispatch.go` (`handleGetAttr`, `handleSetAttr`, `fatAttr`); the posture in [18-etherdfs.md](18-etherdfs.md).

### No authentication (accept-any-client) — by design

**Observed:** EtherDFS has no login, session, or credential exchange of any kind — a client is identified only by its source MAC, and the original ethersrv serves any client on the segment.

**What we do:** consistent with the compatibility-server posture, EtherDFS serves any client that can reach the server's MAC; access is gated only by a drive's `read_only` flag and `allowed_users` allow-list (which, with no user store wired, means world-accessible). This is the intentional weakness that lets vintage DOS clients connect, mirroring the SMB guest-session and NCP keyed-login entries above.

**Where:** `core/service/etherdfs/etherdfs.go` (package doc, security posture); `core/service/etherdfs/dispatch.go`.

## Storage seam — DOS attributes & name casing

### DOS attribute storage = Samba XATTR_DOSINFO — observation-based interop

**No spec:** there is no published wire spec for how a non-DOS host stores the FAT attribute bits (RO/HID/SYS/ARCH) and DOS create-time that a POSIX/NTFS-non-system-drive filesystem cannot natively represent. The format here is taken from Samba's open source (the canonical reference, attributed in [16-storage-seam.md](16-storage-seam.md)).

**Observed:** Samba stores DOS attributes in the `user.DOSATTRIB` extended attribute as a versioned record (`librpc/idl/xattr.idl` `xattr_DOSAttrib`; `source3/lib/xattr_tdb.c` for the tdb fallback). The version-3 "info_compat" arm carries `valid_flags`, `attrib`, `ext_attrib`, and `create_time` (NTTIME). On a filesystem without user xattrs Samba falls back to a tdb database keyed by path.

**What we do:** ClassicStack persists DOS attributes through a per-share `DOSAttrStore` whose value is byte-compatible with Samba's version-3 `XATTR_DOSINFO` record (`core/metastore.EncodeDOSInfo`/`DecodeDOSInfo`), so a share over a directory Samba also serves reads/writes the same `user.DOSATTRIB` xattr. Where xattrs are unavailable the per-share metastore (sqlite/mem) is our tdb equivalent, and a `.dosattr/<name>` sidecar carrying the identical blob is the all-filesystems fallback; on Windows the bits map straight to the host file attributes. The reader accepts version 1–4 and ignores fields it does not model; a corrupt blob falls back to host-derived attributes rather than mis-decoding.

**Where:** `core/metastore/dosinfo.go` (codec); `core/metastore/dosattr.go`, `core/fs/dosattr.go` (backends + selection); `core/fs/dosattr_xattr.go` (`user.DOSATTRIB`), `core/fs/dosattr_native_windows.go` (Windows passthrough).

### Filename casing — case-insensitive lookup, preserved store

**Observed:** DOS/Windows clients (SMB, EtherDFS) and the NetWare/AFP redirectors treat filenames case-insensitively, but Windows preserves the stored case of a long name. A POSIX host is case-sensitive; macOS is typically case-insensitive-preserving; Windows non-system drives often have the OS 8.3-name service disabled, so the host cannot be relied on to generate or reverse short names.

**What we do:** the `short`/`medium` name engines fold case for both the forward and reverse metastore keys (so `Report.txt` and `REPORT.TXT` share one binding, and two genuinely distinct names that fold equal collide and get a `~N`/`-N` suffix) while storing the original-case long name as the value (so medium names round-trip in their stored case). One generator runs identically on Windows, macOS, and Linux — the engine never consults the host's case rules — so a volume served from any host presents the same names.

**Where:** `core/fs/name.go` (`fwdKey`/`revKey` case-folding, `deriveMedium`/`derive83`).

## Config model — section ownership

### NBT (:139) listen address lives on [NetBIOS], not [SMB]

**No spec:** this is a ClassicStack config-model decision, not a wire deviation.

**Observed:** NBT is NetBIOS-over-TCP (a NetBIOS transport), but ClassicStack physically shares the `:139` session listener between NBT and SMB's direct-TCP transport because they share framing. The NBT listen address (`nbt_addr`) was originally carried on the `[SMB]` server section next to `tcp_addr`, which put a NetBIOS-owned setting under SMB (the web UI showed "NBT listen address" on the SMB panel — a model/UI mismatch).

**What we do:** `nbt_addr` lives on the `[NetBIOS]` section (`netbios.Section.NBTAddr`), alongside the `nbt` transport binding. The compose cross-wire (`wireSMBTCP`) reads the address from the NetBIOS service (§B) when the nbt binding is on; SMB owns only the direct-TCP (`:445`) address. Neither auto-defaults to its conventional port (Windows owns `:139`/`:445`, Unix guards them as privileged) — an empty address leaves that listener inert.

**Where:** `core/service/netbios/section.go` (`NBTAddr`), `core/service/netbios/netbios.go` (`SetNBTListenAddr`/`NBTListenAddr`), `compose/registry/reg_netbios.go` (wire from section), `compose/runtime/transports.go` (`wireSMBTCP`); `core/service/smb/serversection.go`+`smb.go` (SMB now carries only the direct-TCP address).

### The only interface is the uplink bridge; a port owns its own binding

**No spec:** this is a ClassicStack config-model decision, not a wire deviation.

**Observed:** the interface namespace originally modelled three interface KINDS — bridge (pcap/tap/raw), serial (TashTalk), and multicast (LToUDP) — and a TashTalk port resolved its serial device/baud from a named `kind=serial` interface (the earlier §3b/D7 move: "the interface owns the device parameters"). Operationally this confused the layering: an operator thinks of the bridge as the only "interface" (the uplink), while a TashTalk is a port that owns its own tty and an LToUDP is a host-wide multicast port.

**What we do:** an INTERFACE is now only the uplink bridge (pcap/tap/raw over a host NIC). Serial and multicast are no longer interface objects. A TashTalk port carries its serial `Device`/`Baud` on the PORT section (`port.Section.Device`/`Baud`); the TashTalk factory reads them directly (falling back to `Iface` as the device path for an older section). An LToUDP port rides host-wide multicast, with `Iface` an optional bind address. This REVERSES the §3b/D7 serial-as-interface move. The web UI reflects it: the Interfaces tab manages only bridge/uplink entries; the TashTalk port editor shows a serial-port dropdown + baud; EtherTalk/IPX/NetBEUI show a bridge dropdown; LToUDP shows neither.

**Where:** `core/port/section.go` (`Device`/`Baud`), `compose/registry/reg_localtalk.go` (`tashtalkLinkOpener` reads the port section; `effectiveSerialInterface` removed from `compose/registry/dispatch.go`), `adapter/control/http/spa/app.js` (`instanceForm`/`openInstanceModal` model-aware port widgets; `openInterfaceModal` bridge-only).
