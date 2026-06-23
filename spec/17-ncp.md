# NCP — NetWare Core Protocol File Service (NetWare 3.x bindery emulation)

This document describes ClassicStack's NCP file service: a NetWare 3.x–style
server that lets NETx / VLM / Client32 (DOS, Windows 3.x/9x), Mac (MacIPX), and
OS/2 NetWare requesters attach and use shares over IPX. It is the NetWare analogue
of the AFP and SMB services and reuses the same storage seam (`core/fs` /
`core/share`), config model (repeated named sections), and auth seam
(`core/auth`).

> **Implementation source.** There is no internal protocol spec for NCP; this is an
> observation/reference-driven implementation (CLAUDE.md #6). The wire formats and
> function/subfunction codes below are taken from the openly documented Novell NCP
> and the canonical open-source references — **mars_nwe** (Martin Stover) and the
> Linux kernel **ncpfs**/**ipx** (Volker Lendecke et al). The
> github.com/davidrg/mars_nwe fork was used to confirm the DTOs and codes: the NCP
> request/reply headers, function/subfunction codes, file-handle framing, SAP entry
> layout, and the bindery/login/server-info reply structures here were checked
> field-for-field against mars_nwe `src/nwconn.c`, `src/nwbind.c`, and
> `include/net.h`. Constants and framing are attributed to those works (CLAUDE.md
> #7). Where observed client behaviour differs it is noted here and in
> [errata.md](errata.md).

## Transport

NCP rides **IPX** (`core/protocol/ipx`, `core/router/ipx`), connectionless: one IPX
datagram carries one whole NCP request or reply (no reassembly), exactly like the
SMB direct-hosted-over-IPX transport it is modelled on.

| Service | IPX socket | IPX type |
|---|---|---|
| NCP file service | `0x0451` | `0x11` (NCP); type `0` also accepted |
| SAP (advertising/queries) | `0x0452` | `0x04` (PEP) |

NCP-over-IP (port 524) is **out of scope** for this milestone.

The transport (`core/service/ncp/overipx.go`) is registered on the IPX mini-router
as the `SocketHandler` for `0x0451` during compose cross-wiring
(`compose/runtime/transports.go::wireIPX`); the SAP advertiser is registered on
`0x0452`. Both reach the wire only through a local `IPXSender` seam (the mini-router
satisfies it), so the service never imports the router or a port.

## NCP request/reply framing

All multi-byte header fields are **big-endian**.

**Request header** (6 bytes), then the function code and arguments:

| Field | Size | Notes |
|---|---|---|
| Request type | 2 | `0x1111` create-connection, `0x2222` request, `0x5555` destroy-connection, `0x7777` burst (rejected) |
| Sequence number | 1 | per-connection; echoed in the reply |
| Connection low | 1 | connection number low byte |
| Task number | 1 | client task; echoed |
| Connection high | 1 | connection number high byte |
| Function | 1 | NCP function code (only for `0x2222`) |
| Arguments | … | function-specific |

**Reply header** (8 bytes), then the function-specific body:

| Field | Size | Notes |
|---|---|---|
| Reply type | 2 | `0x3333` |
| Sequence number | 1 | echoed |
| Connection low | 1 | assigned/echoed |
| Task number | 1 | echoed |
| Connection high | 1 | |
| Completion code | 1 | `0x00` success; see codes below |
| Connection status | 1 | `0x00` good, `0x40` down |

Completion codes implemented: `0x00` success, `0x7C` not-logged-in, `0x8C`
access-denied, `0x96` no-such-object, `0x9B`/`0x9C` invalid connection / no-more-
files, `0xFB` function-not-supported, `0xFF` no-such-file.

## Connections

A NetWare server assigns each client a numbered **service connection** (1..250) on
its create-connection request; the number is carried (split low/high) in every
subsequent header and identifies the per-client state: logged-in identity, open
directory handles, and open file handles. Connections are keyed by the client's IPX
endpoint (network+node) so a retransmitted create-connection reuses the slot.
Absent SPX (NetWare's watchdog), idle connections are reaped after 15 minutes.

## Function codes implemented

Codes verified against **mars_nwe** `src/nwconn.c` (file/dir dispatch) and
`src/nwbind.c` (bindery/login/server-info):

| Function | Name | Action |
|---|---|---|
| `0x14` | Get File Server Date/Time | server clock (year since 1900, mon 1-12, day, hr, min, sec, dow 0=Sun) |
| `0x16` | Dir-handle / Volume Services (mux) | see subfunctions |
| `0x17` | Connection/Bindery Services (mux) | see subfunctions |
| `0x18`/`0x19` | End Of Job / Logout | clears login identity |
| `0x3E`/`0x3F` | File Search Init/Continue | directory scan (dir_id + searchsequence model) |
| `0x41` | Open File For Read | allocates an open-file handle |
| `0x42` | Close File | close seam handle |
| `0x43`/`0x4D` | Create File (overwrite / new) | allocates an open-file handle |
| `0x44` | Erase File | `FS().Remove` + DeleteMetadata |
| `0x45` | Rename File | `FS().Rename` + MoveMetadata (NOTE: `0x46` is *set-attributes*, not rename) |
| `0x47` | Get File Size | seek-to-end size |
| `0x48`/`0x49` | Read / Write File | offset+length over the seam |
| `0x4C` | Open File | allocates an open-file handle |

`0x16` subfunctions (dir-handle / volume): `0x12`/`0x13`/`0x16` Allocate Directory
Handle (permanent/temp/special-temp), `0x14` Deallocate Directory Handle, `0x15`
Get Volume Info with Handle.

`0x17` subfunctions: `0x11` Get File Server Information, `0x14` Login (cleartext),
`0x15` Get logged identity, `0x17` Get login key, `0x18` Keyed login, `0x46` Get
Bindery Access Level. (In mars_nwe these are handled by the separate `nwbind`
bindery process; we handle them inline.)

**Wire layouts** (from mars_nwe, big-endian):
- *Open/create reply* = `ext_fhandle[2]=0, fhandle[4], reserved[2]=0, …` — the
  6-byte `ext+fhandle` prefix is the file handle the client echoes, preceded by a
  filler byte, on read/write/close.
- *Read/Write/GetSize/Close args* = `filler(1), ext_fhandle[2], fhandle[4], …`.
- *Read reply* = `size[2]` then data, with a leading pad byte when the read offset
  is odd (mars_nwe `zusatz`).
- *Get File Server Info reply* matches the mars_nwe XDATA exactly
  (servername[48], version, subversion, maxconnections[2], connection_in_use[2],
  max_volumes[2], os_revision, sft_level, tts_level, peak_connection[2], six
  version bytes, security_level, internet_bridge_version, reserved[60]).
- *Get Bindery Access reply* = `access_level(1) + object_id[4]` (0xFFFFFFFF when not
  logged in; 0x33 supervisor / 0x22 user).
- *Login (cleartext)* args = `object_type[2], name_len, name, pw_len, pw`.
- *Keyed login* args = `crypt_key[8], object_type[2], name_len, name`.

Unimplemented functions (set-attributes `0x46`, byte-range locking, TTS `0x22`,
queue/print, accounting, burst mode) answer completion `0xFB`
(function-not-supported) rather than dropping silently.

Multiplexed functions (`0x16`/`0x17`) frame their body as a 2-byte big-endian
subfunction-length, then the subfunction byte, then its arguments (the subfunction
is at `requestdata+2` in mars_nwe).

## Login / bindery

NCP login maps onto the shared `core/auth` seam (the same user store AFP/SMB use):

- **Cleartext login** (`0x17/0x14`): validated directly against the
  `Authenticator` when one is wired; a world-open server with no store wired grants
  a guest login (the compatibility-server default).
- **Keyed (encrypted) login** (`0x17/0x18`): the documented NetWare challenge-
  response. We cannot reverse the client's shuffled hash to a cleartext password to
  feed `Authenticate`, so a keyed login is **accepted as a guest-equivalent login**
  bound to the supplied object name (mirrors SMB's "hashed-credential accept-as-
  guest" — see [errata.md](errata.md)). A future slice that stores the NetWare-
  hashed credential can validate the shuffle exactly.

A per-volume `allowed_users` allow-list then gates which volumes the identity may
use (login-time gating, consistent with AFP/SMB).

## SAP advertising

So NETx/VLM discover the server without a preferred-server binding, the service
advertises via **SAP** on socket `0x0452`:

- a periodic unsolicited **General Service Response** (every 60 s) carrying the
  file-server entry (type `0x0004`, the server name, the server's IPX net/node, and
  NCP socket `0x0451`), and
- answers to **Nearest Service** (`0x03`) and **General Service** (`0x01`) queries
  for the file-server type.

The advertised IPX identity is the mini-router's configured network + node.

## Name spaces (long filenames — function 0x57)

Beyond DOS 8.3, the server serves the **OS/2** and **Macintosh** name spaces (long
filenames) via NCP **function `0x57`** (verified against mars_nwe `src/namspace.c`).
Get-Name-Spaces-Loaded advertises `DOS, OS2, MAC`; NFS/FTAM are not served.

> Dispatch quirk: for function `0x57` the subfunction byte is the **first**
> request-data byte (`requestdata[0]`), not behind a 2-byte length prefix as for the
> `0x16`/`0x17` multiplexed functions.

Subfunctions implemented:

| sub | call |
|---|---|
| `0x18` | Get Name Spaces Loaded → `count[2 LE]` + id bytes (`DOS,OS2,MAC`) |
| `0x16` | Generate Dir Base and Volume Number → `ns_base[4] + dos_base[4] + volume` |
| `0x02` | Initialize Search → `volume + base[4] + sequence[4]=0xFFFFFFFF` |
| `0x03` | Search for File or Dir → `next_seq[4]` + info-mask-selected entry |
| `0x06` | Obtain File or Subdir Info → info-mask-selected entry |
| `0x01` | Open/Create File or Subdir → `handle[6] + action + pad` + entry |

**Name spaces (`namspace.h` ids):** `DOS 0, MAC 1, NFS 2, FTAM 3, OS2 4`. Names are
carried/returned in the request's name space, rendered via the **shared AFP/SMB
filename codec + name engine** (`core/fs`): DOS → 8.3 upper-case; MAC → 31-char
MediumName in MacRoman; OS2 → store-native long name in OEM/ANSI; (NFS → UTF-8,
case-sensitive — not advertised).

**Paths** use `NW_HPATH` = `volume(1), base[4], flag(1), components(1), pathes[]`
(each component a length-prefixed Pascal string). `flag`: `0`=anchor on a 1-byte
DOS dir handle (low byte of base), `1`=anchor on a 4-byte name-space dir base,
`0xFF`=neither. The service keeps a separate 4-byte dir-base table per connection
(`AllocBase`), distinct from the 1-byte DOS dir handles.

**Info-mask** (`INFO_MSK_*`, 0x01..0x800): a get-info/search request selects which
sections the reply entry carries; the reply appends them in ascending bit order
(matching mars_nwe `build_dir_info`), with the entry name (`INFO_MSK_ENTRY_NAME`)
appended last as a length-prefixed field. Reply fields are **little-endian** (unlike
the big-endian NCP header).

**Long names are stored natively** on the backend (no shadow DB), exactly as
mars_nwe does, reusing what `local_fs` already keeps on disk.

### Case-insensitivity with native long names

The legacy contract is case-insensitive matching (DOS/OS2/MAC); only NFS is
case-sensitive. Native long-name lookup would break this on a **case-sensitive host**
(Linux/ext4): an open of `REPORT.TXT` would miss a stored `Report.txt`. The
name-space handlers resolve through **`fs.ResolveFold`** (a shared `core/fs` helper):
it tries the exact path first, and on a miss folds each component by scanning its
parent directory for a case-insensitive (`EqualFold`) match — the protocol-neutral
equivalent of mars_nwe's `VOL_OPTION_IGNCASE`. The fold runs for DOS/OS2/MAC and is
skipped for NFS. On a case-insensitive host (NTFS/APFS) the exact `Stat` succeeds
first, so the scan is a Linux-only slow path. The filename **codec** handles charset
(MacRoman/ANSI↔store); the **fold** handles case — neither relies on the host FS's
own case rules.

## Assumptions / deviations

- **Single IPX segment.** RIP (`0x0453`) is not implemented; clients reach the
  server on the local segment (network 0 / the configured IPX network). Multi-
  segment IPX routing is out of scope.
- **No SPX.** There is no SPX watchdog; idle connections are aged on inactivity.
- **Server name** comes from `[identity].hostname`, upper-cased to a NetWare name;
  default `OMNITALK`.
- Wire deviations observed against real clients are recorded in
  [errata.md](errata.md).
