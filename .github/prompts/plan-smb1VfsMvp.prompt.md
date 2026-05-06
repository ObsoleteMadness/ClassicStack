# Plan: SMB1 VFS MVP for DOS and Win9x

Build a dependency-ordered SMB1 server MVP on your existing NetBIOS stack and VFS registry, targeting MS-DOS + Windows 95/98/ME, with guest auth first. The approach is to convert the current browser-focused SMB stub into a stateful file server (UID/TID/FID/search/lock lifecycle), implement enumeration first, then layer read/write/locking/path operations, and implement copy as read+write fallback.

## Steps

### Phase 0: Baseline and guardrails
Confirm build-tag/runtime paths for smb + netbios + netbeui + ipx. Preserve existing browser behavior while adding file-serving commands. Add a short command support matrix (supported vs deferred) for SMB1 scope.

### Phase 1: Session and share state foundation *(blocks all later phases)*
Add per-session state keyed by session context, with UID allocation, TID map per tree connect, FID map per open file, and search-handle map for enumeration continuation.

### Phase 1: Share binding to VFS *(depends on session state)*
Instantiate one VFS backend per share at service start via `vfs.New(share.FSType, vfs.Params{Name, Path, ReadOnly})`, validate share roots, and enforce share read-only behavior at command handlers.

### Phase 2: Command decode/encode scaffold *(depends on session state)*
Keep the existing dispatcher shape, but move command logic into dedicated handlers for incremental implementation and testability. Add a small error-mapping helper early (os/vfs errors → SMB status codes) to keep handlers consistent and testable.

### Phase 3: Share enumeration MVP *(depends on phases 1–2)*
Keep RAP `NetServerEnum2` path working and harden it for stable output and stricter request validation for legacy clients. Ensure it enumerates configured SMB server identity plus observed browser servers.

### Phase 4: Directory and file enumeration MVP *(depends on phases 1–2)*
Implement `SMB_COM_SEARCH` first (DOS/Win9x priority), then add `SMB_COM_TRANSACTION2` support for `TRANS2_FIND_FIRST2` and `TRANS2_FIND_NEXT2`. Use VFS `ReadDir` + `Stat`, wildcard matching, DOS attribute filtering, and optional shortname mapping when configured.

### Phase 4: Search state and paging *(depends on enumeration)*
Add server-side enumeration handles, pagination/cursor continuation, timeout/cleanup, and `FIND_CLOSE2` handling where applicable.

### Phase 5: Open/read/write/close core I/O *(depends on phases 1–2)*
Implement open path plus `READ_ANDX`, `WRITE_ANDX`, and `CLOSE` via VFS `OpenFile`, `ReadAt`, `WriteAt`, `Close`, with I/O size bounded by negotiated buffer size and robust SMB status-code mapping.

### Phase 6: File locking *(depends on phase 5)*
Implement SMB-local byte-range lock table keyed by file identity + range + owner (session/FID), with overlap conflict detection and automatic release on close/session end.

### Phase 7: Rename/move/delete for files and directories *(depends on phases 1–2 and 5)*
Implement rename/move via VFS `Rename` and delete semantics via VFS `Remove` with SMB-compatible precondition checks for both files and directories.

### Phase 8: Copy fallback *(depends on phase 5)*
Implement copy as server-side read+write loop for MVP (same-share first), defer full SMB `COPY` command semantics unless client traces require it.

### Phase 9: Transport hardening *(parallel with late phases)*
Verify parity across NetBIOS over NetBEUI, NetBIOS over IPX, and SMB direct IPX socket 0x0550. Ensure reply routing uses contextual session/datagram endpoints correctly and does not regress browser datagram handling.

### Phase 10: Observability and cleanup *(parallel with phase 9)*
Add targeted debug logging and lifecycle counters for UID/TID/FID/search/lock events and session teardown cleanup.

---

## Relevant files

- `service/smb/server.go` — primary SMB command dispatcher; add state tables, per-command handlers, and lifecycle cleanup
- `service/smb/constants.go` — verify/add command and flag constants used by implemented SMB1 requests
- `service/smb/share.go` — share config contract consumed by VFS instantiation logic
- `service/smb/server_test.go` — extend with SMB session/enum/file-op tests, including transport-context cases
- `pkg/vfs/vfs.go` — leverage `FileSystem` and `File` interfaces directly for SMB backend operations
- `pkg/vfs/local_fs.go` — expected initial backend behavior for local share paths
- `service/netbios/service.go` — contextual session/datagram handling and routing constraints
- `service/smb/over_ipx_direct/transport.go` — SMB direct IPX transport path for socket 0x0550
- `cmd/classicstack/smb_enabled.go` — SMB wiring into NetBIOS and direct IPX transport
- `cmd/classicstack/netbios_enabled.go` — NetBIOS transport selection for tcp, netbeui, ipx
- `spec/ms-smb.md` — normative SMB1 behavior reference for command semantics and status handling

---

## Decisions

| Decision | Choice |
|---|---|
| Client priority | Windows 95/98/ME and MS-DOS |
| Transport priority | SMB direct IPX 0x0550 and NetBIOS over NetBEUI/IPX |
| Auth mode | Guest-only for MVP |
| Operation order | Dependency-driven |
| Path ops scope | Files and directories in MVP |
| Copy mode | Read+write fallback for MVP |

---

## Verification

1. Expand SMB unit tests for each phase: session lifecycle, TID/FID allocation, search pagination, read/write boundaries, lock conflicts, rename/delete/copy outcomes.
2. Add temp-dir-backed tests for `local_fs` share behavior and read-only enforcement.
3. Add transport-level integration checks for netbeui + ipx + direct IPX 0x0550 using the same SMB request sequences.
4. Run manual DOS/Win9x smoke: browse shares, enumerate dirs/files, read/write, lock behavior, rename/move/delete/copy.
5. Re-run existing browser tests to ensure election/announcement/NetServerEnum2 behavior remains intact.

---

## Further Considerations

1. Prefer `SMB_COM_SEARCH` before full TRANS2 to maximise DOS/Win9x compatibility and reduce initial parser complexity.
2. Keep command parsing incremental in `service/smb/server.go` first; split into separate request/response files only after behaviour stabilises to avoid churn.
3. Add the error-mapping helper (`os/vfs error → SMB NTSTATUS`) early so all handlers share consistent status codes from the start.
4. Use `SessionContext.SourceConnID` as the session key when non-zero; fall back to the remote endpoint tuple for transports that don't provide it (e.g. direct IPX 0x0550).
5. Prefer `SMB_COM_SEARCH` first before full TRANS2 to maximize DOS/Win9x compatibility and reduce initial parser complexity.
