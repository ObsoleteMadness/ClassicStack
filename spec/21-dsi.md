# 21 — DSI (AFP over TCP/IP)

DSI (Data Stream Interface) is the session-layer protocol that carries AFP over
TCP/IP — the "modern" AFP transport (`[AFP].transports = ["tcp"]`, conventionally
`:548`), alongside the classic DDP/ATP/ASP stack (`spec/10-asp.md`). Where ASP frames
one AFP command as an ATP TReq/TResp UserData+data pair over a AppleTalk network, DSI
frames it as a fixed 16-byte header plus a variable-length data block on a TCP byte
stream. Both ultimately drive the exact same AFP command core
(`core/service/afp/conn.go`'s `CommandHandler`/`CommandCircuit` seam) — this document
covers only the DSI framing; the AFP command set itself is documented in the `afp`
family of specs and needs no DSI-specific treatment.

## Sources

There is no local published Apple spec file for DSI (unlike `spec/19-netboot.md`'s
Apple source-tree citations). This document is compiled from:

- **Apple's "AFP over TCP" / DSI specification** (the AppleShare IP era; the header
  shape and command set are unchanged through AFP 3.x) — general AFP/DSI engineering
  knowledge, not a locally-held document.
- **Netatalk's `libatalk/dsi`** (`dsi.h`'s `struct DSI`, `dsi_stream.c`) as the
  long-lived, widely-interoperable open-source reference implementation other AFP
  clients and servers exchange DSI with — used here as the "golden" cross-check in the
  absence of a local packet capture.
- **This project's pre-refactor `service/dsi`** (deleted at the M10 cutover,
  `511299a`; recovered from git history for this rewrite): a prior implementation
  existed and worked well enough to be wired into the legacy runtime, but — see
  Errata below — placed the AFP result code in the wrong location on the wire. That
  bug is NOT preserved in the current implementation.

**No DSI capture exists yet** under `spec/captures/` (unlike AFP-over-DDP, which has
`captures/client-afp.pcap`). The wire format below has not been byte-verified against
a real classic Mac AppleShare-over-TCP client or a modern DSI implementation
interoperating with this server. `test/e2e`'s `afp/dsi` case proves the client and
server sides of *this* implementation agree with each other end-to-end (login,
volume open, file ops with forks), which is a strong internal-consistency check, but
is not the same as third-party interop proof. Treat this document as a well-sourced
reconstruction pending a real capture, per the project's errata policy (`errata.md`)
— if a future capture disagrees with anything here, the capture wins and this file
gets corrected, not the other way around.

## Header (16 bytes, all fields big-endian)

```
 0               1               2               3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|     Flags     |    Command    |           Request ID          |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                    ErrorCode / DataOffset                     |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Total Data Length                      |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                           Reserved                             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

Implemented in `core/protocol/dsi` (`Header`, `HeaderSize = 16`, `Marshal`/`Unmarshal`)
— a pure codec (no I/O), shared verbatim by the server transport (`adapter/dsi`) and
the client session (`client/dsi`), the same split ASP's codec (`core/protocol/asp`)
has from its own client/server transports.

| Field | Size | Meaning |
|---|---|---|
| Flags | 1 | `0x00` Request, `0x01` Reply |
| Command | 1 | see Commands below |
| Request ID | 2 | client-chosen, echoed on the reply; demultiplexes concurrent/interleaved exchanges on one TCP connection |
| ErrorCode / DataOffset | 4 | **dual-purpose, distinguished by Flags** — see below |
| Total Data Length | 4 | length of the data block immediately following the header |
| Reserved | 4 | always 0 |

**The third field is where implementations most often get this wrong** (see Errata):

- On a **Reply**, it is the signed AFP/DSI **result code** — the same code space AFP
  commands return everywhere else (`kFPNoErr` = 0, `kFPAccessDenied`, …). A
  `Command`/`Write`/`OpenSession`/`GetStatus` reply carries its result **here**, in the
  header, encoded as a plain two's-complement `uint32` — **not** as bytes prepended to
  the data payload. The data block that follows is the AFP reply body alone.
- On a **Write request** specifically, it is the **DataOffset**: the byte offset
  within the payload where the raw write bytes begin, after the fixed-length AFP write
  command header (`FPWrite` = 12 bytes, `FPAddIcon` = 20 bytes — `core/service/afp/
  forkio.go`'s `writeDataCount`). For a well-formed request this is always exactly
  that fixed length, so a correctly-framed Write can be forwarded to the AFP command
  core unchanged (header + data concatenated, exactly the shape `conn.Command`/
  `conn.Write` already expect from the ASP two-phase-write reconstruction) without
  ever consulting this field.
- On every other request it is unused (0).

## Commands

| # | Name | Direction | Session required | Reply |
|---|---|---|---|---|
| 1 | CloseSession | either | yes | empty, then the connection closes |
| 2 | Command | workstation → server | yes | AFP reply block; result in the header |
| 3 | GetStatus | workstation → server | **no** | `FPGetSrvrInfo` block (`core/service/afp`'s `serverInfoBlock`) |
| 4 | OpenSession | workstation → server | no (establishes it) | empty |
| 5 | Tickle | either | no | **none** — fire-and-forget keep-alive, mirrors ASP's `SPTickle` ("no reply required") |
| 6 | Write | workstation → server | yes | AFP reply block; result in the header. Payload is the AFP write command header with the bulk data concatenated directly after it |
| 8 | Attention | server → workstation | yes | **none** — unsolicited; a 2-byte big-endian attention code, mirroring ASP's `AspAttnMsg` shape |

Command/Write both funnel into the identical `CommandCircuit.Command(block)` call on
the server (`adapter/dsi`'s `serve`) — the AFP command core does not distinguish which
DSI command carried a given block, matching the ASP side's `sess.conn.Command(...)`.

## Session lifecycle

Unlike ASP (where a session is a logical id multiplexed over a shared DDP socket, so
the server tracks a `sessionTable` keyed by session id), one DSI TCP connection **is**
the session — there is no separate id to allocate or look up. The server opens one AFP
`CommandCircuit` (`handler.NewConn()`) on `OpenSession` and closes it when the
connection ends (`CloseSession`, or the peer disconnecting). A `Command`/`Write`
received before `OpenSession` is a protocol violation the server answers by dropping
the connection outright (there is no well-defined DSI-level "no session" error code to
send back, unlike ASP's `SPErrorParamErr`).

A real DSI server periodically tickles an idle client (and vice versa) to detect a
dead peer; this implementation does not yet send tickles of its own — it answers any
it receives with nothing (per the table above) and otherwise relies on the TCP
connection's own liveness. Not sending keepalives is a conservative simplification,
not a spec violation (Tickle needs no reply either direction), but a very long idle
connection through a stateful NAT/firewall could be dropped without one; this is a
candidate follow-up, not a correctness gap.

## Implementation

| Piece | Package | Ring |
|---|---|---|
| Wire codec (`Header`) | `core/protocol/dsi` | core |
| Server transport (TCP listener, drives `afp.CommandHandler`) | `adapter/dsi` | adapter |
| Client session (dial, `Command`/`Write`/`Close`/`SetAttentionHandler`) | `client/dsi` | client |
| Compose wiring (`AFP.tcp_addr` → the listener) | `compose/runtime/transports.go`'s `wireDSI` | compose |

Config: `[AFP].transports` must include `"tcp"` and `tcp_addr` must be set (there is
no implicit `:548`, matching SMB's direct-TCP posture) — see `docs/config.md`.

The client dials it via `-ifacetype tcp -iface <host[:548]>` with an `afp://` URI
(`client/afp`'s `dialAndLoginDSI`); the client-side session
(`client/dsi.Session`) implements the same `client/afp.Session` interface
(`Command`/`CommandMax`/`Write`/`Close`/`SetAttentionHandler`) that the ASP client
session does, so `client/afp`'s command plumbing — including reconnect-on-drop
(`FS.reestablish`) — does not care which transport carried the session.

Unlike ASP, whose DDP transport can receive a server-initiated packet (Tickle,
Attention) at any time independent of an in-flight request (packet-multiplexed), a
naive synchronous "read exactly one frame per write" TCP client would deadlock or
misparse if a push arrived interleaved with a reply. `client/dsi.Session` runs a
background read loop that demuxes inbound frames by Request ID, so an Attention or
Tickle arriving mid-`Command` is absorbed without disturbing the caller waiting on its
own reply — see `client/dsi`'s `TestTickleAndAttentionDoNotStallCommand`.

## Errata / observations

- **The AFP result code belongs in the header's ErrorCode field, not the payload.**
  The pre-refactor `service/dsi` (deleted at the M10 cutover) manually prepended a
  4-byte big-endian result code to the front of every `Command`/`Write` reply's data
  block instead, leaving `ErrorOffset` zeroed. A real DSI client reads the result from
  the header (per Netatalk's `dsi_cmdreply`) and would have misinterpreted the first 4
  bytes of every genuine AFP reply as a status code, corrupting the actual response —
  this is why the note above calls it out explicitly rather than silently fixing it: a
  future contributor porting logic from the old code must not carry this shape
  forward. **Own bug, not upstream errata** (per this project's convention that only
  deviations from a real peer's observed behaviour count as spec errata) — recorded
  here because it is exactly the kind of thing that is easy to reintroduce by copying
  the old implementation without re-deriving the header contract from first
  principles.
