# EtherDFS — The Ethernet DOS File System (raw-Ethernet drive server)

This document describes ClassicStack's EtherDFS file service: a server that lets a
DOS client (the EtherDFS TSR redirector) map a remote directory to a local drive
letter over **raw Ethernet frames** with the custom EtherType **`0xEDF5`** — no IP,
no TCP, no NetBIOS. It is the layer-2 analogue of the AFP / SMB / NCP services and
reuses the same storage seam (`core/fs` / `core/share`), config model (repeated
named sections), and 8.3 short-name engine.

> **Implementation source.** There is no internal protocol spec for EtherDFS; this
> is an observation / reference-driven implementation (CLAUDE.md #6). The frame
> layout, the `AL_*` function opcodes, the FCB / FAT-attribute conventions, and the
> BSD checksum below are taken from the EtherDFS protocol description
> (`spec/etherdfs.txt`) and the canonical open-source references — Mateusz Viste's
> original **etherdfs** client/server, **github.com/unterwulf/etherdfs**
> (`etherdfs.txt`), **github.com/BrianHoldsworth/etherdfs-server**, and
> E. Voirin's **github.com/oerg866/ethersrv-866**. Opcode values and framing here
> were checked against those servers and are attributed to them (CLAUDE.md #7).
> Where observed client behaviour differs it is noted here and in
> [errata.md](errata.md).

## Transport

EtherDFS rides Ethernet II directly: each request and reply is a single frame with
EtherType `0xEDF5`. There is no session, login, or connection — the protocol is
stateless request/response, and the server identifies a client only by its source
MAC address. The wire half is `core/port/etherdfs` (a `frameport.Port` that opens
the NIC link, demuxes the EtherType, and frames replies); the file-serving half is
`core/service/etherdfs`. The two are ONE component (`EtherDFS`): the service embeds
the port. There is no transport cross-wire because the framing is single-purpose.

A request is accepted only when its destination MAC is the server's own station
address or the Ethernet broadcast (`FF:FF:FF:FF:FF:FF`, used by `AL_INSTALLCHK`).

## Frame layout

All multi-byte fields are little-endian (the client is a real-mode x86 TSR).

| offset | field | meaning |
|--------|-------|---------|
| 0  | dst MAC (6) | server MAC, or broadcast for `AL_INSTALLCHK` |
| 6  | src MAC (6) | client MAC |
| 12 | EtherType (2) | `0xEDF5` |
| 14 | padding (38) | filler so a minimal frame meets the 46-byte Ethernet minimum |
| 52 | size (2) | total frame length (0 = "use the Ethernet length") |
| 54 | checksum (2) | 16-bit BSD checksum over `[56:size]`, present only if the CKS flag is set |
| 56 | version+CKS (1) | low 7 bits = protocol version (**2**); high bit = CKS flag |
| 57 | sequence (1) | client request sequence; echoed in the reply, used for retransmit dedup |
| 58 | drive (1) | low 5 bits = drive number (0 = A … 25 = Z) |
| 59 | opcode (1) | `AL_*` function |
| 60 | payload | per-opcode request/reply body |

A reply mirrors the header, **swaps the source/destination MACs**, preserves the
sequence/drive/opcode and the CKS preference, and carries the per-opcode reply body
(see below). When the request set the CKS flag, the reply's BSD checksum is computed
over `[56:size]`. `core/protocol/etherdfs/frame.go` implements `ParseFrame` /
`Frame.Encode` / `Frame.Reply`; `bsdsum.go` implements the checksum.

The leading 16 bits of most reply bodies are an **AX status word**: `0` = success,
otherwise an INT 21h DOS error code (`0x02` file-not-found, `0x03` path-not-found,
`0x05` access-denied, `0x12` no-more-files, …).

## Opcodes

| opcode | value | request body → reply body |
|--------|-------|---------------------------|
| `AL_INSTALLCHK` | `0x00` | (broadcast) → AX=0 + server name |
| `AL_RMDIR` | `0x01` | path → AX |
| `AL_MKDIR` | `0x03` | path → AX |
| `AL_CHDIR` | `0x05` | path → AX (validates the dir exists) |
| `AL_CLSFIL` | `0x06` | fileid(2) → AX=0 |
| `AL_CMMTFIL` | `0x07` | fileid(2) → AX=0 (flush) |
| `AL_READFIL` | `0x08` | off(4)+fileid(2)+len(2) → data |
| `AL_WRITEFIL` | `0x09` | off(4)+fileid(2)+data → written(2) |
| `AL_LOCKFIL` | `0x0A` | (no-op) → AX=0 |
| `AL_UNLOCKFIL` | `0x0B` | (no-op) → AX=0 |
| `AL_DISKSPACE` | `0x0C` | → status+spc+bps+total+free clusters |
| `AL_SETATTR` | `0x0E` | attr(1)+name → AX (best-effort; see errata) |
| `AL_GETATTR` | `0x0F` | name → time(4)+size(4)+attr(1) |
| `AL_RENAME` | `0x11` | srclen(1)+src+dst → AX |
| `AL_DELETE` | `0x13` | name → AX |
| `AL_OPEN` | `0x16` | attr(2)+name → attr+fcb(11)+time+size+fileid+mode |
| `AL_CREATE` | `0x17` | attr(2)+name → (create/truncate) same as OPEN |
| `AL_FINDFIRST` | `0x1B` | attr(1)+searchpath → attr+fcb(11)+time+size+dirid+pos |
| `AL_FINDNEXT` | `0x1C` | dirid(2)+pos(2)+attr(1)+fcbmask(11) → same as FINDFIRST |
| `AL_SKFMEND` | `0x21` | off(4, signed)+fileid(2) → newoff(4) |
| `AL_SPOPNFIL` | `0x2E` | attr(2)+action(2)+name → OPEN reply + action result |

The request/reply bodies are self-serialising DTOs (CLAUDE.md #10) in
`core/protocol/etherdfs/requests.go` and `replies.go`; the dispatch never
hand-slices bytes in a handler body.

## Names and attributes

A path arrives as a DOS path (backslash separators, possibly a leading drive
letter) and is normalised to the seam's `/`-separated, drive-less store path
(`NormalizePath`), then cleaned of `.`/`..` so a client cannot escape the drive
root. Directory listings report each entry's **8.3 short name** as an 11-byte FCB
(8 base + 3 extension, space-padded, upper-cased), derived by the share's
`name_engine = "short"` engine — the same derived-name engine SMB uses, so two
files whose long names collide on one 8.3 stem get distinct `NAME~1` / `NAME~2`
forms, persisted in the share metastore.

The FAT attribute byte is `1=RO 2=HID 4=SYS 8=VOL 16=DIR 32=ARCH`. The server
reports `DIR` for directories, `ARCH` for files, and adds `RO` when the drive is
read-only or the host file is not writable.

## Sequence dedup

EtherDFS runs over an unreliable layer-2 segment, so a client retransmits a request
(reusing its sequence number) when a reply is lost. The server keeps a one-entry
per-client reply cache keyed by the last handled sequence: a frame whose sequence
matches replays the cached reply rather than re-running the side effect. This makes
non-idempotent operations (WRITE / RENAME / DELETE / MKDIR) safe under retransmit.
`AL_INSTALLCHK` bypasses the cache (it is a stateless broadcast probe).

## File handles and sessions

`AL_OPEN` / `AL_CREATE` / `AL_SPOPNFIL` register an open `fs.File` in a per-client
table and return a 16-bit file ID the client passes to subsequent
READ/WRITE/SEEK/CLOSE. The client tracks the seek position itself (every READ/WRITE
carries an explicit offset), so the server holds none. Per-client state (open files,
find cursors, the reply cache) is keyed by the client MAC and reclaimed after an
idle timeout, since DOS clients never log off.

## Configuration

The singleton `[EtherDFS]` section carries the wire binding and the advertised
server name; repeated `[[EtherDFSDrives]]` sections map a DOS drive letter to a
backend, exactly like SMB shares:

```toml
[EtherDFS]
enabled    = true
iface      = "eth0"          # the NIC to bind (empty = shared bridge)
# mac      = "..."           # optional station MAC override (blank = NIC's own)
# server_name = "..."        # advertised in install checks (empty = Identity.hostname)

[[EtherDFSDrives]]
name        = "E"            # DOS drive letter
fs_type     = "local_fs"
path        = "/srv/dosfiles"
name_engine = "short"        # 8.3 names for DOS
read_only   = false
```

A blank `mac` (and blank interface `hw_address`) stamps the host NIC's hardware
address — required on WiFi. EtherDFS identifies the server by that MAC, so only
**one EtherDFS instance** can run per NIC.

A drive that backs the same host path as an AFP volume or SMB share shares the
§10d FS-mutation bus, so a file created over EtherDFS is visible over the others and
vice-versa.

## Limitations / errata

- **FAT attributes on a non-FAT host.** The shared FS seam does not model the FAT
  HID/SYS/ARCH bits, so `AL_SETATTR` is accepted as a no-op when the target exists
  (and `RO` is reported from host write permission). This matches the reference
  server's best-effort behaviour on non-FAT backends. See [errata.md](errata.md).
- **No authentication.** EtherDFS has no login; any client that can reach the
  server's MAC may use any configured drive (gated only by the drive's read-only
  flag and `allowed_users` allow-list). This is the intentional compatibility
  weakness, matching the original ethersrv.
- **One station MAC / one server instance.** The server accepts frames addressed
  to its station MAC (or Ethernet broadcast). Blank `mac` uses the host NIC so
  WiFi APs accept replies; only one EtherDFS server can run per NIC.
