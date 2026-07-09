# Spec Errata

This document records places where ClassicStack's wire behavior intentionally differs from the published spec, because the spec contradicts what real clients actually require. Each entry cites the spec section we deviate from, the client behavior that drove the change, and the file/function where the deviation lives.

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
