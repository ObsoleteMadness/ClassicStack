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

The server may also issue an `ASPAttention` packet (AFP attention code `0x4000` = server is shutting down) to prompt the client to disconnect gracefully.

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
