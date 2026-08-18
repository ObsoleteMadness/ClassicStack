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

**Seeded LOGIN handle:** on create-connection, directory handle **1** is
pre-bound to the first volume's `LOGIN` directory (the volume root when none
exists) — mars_nwe `nw_init_connect` seeds `dirs[0]` to volume 0's `LOGIN/`
identically. DOS requesters use handle 1 (`SYS:LOGIN`, where LOGIN.EXE lives)
without ever allocating it: the first thing a requester does after attach is
`Get Directory Path(handle 1)` (observed in ipx.pcap frame 122). `AllocDir`
never hands out id 1.

## Function codes implemented

Codes verified against **mars_nwe** `src/nwconn.c` (file/dir dispatch) and
`src/nwbind.c` (bindery/login/server-info):

| Function | Name | Action |
|---|---|---|
| `0x03`–`0x0E` | Log/Lock/Release/Clear (files, logical records) | granted unconditionally — no cross-connection lock manager (compatibility posture); `0x04`/`0x0C` stay `0xFB` like mars_nwe |
| `0x12` | Get Volume Info with Number | same body as `0x16/0x15`, selected by volume number |
| `0x13` | Get Station Number | 1-byte connection number (mars_nwe shape) |
| `0x14` | Get File Server Date/Time | server clock (year since 1900, mon 1-12, day, hr, min, sec, dow 0=Sun) |
| `0x16` | Dir-handle / Volume Services (mux) | see subfunctions |
| `0x17` | Connection/Bindery Services (mux) | see subfunctions |
| `0x18`/`0x19` | End Of Job / Logout | clears login identity |
| `0x1A`/`0x1E`/`0x1F` | Log/Clear Physical Record (Set) | granted unconditionally (as above) |
| `0x21` | Negotiate Buffer Size | accepted size (2 BE) = min(1024, proposed); proposals < 512 ignored |
| `0x22` | TTS family | subfn 0 ("TTS available?") succeeds = no transaction tracking; other subfns `0xFB` (mars_nwe) |
| `0x23` | AFP-namespace family | answered `0xBF` invalid-name-space (mars_nwe) — the client falls back to DOS calls |
| `0x3B`/`0x3D` | Commit File | `File.Sync()` |
| `0x3E`/`0x3F` | File Search Init/Continue | directory scan (dir_id + searchsequence model); entry = NW_FILE_INFO / NW_DIR_INFO |
| `0x40` | Search for a File | FCB-era one-call-per-entry search (DOS `DIR`); same entry shapes |
| `0x41` | Open File For Read | allocates an open-file handle |
| `0x42` | Close File | close seam handle |
| `0x43`/`0x4D` | Create File (overwrite / new) | allocates an open-file handle |
| `0x44` | Erase File | `FS().Remove` + DeleteMetadata |
| `0x45` | Rename File | `FS().Rename` + MoveMetadata |
| `0x46` | Set File Attributes | target validated; DOS attribute bits accepted and discarded (the seam stores none) |
| `0x47` | Get File Size | seek-to-end size |
| `0x48`/`0x49` | Read / Write File | offset+length over the seam |
| `0x4C` | Open File | allocates an open-file handle |

`0x16` subfunctions (dir-handle / volume): `0x00` Set Directory Handle, `0x01`
Get Directory Path, `0x02` Scan Directory Information, `0x03` Get Effective
Directory Rights, `0x05` Get Volume Number, `0x06` Get Volume Name,
`0x0A`/`0x0B`/`0x0F` Create/Delete/Rename Directory, `0x12`/`0x13`/`0x16`
Allocate Directory Handle (permanent/temp/special-temp), `0x14` Deallocate
Directory Handle, `0x15` Get Volume Info with Handle, `0x19` Set Directory
Information (target validated, metadata discarded), `0x20` Scan Volume User Disk
Restrictions (always zero entries), `0x2C` Get Volume and Purge Information,
`0x2D` Get Directory Information.

`0x17` subfunctions: `0x11` Get File Server Information, `0x13`/`0x1A` Get
Connection Internet Address (old/new), `0x14` Login (cleartext), `0x15`/`0x1B`
Get Object Connection List (old/new), `0x16`/`0x1C` Get Connection Information
(old/new — Wireshark labels the old form "Get Station's Logged Info"), `0x17`
Get login key, `0x18` Keyed login, `0x35` Get Bindery Object ID, `0x36` Get
Bindery Object Name, `0x37` Scan Bindery Object, `0x46` Get Bindery Access
Level. (In mars_nwe these are handled by the separate `nwbind` bindery process;
we handle them inline.) The Windows 9x NetWare client issues Get Connection
Information about its **own** connection right after the login verb and treats
any failure as "station not logged in" — answering it `0xFB` blocks the login
even though the login itself succeeded.

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
- *Get Connection Information (`0x17/0x16` old / `0x17/0x1C` new)* args = the
  target connection number — 1 byte (old) or 4 bytes **little-endian** (new;
  mars_nwe `GET_32`). Reply (mars_nwe `struct XDATA`, 62 bytes) =
  `object_id[4 BE], object_type[2 BE], object_name[48]` (NUL-padded upper),
  `login_time[7]` = year-1900, month 1-12, day, hour, minute, second, weekday
  (0 = Sunday) — `struct tm` fields verbatim (mars_nwe `get_login_time`) —
  `reserved(1)`. Number out of range → `0xFD` (bad station); an in-range
  connection that is not live or not logged in answers **success with an
  all-zero struct**.
- *Get Connection Internet Address (`0x17/0x13` old / `0x17/0x1A` new)* args =
  the same 1-byte / 4-byte-LE connection number; reply = the connection's IPX
  address `network[4], node[6], socket[2]`; the new form appends
  `connection_type(1) = 0x02` (NCP). Any miss → `0xFF`.
- *Get Object Connection List (`0x17/0x15` old / `0x17/0x1B` new)* args =
  (new only: `search_offset[4 BE]`, resume after that connection number),
  `object_type[2 BE], name_len, name`; reply = `count(1)` then the connection
  numbers the object is logged in on — 1 byte each (old) or 2 bytes **LO-HI**
  (new; mars_nwe `U16_TO_16`). Name miss → `0xFC`.
- *Get Bindery Object ID (`0x17/0x35`)* args = `object_type[2], name_len, name`
  (no wildcards); reply = `object_id[4], object_type[2], object_name[48]`
  (NUL-padded). Miss → completion `0xFC` (no such object).
- *Get Bindery Object Name (`0x17/0x36`)* args = `object_id[4]`; reply as above.
- *Scan Bindery Object (`0x17/0x37`)* args = `last_object_id[4]` (`0xFFFFFFFF`
  starts the scan), `object_type[2]` (`0xFFFF` wildcard), `name_len, name`
  (`*`/`?` wildcards); reply = the get-ID shape plus `object_flag(1),
  object_security(1), object_has_properties(1)`. Returns the first match after
  `last_object_id` in bindery order; scan end → `0xFC`.
- *Search entry info* (`0x3F`/`0x40` replies; mars_nwe `connect.h`): a file is
  **NW_FILE_INFO** = `name[14]` (upper 8.3, NUL-padded), `attrib LO-HI[2]`
  (little-endian pair: `0x20` archive, `|0x01` on a read-only volume), `size[4 BE]`,
  `create_date[2 BE]`, `access_date[2 BE]`, `modify_date[2 BE]`, `modify_time[2 BE]`;
  a directory is **NW_DIR_INFO** = `name[14]`, `attrib[2]` (`0x10`),
  `create_date[2]+create_time[2]`, `owner_id[4]=0`, `access_rights_mask(1)=0`,
  `reserved(1)`, `next_search[2]=0` (mars_nwe zeroes those three). Dates are DOS
  words `(year-1980)<<9|month<<5|day`, times `hour<<11|min<<5|sec/2`, big-endian.
  The search-attribute's `0x10` bit selects directories vs files (mars_nwe
  `func_search_entry`). **Pattern encoding** (observed in ipx.pcap; mars_nwe
  `x_str_match`): requesters send wildcards as high-bit metacharacters —
  `0xAA` = `*`, `0xBF` = `?`, `0xAE` = `.` — so a `DIR` of `*.*` is the bytes
  `AA AE AA` on the wire; `0xFF` prefix bytes are dropped, and the ASCII forms
  are accepted too. Matching is FCB-style: base and extension match
  independently (`*.*` matches an extension-less name) and `?` matches **one or
  zero** characters (`????????.???` matches `FOO.TXT`). `0x3F`/`0x40` scan end
  → `0xFF`.
- *Allocate Directory Handle (`0x16/0x12`/`0x13`/`0x16`)* args = `source
  dir_handle(1), drive_letter(1), pathlen(1), path` — the path is
  LENGTH-PREFIXED and may be `VOL:`-qualified, relative to the source handle, or
  **empty** (= the source handle's own directory; requesters allocate a
  zero-length-path temp handle when mapping the current directory). Reply =
  `new_handle(1), effective_rights_mask(1)`.
- *Get Directory Path (`0x16/0x01`)* arg = dir handle; reply = len-prefixed
  upper-case `VOL:path`, no trailing slash. Unknown handle → `0x9B`.
- *Scan Directory Information (`0x16/0x02`)* args = `dir_handle(1),
  subdir_number[2 BE]` (1-based, first call 1), `len, path`; reply =
  `subdir_name[16]`, `create_date[2]+create_time[2]`, `owner_id[4]=0`,
  `inherited_rights(1), reserved(1), subdir_number[2]` echoed. Past the last
  subdirectory → `0x9C`.
- *Get Volume Name (`0x16/0x06`)* arg = volume number; reply = len-prefixed
  upper-case name. A number in 0..31 with no volume bound answers **success with
  an empty name** (mars_nwe `nw_get_volume_name` — clients scan the whole range
  building their volume table); only ≥ 32 is completion `0x98`.
- *Get Volume and Purge Info (`0x16/0x2C`)* / *Get Directory Info (`0x16/0x2D`)*:
  all 32-bit fields **little-endian** (mars_nwe `U32_TO_32`) — total blocks,
  available blocks, [`0x2C` only: purgeable(0), not-yet-purgeable(0)], total dir
  entries, available dir entries, reserved[4], `sectors_per_block(1)=8` (4 KiB
  blocks), then the len-prefixed volume name.

Unimplemented functions (queue/print, accounting, burst mode, the trustee and
volume-restriction mutators, `0x16/0x1E`/`0x1F` extended directory scans) answer
completion `0xFB` (function-not-supported) rather than dropping silently.

**Packet-size negotiation** (observed attach sequence, ipx.pcap): after Create
Service Connection the client tries `0x65` Packet Burst Connection Request, then
`0x61` Get Big Packet NCP Max Packet Size, then `0x21` Negotiate Buffer Size.
Answering `0x65`/`0x61` with `0xFB` is the correct no-burst/no-big-packet
fallback path — mars_nwe without `ENABLE_BURSTMODE` does exactly that
(`nwconn.c` cases `0x61`/`0x65`) — but `0x21` **must** succeed or the client
aborts the attach. Per mars_nwe `nwconn.c` case `0x21`: request = proposed size
(2 BE, e.g. 1500 on Ethernet), reply = accepted size (2 BE) =
`min(RW_BUFFERSIZE, proposed)` with proposals `< 512` ignored (the Atari
PAM's Net/E client sends 0); our `RW_BUFFERSIZE` equivalent is 1024
(`maxRWBufferSize`), matching mars_nwe's Ethernet value, stored per connection.

Multiplexed functions (`0x16`/`0x17`) frame their body as a 2-byte big-endian
subfunction-length, then the subfunction byte, then its arguments (the subfunction
is at `requestdata+2` in mars_nwe).

## Login / bindery

**Static bindery** (bindery.go): the server carries the well-known NetWare 3.x
objects — `SUPERVISOR` (user, id `0x00000001`), `GUEST` (user, `0x02000001`),
`EVERYONE` (group, `0x01000001`), and the server's own file-server object
(`0x03000001`, live server name) — following mars_nwe `nwdbm.c
nw_fill_standard`'s well-known ids. Clients resolve the login user object
(typically GUEST) via `0x35`/`0x37` **before** issuing the login verb, so these
must answer; a `0xFB` here stalls the attach (observed in ipx.pcap). Objects
report no properties (`object_has_properties = 0`).

NCP login maps onto the shared `core/auth` seam (the same user store AFP/SMB use):

- **Cleartext login** (`0x17/0x14`): validated directly against the
  `Authenticator` when one is wired; a world-open server with no store wired grants
  a guest login (the compatibility-server default). **GUEST — or an unnamed
  login — is always granted as a guest connection, even with an Authenticator
  wired**: the NetWare convention is a passwordless GUEST account, and vintage
  clients attach as GUEST when the user supplies no credential.
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
  for the file-server type. A nearest response carries **exactly one** entry (the
  client attaches to it; mars_nwe `send_server_response` picks a single best server,
  and a real NetWare 4 server answers the same way).

## Diagnostics

The attach path is narrated through `core/log` so a stalled client is diagnosable
without a capture: SAP query answers (and queries ignored for want of a matching
entry) at **Debug**; NCP create/destroy connection and Negotiate Buffer Size at
**Debug**; every non-success completion at **Debug** with the function (and
subfunction for the `0x16`/`0x17`/`0x57` muxes) and completion code; login
grants/denials at **Info**; per-request narration at **Trace**.

## Discovery plumbing: internal network + RIP (GetLocalTarget)

The NetWare client attach sequence (observed against a real NetWare 4.1 server in
`ipx.pcap`, matching mars_nwe) is:

1. client broadcasts SAP **GetNearestServer**;
2. server answers with the file-server entry at its **internal network** address —
   `internal-net : 00-00-00-00-00-01 : 0x0451` — never the wire address (mars_nwe
   `my_server_adr`: nw.ini entry 1 net + node default 1);
3. client broadcasts a **RIP Request** (socket `0x0453`, IPX type 1) for that
   network — the *GetLocalTarget* step — and will not open an NCP connection until
   it is answered;
4. server answers **RIP Response, hops 1 / ticks 2**, unicast; the client takes the
   answer's source node as the MAC to frame NCP packets to;
5. client sends **Create Service Connection** to the internal address; NCP replies
   are sourced *from* that internal address (the client matches replies against the
   address it attached to).

Implementation: the mini-router (`core/router/ipx`) holds the internal network
(`SetInternalNetwork`; default derived from the low 4 bytes of the node MAC, the
same spirit as mars_nwe's `AUTO` mode deriving it from the host IP) and accepts
datagrams addressed to `internal-net:00-00-00-00-00-01`; broadcast-node datagrams
are accepted regardless of destination network (a client that learned a real wire
net from a coexisting server addresses its broadcasts to it). The RIP responder
(`core/service/rip`, codec `core/protocol/rip`) answers route queries for the
internal network, broadcasts it every 60 s, and advertises it at hops 16
(unreachable) on shutdown — mars_nwe `nwroute.c` `handle_rip`/`build_rip_buff`/
`send_rip_broadcast`.

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

- **Single IPX segment.** RIP (`0x0453`) is a responder for the server's own
  internal network only (the GetLocalTarget answer above) — it learns no routes and
  forwards nothing. Multi-segment IPX routing is out of scope.
- **One station MAC / one server instance.** The IPX node ID on Ethernet **is**
  the station MAC (`[[interface]].hw_address`, else the NIC's own). Blank config
  uses the host MAC so WiFi APs accept injected frames. Because identity is that
  MAC, only one IPX (and therefore one NCP) server can run per NIC.
- **One outbound frame type.** All three Ethernet framings (Ethernet II, raw 802.3,
  802.2 LLC) are accepted inbound, but replies use the port's single configured
  `ipx_frame_type`; a client bound to a different framing never sees them. mars_nwe
  treats each device+frame pair as its own network — per-framing reply is a known
  gap, deferred.
- **No SPX.** There is no SPX watchdog; idle connections are aged on inactivity.
- **Server name** comes from `[identity].hostname`, upper-cased to a NetWare name;
  default `OMNITALK`.
- Wire deviations observed against real clients are recorded in
  [errata.md](errata.md).
