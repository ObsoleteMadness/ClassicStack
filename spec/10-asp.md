# AppleTalk Session Protocol (ASP)

Source: Apple IM:Networking Chapter 8
`https://developer.apple.com/library/archive/documentation/mac/pdf/Networking/ASP.pdf`
`https://dev.os9.ca/techpubs/mac/Networking/Networking-204.html` (overview)
`https://dev.os9.ca/techpubs/mac/Networking/Networking-222.html` (assembly summary)

---

## Overview

ASP is an asymmetric, session-oriented protocol layered on top of ATP/DDP.
- The **workstation** always initiates; the **server** responds.
- Exception: the server may send attention notifications or aspDataWrite requests to the workstation.
- ASP is used by AFP (AppleTalk Filing Protocol) for all file-server communication.
- Implemented in the Mac `.XPP` driver.

---

## ATP Packet Conventions

Every ASP packet is an ATP packet (DDP type 3).
The 4-byte **ATP UserData** field carries the ASP function code and session/sequence info.
All multi-byte integers are **big-endian**.

### UserData byte offsets (from MSB = byte 0)

```
Byte 0  aspCmdCode   ASP function code (see table below)
Byte 1  aspWSSNum    WSS socket in OpenSession requests
        aspSSSNum    SSS socket in OpenSession replies
        aspSessID    Session ID in all other packets
Byte 2  aspVersNum   ASP version (high byte) in OpenSession requests
        aspOpenErr   Error code in OpenSession replies
        aspSeqNum    Sequence number (high byte) in command requests
        aspAttnCode  Attention code (high byte) in attention packets
Byte 3  aspVersNum   ASP version (low byte) in OpenSession requests
        aspSeqNum    Sequence number (low byte) in command requests
        aspAttnCode  Attention code (low byte) in attention packets
```

### ATP Data offsets (for aspDataWrite only)

```
Byte 0-1  aspWrBSize   Write-buffer size (uint16 big-endian) — how many bytes of write data the server expects
Total: aspWrHdrSz = 2 bytes
```

---

## ASP Function Codes (byte 0 of ATP UserData)

```
aspCloseSess  = 1   close session               (workstation → server)
aspCommand    = 2   user command                (workstation → server)
aspGetStat    = 3   get server status           (workstation → server)
aspOpenSess   = 4   open session                (workstation → server)
aspTickle     = 5   keep-alive tickle           (both directions)
aspWrite      = 6   write command               (workstation → server)
aspDataWrite  = 7   write-data request          (server → workstation)  ← server-initiated
aspAttention  = 8   server attention            (server → workstation)  ← server-initiated
```

---

## Session Establishment

### OpenSession Request (workstation → server SLS)

ATP TReq (XO) to the server's Session Listening Socket (SLS, typically socket 252).

```
UserData[0]  = 4 (aspOpenSess)
UserData[1]  = WSS — workstation's session socket number (for server tickles and aspDataWrite)
UserData[2]  = 0x01  (ASP version high = 1)
UserData[3]  = 0x00  (ASP version low = 0)
ATP Data     = empty
```

### OpenSession Reply (server → workstation)

ATP TResp to the above TReq.

```
UserData[0]  = SSS — server's session socket number (for future commands)
UserData[1]  = Session ID assigned by server (1–255)
UserData[2]  = Error code (0 = success, non-zero = failure)
UserData[3]  = 0 (unused)
ATP Data     = empty
```

> **Note:** There is no write quantum in the OpenSession reply. The workstation uses its own
> local `aspQuantumSize = atpMaxData × atpMaxNum = 578 × 8 = 4624 bytes` (from `ASPGetParms`)
> to determine the maximum write size per `ASPUserWrite` call.

---

## Commands (workstation → server)

### aspCommand (2) — User Command (e.g. FPLogin, FPEnumerate, …)

ATP TReq (XO). Reply is ATP TResp with AFP result.

```
UserData[0]  = 2 (aspCommand)
UserData[1]  = Session ID
UserData[2-3]= Sequence number (uint16 big-endian, incremented per command)
ATP Data     = AFP command block (variable length, max atpMaxData = 578 bytes)

Reply:
  ATP UserData = AFP result code (int32, stored as uint32)
  ATP Data     = AFP reply block (variable length, up to 8 × 578 = 4624 bytes)
```

### aspGetStat (3) — Get Server Status

ATP TReq (ALO, not XO). Sent to SLS before opening a session.

```
UserData[0]  = 3 (aspGetStat)
UserData[1-3]= 0
ATP Data     = empty

Reply:
  ATP UserData = 0
  ATP Data     = AFP server status block (FPGetSrvrInfo response)
```

### aspCloseSess (1) — Close Session

ATP TReq (XO).

```
UserData[0]  = 1 (aspCloseSess)
UserData[1]  = Session ID
UserData[2-3]= 0
ATP Data     = empty

Reply:
  ATP UserData = 0
  ATP Data     = empty
```

### aspTickle (5) — Keep-Alive

ATP TReq (ALO). No reply required. Sent periodically by both sides (~30 s interval).

```
UserData[0]  = 5 (aspTickle)
UserData[1]  = Session ID
UserData[2-3]= 0
ATP Data     = empty
```

---

## Two-Phase Write Protocol (ASPUserWrite → FPWrite)

When a workstation calls `ASPUserWrite`, the `.XPP` driver uses a two-phase ATP exchange
to deliver both the AFP command block and the write data to the server.

### Phase 1 — aspWrite Request (workstation → server)

ATP TReq (XO). Contains only the AFP command block; **no write data**.

```
UserData[0]  = 6 (aspWrite)
UserData[1]  = Session ID
UserData[2-3]= Sequence number (same value used throughout this write)
ATP Data     = AFP command block (e.g. FPWrite header, 12 bytes)
              [cmd, flag, forkRef(2), offset(4), reqCount(4)]
ATP Bitmap   = 0x01 (workstation expects 1 final response packet)
```

### Phase 2a — aspDataWrite Request (server → workstation WSS)

Server sends an ATP TReq **to the workstation's WSS** (saved from OpenSession).
This is a **server-initiated** packet, not a reply.

```
UserData[0]  = 7 (aspDataWrite)
UserData[1]  = Session ID
UserData[2-3]= Sequence number (same as the aspWrite above)
ATP Data[0-1]= Write buffer size (uint16 big-endian) — how many bytes of data server wants
               Typically = FPWrite.ReqCount from Phase 1 command block
ATP Bitmap   = number of response packets expected (ceil(wantBytes/578), max 8 → 0xFF)
```

### Phase 2b — Write Data Response (workstation → server)

Workstation's `.XPP` driver automatically sends ATP TResp in response to aspDataWrite TReq.

```
ATP TResp seq 0..N, EOM on last packet
ATP Data     = raw write data bytes (split across up to 8 × 578 = 4624 bytes total)
```

### Phase 3 — Final Reply (server → workstation)

After receiving all write data, server sends ATP TResp to the **original aspWrite TReq**
(Phase 1 TransID).

```
ATP UserData = AFP result code (int32 as uint32)
ATP Data     = AFP response block (e.g. FPWriteRes: lastWritten uint32)
```

---

## Server-Initiated Packets

Two ASP commands flow **from server to workstation**:

| Code | Name         | Direction           | Purpose                        |
|------|--------------|---------------------|--------------------------------|
| 7    | aspDataWrite | server → workstation| Request write data (see above) |
| 8    | aspAttention | server → workstation| Notify workstation of an event |

Both are ATP TReq packets routed to the workstation's WSS socket.
The workstation replies with ATP TResp. The server must track the TransID to match replies.

---

## Sequence Numbers

Each `aspCommand` and `aspWrite` request carries a 16-bit sequence number (UserData bytes 2–3).
The workstation increments the sequence number for each new command.
The server uses this to match aspWrite ↔ aspDataWrite pairs.

---

## Key Constants

```
aspVersion     = 0x0100          ASP version advertised in OpenSession
maxCmdSize     = atpMaxData      Maximum AFP command block size (578 bytes)
quantumSize    = atpMaxData × atpMaxNum = 578 × 8 = 4624 bytes  (workstation-local, not sent from server)
tickleInt      = 30 seconds      Tickle interval
atpMaxData     = 578 bytes       Max data per ATP packet
atpMaxNum      = 8               Max response packets per ATP transaction
```

---

## Error Codes

```
aspBadVersNum   = -1066   Server cannot support client's ASP version
aspBufTooSmall  = -1067   Reply data exceeds reply buffer
aspNoMoreSess   = -1068   Server at session capacity
aspNoServers    = -1069   No server at the given address
aspParamErr     = -1070   Invalid session reference number or session closed
aspServerBusy   = -1071   Server cannot accept another session
aspSessClosed   = -1072   Session is closing
aspSizeErr      = -1073   Command block exceeds aspMaxCmdSize
```

---

## Implementation Notes

### Server responsibilities

1. **OpenSession**: Reply with `(SSS, sessID, 0, 0)` in UserData. Store WSS from request byte 1.
2. **aspCommand**: Pass ATP data directly to AFP handler; return result as UserData + data TResp.
3. **aspWrite** (phase 1): Save cmd block + original TReq info; send aspDataWrite to WSS; await TResp.
4. **aspDataWrite** (phase 2): Server **sends** this TReq (do not handle it as an incoming command).
5. **TResp for aspDataWrite**: Accumulate data; on EOM send final reply to original aspWrite TReq.
6. **aspTickle**: No reply needed; just reset the session watchdog.
7. **aspGetStat**: Reply with AFP server info block.
8. **aspCloseSess**: Clean up session state; reply empty.
9. **Tickle server→workstation**: Send periodic aspTickle TReq to WSS to keep session alive.

### Common mistakes

- Setting non-zero bytes in OpenSession reply UserData[2] (error code) will cause the Mac to reject the session.
- `aspDataWrite` (7) is **from server**, not from workstation. Receiving cmd=7 as an inbound command is wrong.
- The write quantum is **not** sent in OpenSession; it is a workstation-local constant.
- aspWrite carries **only** the command block — not the write data. Write data arrives via aspDataWrite TResp.
