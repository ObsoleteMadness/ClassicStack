# AFP Connection Flow

A walkthrough of how an AFP client connects to a server, authenticates, enumerates volumes, and mounts one — from NBP discovery through to an open volume reference.

---

## Protocol Stack

```
AFP (Apple Filing Protocol)      — application semantics
────────────────────────────────
ASP (AppleTalk Session Protocol) — session management, request/reply framing
────────────────────────────────
ATP (AppleTalk Transaction Protocol) — reliable datagram transactions
────────────────────────────────
DDP (Datagram Delivery Protocol) — network/socket addressing
```

AFP does not speak directly to the network. Every AFP command is carried as the payload of an **ASP command or write request**, which is itself carried over **ATP transactions**.

---

## Phase 1 — Server Discovery (NBP)

The client uses the **Name Binding Protocol** to locate AFP servers on the network.

```
Client                              NBP / Zone
  │                                      │
  │── BrLkUp (type="AFPServer") ────────►│  broadcast lookup
  │◄─ LkUp-Rply (name, net, node, skt) ─│  one reply per matching server
```

Each reply gives the server's:
- Entity name: `ServerName:AFPServer@Zone`
- DDP address: network, node, socket (usually socket 2 for ASP)

The client presents this list to the user. When the user picks a server, the next phases begin.

---

## Phase 2 — Get Server Info (before opening a session)

Before committing to a session the client retrieves the server's capabilities using `ASPGetStatus`. This is a single ATP transaction — **no session is opened yet**.

```
Client                              Server
  │                                   │
  │── ATP TReq (ASPGetStatus) ───────►│
  │◄─ ATP TResp (FPGetSrvrInfo) ──────│
```

### FPGetSrvrInfo response fields

| Field | Description |
|---|---|
| Machine type | e.g. `Macintosh` |
| AFP versions | e.g. `AFPVersion 2.1`, `AFP2.2`, `AFPX03` |
| UAMs supported | list of auth method strings |
| Server name | display name |
| Server signature | 16-byte unique server identifier |
| Network addresses | DDP and/or TCP addresses |
| Directory services | (AFP 3.x) |
| UTF-8 server name | (AFP 3.x) |

The client uses the UAM list and AFP version list to decide what it can negotiate.

---

## Phase 3 — Open an ASP Session

The client picks the highest AFP version both sides support, then opens an ASP session.

```
Client                              Server
  │                                   │
  │── ATP TReq (ASPOpenSession) ──────►│
  │   • QuantumSize (max write size)  │
  │◄─ ATP TResp ───────────────────────│
  │   • SessionRefNum                 │
  │   • QuantumSize (server's limit)  │
```

All subsequent AFP commands travel inside **ASPCommand** or **ASPWrite** requests tagged with `SessionRefNum`. The negotiated `QuantumSize` caps the payload of each write transaction.

---

## Phase 4 — Login and Authentication

### 4a. Single-step UAMs — FPLogin

For UAMs that fit in one round trip:

```
Client                              Server
  │                                   │
  │── ASPCommand ──────────────────────►│
  │   FPLogin                         │
  │   • AFPVersion  "AFP2.2"          │
  │   • UAM         <see table below> │
  │   • UAM data    <UAM-specific>    │
  │◄─ ASPReply ────────────────────────│
  │   • Result code (0 = success)     │
  │   • UAM data    <UAM-specific>    │
```

### UAMs and their data fields

| UAM string | Direction | Data |
|---|---|---|
| `No User Authent` | → server | *(none — guest access)* |
| `Cleartxt Passwrd` | → server | Username (≤31 bytes), Password (8 bytes, zero-padded) |
| `Randnum Exchange` | → server | Username; ← server sends 8-byte random challenge; → client sends DES-encrypted response |
| `2-Way Randnum` | → server | Username; ← server challenge; → client DES response + client's own challenge; ← server DES response |
| `DHCAST128` | → server | Diffie-Hellman key exchange + CAST-128 encrypted credentials |
| `DHX2` | → server | Extended DH exchange (AFP 3.x, stronger) |

### 4b. Multi-step UAMs — FPLoginCont

`Randnum Exchange`, `2-Way Randnum`, and the DH-family UAMs require more than one round trip. After `FPLogin` returns result code `kFPAuthContinue` (5), the client sends `FPLoginCont`:

```
Client                              Server
  │                                   │
  │── FPLogin ─────────────────────────►│
  │◄─ kFPAuthContinue (5) + challenge ─│  server sends random number
  │                                   │
  │── FPLoginCont ─────────────────────►│
  │   • ID (from previous reply)      │
  │   • UAM data (DES response, etc.) │
  │◄─ result 0 (success) ─────────────│
```

For `2-Way Randnum` the server's final reply also contains data the client must verify before trusting the server (mutual authentication).

---

## Phase 5 — Get Server Parameters

Once logged in the client calls `FPGetSrvrParms` to get the current volume list and server clock.

```
Client                              Server
  │                                   │
  │── ASPCommand (FPGetSrvrParms) ────►│
  │◄─ ASPReply ────────────────────────│
  │   • ServerTime (Mac epoch)        │
  │   • Volumes[]                     │
  │     – Volume name                 │
  │     – Volume flags                │
  │       (HasPassword, IsReadOnly,   │
  │        HasConfigInfo, …)          │
```

This is the definitive volume list for the authenticated user. Volumes may differ from what `FPGetSrvrInfo` showed (access control, per-user shares).

---

## Phase 6 — Mount a Volume (FPOpenVol)

The client sends `FPOpenVol` for the chosen volume name.

```
Client                              Server
  │                                   │
  │── ASPCommand (FPOpenVol) ─────────►│
  │   • Bitmap   (requested fields)   │
  │   • VolName  "My Share"           │
  │   • Password (if HasPassword set) │
  │◄─ ASPReply ────────────────────────│
  │   • VolumeID  (16-bit handle)     │
  │   • Bitmap    (fields returned)   │
  │   • Volume parameters:            │
  │     – Attributes                  │
  │     – Signature                   │
  │     – CreateDate / ModDate        │
  │     – BackupDate                  │
  │     – VolumeID                    │
  │     – BytesFree / BytesTotal      │
  │     – Name                        │
  │     – RootDirID (always 2)        │
```

The returned `VolumeID` is a short integer used as a handle in all subsequent file-system calls on this volume.

---

## Phase 7 — Working on the Volume

With a `VolumeID` in hand the client can now do file-system operations. Common next steps:

| AFP Command | Purpose |
|---|---|
| `FPGetVolParms` | Re-read volume parameters |
| `FPEnumerate` | List directory contents (name, type, dates, …) |
| `FPGetFileDirParms` | Stat a specific file or directory |
| `FPOpenDir` | Open a directory to get a DirID handle |
| `FPOpenFork` | Open a file's data or resource fork |
| `FPRead` / `FPWrite` | Read/write fork data |
| `FPCloseFork` | Close an open fork |
| `FPCloseVol` | Unmount the volume |

---

## Session Teardown

```
Client                              Server
  │                                   │
  │── FPLogout ────────────────────────►│  (AFP-level logout)
  │── ASPCloseSession ─────────────────►│  (ASP-level close)
  │◄─ acknowledgement ─────────────────│
```

The server may also end a session itself: it announces the shutdown with an `ASPAttention` and then sends a server-initiated `ASPCloseSession` (see "Server Messages & Attention" below). The AFP attention word's shutdown flag is bit 15 (`0x8000`) — an earlier revision of this document said `0x4000`, which an observed capture of a real AppleShare server disproved (see `errata.md`, "AFP attention codes / FPGetSrvrMsg").

---

## Server Messages & Attention

AFP has a server→client notification path: the ASP **Attention** packet. The server uses it to tell a workstation "something happened"; when the attention word carries the *server message* flag, the client fetches the text with `FPGetSrvrMsg` and displays it in a dialog. All of the following is from an observed capture of a real AppleShare server.

### Capability advertisement

The `FPGetSrvrInfo` / `ASPGetStatus` reply's `Flags` word must set **bit 3 (`0x0008`, SupportsSrvrMsg)**. Without it clients neither fetch the login greeting nor honour message attentions.

### FPGetSrvrMsg (command 38)

```
Request:  cmd(1)=38  pad(1)  MessageType(2)  MessageBitmap(2)
Reply:    MessageType(2)  MessageBitmap(2)  PascalString(message)
```

- `MessageType` 0 = **login message** (greeting): the client requests it unprompted right after `FPOpenVol` and shows it once per mount.
- `MessageType` 1 = **server message**: requested after each attention with the message flag.
- `MessageBitmap` bit 0 = message as text (bit 1 = UTF-8, AFP 3.x only). The observed server always answers with bitmap `0x0001`.
- The message is a MacRoman Pascal string, at most 199 bytes. No pending message answers a zero-length string.

### ASP Attention wire form

An ATP **TReq** from the server's session socket to the client's *workstation session socket* (the socket the client opened the session from), control `0x40` (ALO — XO is **not** set), bitmap `0x01`. The ASP payload rides entirely in the 4 ATP user bytes:

```
[0] SPFunction = 8 (Attention)   [1] SessionID   [2:3] AttentionCode
```

The client acknowledges with a TResp carrying 4 zero user bytes.

Attention code bits (netatalk's AFPATTN_* names):

| bit(s) | mask | meaning |
|---|---|---|
| 15 | `0x8000` | server is shutting down |
| 14 | `0x4000` | server crashed (no clean shutdown) |
| 13 | `0x2000` | server message waiting — fetch with `FPGetSrvrMsg` type 1 |
| 12 | `0x1000` | do not attempt reconnection |
| 0–11 | `0x0FFF` | minutes until the announced shutdown (0 = now) |

Observed words: `0x2000` (plain message), `0xB001` (shutdown in 1 minute, message, no reconnect), `0xB000` (shutdown now, message, no reconnect).

### Message push sequence

```
Client                              Server
  │◄─ ASPAttention (0x2000) ──────────│  message waiting
  │── TResp ack ──────────────────────►│
  │── FPGetSrvrMsg (type 1) ──────────►│
  │◄─ type 1, bitmap 0x0001, text ────│  client shows the dialog
```

### Disconnect-with-warning sequence (two-phase)

```
Client                              Server
  │◄─ ASPAttention (0xB001) ──────────│  shutdown in 1 min + message
  │── FPGetSrvrMsg (type 1) ──────────►│
  │◄─ warning text ───────────────────│  server keeps serving the countdown
  │            … 1 minute …           │
  │◄─ ASPAttention (0xB000) ──────────│  shutdown NOW + message
  │── FPGetSrvrMsg (type 1) ──────────►│
  │◄─ warning text ───────────────────│
  │◄─ ASPCloseSession (TReq) ─────────│  server-initiated close:
  │── TResp ack ──────────────────────►│  user bytes 01 | SessionID | 00 00
```

ClassicStack implements this surface as: the `[AFP] login_message` config option (type 0 greeting), the management-plane `SendMessage`/`Disconnect` actions (`core/service/afp/message.go`), and a service `Stop()` that announces `0xA000` (shutdown + message), keeps serving through a short fetch grace, then sends the server-initiated CloseSession per session.

---

## Summary Sequence Diagram

```
Client              NBP     Server
  │                  │        │
  │── BrLkUp ───────►│        │
  │◄─ LkUp-Rply ─────│        │
  │                           │
  │── ASPGetStatus ───────────►│   (no session yet)
  │◄─ FPGetSrvrInfo ───────────│
  │                           │
  │── ASPOpenSession ──────────►│
  │◄─ SessionRefNum ───────────│
  │                           │
  │── FPLogin ─────────────────►│   (+ FPLoginCont if needed)
  │◄─ result 0 ────────────────│
  │                           │
  │── FPGetSrvrParms ──────────►│
  │◄─ volume list ─────────────│
  │                           │
  │── FPOpenVol ───────────────►│
  │◄─ VolumeID ────────────────│
  │                           │
  │   ... file operations ...  │
  │                           │
  │── FPLogout ────────────────►│
  │── ASPCloseSession ─────────►│
```
