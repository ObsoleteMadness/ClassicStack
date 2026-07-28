# Spec Errata

This document records places where ClassicStack's wire behavior intentionally differs from the published spec, because the spec contradicts what real clients actually require. Each entry cites the spec section we deviate from, the client behavior that drove the change, and the file/function where the deviation lives.

## ATP (client)

### ATP response UserData is authoritative only in the seq-0 packet — observed

**Spec (Inside AppleTalk, ATP):** every ATP response packet carries the 4-byte UserData; the reassembled transaction's UserData (which ASP maps to the command result / AFP result code) is a single value for the transaction.

**Observed (live LToUDP capture of an FPRead reply from System 7.5.3 Personal File Sharing):** a real System 7.x ASP responder fills UserData correctly **only in the first response packet (seq 0)**; seq 1..N carry **stale bytes left over from a prior transaction**. In the captured multi-packet FPRead reply (8 ATP response packets, seq 0-7), seq 0 UserData = `0x00000000` (success) but seq 1-7 = `0x07270011` — the leftover ASPWriteContinue user bytes (function 0x07, session 0x27, a fragment index) of an earlier write transaction on the same node. Our requester overwrote `respUserData` on every packet, so the reassembled UserData became the **last** packet's garbage. This surfaced as bogus AFP result codes (`kFP#0x0727xxxx`) and truncation on **any** read larger than one ATP-response quantum (~4 KB) — data forks and resource forks alike — including through the `csmount` WinFsp mount.

**What we do:** capture `respUserData` from the **seq-0** response packet only. **Where:** `client/atalk/atp.go` (`(*ATP).Request`). Test: `TestRequestUserDataFromFirstPacket`.

## Metadata mapping (client)

### Remote DOS attributes/dates reach a DOS/Windows view through an fs-native MetaEngine — design

A remote file client (SMB/AFP) carries the file's DOS-equivalent attributes and dates in
its own `Stat`/`ReadDir` result (off the wire), not in a local metastore. So a share whose
base FileSystem advertises `Capabilities().DirAttributes` gets the `fsNativeDOSAttrStore`
(`core/fs/dosattr.go`): `Meta().Attrs()` reads attributes/create-time straight from
`base.Stat(path).Sys()`, which implements `fs.DOSAttrInfo` (attribute bits) and optionally
`fs.DOSCreateTimeInfo` (creation date). The WinFsp adapter is protocol-agnostic — it reads
`Meta().Attrs()` and `FileInfo.ModTime()` only.

- **SMB**: `FileAttributes` are already DOS/FILE_ATTRIBUTE_* bits (no translation).
  QUERY_INFORMATION carries a UTIME LastWriteTime; FIND records carry full FILETIMEs
  (creation + write). `Stat` also issues a TRANS2 QUERY_PATH_INFORMATION
  (SMB_QUERY_FILE_BASIC_INFO, level 0x0101) to enrich the timestamps with reliable
  FILETIMEs + a creation date. A server that does not implement it (observed: Win98
  answers "invalid function") is remembered per session (`Session.MarkPathInfoUnsupported`)
  so the client stops issuing it after one probe and falls back to QUERY_INFORMATION.
  Where: `core/protocol/smb/clientfileops.go` (`BuildQueryPathInfo`/`ParseQueryPathInfo`),
  `client/smb/{filesystem.go,session.go}`. Test: `TestParseQueryPathInfoBasicInfo`.
- **AFP**: the `FPGetFileDirParms`/`FPEnumerate` Attributes word maps Invisible→Hidden,
  System→System, WriteInhibit→ReadOnly (`client/afp/afp.go afpAttrsToDOS`); ModDate/
  CreateDate are surfaced directly. The client now requests `FDBitmapAttributes` +
  `FDBitmapCreateDate` in its stat/enumerate bitmap.

### WinFsp: never feed a zero time.Time to filetime.Timestamp — observed

A zero `time.Time` through go-winfsp's `filetime.Timestamp` maps to ~year 1754, which
Explorer displays as a garbage date. The adapter falls back to the FAT epoch (1980-01-01)
for any timestamp a backend does not surface. **Where:** `client/winfsp/fileinfo_windows.go`
(`filetimeOr`/`fatEpochFiletime`).

## AFP (client)

Both entries below were found live driving the AFP **client** against **System 7.5.3 Personal File Sharing** running in Mini vMac (server offers AFPVersion 1.1/2.0/2.1, UAMs Cleartxt/Randnum/2-Way-Randnum), reached over DDP/LToUDP and mounted with `csmount` (WinFsp).

### FPOpenFork bitmap must request only the opened fork's length bit — observed

**Spec:** `FPOpenFork`'s Bitmap requests the file parameters to return for the fork being opened; `FileBitmapDataForkLen`/`FileBitmapRsrcForkLen` are independent bits.

**Observed (live):** our client sent `Bitmap = DataForkLen | RsrcForkLen` for *every* open. System 7.5 Personal File Sharing returned **kFPBitmapErr (-5004)** — it rejects a data-fork open that also asks for the resource-fork length (and vice-versa).

**What we do:** request `FileBitmapDataForkLen` when opening the data fork and `FileBitmapRsrcForkLen` when opening the resource fork — never both. **Where:** `client/afp/fork.go` (`(*FS).OpenFork`).

### FPRead command block must be the full fixed 14 bytes (newLineMask/Char emitted) — observed

**Spec (`spec/*` FPRead, cmd 27):** request is `cmd(1) pad(1) forkRefNum(2) offset(4) reqCount(4) [newLineMask(1) newLineChar(1)]`; 0/0 disables newline substitution, and the trailing two bytes read as "optional."

**Observed (live):** omitting the two trailing bytes (a 12-byte block) drew **kFPParamErr (-5019)** from System 7.5 Personal File Sharing, which expects the full fixed 14-byte block. (Our own server accepts either — it only checks `len >= 12`.)

**What we do:** always emit `newLineMask=0, newLineChar=0`, so the FPRead block is 14 bytes. Substitution stays disabled. **Where:** `core/protocol/afp/fork.go` (`ReadRequest.Marshal`).

## SMB (client)

The three entries below were found live driving the SMB **client** and the `csmount` WinFsp mount against **real Windows 98 SE** file sharing (server `WIN98-NBF`, anonymous GUEST, over NetBEUI/NBF), which negotiates NT LM 0.12 **without CAP_STATUS32** — i.e. it is a DOS-error server with a small buffer.

### READ_ANDX/WRITE_ANDX must be clamped to the server's negotiated MaxBufferSize — observed

**Spec ([MS-CIFS]):** MaxBufferSize in the NEGOTIATE response is the largest SMB message the server can receive/send; a READ_ANDX MaxCount must not exceed it.

**Observed (live):** the client capped reads only by the transport budget (a reassembling NBF/NBT/TCP carrier reports a huge value), so `maxIO` stayed at the 12 KiB default. Win98 advertised **MaxBufferSize = 2920** and rejected a `READ_ANDX, 12288 bytes` with **ERRDOS/87 "invalid parameter"** (status `0x00570001`) — every read over ~2.8 KB failed.

**What we do:** after NEGOTIATE, clamp `maxIO` to `MaxBufferSize - smbReplyOverhead`. **Where:** `client/smb/session.go` (`establishSession`).

### DOS-error servers return ErrorClass/ErrorCode, not NTSTATUS — observed

**Spec ([MS-CIFS] 2.2.1.5):** when SMB_FLAGS2_NT_STATUS is not negotiated, the header's 4-byte status field is `ErrorClass(1) Reserved(1) ErrorCode(2)`, not a 32-bit NTSTATUS.

**Observed (live):** `translateErr` mapped only NTSTATUS values (`0xC00000xx`), so a Win98 DOS status like `0x00020001` (ERRDOS class 1, ERRbadfile code 2 = not found) fell through as a raw error that did **not** satisfy `errors.Is(err, os.ErrNotExist)`. Through the mount this broke **create**: WinFsp's `GetSecurityByName` on a new name got a non-not-found error, so WinFsp never issued the create (Explorer/`copy` reported "an internal error occurred"; no OPEN_ANDX ever reached the wire).

**What we do:** `translateErr` decodes the DOS class/code form (ERRDOS/ERRSRV class in the low byte, code in the high 16 bits) to the fs sentinels before the NTSTATUS switch. Such values never collide with a real NTSTATUS (whose top severity bits are always set). **Where:** `client/smb/filesystem.go` (`translateErr`, `dosErr*` consts). Test: `TestTranslateErrDOSAndNTStatus`.

### OPEN_ANDX SearchAttributes must include hidden/system to open DOS system files — observed

**Spec ([MS-CIFS] §2.2.4.41.1):** OPEN_ANDX SearchAttributes is the set of attributes a file may carry and still be opened; 0 means "normal files only."

**Observed (live):** the client sent SearchAttributes = 0, so Win98 refused to open a hidden+system file (MSDOS.SYS) with "file not found" (the OPEN_ANDX failed even though QUERY_INFORMATION found it). **What we do:** set SearchAttributes = ReadOnly|Hidden|System|Archive on OPEN_ANDX, matching the FIND/QUERY builders. **Where:** `core/protocol/smb/clientfileops.go` (`BuildOpenAndX`).

### The mount must close the data handle before a classic SMB rename — observed

**Spec:** `SMB_COM_RENAME` renames by path; the file must not have an open handle on a classic (share-mode) server.

**Observed (live):** WinFsp holds the source file open across a rename and calls the `Rename` delegate with that handle; Win98 then rejected `SMB_COM_RENAME` with "access denied" (a direct `csfs mv`, which holds no handle, succeeded). **What we do:** the `client/winfsp` `Rename` delegate closes the open data handle (nils `openFile.f`) before calling `FileSystem.Rename`. **Where:** `client/winfsp/adapter_windows.go` (`(*Adapter).Rename`).

## CIFS / SMB1

### SMB_COM_NEGOTIATE response WordCount MUST match the selected dialect family ([MS-CIFS] 2.2.4.52.2) — spec-based

**Spec ([MS-CIFS] 2.2.4.52.2; [smb6.0] §NEGOTIATE; `spec/COREP.TXT`):** the NEGOTIATE response format is keyed by the selected dialect, and *"the value of WordCount MUST be considered variable until the dialect has been determined. All dialects MUST return the DialectIndex as the first entry."*
- **Core** ("PC NETWORK PROGRAM 1.0") or no dialect supported → **WCT=1**: DialectIndex only, ByteCount=0 (DialectIndex 0xFFFF when nothing matched).
- **LAN Manager 1.0 … 2.1** (incl. `DOS LANMAN2.1`, `Windows for Workgroups 3.1a`) → **WCT=13**: DialectIndex, **SecurityMode(2)**, **MaxBufferSize(2)**, MaxMpxCount, MaxNumberVcs, RawMode, SessionKey(4), **SMB_TIME ServerTime(2)** + **SMB_DATE ServerDate(2)**, ServerTimeZone, EncryptionKeyLength, Reserved; ByteArea = EncryptionKey(none) + NUL-terminated PrimaryDomain. **No Capabilities field.**
- **NT LM 0.12** → **WCT=17**: DialectIndex, **SecurityMode(1)**, MaxMpxCount, MaxNumberVcs, **MaxBufferSize(4)**, MaxRawSize(4), SessionKey(4), **Capabilities(4)**, **FILETIME SystemTime(8)**, ServerTimeZone, ChallengeLength; ByteArea = Challenge(none) + DomainName.

Note the **field widths differ** between the LANMAN and NT forms (SecurityMode 2 vs 1 byte; MaxBufferSize 2 vs 4 bytes) and the timestamp differs (DOS SMB_TIME/SMB_DATE vs 64-bit FILETIME). Emitting the NT WCT=17 block for a selected LANMAN dialect (or vice-versa) is a malformed response a client may reject.

**Dialect selection ([smb6.0]):** the server selects the **most recent** dialect known to both client and server. `DialectIndex` is the 0-based index into the client's offered list.

**PrimaryDomain in the WCT=13 (LANMAN) response — only for LANMAN2.1 ([smb6.0] 1127):** the LANMAN-family response byte area includes the NUL-terminated `PrimaryDomain` string **only** when the negotiated dialect is `DOS LANMAN2.1` or `LANMAN2.1`. For every earlier LANMAN-family dialect (`MICROSOFT NETWORKS 3.0`, `LANMAN1.0`, `LM1.2X002`, `DOS LM1.2X002`, `Windows for Workgroups 3.1a`) the byte area is EMPTY (ByteCount=0). **Observed (`captures/ipx.pcap` frames 336-337):** Win3.11 offered up to WfW 3.1a; our response selected WfW 3.1a (index 4, WCT=13) but appended `WORKGROUP\0`, which Wireshark flags as trailing "Unknown Data" — a WfW 3.1a client does not expect a PrimaryDomain there. Fixed: `buildNegotiateLanMan` takes the selected dialect name and appends PrimaryDomain only for the two LANMAN2.1 dialects (`protocol.DialectDOSLANMAN2` / `DialectLANMAN21`). Test: `TestNegotiate_LanManPrimaryDomainOnlyForLanMan21`.

**SMB header on the response:** copy the request header, set the reply flag + SUCCESS status, and preserve the request's Flags2 (same Mid/Pid). NEGOTIATE does NOT stamp SMB_FLAGS2_KNOWS_LONG_NAMES the way the generic response-header helper does; the legacy server copies the request header verbatim.

**What we do:** `handleNegotiate` parses the offered dialects, calls `protocol.SelectDialect` (most-recent by rank → index + family), and dispatches to `buildNegotiateCore` (WCT=1) / `buildNegotiateLanMan` (WCT=13, via `smbServerTimeDate` for SMB_TIME/SMB_DATE) / `buildNegotiateNT` (WCT=17). SecurityMode is user-level plaintext, no challenge. Authentication/SESSION_SETUP behaviour is unchanged (out of scope).

**Where:** `core/service/smb/negotiate.go` (`handleNegotiate`, `buildNegotiate{Core,LanMan,NT}`, `negotiateResponseHeader`, `smbServerTimeDate`, `parseNegotiateDialects`); dialect strings + `SelectDialect`/`DialectFamily` in `core/protocol/smb/smb.go`. Tests: `TestNegotiate_WordCountMatchesDialectFamily`, `TestNegotiate_NoSupportedDialect`, `TestNegotiate_PreservesRequestFlags2`, `TestNegotiate_LanManFieldWidths`, `TestNegotiate_NTFieldWidths`, `TestSMBServerTimeDate`, `TestDispatch_Negotiate`.

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

### SMB_COM_SEARCH 8.3 matching ([MS-CIFS] 2.2.4.58.1)

**Spec:** The FileName in a Search request is an "OEM_STRING that MAY contain wildcards", where `?` matches exactly one character. A literal left-to-right glob reading of `?` (consume one char, no more, no less) makes `????????.???` require an 8-char base, a dot, and a 3-char extension.

**Observed:** Windows for Workgroups 3.11 browses a folder by sending FileName `\????????.???` (see `netbeui.pcap` frame 58: SearchAttributes 0x0031 = ReadOnly|Directory|Archive). A dotless directory such as `SUBA` — and any name shorter than 8.3 — must match this pattern, or the folder shows its files but *no subdirectories*. A strict glob drops every extensionless directory, which is the "6 files and no directories" browse failure.

**What we do:** Match 8.3-segmented — split both the name and the pattern on their first `.` into base and extension, and match each segment with DOS semantics where `?` matches one character **or nothing** once the name's segment has run short (so `????????.???` matches `SUBA`, `README`, `A.B`, and `FILE1.TXT` alike). A name with no extension still matches a pattern whose extension segment is all wildcards.

**Where:** `core/service/smb/match.go` — `wildcardMatch`, `matchDOSSegment` (ported from the legacy `service/smb/command_fs_search.go` `matchesPattern`, which had this logic but no regression test; the refactor's first rewrite replaced it with a generic glob and lost the DOS quirk).

### FS command engine path resolution over the share seam (M7)

**Spec:** [MS-CIFS] file/path commands carry a server-side path that the server resolves against the share root.

**Observed / design:** the legacy `service/smb` resolved wire paths with a DOS-name-mangling fuzzy matcher (`findDOSLikeComponentMatch`: uppercased, punctuation-stripped, prefix-or-truncation matching, e.g. `VOLUME68K` → `Volume 68k`). That conflated two concerns — wire→store charset transcoding and 8.3 short-name resolution — inside the SMB service, which the §9 refactor inverts.

**What we do:** the new `core/service/smb` FS command engine resolves a wire path through the share's `FilenameCodec` only (`Share.ResolvePath`, threading the per-request UTF-16/ANSI charset off the FLAGS2 Unicode bit), then reaches the backend via `sh.FS()`. Exact (codec-decoded) names resolve directly; the FileSystem backend owns case-folding. DOS-mangled-name resolution (the `VOLUME68K` case) is deferred to a `core/fs` NameEngine (`short`/`medium`), not re-implemented in the protocol service. RENAME/DELETE ride the metadata-carrying `FS().Rename`/`Remove`, so SMB never pairs MoveMetadata/DeleteMetadata itself. NT_CREATE_ANDX and the locking/MPX/raw-mode paths answer STATUS_NOT_SUPPORTED in this slice (Win9x/WfW/classic-Mac use OPEN_ANDX, not NT_CREATE_ANDX).

**Where:** `core/service/smb/{resolve,fileio,pathops,trans2}.go`.

### SMB hashed-credential accept-as-guest (M8a auth)

**Spec:** [MS-CIFS] SESSION_SETUP_ANDX carries a CaseInsensitivePassword (LM) and CaseSensitivePassword (NTLM) the server validates against the account's stored hash.

**Observed / design:** ClassicStack stores passwords as salted PBKDF2-SHA256 (modern at rest, see the charter "compatibility over correctness" stance) — there is no LM/NTLM hash to compare a wire response against, and an LM/NTLM response cannot be reversed to the cleartext we *can* validate. A legacy client that sends a hashed response (CaseSensitivePasswordLength > 0, or a 24-byte case-insensitive response) therefore cannot be authenticated as a named user.

**What we do:** with a user store wired, SESSION_SETUP validates only a **cleartext** case-insensitive password (the form Win9x/WfW send when the negotiated security mode does not demand a challenge response) against the store; a hashed response is accepted **as guest** (UID granted, Action=guest) rather than refused, so the client still connects and sees guest-open shares. A named account that presents **no password at all** (empty CaseInsensitive/CaseSensitive length — e.g. `captures/ipx.pcap`'s `WIN98USER` naming itself with no credential to a guest-open server) is likewise granted a **guest** session, NOT authenticated-and-failed: the client offered no credential to validate. Only a named account that actually presents a non-empty cleartext password is authenticated; a wrong password for such an account is refused with STATUS_LOGON_FAILURE. With no store wired, every session is guest (the historical world-readable default). The gate is at login (legacy clients log in once and bind shares under one identity); a per-share allow-list then filters which shares the resulting identity may enumerate (NetShareEnum/NetServerEnum2) and bind (TREE_CONNECT).

> **Refactor regression (fixed):** the M7/M8a spine authenticated *any* named setup with a non-empty account, so `WIN98USER` with an empty password was treated as a failed login and refused — the client never established a session. Restored to the legacy `buildSessionSetupResponse` behaviour (always grant guest unless a real password is presented and validated). The legacy `service/smb` is the known-good reference.

**Where:** `core/service/smb/{negotiate.go,lanman.go}`; the store + PBKDF2 in `core/auth` + `adapter/auth/local`.

> **Re-regression note (`captures/ipx.pcap` frames 110-111, re-fixed):** the empty-password-is-guest and DOS-wire-status fixes below were both re-lost at one point (an errant `git checkout` reverted `negotiate.go`), so `WIN98USER`'s credential-less SESSION_SETUP_ANDX to `\\CLASSICSTACK\IPC$` was again authenticated-and-failed and the failure encoded as raw `0xC000006D` (Wireshark: "Unknown error class 0x6d"). Re-applied: `handleSessionSetup` authenticates only a named account WITH a non-empty cleartext password (`user != "" && pass != "" && !hashed`), and `toWireStatus` maps STATUS_LOGON_FAILURE → ERRSRV/ERRbadpw for DOS-codes clients. The SESSION_SETUP_ANDX response byte area also now carries NativeOS/NativeLanMan (= server name) + PrimaryDomain (= workgroup), OEM or UTF-16LE (with a leading pad byte) per the request's Unicode flag, rather than two bare NULs.

### DOS-error wire form for CORE-dialect clients ([MS-CIFS] 2.2.3.1, SMB_FLAGS2_NT_STATUS)

**Spec:** the 4-byte SMB header Status field is a 32-bit NTSTATUS when the request set `SMB_FLAGS2_NT_STATUS`; when clear (Win9x/WfW/DOS clients), it is a DOS `{ErrorClass(1 byte), reserved(1 byte), ErrorCode(2 bytes LE)}` triple. A server MUST match the client's chosen form or the client mis-reads the status.

**Observed (`captures/ipx.pcap`):** our SESSION_SETUP_ANDX failure to `WIN98USER` (a client that negotiated DOS error codes, Flags2=0x0000) put the raw NTSTATUS `0xC000006D` (STATUS_LOGON_FAILURE) into the Status field. On the wire (LE) that is `6D 00 00 C0`, which the client parses as **ErrorClass 0x6d** — an undefined class Wireshark shows as "Unknown error class!". The session dies with an unintelligible error.

**Root cause (refactor regression):** `toWireStatus` (the DOS-form substitution) had (a) no mapping for STATUS_LOGON_FAILURE, so it fell through `default` and returned the raw NTSTATUS, and (b) several existing mappings with the **class/code bytes transposed** vs the field-validated legacy table (`service/smb/server.go` `smbStatusErr*` / `toWireErrorStatus`): BadNetworkName `0x00060001`→should be `0x00430001` (ERRinvnetname code 67, not 6), InvalidHandle `0x00010006`→`0x00060001`, NoMoreFiles `0x00010012`→`0x00120001`, NameInvalid `0x0002000C`→`0x00030001`.

**What we do:** `toWireStatus` now (1) maps STATUS_LOGON_FAILURE→`0x00020002` (ERRSRV/ERRbadpw), (2) uses the legacy table's exact `0x00<code>00<class>` values, and (3) has the legacy `default` guard — any *unmapped* NTSTATUS (high byte set) with the NT-status bit clear becomes `0x00010002` (ERRSRV/ERRerror) rather than leaking a raw NTSTATUS a CORE client would mis-read. When the request DID set NT_STATUS, the NTSTATUS is passed through unchanged.

**Where:** `core/service/smb/negotiate.go` (`toWireStatus`); reference `service/smb/server.go` + `command_core.go` (`toWireErrorStatus`) in the legacy tree.

### RAP unrecognised-function handling over IPC$ \PIPE\LANMAN — MUST be empty-success, NOT ERRbadfunc and NOT a synthesized record ([MS-RAP]) — refactor regression

**Observed (`captures/ipx.pcap`, Win98 NetBIOS-over-IPX):** Win98 does not list `\\CLASSICSTACK` and `net view` omits it, even though the NetServerEnum2 browse list correctly returns CLASSICSTACK/VM-WFW311/WIN98. Two RAP calls that Win98 issues over the IPC$ `\PIPE\LANMAN` pipe were answered **ERRDOS/ERRbadfunc "Invalid function"**:
- **NetServerGetInfo (function 13 / 0x000D)** — issued right after connecting (ParamDesc `WrLh`, ReturnDesc `B16BBDz`, detail level 1), to read the server's own SERVER_INFO (frames 34→35). Its failure makes Win98 abandon the server.
- **NetWkstaGetInfo (function 63 / 0x003F)** — issued when a user opens `\\server` (ParamDesc `WrLh`, ReturnDesc `zzzBBzz`, detail level 10), to read the workstation identity.

**Root cause (refactor regression):** the refactored `\PIPE\LANMAN` handler served only NetShareEnum (0x0000) and NetServerEnum2 (0x0068) and returned **STATUS_NOT_SUPPORTED for every other function** → a CORE-dialect (Win9x/WfW, Flags2=0) client reads that as ERRbadfunc and treats it as a fatal server error. The legacy service (`service/smb/session_dispatch.go` → `buildSMBTransactionEmptySuccess`) instead returned an **empty-success** TRANSACTION reply (SMB status SUCCESS, WCT=10, zero params/data) for any unrecognised RAP function, which the client tolerates.

**Do NOT synthesize the info records.** An intermediate fix that answered NetServerGetInfo/NetWkstaGetInfo with a hand-built SERVER_INFO_1 / WKSTA_INFO_10 (RAP string-pointer records, Converter=0, data-relative offsets — the exact convention NetShareEnum/NetServerEnum2 use, and the pointers verified in-range on the wire) **bluescreened the Win98 client**. The Win98 RAP receive path for these level-1/level-10 Get calls does not consume a server-supplied record the way NetServerEnum2 does; feeding it a non-empty reply is unsafe. Compatibility-over-correctness (rule #1): the legacy service NEVER produced these records — it answered empty-success — and that is the only form the client is known to tolerate.

**What we do:** the RAP dispatch answers only NetShareEnum and NetServerEnum2 with data; **every other function (incl. 0x000D and 0x003F) returns empty-success** (`buildTransactionResponse(h, nil, nil)` — WCT=10, zero param/data), matching the legacy `buildSMBTransactionEmptySuccess` exactly.

**Follow-up (second `captures/ipx.pcap`, still looping):** even after the ERRbadfunc→empty-success fix, Win98 kept re-issuing NetWkstaGetInfo ~5× at ~5 ms and then abandoned `\\CLASSICSTACK` without ever opening a disk share. Cause: the refactor's empty-success reply set a **non-zero ParameterOffset/DataOffset (0x37/55)** in the otherwise-empty 20-byte TRANSACTION word block, whereas the legacy `buildSMBTransactionEmptySuccess` leaves the **entire word block ALL ZERO** (offsets included). A Win98 RAP client reading a zero-count reply still follows ParameterOffset; a non-zero offset points past the frame end, so it treats the transaction as incomplete and retries forever. **Empty-success MUST zero the offsets too, not just the counts** — the reply is byte-identical to the legacy builder only when ParameterCount, DataCount, ParameterOffset, DataOffset, and ByteCount are all 0. Fixed in `buildTransactionResponse`: the offsets are computed only when there are real params/data; the empty case emits an all-zero block.

**Where:** `core/service/smb/lanman.go` (`handleTransaction` fallthrough → `buildTransactionResponse`, which now emits an all-zero word block for the no-params/no-data case); reference `service/smb/session_dispatch.go` + `command_rap_lanman.go` (`buildSMBTransactionEmptySuccess`) in the legacy tree.

### RAP NetWkstaGetInfo (level 10) over NetBEUI MUST return a real WKSTA_INFO_10 record ([MS-RAP]) — observation-based, transport divergence with IPX

**Observed (`captures/netbeui.pcap`, Win98 over NetBEUI):** a Win98 client opening `\\CLASSICSTACK` in Explorer gets all the way through the stack — LLC2 SABME/UA, NBF Session Init/Confirm, SMB Negotiate, `TREE_CONNECT \\CLASSICSTACK\IPC$` and `\\CLASSICSTACK\FOO`, and even a full `FIND_FIRST2`/`FIND_NEXT2` directory listing of the disk share (frames 274→310, real filenames) — but then **loops NetWkstaGetInfo (function 63 / 0x003F, ReturnDesc `zzzBBzz`, level 10) forever** (frames 128→192, again 339→363 at the capture tail, ~3–30 ms apart). The server answers each one **empty-success** (WCT=10, all-zero word block, ByteCount=0 — verified all offsets zero, i.e. the "correct" empty-success from the section above); the client NBF-ACKs the reply, rejects it at the RAP layer, and re-issues immediately. Explorer never finishes opening `\\classicstack`.

**Root cause:** the empty-success form for NetWkstaGetInfo that the **IPX** Win98 client tolerates (previous section) is **rejected by the NetBEUI Win98 redirector** — it requires an actual WKSTA_INFO_10 record with a valid RAP Status word. This is a genuine per-transport behavioural divergence in the same OS: IPX-hosted vs NetBEUI-hosted NetBIOS drive the RAP receive path differently. (`main` also answers empty-success here, so this is a gap in both trees, exposed only once the NetBEUI session path reached SMB — see [[netbeui-llc2-regression]].)

**What we do:** NetWkstaGetInfo **level 10** now returns a real WKSTA_INFO_10 record (`zzzBBzz`): computername(z), username(z), langroup/workgroup(z), ver_major(B)=4, ver_minor(B)=0, logon_domain(z), oth_domains(z) — each `z` a 4-byte data-relative pointer (low word = offset into the data heap, high word 0; the NetShareEnum/NetServerEnum2 convention), strings NUL-terminated in the heap after the 22-byte fixed part. RAP Status/Converter = 0. A non-level-10 WkstaGetInfo still falls through to empty-success. NetServerGetInfo (0x000D) is unchanged (empty-success).

**⚠ Conflict with the IPX bluescreen note above — must re-verify.** The previous section records that a hand-built WKSTA_INFO_10 **bluescreened the Win98-over-IPX client**. This fix was made per an explicit request to always return the record (chosen over a transport-conditional variant). If a regression appears on the IPX path, the correct resolution is to gate the WKSTA_INFO_10 record to NetBEUI-transport sessions only (the SMB session would need to learn its transport family) and keep empty-success for IPX. Re-capture Win98-over-IPX against this build to confirm the record no longer crashes it before treating the always-on behaviour as settled.

**Where:** `core/service/smb/lanman.go` (`handleTransaction` case `rapNetWkstaGetInfo` → `handleNetWkstaGetInfo` → `buildNetWkstaGetInfoResponse`; level parsed by `parseRAPDetailLevel`). Tests: `TestNetWkstaGetInfoReturnsIdentity`, `TestNetWkstaGetInfoNonLevel10EmptySuccess`.

### Browser Local Master Announcement server-type must set the Master Browser bit ([MS-BRWS] §2.2.1) — refactor regression

**Observed (`captures/ipx.pcap`, frame 201):** after ClassicStack wins the browser election and sends a Local Master Announcement (browser opcode 0x0F), the frame's `ServerType` was `0x00402003` (Workstation|Server|WfW|Win95) with the **Master Browser bit (0x00040000) clear**. A client does not accept a master browser whose announcement omits the master bit.

**Root cause (refactor regression):** the refactored browser used the same `ServerTypeWorkstationSet` for both host (0x01) and local-master (0x0F) announcements. The legacy service (`service/smb/server.go` `sendLocalMasterAnnouncement`) set `ServerType = WorkstationMask | MasterMask` for the 0x0F frame.

**What we do:** `announcementBody` now ORs in `ServerTypeMasterBrowser` (0x00040000) when the opcode is `OpLocalMasterAnnounce`, so the local-master announcement advertises `0x00442003`; the plain host announcement keeps the base workstation set.

**Where:** `core/service/browser/handle.go` (`announcementBody`).

### Browser must self-elect on a master-less segment ([MS-BRWS] §3.2.5) — behavioural gap (both trees)

**Observed (`ipx.pcap` vs `netbeui.pcap`, same 4-min run, 2026-07-07):** `net view` lists `\\CLASSICSTACK` over NetBEUI but is **empty over IPX/NBIPX**. Over NetBEUI a client sent a RequestElection (0x08) → ClassicStack won → sent a Local Master Announcement (0x0F) at t=63.9s and thereafter owned the browse list. Over IPX **no client ever sent a RequestElection**: a Win98 box self-declared local master (0x0F) unopposed, ClassicStack emitted only periodic Host Announcements (0x01, no Master bit), and **every NetServerEnum2 (`net view`'s RAP browse-list call) went client-to-client — not one was ever addressed to ClassicStack**. Direct-IPX file access (`\\CLASSICSTACK\IPC$`, `\\CLASSICSTACK\FOO`) worked throughout; the failure is purely browser presence.

**Root cause (NOT a refactor regression — present in `main` too):** the election machine is purely *reactive* — a browser only starts an election when it receives a RequestElection (0x08). On a segment where every station is a Win9x/WfW box that elects one of its own and never asks ClassicStack, ClassicStack never advertises master-browser presence, so clients never query it for the browse list. `main`'s `service/smb/server.go` has the identical reactive-only design.

**What we do:** on `Start`, after the first Host Announcement, a `discoverMaster` watcher waits `masterDiscoveryDelay` (30s) for an existing master. If none announced (no Local Master Announcement from another node, no election we lost) and we are still a potential browser, we **force our own election** (broadcast RequestElection, run the uncontested transmit loop, become local master, emit the 0x0F announcement). A real master that announces within the window sets `masterSeen` and suppresses the self-election, so ClassicStack never fights a legitimate Windows master browser. The reactive path is unchanged.

**Where:** `core/service/browser/browser.go` (`discoverMaster`, `masterDiscoveryDelay`, `masterSeen`, wired from `Start`); `core/service/browser/handle.go` (`observeAnnouncement`/`handleElection` set `masterSeen`).

### AndX chaining must be processed server-side — [smb6.0] 988 "ANDX SMB Messages", NT 3.51 depends on it

**Spec ([smb6.0] 988–1008):** LANMAN1.0+ clients may chain multiple requests in one message; "There is one message sent containing the chained requests and there is one response message" (rule 3); "The server will implicitly use the result of the first command in the 'X' command" — the SESSION_SETUP_ANDX UID / TREE_CONNECT_ANDX TID flow into the chained blocks (rule 5); "The first Command to encounter an error will stop all further processing" (rule 7), with the error in the single response header (rule 8); AndXOffset is measured from the start of the SMB header (rules 1, 9).

**Observed (`netbeui.pcap` frames 174/175, NT 3.51, 2026-07-09):** NT opens a share with one message chaining SESSION_SETUP_ANDX → TREE_CONNECT_ANDX (`\\CLASSICSTACK\FOO`). The refactor dispatch served only the primary command and replied with `AndXCommand = 0xFF` — the chained tree connect was silently ignored. NT treats that as a failed tree connect: it does **not** retry the share; it falls back to `\\CLASSICSTACK\IPC$` + an NT_CREATE_ANDX of the `\srvsvc` RPC pipe and ultimately reports "access denied" to the user. Win9x/WfW sends these commands unchained, which is why the gap was invisible until an NT client was tested.

**Related observation (frames 189/190):** the NT redirector surfaces the status of the `\srvsvc` pipe open verbatim. Our IPC$ tree answered NT_CREATE_ANDX with the generic `treeFor` ACCESS_DENIED → the user sees "Access denied". A server that serves no RPC pipes must answer STATUS_OBJECT_NAME_NOT_FOUND ("no such pipe") so the user at least sees a truthful error.

**Correction (netbeui.pcap 2026-07-10, frames 265–285):** the earlier theory that NOT_FOUND "steers the redirector to its RAP fallback" is WRONG. Observed: NT 3.51 `net view` against our NT LM 0.12 server opens IPC$, NT_CREATEs `\srvsvc`, receives ERRDOS/ERRbadfile (the correct DOS-status mapping of OBJECT_NAME_NOT_FOUND for its Flags2=0x0003 session), then tree-disconnects and LOGOFFs without ever attempting a RAP NetShareEnum over \PIPE\LANMAN — the user sees "access denied". CAP_RPC_REMOTE_APIS was already clear in our Capabilities, so its absence does not trigger the fallback either; NT appears to commit to MS-RPC share enumeration purely from the negotiated NT LM 0.12 dialect (it RAPs only against pre-NT-dialect servers, e.g. WfW's LANMAN2.1). Serving `net view` from NT therefore requires an actual `\srvsvc` pipe implementing NetrShareEnum ([MS-SRVS]) — RAP alone is not reachable from an NT client on an NT dialect.

**What we do:** `Dispatch` now walks the AndX chain (`processAndXChain`): each chained block is re-framed with the shared header and dispatched, its response block is spliced onto the reply with the previous block's AndXCommand/AndXOffset patched, and the response header accumulates the chained status/TID/UID. FID inheritance for chained OPEN_ANDX → I/O is not implemented (no client in the compatibility set chains an open with I/O). NT_CREATE_ANDX on the IPC$ tree returns STATUS_OBJECT_NAME_NOT_FOUND.

**Where:** `core/service/smb/andx.go` (`isAndXRequest`, `processAndXChain`), `core/service/smb/dispatch.go` (`Dispatch`/`dispatchOne` split), `core/service/smb/ntcreate.go` (IPC$ pipe-open status).

### TRANS2_QUERY_FS_INFORMATION + SMB_QUERY_FILE_NAME_INFO are mandatory for NT clients; NT info-level strings are ALWAYS Unicode — [smb6.0] 4097/4116, [MS-CIFS] §2.2.8.2/§2.2.8.3.9

**Observed (`netbeui.pcap` frames 486–493, NT 3.51, 2026-07-09):** immediately after opening a share root, NT issues TRANS2 QUERY_FILE_INFO level 0x0104 (SMB_QUERY_FILE_NAME_INFO) on the root FID and TRANS2 QUERY_FS_INFO level 0x0102 (SMB_QUERY_FS_VOLUME_INFO). Both were answered ERRDOS/1 "Invalid function" (QUERY_FS_INFO had no handler; NAME_INFO was an unsupported pack level) — NT then closed everything, logged off, and reported the share connect as failed ("access denied") to the user.

**Spec traps:** (1) the QUERY_FS_INFO response carries **no parameter bytes** — data block only ([MS-CIFS] §2.2.6.4.2), unlike the QUERY_PATH/FILE_INFO responses (one ignored EaErrorOffset param word). (2) Info levels above 0x102 "are mapped to corresponding calls to NtQueryVolumeInformationFile" ([smb6.0] 4116) — their strings (volume label, filesystem name, file name) are **UTF-16LE regardless of the negotiated wire charset**; this NT 3.51 session was ASCII (no Flags2 Unicode) yet expects Unicode in these structures. (3) SMB_QUERY_FS_VOLUME_INFO has an 18-byte fixed part (the 2 bytes after VolumeLabelSize are SupportsObjects+Reserved per [MS-FSCC] 2.5.9).

**What we do:** `queryFSInfo` serves SMB_INFO_ALLOCATION (1), SMB_INFO_VOLUME (2, wire-charset label — the pre-NT form Win9x asks without CAP_NT_SMBS), SMB_QUERY_FS_VOLUME_INFO (0x102), SIZE (0x103), DEVICE (0x104, disk+mounted), ATTRIBUTE (0x105, "NTFS", case-preserved, 255-byte names — reporting FAT would trigger 8.3 name rules). Geometry mirrors SMB_COM_QUERY_INFORMATION_DISK (512-byte sectors × 64/unit); serial is an FNV-1a of the share name. `packFileNameInfo` serves level 0x0104 for both QUERY_PATH_INFO and QUERY_FILE_INFO ('\\'-rooted share-relative path, UTF-16LE).

**Where:** `core/service/smb/trans2.go` (`queryFSInfo`, `packFileNameInfo`, `utf16LEBytes`, `volumeSerial`).

### NT refuses USER-level security without a challenge — NEGOTIATE must advertise SHARE-level when the server is guest-only ([MS-CIFS] 2.2.4.52.2 SecurityMode)

**Observed (`netbeui.pcap` frames 51–61, NT 3.51 `net view \\classicstack`, 2026-07-09):** our NT LM 0.12 NEGOTIATE response advertised SecurityMode 0x01 (USER-level) with ChallengeLength 0 (plaintext). NT answered with NBF Session End + LLC DISC immediately — it never sent a SESSION_SETUP — and reported "access denied" to the user; it then re-negotiated twice with the same result. The NT-family redirector will not send a plaintext password (EnablePlainTextPassword defaults off), so a user-level server that offers no challenge is simply unusable by NT. `main` had the same posture (`negotiateSecurityMode = 0x01`, only ever validated against Win9x/DOS, which do send plaintext).

**What we do:** `securityMode()` decides per NEGOTIATE: SHARE-level (0x00) when no named users exist — no credentials are wanted, so NT proceeds without any password, matching the "no users ⇒ guest-open" policy — and USER-level (0x01) once the wired store holds users. Because the compose root wires the built-in store even when empty, wiring alone is not the signal: the store reports `HasUsers()` (structural upgrade on the Authenticator seam), read live so adding the first user via the web UI flips subsequent negotiates. A store WITH users keeps the historical limitation: Win9x/DOS clients authenticate in cleartext; NT clients would need LM/NTLM challenge-response, which is not implemented.

**Where:** `core/service/smb/negotiate.go` (`securityMode`, `negotiateSecurityModeShare`/`User`), `adapter/auth/local/store.go` (`HasUsers`).

### OS/2 LAN Manager volunteers user + password on every SESSION_SETUP — an empty store must accept it as guest

**Spec:** [smb6.0] 289–291 covers the credential-less "implicit user logon" (empty password → admit). It does not say what a server without accounts should do with a credential it never asked for.

**Observed (`netbeui.pcap`, OS/2 LAN Manager client 02:60:8c:c6:dc:44, frames 31–32, 2026-07-10):** unlike Win9x — which sends its logon name with an EMPTY password to a guest-open server — the OS/2 redirector sends its logged-on **username and a non-empty password** in SESSION_SETUP_ANDX even against a server it should treat as passwordless. Validating that pair against an empty user store necessarily fails (unknown account), and the resulting ERRSRV/ERRbadpw surfaces on the client as "access denied" for `net view \\SERVER`.

**What we do:** SESSION_SETUP only authenticates when the wired store actually HAS named users (`storeHasUsers`, the same live signal NEGOTIATE's `securityMode` uses); with an empty store every presented credential — named, passworded, or hashed — is accepted as a guest session (Action=0x0001).

**Where:** `core/service/smb/negotiate.go` (`handleSessionSetup`, `storeHasUsers`).

### NEGOTIATE MaxMpxCount=1 starves the NT redirector client-side — error 1450 with nothing on the wire

**Spec:** [MS-CIFS] 2.2.4.52.2 — MaxMpxCount is "the maximum number of outstanding SMB operations the server supports"; it caps how many requests the *client* may pipeline, not server concurrency.

**Observed (`netbeui.pcap`, NT 3.51 client 00:00:d8:50:ae:d3, 2026-07-10):** with MaxMpxCount=1 advertised, NT completed NEGOTIATE → SESSION_SETUP+TREE_CONNECT → both \srvsvc probes (every server frame Wireshark-clean, all LLC2-acked), then went silent — no request, no disconnect — and `net view` surfaced error 1450 (ERROR_NO_SYSTEM_RESOURCES). The failure is generated *inside* the NT redirector: it reserves multiplex slots for oplock breaks, echoes and transaction secondaries, and with one credit fails the next operation with STATUS_INSUFFICIENT_RESOURCES before anything reaches the wire. Confirmed fixed e2e by raising the advertisement.

**What we do:** advertise MaxMpxCount=50 (what NT and Samba servers advertise). We process pipelined requests in arrival order regardless, so the value is a client-behavior promise, not a server capacity.

**Where:** `core/service/smb/negotiate.go` (`negotiateMaxMpxCount`).

### FIND_FIRST2 BOTH_DIRECTORY_INFO: FileNameLength counts one NUL on ASCII sessions; ShortName is ALWAYS Unicode — [MS-CIFS] §2.2.8.1.7 <167>/<168>, NT 3.51 enforces

**Spec:** SMB_FIND_FILE_BOTH_DIRECTORY_INFO's ShortName field "MUST contain the 8.3 name, if any, of the file **in Unicode format**" (UTF-16LE regardless of session charset; ShortNameLength 0 = no 8.3 name). Footnotes <167>/<168>: NT servers NUL-terminate FileName, and when CAP_UNICODE is NOT negotiated the one NUL byte **is counted** in FileNameLength (on Unicode sessions the padding NULs are uncounted).

**Observed (`netbeui.pcap`, NT 3.51, frames 166/169, 2026-07-10):** we packed FileName with no terminator and FileNameLength = exact name bytes, and ShortName as the wire-charset long name (14 ASCII bytes — neither Unicode nor 8.3). Wireshark parsed all 27 entries cleanly, NT acked and Find-Close2'd the search — then displayed an **empty directory**: the redirector silently discarded every entry. The NT redirector was written against NT servers and expects their exact termination/counting behavior. Confirmed fixed e2e (NT and OS/2 both list correctly).

**What we do:** FileName always carries a NUL terminator (1 byte ASCII / 2 bytes UTF-16LE); on non-Unicode sessions FileNameLength = name+1, on Unicode sessions the terminator is uncounted padding. ShortName is emitted as uppercase UTF-16LE only when the backend supplies a *distinct, valid 8.3* alternate name, else ShortNameLength=0 (the Samba "mangled names = no" posture).

**Where:** `core/service/smb/trans2.go` (`packFindBothDir`, `shortNameUTF16`, `is8dot3`); regressions in `trans2_test.go`.

### CLIENT: a FIND_NEXT2 page that returns zero entries is end-of-search, even when the server never sets the EndOfSearch flag — [MS-CIFS] §2.2.6.3.2

**Spec:** the TRANS2 FIND response parameter block carries `EndOfSearch` — "if nonzero, the search can be closed... the last entry has been returned" ([MS-CIFS] §2.2.6.2.2 / §2.2.6.3.2). A well-behaved server sets it on the response that carries the final batch (or on the first empty batch after it), and a client is entitled to page with FIND_NEXT2 until that flag appears. The spec does not say a server MUST set it — only that a nonzero value means end.

**Observed (live `csfs` over SMB-over-NBF → real Windows 98, 2026-07-28):** listing `\WINDOWS` (240 entries, paged across ~13 FIND_NEXT2 batches at Win98's MaxBufferSize 2920). Win98 answers the FIND_NEXT2 that runs off the end of the directory with **SearchCount=0, DataCount=0, and EndOfSearch=0** — it signals exhaustion by returning an empty page, NOT by setting the flag. Two client bugs compounded on top of this:

1. **The search id was not carried across pages.** FIND_NEXT2 responses do not repeat the SID (their param block is SearchCount/EndOfSearch/EaErrorOffset/LastNameOffset only), but `ReadDir` re-read `res.SID` from each FIND_NEXT2 result — which parsed as 0. The *second* FIND_NEXT2 therefore carried SID=0 and Win98 rejected it with **ERRDOS/ERRbadfid (status `0x00060001`)**. Every directory needing three or more pages failed with an "unhandled response"; a directory that fit in two pages happened to work (the one and only FIND_NEXT2 still had the correct SID from FIND_FIRST2).

2. **Relying on the EndOfSearch flag alone looped forever.** With the SID fixed, Win98 stopped erroring but the loop condition `for !res.EndOfSearch` never became true — the client sent the same FIND_NEXT2 hundreds of thousands of times (each returning the empty end-of-search page), so the listing never terminated.

**What we do:** `ReadDir` captures the SID once from the FIND_FIRST2 reply and reuses it for every FIND_NEXT2, and treats a page with **zero entries** as end-of-search in addition to the EndOfSearch flag (and the ERRnofiles/`NO_MORE_FILES` status). One of the three signals ends the paging run. Confirmed live: `\WINDOWS` lists all 240 entries in ~1.9 s with no duplicates and no error.

**Where:** `client/smb/filesystem.go` (`ReadDir`); regression in `client/smb` (`TestReadDirPagesUntilEmptyPage`).

### TRANS2 FIND over a connectionless transport (direct SMB over IPX) MUST honour MaxDataCount — one datagram, then page — [MS-CIFS] §2.2.4.46.1

**Spec:** a TRANS2 request carries `MaxDataCount`, "the maximum number of data bytes the client will accept in the transaction response" ([MS-CIFS] §2.2.4.46.1). Over a reassembling transport (NBT/TCP) the server MAY chunk a larger reply into TRANS2 continuations (DataDisplacement reassembly), so packing beyond one message is tolerable there; the spec does not spell out the connectionless case.

**Observed (live `csfs` client over direct SMB-over-IPX on socket 0x0550, pcap, 2026-07-23):** a `FIND_FIRST2 "\*"` of a ~30-entry share hung. The client's request advertised `MaxDataCount` capped to one datagram (1272), but the server IGNORED it: `packFindBothDir` packed by entry count only (SearchCount up to 256), producing a single 4434-byte SMB response. Direct-hosted SMB over IPX is connectionless — one IPX datagram = one whole SMB message, no reassembly — so a 4434-byte reply becomes a ~4470-byte datagram that **exceeds the Ethernet MTU and is never transmitted** (server log showed `response status=0 bytes=4434`; no 0x32 frame reached the wire; NEGOTIATE/SESSION_SETUP/TREE_CONNECT and the tiny teardown replies all transmitted fine). The `maxBufferSize` continuation-chunker (4356) does not help: its *primary* fragment still overflows the MTU, and a connectionless request/response client cannot collect the pushed continuations.

**What we do:** the FIND packers (`packFindBothDir`/`packFindStandard`) now take a byte budget derived from the request's `MaxDataCount` (`findDataBudget`), stopping before a record that would overflow it while always emitting at least one record. A partial batch returns end-of-search clear, and the client pages the rest via `FIND_NEXT2` — the connectionless-correct behaviour (one datagram per exchange), matching the classic DOS/WfW redirectors. A stream client sends `MaxDataCount` 0xFFFF → no byte cap → the single-message behaviour is unchanged. The client side caps `MaxDataCount` and READ/WRITE sizes from a transport `MaxResponse()` seam (`client/smb`). Confirmed e2e: the live `ls` lists the whole share, FIND replies now 1264–1266 bytes each.

**Where:** `core/service/smb/trans2.go` (`parseTransaction2` MaxDataCount, `findDataBudget`, `packFindEntriesBudget`, `packFindBothDir`/`packFindStandard` byte budget); `client/smb` (`Transport.MaxResponse`, `Session.applyTransportLimits`, IPX `ipxMaxResponse`); regression `TestTrans2_FindFirst2MaxDataCountPages` in `trans2_test.go`.

### FIND_FIRST2 pre-NT info levels SMB_INFO_STANDARD (0x0001) / SMB_INFO_QUERY_EA_SIZE (0x0002) are mandatory for OS/2 — [MS-CIFS] §2.2.8.1.1/§2.2.8.1.2

**Spec:** the LANMAN2.0 find levels: optional ResumeKey(4, only when SMB_FIND_RETURN_RESUME_KEYS is set in the request Flags), SMB_DATE/SMB_TIME creation/access/write pairs, FileDataSize(4), AllocationSize(4), Attributes(2), then (EA level only) EaSize(4), FileNameLength(1) and the name. Footnotes <153>/<154>: NT servers NUL-terminate the name and do NOT count the terminator in FileNameLength — the opposite counting rule from the 0x0104 level.

**Observed (`netbeui.pcap`, OS/2 LAN Server 4.06 client 02:60:8c:c6:dc:44, frames 308–337, 2026-07-10):** the OS/2 redirector enumerates directories with level 0x0002 (and 0x0001), never 0x0104. Our ERRbadfunc reply made `dir` fail; OS/2 then tried to read its own message file `\OSO001.MSG` **over the same share** with a level-0x0001 find — which also failed — so the user saw the unrenderable-message fallback **SYS0318** instead of an error. Records are packed back to back with no alignment. Confirmed fixed e2e.

**What we do:** serve 0x0001/0x0002 from the same snapshot search the 0x0104 path uses (`packFindStandard`), honoring the resume-key flag; EaSize is 0 (no EAs).

**Where:** `core/service/smb/trans2.go` (`supportedFindLevel`, `packFindEntries`, `packFindStandard`); regressions in `trans2_test.go`.

### SMB_INFO_QUERY_EAS_FROM_LIST: requested-but-missing names MUST get a zero-length placeholder FEA, not be omitted — [MS-CIFS] §2.2.8.3.3, confirmed against real IBM Peer traffic

**Spec:** SMB_INFO_QUERY_EAS_FROM_LIST's response is a SMB_FEA_LIST containing "pairs where the AttributeName field values match those that were provided in the request" ([MS-CIFS] §2.2.8.3.3). The spec text doesn't explicitly state whether a requested name absent from the file's EA set must still appear in the response — it is silent on omission vs. placeholder.

**Observed (`captures/ibm-peer-clients.pcapng`, real IBM Peer/OS-2 client ↔ IBM Peer server, 2026-07-15):** frames 505→507 (`\Desktop`, no EAs stored) — the client's GEA list requests `.ICON`/`.APPTYPE`/`.CHECKSUM`/`.ASSOCTABLE`; the real IBM server answers with **all four** as zero-length FEA records (`EA Data Length: 0` each), not an empty/short list. Frames 1428→1432 (`\OS!2 Warp Readme`, has a real icon) — same 4-name request; the response has `.ICON` populated (3041 bytes, EAT_ICON `0xFFF9` marker intact) **and still lists** `.APPTYPE`/`.CHECKSUM`/`.ASSOCTABLE` as zero-length placeholders. The server always answers with one FEA record per requested GEA name, positionally, regardless of whether the file has that EA.

We had initially implemented `filterEAs` to omit not-found names entirely (diagnosed against `netbeui.pcap` frames 554/559 against our own ClassicStack server, where the wire delivery and EA value were confirmed byte-correct but the omission was flagged as a possible cause of an OS/2 WPS icon-not-kept report). The IBM Peer capture settles it: omission is wrong.

**What we do:** `filterEAs` now emits `fs.EA{Name: n}` (zero Value) for any requested name with no stored match, preserving request order — one FEA record per requested GEA name, always.

**Where:** `core/service/smb/trans2.go` (`filterEAs`); regression in `trans2_test.go` (`TestTrans2_QueryEasFromListFiltersByName`).

### SMB_COM_WRITE_AND_CLOSE truncates the file to the write's end — [MS-CIFS] §3.3.5.34 is silent on resize, OS/2 Workplace Shell requires it

**Spec ([MS-CIFS] §3.3.5.34):** WRITE_AND_CLOSE is specified as seek-to-offset, write CountOfBytesToWrite bytes, then close the FID — no mention of resizing the file to the write's extent. Taken literally, a WRITE_AND_CLOSE that writes fewer bytes than the file's current size leaves the file at its old (larger) size, with stale bytes past the new write's end.

**Observed (`netbeui.pcap` 2026-07-15, OS/2 Workplace Shell client 02:60:8c:c6:dc:44):** WPS rewrites its `\WP ROOT. SF` desktop-state file entirely via a single OPEN_ANDX (OpenFunction 0x0011 — open-existing, **no truncate**) + WRITE_AND_CLOSE from offset 0, on a fresh FID each time, and never issues a separate resize (no SET_FILE_INFO EndOfFile, no truncating open). Frame 1044/1045: FID 0x0022, 383 bytes written, file becomes 383 bytes. Frame 1093/1094: FID 0x0023 (same path, freshly reopened non-destructively), only 346 bytes written at offset 0. WRITE_AND_CLOSE alone is WPS's only mechanism for shrinking the file — a spec-literal implementation left 37 stale trailing bytes from the previous write past the new EOF.

**What we do:** `handleWriteAndClose` truncates the file to `offset + bytesWritten` immediately before closing the FID, treating WRITE_AND_CLOSE as this FID's terminal write. `handleWrite` (plain SMB_COM_WRITE, no close) is unaffected — only the write-and-close-in-one-command form resizes.

**Where:** `core/service/smb/fileio.go` (`handleWriteAndClose`); regression in `fileio_test.go` (`TestFS_WriteAndCloseTruncatesShorterOverwrite`, reverted-and-reconfirmed against the bug before restoring the fix).

## LocalTalk / LLAP

### ZIP GetZoneList/GetLocalZones must set LastFlag on an empty page

**Spec (Inside AppleTalk, ZIP GetZoneList):** the reply's `LastFlag` byte tells the client no further pages remain. A paging client re-requests from the next start index until it sees `LastFlag == 1`.

**Observed:** our responder set `LastFlag` only when it emitted the final non-empty zone tuple. An **empty** page — an empty ZIT, or a `startIndex` past the end of the list — returned `numZones = 0` with `LastFlag = 0`, a *successful* reply that says "ask again." A paging client (the Mac Chooser, the Network control panel, and our own `AtalkGetZones` loop) then re-asks forever with no error and no timeout — the same "successful reply that never completes" freeze shape as the RTS bug, one layer up.

**What we do:** `handleGetZoneList` sets `LastFlag = 1` whenever `len(zones) == 0` after applying the start-index skip. The e2e client also defensively breaks its paging loop on a zero-zone page even if a buggy router forgot the flag.

**Where:** `core/service/zip/responding.go` (`handleGetZoneList`); client guard in `tools/end-to-end/macos/src/afp/atalk.c` (`AtalkGetZones`).

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

### Volume byte counts must cap at 2 GiB − 1, not the field's 4 GiB − 1 (FPOpenVol / FPGetVolParms)

**Spec:** The AFP 2.x volume bitmap's `BytesFree`/`BytesTotal` are unsigned 32-bit byte counts, so the wire format can express up to 4 GiB − 1.

**Observed (System 7.5 in snow over LToUDP, 2026-07-18):** Reporting a saturated `BytesTotal = 0xFFFFFFFF` crashes the classic AppleShare workstation client at mount with the Finder's "divide by zero" alert. The client derives an HFS allocation-block size from `BytesTotal` (≈ total/65536 → 0x10000), which overflows a 16-bit register to **zero**, and its next division faults. Whether a client is exposed depends on its FPOpenVol request bitmap: the crashing 7.5 client requested `0x01FF` (bytes included) at mount; the EtherTalk-side client in the same test period requested only `0x0020` (VolumeID) at mount and read sizes later via FPGetVolParms without crashing — so the failure looked transport-specific (LToUDP-only) when it was really client-version-specific.

**What we do:** `sat32` saturates both fields at **`0x7FFFFFFF`** (2 GiB − 1) — main's proven `capAFPBytes32` behaviour — which keeps the derived allocation-block size within 16 bits (0x8000) and also protects clients that treat the count as signed. A disk larger than the cap reports exactly the cap ("volume full-size"), never a wrapped value.

**Where:** `core/service/afp/handlers.go` — `afpMaxVolumeBytes`, `sat32` (used by `packVolParams` for both FPOpenVol and FPGetVolParms).

### Reported volume size drives the client's "size on disk" granularity (no AFP 2.x block-size field)

**Spec:** The AFP 2.x volume bitmap ends at bit 8 (Name); there is NO field for the allocation block size. Bits 9–11 are AFP 3.x additions (ExtBytesFree, ExtBytesTotal, BlockSize). The classic AppleShare workstation client derives the HFS allocation block size itself from the reported byte counts with 16-bit block math (block ≈ BytesTotal/65536, rounded up).

**Observed:** With BytesFree/BytesTotal saturated at the 2 GiB − 1 crash-safety cap (see the previous entry), every classic client derives 32 KiB allocation blocks, so the Finder shows every file's "size on disk" rounded up to a 32 KiB multiple — a 1 KB file reads "32K on disk". A capture comparing a real AppleShare server against ClassicStack on the same client shows the real server reporting its actual HFS figures (hundreds of MB) while we reported `0x7FFFFFFF`. The same frames showed two more divergences: the real server echoes a GetVolParms request bitmap exactly (0x0048 → 0x0048; we injected an unrequested VolumeID field, answering 0x0068), and reports a live volume ModDate (we reported the constant 2000 epoch). An earlier revision also served a "block size" under volume-bitmap bit 9 — which is actually AFP 3.x ExtBytesFree — dead code no classic client ever requested.

**What we do:** The reported figures are presentation values, not the host's: `reportVolBytes` clamps BytesTotal to the volume's configured `size_limit` (MiB, netatalk `volsizelimit` parity) — default **512 MiB**, giving 8 KiB blocks, a period-typical disk — and BytesFree to min(host free, total). A backend that cannot report usage presents an empty virtual disk of the reported size. The mislabeled bit-9 field is removed; FPGetVolParms echoes the requested bitmap verbatim (FPOpenVol keeps its forced VolumeID — the mount handshake needs it); the volume ModDate is the root directory's mtime when available. `sat32`'s 2 GiB − 1 cap remains the final wire guard for an operator limit set higher.

**Where:** `core/service/afp/handlers.go` (`reportVolBytes`, `defaultVolumeSizeLimit`, `afpGetVolParms`, `packVolParams`), `volume.go` (`SizeLimit`), `config.go` (`SizeLimitMB`), `compose/registry/reg_afp.go`; regressions in `core/service/afp/handlers_test.go` (`TestReportVolBytes_DefaultAndClamp`, `TestGetVolParms_EchoesRequestedBitmap`).

### FPGetSrvrInfo must keep advertising the pre-2.1 AFP version strings

**Spec:** FPGetSrvrInfo lists the AFP version strings the server accepts in FPLogin; the client picks the newest it shares.

**Observed:** The System 6 AppleShare workstation client only speaks `AFPVersion 1.1` / `AFPVersion 2.0`. A server whose FPGetSrvrInfo lists nothing older than `AFPVersion 2.1` is reported to the user as "the AFP server version is not supported" before FPLogin is ever attempted. The M7 spine's default list (`AFPVersion 2.1`, `AFP2.2`) silently dropped 2.0 that main's known-good set (`AFPVersion 2.0`, `AFPVersion 2.1`) carried.

**What we do:** The default advertisement is `AFPVersion 1.1`, `AFPVersion 2.0`, `AFPVersion 2.1`, `AFP2.2` (the netatalk-style span). The 2.x dispatch serves the older dialects unchanged — an old client simply never issues the newer commands.

**Where:** `core/service/afp/afp.go` — `defaultAFPVersions`; `supportsVersion` gates FPLogin against the advertised list.

### AFP attention codes / FPGetSrvrMsg (observed AppleShare capture vs AFP_Connection_Flow.md)

**Spec:** An earlier revision of `spec/AFP_Connection_Flow.md` gave `0x4000` as the "server is shutting down" attention code and did not document FPGetSrvrMsg or the message-push flow. Inside Macintosh documents the attention mechanism but not the AFP attention word's bit layout in the sections we hold.

**Observed (real AppleShare server):** The attention word is a bit field matching netatalk's `AFPATTN_*` constants — bit 15 `0x8000` = shutting down, bit 14 `0x4000` = crashed, bit 13 `0x2000` = server message waiting, bit 12 `0x1000` = don't reconnect, low 12 bits = minutes until shutdown. Observed words: `0x2000` (message push), `0xB001` (shutdown in 1 minute + message + no-reconnect), `0xB000` (the same, now). The attention TReq is sent **ALO** (XO clear), bitmap `0x01`, from the server session socket to the client's workstation session socket, with the entire ASP payload in the 4 ATP user bytes; the client acks with a 4-zero-byte TResp. After the final attention the server ends the session itself with a server-initiated `ASPCloseSession` TReq (user bytes `01 | SessionID | 00 00`), which the client TResp-acks. On the AFP side, the client fetches the login message (type 0) unprompted right after `FPOpenVol` and the server message (type 1) after each message attention; the reply's `MessageBitmap` is always `0x0001` (text), and the capability gate is `FPGetSrvrInfo` Flags **bit 3** (`0x0008`, SupportsSrvrMsg). Fetching a message does not clear it — the observed server re-serves the same text on every poll.

**What we do:** `spec/AFP_Connection_Flow.md` is corrected (shutdown = `0x8000`) and gains a "Server Messages & Attention" section. The service advertises `0x0008` always, serves the configured `login_message` as type 0 and the per-session pending operator message as type 1 (kept until replaced, MacRoman, capped at 199 bytes), sends message attentions as `0x2000`, and disconnects with the observed two-phase sequence (`ShutDown|Msg|NoReconnect|minutes` → final time-zero attention → fetch grace → server-initiated CloseSession). `Stop()` announces `0xA000` (shutdown + message), keeps serving for a short fetch grace, then closes every session.

**Where:** `core/protocol/asp/asp.go` (`AspAttn*`, `CloseSessPacket.MarshalUserData`); `core/service/afp/message.go` (`SendMessage`, `Disconnect`, `Sessions`), `handlers.go` (`afpGetSrvrMsg`, `srvrInfoSupportsSrvrMsg`), `asp.go` (`sendAttention`, `sendCloseSession`), `afp.go` (`Stop`); regressions in `core/service/afp/message_test.go` and `core/protocol/asp/asp_test.go`.

## Config codecs (§4)

### UCI empty-quoted-value tokenizer (M8)

**Spec:** OpenWRT UCI renders a string option as `option <key> '<value>'`; an unset string is `option <key> ''` — an empty single-quoted value is a valid, present value (the empty string), distinct from an absent option.

**Observed:** the `adapter/config/uci` tokenizer dropped any token whose accumulated text was empty, so `''` produced zero tokens. An `option key ''` line then had only two tokens, tripped the `len(tokens) < 3` arity check, and failed the **whole** `Unmarshal`. Because a default `config.Model` has empty well-known string fields (e.g. `Logging.Level == ""`), a freshly-marshalled default model could not be reloaded through UCI — only models that happened to set every string field round-tripped.

**What we do:** the tokenizer now tracks whether the current token was opened with a quote (`quoted`) and emits an empty token at the next separator/EOL when it was, so `''` yields one empty-string token. An unquoted run of whitespace still collapses to nothing. TOML was never affected (go-toml represents empty strings natively).

**Where:** `adapter/config/uci/uci.go` (`tokenize`); regression in `adapter/config/uci/auth_roundtrip_test.go` (`TestEmptyStringOptionRoundTrips`).

## IPX

### NBIPX (NWLink) name query must be answered on socket 0x0551 — observation-based

**No spec:** ClassicStack ships no formal document for Microsoft's NBIPX (NetBIOS-over-IPX / "NWLink") name-management protocol. The layout is from observation of Windows for Workgroups 3.11 / Win9x NWLink traffic in `captures/ipx.pcap`.

**Observed:** before a WfW/Win9x client opens an NBIPX session to a file server, it must first *resolve the server name to an IPX node*. It broadcasts the query two ways, and expects a positive reply naming the holder:

- **NMPI Query-name** — opcode `0xF3` on socket **0x0551** (NWLink SMB Name Query), IPX type PEP(4). Source socket is 0x0552 (Redirector). Answered with an NMPI **Name-found** (`0xF4`) echoing the `MessageID`/`NameType`/requested name, sent **unicast** back to the querier. This is the query the client actually retries in the capture (frames 138/141/144/148/151…) for `CLASSICSTACK<20>`.
- **NBIPX Find-name** — the IPX type-20 name-service packet (32 router-network bytes + NameTypeFlag + DataStreamType `0x01` + 16-byte name) on socket **0x0455**. Answered with the same packet shape carrying DataStreamType `0x02` (Name-recognized).

The name's 16th byte is the type suffix; `CLASSICSTACK<20>` (0x20 = Server service) is the file-server name and is compared exactly.

**Regression:** the M7 refactor's NBIPX engine (`core/service/netbios/nbipx.go`) scoped itself to the *session* data path only and was registered on socket 0x0455 alone. Socket 0x0551 was never registered and the name query was silently dropped, so a WfW client's "FindName for CLASSICSTACK" went unanswered and no session was ever opened — the legacy `service/netbios/over_ipx/transport.go` (its `handleNMPI` NameFound path) had answered it. The IPX port BPF filter (`"ipx"`) and 802.2-LLC / Ethernet-II demux were NOT at fault — the query reached the mini-router fine; the responder was simply missing.

**Regression (second client, 2026-07 `ipx.pcap`):** the NMPI-Query fix above (0x0551) covered only one client dialect. A *second* Win98 station in the newer capture (`WIN98-2`, node `00:86:b0:90:8e:3a`) resolves names **only** via the type-20 **Find-name (0x01) on socket 0x0455** — it never emits the NMPI Query on 0x0551 at all (verified: all of its packets target 0x0455). The engine's `handleNameService` decoded that Find-name but only ran claim-conflict detection; it never emitted the Name-recognized (0x02) reply the earlier errata (this section) had *described* but the responder never actually sent. So `WIN98-2` retried Find-name for `CLASSICSTACK<20>` indefinitely and never resolved the server, while the NMPI-Query client (`00:86:b0:ae:29:6f`) in the same capture opened its session fine.

**Regression (reply FORMAT + IPX type, 2026-07 `ipx.pcap` WIN98↔WIN98):** a follow-up capture between two real Win98 boxes (WIN98-1 `…ae:29:6f` serving, WIN98-2 `…90:8e:3a` browsing) shows the working handshake exactly: `Find name X<20>` → `Name recognized X<20>` → **`Session data` (SESSION_INITIALIZE)** → SMB Negotiate. After the fix above, CLASSICSTACK *did* send `Name recognized CLASSICSTACK<20>` (frames 45–52) — but WIN98-2 **still never followed up with Session-data** and kept retrying, exactly the spec's "if NAME_RECOGNIZED indicates a session, a SESSION_INITIALIZE is expected" contract failing. Byte-diffing our reply (frame 45) against the working WIN98-1 reply (frame 54) found two faults:

1. **Wrong IPX packet type.** WIN98-1 sends NAME_RECOGNIZED as **IPX type 4 (PEP)**; ours went out as **type 20 (NetBIOS broadcast)**. (FIND.NAME *queries* and name-claims are type 20; the directed reply is PEP.)
2. **The 32-byte "router" prefix is NOT a router list on this dialect — it is a self-identifying NetBIOS prefix the client validates.** The observed 50-byte reply body (frames 40/54, byte-identical regardless of the queried name) is:

   | Offset | Len | Contents (frame 54) | Meaning |
   |---|---|---|---|
   | 0 | 1 | `0x10` | leading status flag |
   | 1 | 1 | `0x02` | DataStreamType (NAME_RECOGNIZED) |
   | 2 | 16 | `WIN98-1`+`0x00` | responder's own name (workstation form) |
   | 18 | 14 | `WORKGROUP` (space-pad) | responder's workgroup |
   | 32 | 1 | `0x44` | NameTypeFlag: In-use (0x40) \| Registered (0x04) |
   | 33 | 1 | `0x02` | DataStreamType (echoed) |
   | 34 | 16 | queried name | the name being resolved |

   Our reply zero-filled bytes 0–31 and set byte 32 to `0x00`. Wireshark reads only offsets 32/33/34 so it *labelled* our packet "Name recognized CLASSICSTACK" and looked fine — but WIN98-2 validates the leading identity/status and silently discarded it, so no SESSION_INITIALIZE followed. This is a **NetBIOS-layer** frame (cf. `spec/iee802.md` NAME_RECOGNIZED, Table 5-20: DATA2 tt/ss state, dest/source names) carried inside NBIPX with the LLC LENGTH/DELIMITER stripped.

**What we do:** `handleNameService` answers a type-20 Find-name (0x01) for one of our owned names with a NAME_RECOGNIZED reply built by `protocol.EncodeNameRecognized(own, workgroup, queried)` — filling the `0x10`/own-name/workgroup/`0x44` prefix — sent as an **IPX type-4 (PEP)** datagram, unicast back to the querier (the query arrives broadcast). Our own name is the first local name in workstation form; the workgroup is the shared `Identity.Workgroup` (`netbios.Service.SetWorkgroup`, wired in `reg_netbios.go`, read live by the engine). Claim-conflict detection is now scoped to *positive* replies (Name-recognized / Name-in-use) from another node — a bare Find-name query no longer counts as an objection to our own name claim. The NMPI-Query path on 0x0551 is unchanged. `EncodeNameService` still zero-fills the prefix (a same-segment query/claim; the querier does not validate it there).

**Where:** `core/protocol/netbios/nbipx.go` (`EncodeNameRecognized`, `NBIPXNameRecogLeadStatus`/`NBIPXNameRecogNameFlag`, `NBIPXNameServicePacket` ERRATA), `core/service/netbios/nbipx.go` (`handleNameService`/`replyNameRecognized`/`ownName`/`workgroupName`), `core/service/netbios/netbios.go` + `session.go` (`SetWorkgroup`, workgroup callback into the engine), `compose/registry/reg_netbios.go` (`SetWorkgroup(m.Identity.Workgroup)`), `compose/runtime/transports.go` (`wireIPX` registers 0x0551). Capture-replay coverage in `core/service/netbios/nbipx_test.go` (NMPI Query) and `nbipx_name_test.go` (`TestNBIPX_FindNameAnswered` + `TestNBIPX_FindNameReplyMatchesCapture`, pinned to frame 54).

### NBIPX session header is 18 bytes, little-endian; session establishment rides DATA (0x06), not a SESSION_INIT stream type — observation-based

**No spec:** the NBIPX session-protocol layout is from observation of a Win98/WfW NWLink client in `captures/ipx.pcap` (frames 23–26), cross-checked against Timothy Devans' *nbf2cifs* NBIPX notes (<https://timothydevans.me.uk/nbf2cifs/x1570.html>), whose Table 2 documents an 18-byte header ending in "Receive Sequence number" (2) + "Bytes received" (2).

**Observed (session header, on socket 0x0455, IPX type 4 / PEP):** the header is **18 bytes** and its multi-byte fields are **little-endian**:

| off | field | note |
|---|---|---|
| 0 | ConnCtrlFlag | SYS 0x80 / ACK 0x40 / ATT 0x20 / EOM 0x10 |
| 1 | DataStreamType | see below |
| 2–3 | SourceConnID (LE) | sender's circuit id |
| 4–5 | DestConnID (LE) | peer's circuit id; `0xFFFF` = unassigned |
| 6–7 | SendSeq (LE) | |
| 8–9 | TotalDataLen (LE) | SMB message length |
| 10–11 | Offset (LE) | |
| 12–13 | DataLen (LE) | bytes in this frame |
| 14–15 | RecvSeq (LE) | receive sequence number |
| 16–17 | BytesReceived (LE) | |
| 18+ | Data | the SMB PDU begins here |

Observed DataStreamType values are a small set: `0x01` FIND.NAME, `0x02` NAME.RECOGNIZED, `0x06` **DATA** (every SMB session frame — establishment and messages both), `0x07` SESSION.END, `0x08` SESSION.END.ACK. **There is no distinct SESSION.INIT/CONFIRM stream type.** A client opens a circuit with a DATA frame whose `DestConnID == 0xFFFF` carrying a `[called-name(16) || calling-name(16) || 6-byte capability trailer]` payload; the server replies with a DATA frame that assigns its own `SourceConnID`, echoes the client's as `DestConnID`, and swaps the two names. SMB then flows as DATA (0x06) with both circuit ids populated; `EOM` in ConnCtrlFlag marks the last fragment.

**Regression (both trees):** the codec (and the legacy `service/netbios/over_ipx` it was ported from) modelled the header as **16 bytes, big-endian**, with a spurious `ConnCtrlByte`+`Reserved` pair at offsets 14–15 in place of the RecvSeq/BytesReceived words, and dispatched session data on stream types `0x15/0x16` (`DATA_ONLY_LAST`/`DATA_FIRST_MIDDLE`) with a `0x05` `SESSION_INIT` — a different NWLink dialect this client never emits. Consequences: (1) decode read the SMB body from offset 16, prepending two junk bytes (`RecvSeq`'s low half) to every SMB request so it never parsed; (2) replies were framed with a 16-byte header the client rejected; (3) the client's `0x06`-typed session request never matched the `0x05` INIT branch, so no circuit was ever accepted. Net effect: an NBIPX client could resolve `CLASSICSTACK` (the name query works) but **never negotiate a session** — `\\classicstack` and `\\classicstack\share` both failed with "cannot find the computer".

**What we do:** `NBIPXSessionHeader` is now 18 bytes, little-endian, with `RecvSeq`/`BytesReceived` fields; `NBIPXSessionData = 0x06` is the canonical DATA type. `handlePEP` dispatches DATA by `DestConnID`: the `0xFFFF` sentinel is a session request (`handleSessionRequest` allocates a circuit id and replies with the swapped-name accept), any other is an SMB message (`handleData`, reassembled by EOM). SESSION.END/END.ACK unchanged. The legacy `SessionInit`/`Confirm`/`DataOnlyLast`/`DataFirstMiddle` consts are retained as documented aliases for other dialects but are no longer used to frame this client's traffic.

**Where:** `core/protocol/netbios/nbipx.go` (`NBIPXSessionHeader` 18-byte LE layout, `NBIPXSessionData`, `NBIPXSessionHeaderLen`), `core/service/netbios/nbipx.go` (`handlePEP` DestConnID dispatch, `handleSessionRequest`/`sendSessionAccept`, `handleData` EOM, `sendData`/`pushData` stream type). Capture-replay coverage: `TestCaptureReplay_NBIPXSessionHeader` (frames 25/26) in `core/protocol/netbios/nbipx_capture_test.go`; establishment/data/reassembly in `core/service/netbios/nbipx_test.go`.

### NBIPX session-accept (SESSION_CONFIRM) must set ConnCtrlFlag 0x01 + RecvSeq 1 — observation-based

**No spec:** as above, from observation of `captures/ipx.pcap`. This is the fourth NBIPX round; it only surfaced once name resolution and the 18-byte header were both correct and a session actually reached the accept step.

**Observed (the working reference is the WFW-IPX server, not our own reply):** the capture has three NWLink stacks — a Win 3.11 client (`00:00:d8:72:e9:a4`), a Win98 client `WIN98-2` (`00:86:b0:90:8e:3a`), and a Win98 acting as **server** `WFW-IPX` (`00:86:b0:ae:29:6f`). When `WIN98-2` opens a session **to `WFW-IPX`** (frames 366→367→368) it works: its `SESSION_INITIALIZE` (DATA, `ConnCtrlFlag 0x41`, `DestConnID 0xFFFF`) is answered by an accept whose header is **`81 06 …`** — `ConnCtrlFlag = SYS(0x80) | 0x01` — **and `RecvSeq = 1`** (frame 367), after which `WIN98-2` immediately sends its first SMB (`ff 53 4d 42 …`, frame 368). When the *same* client opens a session **to `CLASSICSTACK`** (frames 331→332), our accept was **`80 06 …`** — bare `SYS`, `RecvSeq = 0`. The client did **not** treat that as a confirmed session: it retransmitted `SESSION_INITIALIZE` (frames 334, 337, 340, …) indefinitely and never sent SMB. The `0x01` low bit + `RecvSeq 1` together are the NBIPX-flattened analogue of NBF's distinct `SESSION_CONFIRM` command (`spec/iee802.md` §5.6.16, "SESSION_INITIALIZE acknowledgment"); NBIPX carries the confirmation on a DATA (0x06) frame with these two markers rather than a separate DataStreamType.

**Why the earlier rounds masked this:** the type-4 NBIPX session-establishment path stalled for *every* client against CLASSICSTACK, but the Win 3.11 client (and the earlier working SMB traffic to CLASSICSTACK) reached SMB through a **different** path — NMPI `Query name` → `Name found` → direct SMB — which bypasses the session handshake entirely. Only `WIN98-2`, which resolves solely via the type-20 Find-name → NBIPX session handshake, exposed the missing confirm. Lesson: a client "connecting fine" does not prove the session path works if it has an alternate discovery route.

**What we do:** `sendSessionAccept` now frames the accept as `ConnCtrlFlag = NBIPXConnFlagSYS | NBIPXConnFlagCONFIRM (0x81)` with `RecvSeq = NBIPXSessionAcceptRecvSeq (1)`. New consts `NBIPXConnFlagCONFIRM` and `NBIPXSessionAcceptRecvSeq` in `core/protocol/netbios/nbipx.go`.

**Where:** `core/protocol/netbios/nbipx.go` (`NBIPXConnFlagCONFIRM`, `NBIPXSessionAcceptRecvSeq`), `core/service/netbios/nbipx.go` (`sendSessionAccept`). Coverage: `TestNBIPX_AcceptHeaderMatchesCapture` and the strengthened `establishIPXCircuit` assertions in `core/service/netbios/nbipx_test.go` (pinned to frame 367).

### NBIPX browser mailslot delivery on socket 0x0553 — observation-based

**No spec:** as above, the NBIPX mailslot (browser) datagram form is from observation of WfW/Win9x NWLink traffic.

**Observed:** browser traffic (HostAnnounce / AnnouncementRequest / GetBackupList) over NB-IPX rides an **NMPI MailslotSend (opcode `0xFC`)** on socket **0x0553** (NB-IPX datagram), IPX type 20. The inner NetBIOS datagram (source/destination names + the SMB `\MAILSLOT\BROWSE` transaction) is carried in the NMPI Payload. A client populates its browse list from the HostAnnounce it receives and drives `net view` from it (or via GetBackupList → the master → NetServerEnum2).

**Regression:** after the name-query fix above let the *session* path work (so `\\classicstack` share enumeration worked), the browser path was still broken over IPX: the NBIPX engine's `HandleDatagram` handled only Query-name and dropped every MailslotSend, and the engine was not registered on 0x0553. So an IPX client's browse traffic never reached the browser and **ClassicStack never appeared in `net view`** even though `net view \\classicstack` worked. The legacy `service/netbios/over_ipx/transport.go` `handleNMPI` had routed MailslotSend to the datagram handler (with the remote IPX endpoint for a directed reply).

**What we do:** `handleNMPI` now routes a MailslotSend to the connectionless-datagram consumer (the browser) — the NB-IPX analogue of the NBF engine's `handleDatagram` — decoding the inner names/payload and marking Broadcast for a group destination. Compose registers the engine on socket **0x0553** in addition to 0x0455/0x0551.

**Directed replies (now plumbed).** The inbound `Datagram` carries a `ReplyTo *DatagramEndpoint` — a transport-tagged remote address (family + IPX network/node/socket, or the source MAC for NBF) — which the browser echoes back on its GetBackupList / AnnouncementRequest answer. `Service.SendDatagram` then emits a datagram with `ReplyTo` set out ONLY the transport it names (matched by `datagramEgress.transportFamily`), *unicast* to that node, instead of re-broadcasting on every wire; the NBIPX/NBF `emitDatagram` send directed when `ReplyTo` matches. The GetBackupList reply is sourced from the `<workgroup><1D>` master-browser identity (`backupListResponseSource`) when the request was addressed to our workgroup, the identity a Win9x client requires or it rejects the list and re-runs the election (`captures/ipx.pcap` frames 161–189). The consumer stays transport-agnostic — it treats `ReplyTo` as an opaque token, never reading the wire fields.

**Where:** `core/service/netbios/nbipx.go` (`handleNMPI`/`deliverMailslot`, directed `emitDatagram`, `NBIPXDatagramSocket`), `core/service/netbios/{session.go,netbios.go,nbf.go,nbf_datagram.go}` (`DatagramEndpoint`/`ReplyTo`, `SendDatagram` transport routing, per-engine `transportFamily`, NBF directed emit), `core/service/mailslot/mailslot.go` (`SendMailslotTo`, `Consumer.HandleMailslot` carries `replyTo`), `core/service/browser/handle.go` (`handleGetBackupList`/`replyHostAnnouncement`/`backupListResponseSource`), `compose/runtime/transports.go` (`wireIPX` registers 0x0553). Announcements now source from the `<20>` file-server name (was `<00>`), carry `UpdateCount=0x03` and the server comment, matching the legacy `sendHostAnnouncement`. Coverage: `TestNBIPX_InboundMailslotDeliveredToConsumer`, `TestNBIPX_DirectedMailslotUnicast`, `TestGetBackupListDirectedToRequester`, `TestAnnouncementRequestAnsweredDirected`.

### NBIPX name-claim + SAP advertisement on start — observation-based

**No spec:** as above, from observation of WfW/Win9x NWLink traffic and the legacy `service/netbios/over_ipx/transport.go` claim-then-advertise behaviour.

**Observed:** a NetBIOS-over-IPX server announces itself on start two ways so SAP-browsing and name-resolving clients discover it: (1) it broadcasts a **name-claim** — an IPX type-20 Find-name (DataStreamType `0x01`) plus an **NMPI ClaimName** (opcode `0xF1`) — on the 6×500ms NWLink cadence, and treats a matching inbound name-service packet from *another node* as a conflict that aborts the claim; and (2) on an uncontested claim it registers the server name with **SAP** under the NetBIOS service type **0x0640**, socket 0x0455, so a NETx/VLM-style SAP browse finds it.

**Regression:** the refactor's NBIPX engine was a pure responder — it answered inbound name queries but never broadcast its own claim and never advertised via SAP, so a client relying on SAP discovery (or on seeing the claim) would not find the server. The refactor's only SAP advertiser was NCP-specific and could not co-own socket 0x0452.

**What we do:** the NBIPX engine gained `ClaimName` (broadcast Find-name + NMPI ClaimName, watch `handleNameService` for a conflicting owner, ignoring our own looped-back node). A **shared** `core/service/sap` advertiser now owns socket 0x0452: NCP and NB-IPX both register their `SAPEntry` through it (one handler, many services), and it periodically broadcasts + answers nearest/general queries for every registered type. Compose (`wireIPX`) runs the claim per `<20>` file-server name off the wiring path and registers the NetBIOS SAP entry on an uncontested claim.

**Where:** `core/service/netbios/nbipx.go` (`ClaimName`/`broadcastFindName`/`broadcastNMPIClaim`/`noteClaimConflict`, `NBIPXServerSocket`), `core/service/netbios/session.go` (`IPXEngine.ClaimName`), `core/service/sap/sap.go` (shared advertiser), `core/protocol/ncp/sap.go` (`SAPServerTypeNetBIOS = 0x0640`), `core/service/ncp/sap.go` (`Service.SAPEntry`), `compose/runtime/transports.go` (`wireIPX` shared advertiser + claim). Coverage: `TestNBIPX_ClaimNameUncontested`, `TestNBIPX_ClaimNameContestedAborts`, `core/service/sap/sap_test.go`.

### NBIPX raw directed datagram + alternative name socket 0x0554 — observation-based

**No spec:** as above.

**Observed:** besides the NMPI-wrapped MailslotSend, a directed NetBIOS datagram can ride NB-IPX as a **raw** PEP packet on socket 0x0553 whose second byte is DataStreamType `0x0B` (NBIPXDirectedDatagram), carrying a bare NetBIOS datagram (dest name, source name, payload). Separately, some stacks perform name claim/query on the **alternative name socket 0x0554** rather than the session socket's type-20 broadcast.

**Regression:** the refactor handled only the NMPI-wrapped mailslot form on 0x0553 (dropping the raw directed form the legacy `handlePEP` delivered) and registered only three sockets (0x0455/0x0551/0x0553), dropping 0x0554 which the legacy transport claimed.

**What we do:** the engine's `HandleDatagram` now delivers a raw directed datagram (`deliverDirectedDatagram`, the raw analogue of `deliverMailslot`, with a `ReplyTo` from the sender's IPX address), and compose registers the engine on socket 0x0554 (`NBIPXNameSocket`); those name-service packets dispatch by IPX type exactly like 0x0455.

**Where:** `core/service/netbios/nbipx.go` (`deliverDirectedDatagram`, `NBIPXNameSocket`), `compose/runtime/transports.go` (`wireIPX` registers 0x0554). Coverage: `TestNBIPX_RawDirectedDatagramDelivered`, `TestNBIPX_NameSocket0554Delivered`.

### NBIPX session sequencing: SYS frames consume no SendSeq, RecvSeq is a cumulative ack, zero-data SYS|ACK probes must be answered — observation-based

**No spec:** as above, from observation of WinNT 3.51 and Win98 NWLink clients (`ipx.pcap` 2026-07-10; NT `00:00:d8:2a:2f:22`, Win98 `00:86:b0:90:8e:3a`). WfW 3.11 masked all of this because its `net view` uses connectionless SMB directly over IPX and never exercises the sequenced session path.

**Observed (the sequencing rules):**

1. **SendSeq is consumed by data-carrying frames and SESSION_END** — the SESSION_INITIALIZE (`0x41`, seq 0; the client's first SMB frame arrives with SendSeq 1), every data frame (fragments included), and SESSION_END (`0x40`, zero data). Zero-data SYSTEM/control frames — the `0x81` accept, an `0x80` ack, an `0x88` resend request, NT's `0xC0` probe — consume **nothing**: the accept carries SendSeq 0 and the client's first data frame still says `RecvSeq 0` ("your first data frame must be seq 0"). Ground truth from the WfW-client ↔ NT-server session (frames 488–509): WfW's bare-SYS `0x80` ack (seq 4) didn't consume — its next data frame reused seq 4 — while its SESSION_END (`0x40`, seq 5) did (NT's end-ack said RecvSeq 6). Acking a probe as if it consumed (`RecvSeq 2`) is a protocol error: NT aborts after ~9 probes and the client reports **error 59 "unexpected network error"** (round-3 misstep, corrected).
2. **RecvSeq is the cumulative acknowledgment** (next SendSeq expected from the peer). The accept says RecvSeq 1 (acking the connect); a response to the first SMB request must say RecvSeq 2.
3. **A data frame with the wrong SendSeq/RecvSeq is silently discarded** and answered with a zero-data `SYS|RESEND` (ConnCtrlFlag `0x88`, new flag bit **RESEND `0x08`**) whose RecvSeq names the seq to resend from, while the client re-sends its own frame with `SEND_ACK|EOM` (`0x50`).
4. **BytesReceived is the receive-window edge, and NT-as-client enforces it.** The field is `RecvSeq + posted receives` — the highest peer SendSeq the sender will accept, plus one. NT-as-server advertises `RecvSeq + 5` on every frame (accept = 6, then 7/8/9/10 as it consumes client frames); WfW advertises `+3`; Win9x/WfW **ignore** the field inbound (they transmit against our 0 and accept it). An NT client will not send data while the peer's advertised edge is below its next send seq: with our `BytesReceived 0` it polled with a zero-data `SYS|ACK` probe (`0xC0`, SendSeq 1) every ~600ms. Unanswered, NT gives up after ~7 probes and tears the session down (round 1); answered with a correct ack but a zero window (round 2), it re-probes for minutes until **Error 240 "the session has been cancelled"**. The probe reply is a zero-data SYS frame with unchanged `RecvSeq` and a `BytesReceived` that opens the window.
5. **The 6-byte trailer on SESSION_INITIALIZE/accept is `[max frame data (LE16)][timer][timer]`** — observed 0x05AC=1452 (NT), 0x0590=1424 (WfW), 0x05A0=1440 (Win98), then `15 00 09 00` (NT) / `25 00 0d 00` (Win9x family). NT-as-server echoes the client's max-frame value but substitutes its **own** timer pair; our verbatim echo of the whole trailer produced byte-identical output for NT and is accepted by all three clients.

**Regression:** the refactor's engine mirrored the client's SendSeq into its response (`SendSeq 1` instead of `0`), never stamped RecvSeq on data frames (`0` instead of `2`), and dropped zero-data frames in the fragment path. Effect on the wire: Win98 read our NEGOTIATE response as "server data frame 0 was lost + my request unacked", NAK'd with `0x88` and retransmitted NEGOTIATE forever (frames 275–307); NT's probe went unanswered so it never sent SMB at all (frames 149–176). Both failed `net view \\CLASSICSTACK`; WfW worked (connectionless path).

**What we do:** `ipxCircuit` carries `sendSeq`/`recvSeq` (window-of-one, init `0`/`1` at accept) plus the retained last response (`lastResp`/`lastRespSeq`); `handleData` validates SendSeq, treats `DataLen == 0` as session control (SYS|ACK probe → `sendSystemAck` with unchanged RecvSeq; SYS|RESEND → `resendData`), re-sends the retained response on a duplicate of the last consumed frame instead of re-serving the SMB, and `sendData`/`pushData` allocate one SendSeq per frame, stamp the live RecvSeq, and fragment responses larger than `nbipxMaxFrameData` (1452 = 1500 − IPX 30 − session header 18) via TotalDataLen/Offset/DataLen with EOM on the last frame. Every outbound frame advertises the receive window: `BytesReceived = RecvSeq + nbipxRecvWindow (5)`, mirroring NT's own advertisement. `handleSessionEnd`'s SESSION_END_ACK acknowledges the end frame's consumed seq (`RecvSeq = end SendSeq + 1`) and carries our send counter, matching NT's own end-ack (frame 509).

**Where:** `core/protocol/netbios/nbipx.go` (`NBIPXConnFlagRESEND`, sequencing-rules doc on `NBIPXSessionHeader`), `core/service/netbios/nbipx.go` (`ipxCircuit` seq state, `handleData`, `sendData`/`sendDataFrames`/`resendData`/`sendSystemAck`/`pushData`).

## NetBEUI (NBF)

### NBF transmit flow control (NO_RECEIVE / RECEIVE_CONTINUE / RECEIVE_OUTSTANDING) — [IBM SC30-3587] §5

**Observed:** an NBF peer regulates the server's transmit stream: it sends **NO_RECEIVE** (0x1A) when it has no RECEIVE posted (close its receive window), **RECEIVE_CONTINUE** (0x1C) when it can accept data again, and **RECEIVE_OUTSTANDING** (0x1B) to ask for the last data frame again (it missed a transmission). A server that ignores these loses the held frames — a WfW/Win9x client that throttles mid-reply never gets the response.

**Regression:** the refactor's NBF session engine deliberately dropped this transmit-reliability layer (its header comment called it "adapter-altitude"), whereas the legacy `service/netbios/over_netbeui/transport.go` carried the full NO_RECEIVE/RECEIVE_CONTINUE window + RECEIVE_OUTSTANDING retransmit + a pending-frame queue. Under the parity rule (main is the wire spec) this is a bug.

**What we do:** the engine now holds per-circuit tx state (`txBlocked`/`txPending`/`txLast`): `sendSessionData` queues frames while the window is closed and records the last frame sent; NO_RECEIVE closes the window, RECEIVE_CONTINUE flushes the queue, RECEIVE_OUTSTANDING retransmits the last frame. The state lives on the `circuit` and is dropped on SESSION_END / teardown. Byte-for-byte identical to the legacy transport on the wire.

**Where:** `core/service/netbios/nbf.go` (`handleNoReceive`/`handleReceiveContinue`/`handleReceiveOutstanding`, `sendSessionData`/`sendSessionFramesNow`, `circuit` tx fields). Coverage: `TestNBF_NoReceiveHoldsReplyUntilContinue`, `TestNBF_ReceiveOutstandingRetransmitsLast`.

### NAME_QUERY with Local Session No. 0 ("FIND.NAME request") MUST still be answered with NAME_RECOGNIZED — [IBM SC30-3587] §5.6.8/§5.6.10, observation-based

**Spec ([IBM SC30-3587] §5.6.8 Table 5-18 Data2):** in a NAME_QUERY, Data2's low byte `ss` "indicates the local session number that is assigned to refer to this session if the CALL is completed … A value of zero is not a valid session number and indicates a FIND.NAME request." §5.6.10 (NAME_RECOGNIZED, Function) says the response "indicates whether a session can be established with the queried name (CALL) **or** used to indicate the location of a name (FIND.NAME)" — i.e. a NAME_RECOGNIZED is the correct reply to **both** forms; the FIND.NAME reply just carries Data2 `ss = 0x00` ("No LISTEN command is pending for this name or this is a FIND.NAME response", §5.6.10 Data2).

**Observed (`captures/netbeui.pcap`):** a Windows CALL is **two-phase**. An NT 3.51 client (`00:00:d8:50:ae:d3`) first broadcasts a NAME_QUERY for `CLASSICSTACK<20>` with **Local Session No. 0** (frames 14–16, Wireshark: "Local Session No.: 0 (FIND.NAME request)") — a locate — and only *after* receiving a NAME_RECOGNIZED does it re-query **unicast** with a real session number (see frame 30, `ss = 0x01`), then drive SABME → SESSION_INITIALIZE. Both phases require a NAME_RECOGNIZED. In the same capture a Win98 client answers its own session-0 locate (frames 28→29: query `Data2 = 0x0000`, reply `0x0E` with XmitCorrelator echoing the query's RspCorrelator, `ss = 0`, dest/source names swapped) and the NT client then completes the call to it. ClassicStack (`de:ad:be:ef:ca:fe`) sent **nothing** in response to the `CLASSICSTACK<20>` locate, so the NT 3.51 client never learned the server existed and "MS-DOS/NT can't see \\CLASSICSTACK", even though the LLC2 and session layers were otherwise working.

**Regression:** `handleNameQuery` treated `ss == 0` as "FIND.NAME, not a CALL — no session to set up" and `return`ed silently, answering only the second (unicast, session-numbered) phase. That drops the initial locate every Windows CALL begins with. (Win98 got past this only because *its own* server answers the session-0 locate — a "connecting fine" peer masks the missing reply, cf. the NBIPX SESSION_CONFIRM note above.)

**What we do:** `handleNameQuery` now always replies NAME_RECOGNIZED for a name we own, and allocates a circuit **only** when `ss != 0` (a real CALL — reply carries the assigned local session number in Data2/RspCorrelator). For `ss == 0` (the locate/FIND.NAME) no circuit is created and the reply carries Data2 `ss = 0`, matching the Win98 reference reply byte-for-byte (command `0x0E`, XmitCorrelator = the query's RspCorrelator, dest = querier's source name, source = our name). A foreign name is still ignored.

**Where:** `core/service/netbios/nbf.go` (`handleNameQuery`). Coverage: `TestNBF_LocateQueryIsAnswered` (session-0 locate, pinned to the pcap fields) plus the existing `TestNBF_CallEstablishesCircuit` (session != 0).

### NBF LENGTH field is the HEADER length only (X'000E' / X'002C'), never header+payload — [IBM SC30-3587] §5.6 frame-format tables, NT 3.51 enforces

**Spec ([IBM SC30-3587] Table 5-25 DATA_ONLY_LAST et al.):** every NBF frame-format table gives byte 0–1 `LENGTH` as a **fixed constant** — `X'000E'` (14) for session frames (commands 0x14–0x1F), `X'002C'` (44) for non-session frames — i.e. the length of the NetBIOS header alone. USER DATA following the header is *not* counted.

**Observed (`captures/netbeui.pcap`, NT 3.51 `00:00:d8:50:ae:d3`, 2026-07-09):** our `Frame.Encode` wrote `header+payload` into LENGTH (e.g. `0x005D` = 93 on the 79-byte SMB NEGOTIATE response DOL, frame 2703), while the NT client's own DOL (frame 2702) carries `0x000E`. NT's `netbeui.sys` **silently discards** a session frame whose LENGTH differs — *without even acknowledging it at the LLC level*: its RR stayed at N(R)=1 across the original send and ~40 checkpoint-triggered LLC2 retransmissions (frames 2703–2934), then the client gave up with SESSION_END/DISC and reported **System Error 240** (ERROR_VC_DISCONNECTED, "The session was cancelled"). The failure was invisible on every zero-payload frame — NAME_QUERY replies, SESSION_CONFIRM, DATA_ACK — because there `header+payload == header` and the wrong formula produces the right bytes, which is exactly why name service and session setup interoperated while every data-bearing frame died. Win98 (`00:86:b0:a4:b8:81`, same capture) does not validate the field and accepted the malformed `0x005D` frames throughout. This also retro-invalidates the earlier "frame 191/49 framing is structurally valid" analysis and the NE2000 back-to-back-frame-loss theory: the drops were deterministic LENGTH rejection, not lossy hardware.

**What we do:** `Frame.Encode` writes `LENGTH = header length` (14 or 44 by command class). `Decode` continues to ignore the field on receive (lenient; both 0x000E-strict NT and any legacy sender parse fine).

**Where:** `core/protocol/netbeui/netbeui.go` (`Encode`). Coverage: `TestSessionFrameRoundTrip` now pins `LENGTH == 0x000E` on a payload-bearing DOL; `TestCaptureReplay_AddNameQuery` pins the 0x002C non-session form.

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

### OPEN/CREATE/SPOPNFIL reply's Mode byte was hardcoded to 0, silently disabling writes — confirmed against a live capture

**Spec:** `spec/etherdfs.txt`'s OPEN/CREATE/SPOPNFIL answer's last byte, `o`, is "access and open mode, as defined by INT 21h/AH=3Dh". The reference DOS client stores it straight into the file's SFT (`ETHERDFS.C`): `sftptr->open_mode &= 0xff00u; sftptr->open_mode |= answer[24];` — the SFT's `open_mode` low byte is DOS's own access-mode code (0=read-only, 1=write-only, 2=read/write) and gates whether the redirector will even attempt an `AL_WRITEFIL` through that handle. The reference Linux server derives `resopenmode` (this byte) differently per opcode: `AL_CREATE` hardcodes `2` (read/write); `AL_SPOPNFIL` echoes the request's MM (open-mode) word masked to 7 bits (`spopen_openmode & 0x7f`, "that's what PHANTOM.C does"); plain `AL_OPEN` echoes the request's SS word (`stackattr & 0xff`) — for `AL_OPEN` specifically, SS carries the caller's requested access mode, not a create-attribute (that's only true for `AL_CREATE`'s SS).

**Bug:** `handleOpen` built every `OpenReply` with `Mode: 0` unconditionally, regardless of opcode or the request's SS/MM words. A real DOS `COPY \ETHERDFS\COMMAND.COM` (destination on the same EtherDFS drive) sent an `AL_SPOPNFIL` (SS=0x0020 ARCH, CC=0x0112 "truncate if exists / create if missing", MM=0x0021) that succeeded (attr/FCB/size/fileid all correct, AX=0), but with `Mode=0` in the reply. DOS then treated the handle as read-only, closed it without ever issuing a single `AL_WRITEFIL`, and the subsequent `DELETE` of the empty destination file it had just created succeeded — i.e. the file "copy" silently produced a 0-byte file and every symptom looked like "writes are failing" even though `AL_WRITEFIL` itself was never reached or exercised.

**What we do:** `handleOpen` now derives `Mode` per opcode, matching the reference server: `2` for `AL_CREATE`; `r.OpenMode & 0x7f` for `AL_SPOPNFIL`; `r.Attr & 0xff` for plain `AL_OPEN`. Pinned by `TestOpenReplyModeByte` in `dispatch_test.go`, including the exact MM=0x0021 → Mode=0x21 case from the capture.

**Where:** `core/service/etherdfs/dispatch.go` (`handleOpen`).

### AL_OPEN/AL_CREATE/AL_SPOPNFIL request always carries the full SS/CC/MM 6-byte prefix, even when CC/MM are unused — confirmed against a live capture

**Spec:** `spec/etherdfs.txt`'s OPEN/CREATE/SPOPNFIL request is `SSCCMMfff...` — three FIXED 2-byte words (SS = stack attribute, CC = action code, MM = open mode; "CC/MM only relevant for SPOPNFIL") followed by the path. The reference Linux server reads the path unconditionally at body offset 6 for ALL THREE opcodes — `memcpy(fullpathname + offset, reqbuff + 6, reqbufflen - 6)` inside the single `(query == AL_OPEN) || (query == AL_CREATE) || (query == AL_SPOPNFIL)` branch — so the client always transmits all three words on the wire; CC/MM are simply sent as (and ignored as) zero for a plain AL_OPEN/AL_CREATE.

**Bug:** `DecodeOpenRequest` took a `hasAction bool` and consumed only 2 prefix bytes (SS) for AL_OPEN/AL_CREATE, treating the path as starting at body offset 2 instead of 6. The MM word's 2 bytes (and the tail of CC) then became a garbage prefix on the decoded path, so any AL_OPEN/AL_CREATE/AL_SPOPNFIL call resolved a corrupted path and failed to find the file (`ErrAccessDenied`/`ErrFileNotFound`) even when the target genuinely existed. **Confirmed by a live capture**: an AL_SPOPNFIL request for `\ETHERDFS\ETHERDFS.TXT` (SS=0000 CC=0101 MM=0000) got back a bare 60-byte reply with AX=`ErrAccessDenied` — SPOPNFIL already passed `hasAction=true` so this exact case decoded correctly by luck (3 words consumed), but the shared decoder's variable prefix length meant AL_OPEN/AL_CREATE (2-word decode) were broken.

**What we do:** `OpenRequest`/`DecodeOpenRequest` dropped `HasAction`/the `hasAction` parameter entirely — it always consumes a fixed 6-byte SS/CC/MM prefix (renamed the third field `OpenMode` to match MM) before the path, for AL_OPEN/AL_CREATE/AL_SPOPNFIL alike. Pinned by the captured-frame case in `TestDecodeRequests` (`frame_test.go`).

**Where:** `core/protocol/etherdfs/requests.go` (`OpenRequest`, `DecodeOpenRequest`), `core/service/etherdfs/dispatch.go` (`handleOpen`'s decode call site).

### Reply AX status lives at header offset 58-59, not as leading payload bytes — spec-conformance fix

**Spec:** `spec/etherdfs.txt` says a reply is `DOEEpppssccVS AA xxx...` — `AA` (the 16-bit AX register) sits at the SAME byte offset (58-59) a request's `D` (drive, 1 byte) and `L` (opcode, 1 byte) occupy, i.e. the reply header REPLACES those two bytes with the status word; `xxx` (the payload) starts at offset 60 and does not include the status. Confirmed against both reference implementations: the Linux server writes `ax = (uint16_t *)answ + 29` (word index 29 = byte 58, `answ` = a copy of the request's 60-byte header) and never prepends a status word to its `reslen`-counted payload (e.g. `AL_DISKSPACE`'s payload is 6 bytes: BX/CX/DX, no leading AX); the DOS client's `sendquery()` reads the reply's AX via `*replyax = (unsigned short *)(glob_pktdrv_recvbuff + 58)`.

**Bug (pre-fix):** the implementation never wrote anything to header offset 58-59 on a reply — `Frame.Reply`/`Encode` copied the request's `Drive`/`Opcode` straight through unchanged — and instead prepended the status word as the first 2 bytes of `Payload` (`StatusReply(status)`, `DiskSpaceReply.Status`). A real client reading AX from offset 58-59 therefore saw the stale request Drive/Opcode bytes (typically nonzero, since drive numbers are usually ≥2) instead of the actual status, so every reply — including the very first `AL_DISKSPACE` probe the reference client's auto-discovery (`etherdfs ::`) broadcasts — looked like a failure. This is why auto-discovery (and every other request) never worked end-to-end despite the framing round-tripping correctly in isolation.

**What we do:** `Frame` gained a `Status uint16` + `IsReply bool` field; `Frame.Reply(srcMAC, status, payload)` sets them, and `Encode` writes `Status` LE at offset 58-59 for a reply (vs `Drive`/`Opcode` for a request). Every `handleXxx` in `dispatch.go` returns `(status uint16, payload []byte)` instead of a single `[]byte` with an embedded status prefix. While auditing the other reply shapes against spec/the reference server for the same class of bug, `OpenReply` (AL_OPEN/AL_CREATE/AL_SPOPNFIL) turned out to conditionally omit its 2-byte CX/Action field (`HasAction`) for plain OPEN/CREATE, giving a 23-byte reply where the spec and reference server always send 25 (`reslen` writes `spopres`/CX unconditionally, just 0 when irrelevant) — fixed the same way (Action always encoded, `HasAction` removed). Pinned by `TestReplyStatusAtHeaderOffset` and the updated `TestReplyDTOEncodings` in `frame_test.go`. `DiskSpaceReply` also lost the `Status` field this pass added, but its correct shape needed a SECOND fix — see the next entry.

**Where:** `core/protocol/etherdfs/frame.go` (`Frame`, `Reply`, `Encode`), `core/protocol/etherdfs/replies.go` (`OpenReply`), `core/service/etherdfs/dispatch.go`, `core/port/etherdfs/etherdfs.go` (`Handler` signature).

### AL_DISKSPACE: AX is a DATA word (not ErrNone), payload is exactly 6 bytes — spec-conformance fix, confirmed against a live capture

**Spec:** `spec/etherdfs.txt`'s DISKSPACE answer is `BBCCDD` (3 words, BX/CX/DX, 6 bytes) with the note "The AX value is already handled in the protocol's header, no need to transmit it a second time here" — but AX itself is not a generic 0=success status for this one call. The reference Linux server sets `*ax = 1` unconditionally on success (`/* AX: media id (8 bits) | sectors per cluster (8 bits) -- MSDOS tolerates only 1 here! */`), `wansw[1] = 32768` (CX: bytes per sector, fixed), `wansw[0]`/`wansw[2]` = BX/DX (total/free 32KB clusters), `reslen = 6`. The reference DOS client's own call site is byte-length-strict: `if (sendquery(AL_DISKSPACE, glob_reqdrv, 0, &answer, &ax, 0) == 6) { glob_intregs.w.ax = *ax; /* sectors per cluster */ ... } else { FAILFLAG(2); }` — a reply of any OTHER length (or none at all) is indistinguishable from no reply. The auto-discovery path is even stricter: `sendquery(AL_DISKSPACE, i, 0, &answer, &ax, 1) != 6` prints `"No EtherDFS server found on the LAN"` verbatim.

**Bug (first-pass fix, still wrong):** the initial fix (previous entry) correctly moved AX to the header but kept `DiskSpaceReply` as 4 fields (SectorsPerCluster/BytesPerSector/TotalClusters/FreeClusters, 8 bytes) with status `ErrNone` (0) — plausible-looking (checksum valid, frame well-formed, AX=0 "success") but still wrong on the wire. **Confirmed by a live capture against a real client**: the reply's checksum and framing were byte-perfect, but the client silently discarded it (payload was 8 bytes, not 6) and, after its whole discovery attempt lapsed, displayed "No EtherDFS server found on the LAN (not for the requested drive at least)" — the exact string from a `sendquery(...) != 6` failure. This is why a wire-correct-looking reply (right checksum, right header layout, wrong payload SHAPE) can still make discovery fail outright; get the field count/AX semantics wrong for AL_DISKSPACE specifically and no length of framing correctness saves it.

**What we do:** `DiskSpaceReply` is now exactly `{TotalClusters, FreeClusters}` (BX, DX — CX is the fixed `diskSpaceBytesPerSector` constant, 32768, written unconditionally), `Encode` emits 6 bytes. `dispatch.go`'s `handleDiskSpace` returns the new `proto.DiskSpaceStatus` constant (`= 1`) as the status/AX, not `ErrNone`, and computes cluster counts in 32KB units (`bytesPerCluster = 32768`) to match the fixed one-sector-per-cluster encoding AX's high byte implies. Pinned by `TestAutoDiscoveryProbe` and `TestDiskSpace` in `dispatch_test.go` (payload len == 6, status == `DiskSpaceStatus`).

**Where:** `core/protocol/etherdfs/replies.go` (`DiskSpaceStatus`, `DiskSpaceReply`), `core/service/etherdfs/dispatch.go` (`handleDiskSpace`).

### Auto-discovery is an ordinary broadcast AL_DISKSPACE, not a dedicated opcode — spec-conformance fix

**Observed (reference DOS client, `ETHERDFS.C`):** `AL_INSTALLCHK` (0x00) is a DOS-side INT 2Fh "installation check" subfunction the client's TSR handles locally by chaining to the previous handler (`inthandler`: `if (... r.h.al == AL_INSTALLCHK ...) goto CHAINTOPREVHANDLER`) — it is NEVER sent over the wire. The reference Linux server correspondingly has no case for it in `process()`'s query dispatch (it falls through to `unknown query - ignore`). "Auto-discovery" (the client invoked with `::` as the server MAC) instead sets the destination MAC to broadcast (`FF:FF:FF:FF:FF:FF`), sends an ordinary `AL_DISKSPACE` query for the first drive being mapped (`sendquery(AL_DISKSPACE, i, 0, &answer, &ax, 1)`), and learns the server's real MAC from whichever reply's source MAC arrives (`updatermac` copies `glob_pktdrv_recvbuff+6` into `GLOB_RMAC`). The reference Linux server also unconditionally rejects drive numbers 0-1 (A:/B:) before even looking at the opcode (`reqdrv < 2 || reqdrv > 25` → silently drop, no reply) — our server answers those too (`ErrPathNotFound` rather than dropping), which is more permissive and does not break discovery.

**What we do:** there is no dedicated discovery opcode in `dispatch.go` — a normal drive lookup + `AL_DISKSPACE` handling is what makes discovery work, since the port already accepts broadcast-destined frames of any opcode (`addressedToUs`) and answers from its own MAC. `OpInstallChk` (0x00) is still accepted and answered (status 0 + the advertised server name) for tolerance with any client variant that might probe it, but it is not on the discovery path.

**Where:** `core/service/etherdfs/dispatch.go` (`dispatch` doc comment, the `OpInstallChk` case in `handle`); `core/port/etherdfs/etherdfs.go` (`addressedToUs`).

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

### AFP client: connecting to a real System 7.x Mac — four wire deviations (client)

**Observed (real Macintosh System 7.5.3 over LToUDP, 2026-07-23, `csfs -v` + loopback captures cross-referenced against `captures/vmac-to-vmac.pcapng`, a real Mac↔Mac AFP session):** the `client/` AFP stack could open no session to a genuine classic Mac — the ASP OpenSession's ATP transaction timed out — and after that was fixed the login was silently ignored, then dropped. Four independent deviations from a real Mac's behaviour, none exercised by the in-process e2e (our own server is stricter/looser in exactly the compensating ways):

1. **A single-packet ATP response may carry EOM CLEAR.** A real Mac answers a one-packet OpenSession/GetStatus/Command reply with control `0x80` (TRESP, **EOM not set**), the payload in the ATP UserData. Our requester only completed a transaction once it had seen an EOM packet, so the very first transaction hung forever. Fix: the transaction is also complete when every packet the request BITMAP asked for has arrived (a responder that fills the requested set need not set EOM). Our own server always sets EOM, so the e2e never caught it. `client/atalk/atp.go`.

2. **FPLogin must name a version/UAM the server ADVERTISED, verbatim.** The client hardcoded `"AFP2.2"` + `"Cleartxt Passwrd"`. System 7.5 offers `AFPVersion 1.1/2.0/2.1` (note the space, and NO 2.2) and `Cleartxt passwrd` (**lower-case p**). A classic Mac SILENTLY IGNORES an FPLogin whose version string or UAM name it never advertised. Fix: call FPGetSrvrInfo (ASPGetStatus) first, parse the version/UAM lists (`core/protocol/afp/srvrinfo.go` `ParseServerInfo`/`PickVersion`), and log in with the server's exact strings (`client/afp` `LoginNegotiated`).

3. **The FPLogin credential trailer was keyed on the capital-P constant.** `LoginRequest.Marshal` appended the username + 8-byte password only when `UAM == "Cleartxt Passwrd"` (exact match). Once we echoed the server's lower-case `"Cleartxt passwrd"`, the block carried the UAM with NO credentials, and the Mac discarded it. Fix: append the trailer for any non-guest UAM (`!= "No User Authent"`). `core/protocol/afp/commands.go`.

4. **The first ASP Command sequence number must be 0.** Ground truth (`captures/vmac-to-vmac.pcapng`): the real Mac workstation's first Command is sequence 0, then 1, 2, … A real Mac SERVER tracks the expected sequence and SILENTLY DROPS a Command whose sequence it did not expect. Our client's `nextSeq` pre-incremented, so the first Command was sequence 1 and every Command went unanswered (only the tickles flowed). Fix: the first Command/Write uses sequence 0. `client/asp/asp.go`.

With all four fixed, `csfs ls "afp://pete:@vmac1:*/System 7.5.3"` mounts and lists a real System 7.5 volume (data + resource-fork sizes + Finder type/creator). A server-root URI with no volume (`afp://server/`) now lists the server info + volumes via FPGetSrvrParms instead of failing FPOpenVol with an empty name (`client/afp/browse.go`).

**Where:** `client/atalk/atp.go`, `client/asp/asp.go`, `client/afp/{login.go,register.go,browse.go}`, `core/protocol/afp/{srvrinfo.go,commands.go}`; `csfs -v` wire-trace in `client/atalk/verbose.go`.

### SMB-over-NBIPX and SMB-over-NBF client transports + pcap duplicate frames — observation-based

**Context:** the client SDK (`client/smb`) grew two more SMB carriers beyond direct-hosted-IPX and TCP: **NBIPX** (NetBIOS-over-IPX / NWLink, the sequenced NB-IPX session on socket 0x0455) and **NBF** (raw NetBIOS-over-802.2 / NetBEUI). Both are the CALLER side of the responder engines in `core/service/netbios` (`nbipx.go` / `nbf.go`) and the LLC2 responder in `core/port/netbeui`; there was no prior client-direction implementation, so the caller flow is written to mirror the wire the responders expect (cross-checked against the same `captures/ipx.pcap` / `captures/netbeui.pcap` the server side was built from). Selected by `csfs -transport nbipx|nbf` (default `ipx` = direct-hosted); `client/link.Spec.Carrier` threads the choice.

**NBIPX caller (`client/smb/nbipx.go`).** The client opens the circuit with a DATA frame (`DataStreamType 0x06`) whose `DestConnID` is the unassigned sentinel `0xFFFF` and `SourceConnID` is its own circuit id, `SendSeq 0`, `ConnCtrlFlag = ACK|CONFIRM (0x41)`, payload `[called<20> || calling<00> || 6-byte trailer]` — exactly what `handleSessionRequest` keys on. The server's accept is `SYS|CONFIRM (0x81)`, `RecvSeq 1`, teaching the client the server node and its `SourceConnID`. SMB then rides sequenced DATA (client `SendSeq` from 1, server's first data frame `SendSeq 0`), reassembled by EOM. The client drops an out-of-window inbound frame (`SendSeq != recvSeq`), which is what prevents a duplicate from double-delivering.

**NBF caller (`client/smb/nbf.go`).** This is the CALLER half of **both** the LLC2 Type-2 machine and the NBF session layer, which the server splits between the port (LLC2 responder) and the engine (NBF session responder). Flow: broadcast `NAME_QUERY` for `SERVER<20>` carrying our Local Session No. (a CALL, Data2 low byte != 0) → `NAME_RECOGNIZED` (learns server MAC + its session number) → `SABME` (P=1) → `UA` → `SESSION_INITIALIZE` (I-frame) → `SESSION_CONFIRM` → SMB as `DATA_ONLY_LAST`/`DATA_FIRST_MIDDLE` I-frames. The client sequences its own I-frames (mod-128 N(S)/N(R)), acks the server's with RR, and implements no T1/checkpoint recovery (a lost frame surfaces as a Send timeout the caller retries — sufficient for a client).

**pcap delivers duplicate frames — the caller MUST dedup by sequence.** During NBF e2e bring-up every SMB response was delivered TWICE, shifting the response stream by one so the *next* command read the previous reply (surfacing as `smb: response shorter than command format requires` at the first command whose reply shape differed). The duplicate is a **pcap/NIC artifact** (a frame the capture surfaces more than once, or the server's LLC2 T1 checkpoint re-sending an I-frame whose RR ack it had not yet processed), NOT a protocol error — so the caller cannot assume one-delivery-per-frame. Fix (`handleFrame` I-frame branch): only an **in-order** I-frame (`N(S) == our N(R)`) advances N(R) and is delivered to the SMB layer; a frame whose `N(S)` we already consumed is re-acked with RR but **not** re-delivered. NBIPX's existing out-of-window drop already had this property. This is the SMB-over-NBF analogue of the NBIPX "discard + re-ack a duplicate without re-serving" rule the server side documents.

**Verbose tracing is now shared across every client transport via the server's `core/log` library.** The prior AppleTalk-only `-v` used an ad-hoc stderr printf (`client/atalk/verbose.go`); it now — with direct-IPX, NBIPX, NBF, NCP, and EtherDFS — narrates through one process-wide `core/log` stderr sink whose threshold a single `client/trace.SetVerbose` flips to `log.Trace`. Each transport holds a scope-named `core/log.Logger` (`trace.Logger("nbipx")` etc.), so `csfs -v` shows a uniform `scope [trace] …` narration of the whole connect (NBIPX SESSION_INITIALIZE, NBF NAME_QUERY/SABME, the transport-agnostic SMB NEGOTIATE/SESSION_SETUP/TREE_CONNECT) across all of them.

**Where:** `client/smb/{nbipx.go,nbf.go,register.go,ipx.go,session.go}`, `client/link/link.go` (`Spec.Carrier`), `cmd/csfs/{connect.go,main.go}` (`-transport`), `client/trace/trace.go` (shared `core/log` trace), `client/atalk/verbose.go` (ported onto `core/log`), `client/{ncp/session.go,etherdfs/session.go}` (trace). Coverage: `client/smb/{nbipx_e2e_test.go,nbf_e2e_test.go}` (whole SMB session over the REAL engines + real ports on an inmem Ethernet pair, forks + Finder metadata round-trip) and `client/smb/nbframing_test.go` (caller frame shapes).

### NBF caller: the LLC2 window needs an RR poll/final after UA, and SESSION_INITIALIZE must poll — real Win98 vs. our own server

**Context:** the SMB-over-NBF caller above establishes fine against our own responder e2e (inmem pair), but hung against a **real Windows 98 file server** (`WIN98-NBF`, `00:86:b0:a4:b8:81`): NAME_QUERY/RECOGNIZED, SABME/UA all completed, then `csfs -v` stopped at "SESSION_INITIALIZE (I-frame)" and timed out — Win98 never sent SESSION_CONFIRM. Our own server was too lenient to expose the gap.

**Observed (`captures/nt-98-nbf.pcap`, the real MS redirector `WINNT351-NBF` `00:00:d8:50:ae:d3` calling the SAME Win98 box, frames 204–214):**

```
204 NT→98  UI    NAME_QUERY  (CALL, Local Session 0x05) WIN98-NBF<20>
205 98→NT  UI    NAME_RECOGNIZED (Local Session 0xE0)
206 NT→98  SABME (P=1)
207 98→NT  UA    (F=1)
208 NT→98  RR    COMMAND, P=1, N(R)=0      ← caller polls immediately after UA
209 98→NT  RR    RESPONSE, F=1, N(R)=0     ← server's final answer opens the window
210 NT→98  I P   N(S)=0  SESSION_INITIALIZE (Data1 flags 0x8f, max-recv 1482)
211 98→NT  I P   N(S)=0  SESSION_CONFIRM   ← only NOW does Win98 confirm
212 NT→98  RR    F, N(R)=1
214 NT→98  I P   N(S)=1  DATA_ONLY_LAST → SMB Negotiate
```

Two faults in the caller, both invisible against our own server:

1. **No RR poll/final checkpoint after UA.** The MS caller sends an **RR command with P=1** (frame 208) and waits for the server's **RR response with F=1** (frame 209) BEFORE any I-frame. This is the LLC2 checkpoint that opens the send window; Win98 will not process the SESSION_INITIALIZE I-frame until it has completed. Our caller went straight from UA to the I-frame, which Win98 silently dropped.

2. **SESSION_INITIALIZE must set the Poll bit** (frame 210 is `I P`), and carry sane option flags. Win98 checkpoints on the poll and answers with its SESSION_CONFIRM I-frame; a non-poll INITIALIZE (our old `ctrl1 = N(R)<<1`, P=0) does not prompt the confirm. We now set P=1 and Data1 = Largest-Frame(7) | version-2.00 (`0x0F`; the redirector sends `0x8f`, but we omit SEND.NO.ACK to keep the conventional DATA_ACK contract this transport implements) and Data2 = our max-recv size.

**What we do:** `establish()` now runs SABME→UA, then `sendRRPoll` (RR command, P=1) waiting for the server's returning RR (S-frame addressed to us) on `rrCh`, then `sendSessionInitialize` as an I-frame with the Poll bit set, then waits for SESSION_CONFIRM. The read loop's S-frame branch, which previously dropped every RR, now signals `rrCh` for an RR addressed to us.

**Where:** `client/smb/nbf.go` (`establish` RR-poll phase, `sendRRPoll`, `sendIFramePoll`/`sendIFrameCtl` poll bit, `nbfInitFlags`/`nbfMaxRecvSize`, `handleFrame` S-frame → `rrCh`). The prior section's "SABME → UA → SESSION_INITIALIZE" flow description is superseded by the poll/final-plus-poll flow here.

### NBF caller: LLC2 ack/poll discipline — RR-final answers a poll, RR-command acks data, request DATA_ONLY_LAST polls

**Context:** with establishment fixed, SMB NEGOTIATE round-tripped against the real Win98 box but the next request hung. Two LLC2 faults, both invisible against our own lenient responder.

**Observed (`captures/nt-98-nbf.pcap`, WINNT351-NBF → WIN98-NBF, frames 214–266, plus live captures of our client → Win98):** the MS redirector's LLC2 discipline is precise about the P/F bit:
- It acknowledges the server's data by carrying N(R) on its own COMMAND frames (SSAP 0xF0, Poll clear) — the next request I-frame or an explicit DATA_ACK (0x14).
- It emits an **RR RESPONSE with Final set** (SSAP 0xF1) **only** to answer a server *poll* — an inbound RR-command with Poll set. Win98 polls (RR command, P=1) after acking a request and BLOCKS, refusing to send the reply until it receives the RR-final.
- It polls its own request DATA_ONLY_LAST (frame 214 = "I P") so the server checkpoints and flushes the reply.

Our caller had this backwards: it fired an unsolicited RR-response-Final after *every* inbound I-frame (an LLC2 protocol error — F=1 is valid only as a poll answer), never answered Win98's poll, and left its request DATA frames Poll-clear.

**What we do (`client/smb/nbf.go`):**
- `sendRR` — the plain data ack — is an RR **COMMAND, P=0** (`N(R)<<1`), never an unsolicited F=1.
- `sendRRFinal` — an RR **RESPONSE, F=1** — is emitted *only* to answer an inbound poll. The S-frame handler detects an RR-command-with-Poll and replies with it; `ackInbound(poll)` answers a Poll-set inbound I-frame with RR-final, else RR-command. So the F-bit is emitted iff it answers a poll — never unsolicited, never withheld when demanded.
- `sendSMB` sends the final DATA_ONLY_LAST with the LLC **Poll bit set** and the NBF `ACK_WITH_DATA_ALLOWED` flag (Data1 0x04), matching the redirector.

### NBF caller: DATA_ACK the server's Response Correlator — Win98 withholds the next reply until its response is acknowledged

**Context:** with the LLC2 discipline correct, NEGOTIATE and SESSION_SETUP were delivered and NBF-acked at the LLC layer, but Win98 still sent no SMB reply to SESSION_SETUP — only a bare NBF DATA_ACK. This was the true blocker, at the NBF *session* layer (not LLC2, not SMB content).

**Observed (`captures/nt-98-nbf.pcap` frames 216/217 and live):** Win98's NEGOTIATE response DATA frame carries a **non-zero NBF Response Correlator** (e.g. 0x28) and Flags 0x0c (ACK_INCLUDED | ACK_WITH_DATA_ALLOWED) — it is asking to be acknowledged. Win98 WITHHOLDS the reply to the *next* request until that response is acknowledged. The redirector piggybacks the ack as ACK_INCLUDED + Transmit Correlator on its next request; we never acknowledged it at all, so Win98 sat waiting forever.

**What we do (`client/smb/nbf.go`):** each request DATA_ONLY_LAST carries a non-zero, incrementing NBF **Response Correlator** (`respCorrelator`, 0x0001, 0x0002, …) so the server can correlate the reply; and when an inbound DATA response carries a non-zero Response Correlator, we send an NBF **DATA_ACK (0x14) whose Transmit Correlator echoes it** (`sendDataAck`, called from `handleData`) — the explicit equivalent of the redirector's piggybacked ACK_INCLUDED. With this, SESSION_SETUP is answered and the whole SMB handshake completes.

### SMB client: follow the server's NEGOTIATE (status dialect, SessionKey, header Flags, null-password) rather than assuming NT

**Observed (Win98 `00:86:b0:a4:b8:81` NEGOTIATE response):** Security Mode 0x02 (SHARE-level, encrypted challenge/response offered), Capabilities `0x00000203` — **no CAP_STATUS32, no CAP_UNICODE** — and a per-connection **SessionKey**. The MS redirector logs into this same box (`captures/nt-98-nbf.pcap` frame 217) with header **Flags 0x18** (canonicalized + case-insensitive) and **Flags2 DOS error codes** (NOT NT status), the **SessionKey echoed** from NEGOTIATE, **ANSI Password Length 1** (a lone `0x00` — the null password, not length 0), and a **non-empty Account**. Win98 answers Success (a null-password logon), proving no LM hashing is needed — a plaintext/null password is accepted despite the "encrypted" advertisement.

Our client had hard-set SMB_FLAGS2_NT_STATUS + CAP_STATUS32 (Win98 is a DOS-error server), SessionKey 0, a zero-length password, empty Account, and Flags 0x00. A Win9x server silently discards a request whose header claims a dialect it did not negotiate.

**What we do (all keyed off the NEGOTIATE reply):**
- `NegotiateResult` surfaces `Capabilities`, `SessionKey`, and `MaxBuffer`; `SupportsNTStatus()` reports CAP_STATUS32.
- `Builder.NTStatus` (from `SupportsNTStatus()`) gates the SMB_FLAGS2_NT_STATUS header bit AND CAP_STATUS32 in SESSION_SETUP.
- `Builder.SessionKey` is echoed in SESSION_SETUP; MaxBufferSize/MaxMpxCount follow the server (never exceed what it offered).
- The request header carries `FlagsRequest` (0x18) + Flags2 `Flags2EAS`.
- The case-insensitive password is always at least one NUL (length 1); the Account defaults to `GUEST` when none is given.

With these plus the two NBF sections above, the SMB-over-NBF client completes NEGOTIATE → SESSION_SETUP → TREE_CONNECT against real Win98 and lists a mounted share's contents.

### SMB client: RAP NetShareEnum MaxParameterCount must be the reply-param size (8), not the receive-buffer length

**Context:** the server-root browse (`smb://server/`, no share) connects the IPC$ pipe and runs a RAP NetShareEnum over `\PIPE\LANMAN`. The transaction completed but parsed **0 shares**.

**Observed (live):** our SMB_COM_TRANSACTION request set **MaxParameterCount = 65535** (the same large value as MaxDataCount). Win98 echoed `0xFFFF` back as the reply's TotalParameterCount and misframed the parameter/data split, so the SHARE_INFO_1 records never landed where the reply header pointed.

**What we do:** `BuildNetShareEnum` sets **MaxParameterCount = 8** (the RAP reply param block: Status + Converter + EntriesReturned + EntriesAvailable) and MaxDataCount = the receive-buffer length (the share records). Win98 then returns a correctly-framed reply; the client parses the SHARE_INFO_1 records (20-byte netname/type + remark-heap pointer, Converter-biased) into the share list. `csfs smb://win98-nbf,nbf/` now prints the server + its shares, each with a ready-to-paste URI, and `csfs ls smb://win98-nbf,nbf/C-DRIVE` lists the drive.

**Where:** `core/protocol/smb/smb.go` (`Cap*`, `Flag*`/`FlagsRequest`, `Flags2EAS`, `ShareType*`), `core/protocol/smb/client.go` (`NegotiateResult` fields + `SupportsNTStatus`, `Builder.{NTStatus,SessionKey}`, `flags2()`, `header()` Flags, `BuildSessionSetup`, `BuildTreeConnectIPC`, `BuildNetShareEnum`/`ParseNetShareEnum`), `client/smb/{session.go,browse.go}` (`establishSession`, `OpenIPC`, `EnumShares`, `Browse`), `client/smb/nbf.go` (`sendRR`/`sendRRFinal`/`ackInbound`, `respCorrelator`, `sendDataAck`), `cmd/csfs/{browse.go,main.go}` (SMB server-root listing). Coverage: `core/protocol/smb/netshareenum_test.go`.

### SMB client: TRANS2 FIND MaxDataCount must fit the server's MaxBufferSize; MaxParameterCount must not be 0/0xFFFF

**Context:** `csfs ls smb://win98-nbf,nbf/C-DRIVE/WINDOWS` returned only ~25 directories (of ~240 entries); `ls …/WINCD` failed with `smb: response shorter than command format requires` right after TREE_CONNECT.

**Observed (live Win98 File & Print over NBF, MaxBufferSize=2920):** FIND_FIRST2 with `MaxDataCount=0xFFFF` (and `MaxParameterCount=0`) produced a **multi-part** TRANS2 reply — `TotalDataCount` ≈ 25 KiB while `DataCount` ≈ 2854 (one MaxBuffer-sized fragment), with `EndOfSearch=1` and `SearchCount=242` on WINDOWS (search complete, data still fragmented) or `EndOfSearch=0` on WINCD. This client reads only the first fragment (no TRANS2 response reassembly). Consequences: (1) incomplete listings from the first fragment alone; (2) a follow-up FIND_NEXT2 collides with pending continuation frames and parses as `ErrShortResponse`.

**What we do:** after NEGOTIATE, clamp `Builder.MaxTransactBytes` to `MaxBufferSize − smbReplyOverhead` (same budget already applied to READ/WRITE), so each FIND fits one server message and the client pages via FIND_NEXT2. Set TRANS2 `MaxParameterCount = 32` (covers FIND_FIRST2's 10-byte reply params) — never 0 or 0xFFFF, matching the RAP NetShareEnum Win98 framing erratum above. FIND SearchAttributes also include ReadOnly|Archive (0x0037) so a strict attribute mask still returns ordinary files.

**Where:** `client/smb/session.go` (`establishSession` MaxTransactBytes clamp); `core/protocol/smb/clientfileops.go` (`buildTrans2` MaxParameterCount, `BuildFindFirst2` SearchAttributes).
