---
title: "Protocol Support"
weight: 4
---

# Supported protocol versions

This is a summary of exactly which protocol versions/dialects each service speaks.
For wire-level detail on any of these, see the matching document under
[`spec/`](../spec).

## AppleTalk

| Protocol | Version / notes |
|---|---|
| DDP (Datagram Delivery Protocol) | AppleTalk **Phase 2** (extended networks, cable ranges, multi-zone) |
| RTMP / ZIP | Phase 2 routing table maintenance + zone information |
| NBP | Name Binding Protocol (lookup/register/confirm) |
| AEP | AppleTalk Echo Protocol (`csecho`) |
| ATP | AppleTalk Transaction Protocol (reliable request/response, used under ASP and MacIP) |
| ASP | AppleTalk Session Protocol — classic AFP transport over DDP |
| LLAP | LocalTalk Link Access Protocol (short/long DDP headers, node-claim) — see `spec/09-port-localtalk-base.md` |
| Netboot (ABP) | Apple Boot Protocol v1, plus the ChainBoot EBP extension — see [netboot.md](netboot.md) |

Transports: EtherTalk (raw Ethernet), LocalTalk-over-UDP (LToUDP, RFC-style multicast
tunnel), TashTalk (real LocalTalk over a serial-to-LocalTalk hardware bridge).

## AFP (Apple Filing Protocol)

Advertises and accepts **AFP 1.1, 2.0, 2.1, and 2.2** (`AFPVersion 1.1` /
`AFPVersion 2.0` / `AFPVersion 2.1` / `AFP2.2`). No AFP 3.x (UTF-8 names / 64-bit IDs) —
this targets classic (pre-Mac OS X) clients, where those versions are what ships.

Two transport stacks, simultaneously:

- **Classic**: DDP → ATP → ASP → AFP — joins the AppleTalk router.
- **Modern**: TCP → DSI → AFP (conventionally `:548`) — `[AFP].transports = ["tcp"]`
  plus an explicit `tcp_addr` (never an implicit `:548`, matching SMB's direct-TCP
  posture). See `spec/21-dsi.md` for the DSI wire format.

CNID tracking: SQLite (needs the `sqlite` build tag) or an in-memory backend.
AppleDouble metadata as `._name` sidecars or Netatalk-compatible `.AppleDouble/`
folders.

## SMB (Server Message Block / CIFS)

**SMB1 only** — no SMB2/3. The server negotiates against whatever the client offers
from the classic dialect ladder:

`PC NETWORK PROGRAM 1.0` · `MICROSOFT NETWORKS 3.0` · `DOS LM1.2X002` ·
`DOS LANMAN2.1` · `LANMAN1.0` · `LM1.2X002` · `LANMAN2.1` ·
`Windows for Workgroups 3.1a` · `NT LM 0.12`

i.e. the CORE and LANMAN dialect families plus `NT LM 0.12` (what Windows 95/98/NT
speak). This deliberately covers everything from DOS's Microsoft Network Client and
Windows for Workgroups 3.11 through Windows NT/95/98 — not the newer NTLM/SMB2 stack
Windows 2000+ prefers.

Carriers: direct-TCP `:445`, NetBIOS-over-TCP (`:139`), NetBEUI (NBF), and IPX
(NBIPX, or direct-hosted SMB-over-IPX on socket `0x0550` with no NetBIOS layer at all —
"NWLink direct host").

## NetBIOS

Name service + session service (RFC 1001/1002-shaped where it rides TCP) over three
carriers: NBF (NetBEUI), NB-IPX, and NBT (TCP). Includes the browser service
(`\MAILSLOT\BROWSE`: HostAnnounce, Election, GetBackupList, DomainAnnounce,
LocalMaster) and the Messenger/WinPopup service (`\MAILSLOT\MESSNGR`).

## IPX / SPX

IPX router with RIP/SAP. Ethernet encapsulations: `ethernet_ii` (DIX/`0x8137`),
`802.3` (raw), `802.2` (LLC). No SPX (session-layer) implementation — only the IPX
datagram layer, which is what NCP, direct-hosted SMB, and the MacIPX gateway ride.

## NCP (NetWare Core Protocol)

**NetWare 3.x-style bindery emulation** over IPX — file service + a static bindery
(login, SLIST-style server discovery via SAP). No NDS (NetWare 4.x+ directory
services). See [`spec/17-ncp.md`](../spec/17-ncp.md).

## EtherDFS

The DOS EtherType `0xEDF5` file-sharing protocol as defined by Mateusz Viste's
EtherDFS. See [`spec/18-etherdfs.md`](../spec/18-etherdfs.md).

## Gateways

- **MacIP** — IP-over-AppleTalk for MacTCP clients, bridge or NAT mode. See
  [`spec/14-macip-gateway.md`](../spec/14-macip-gateway.md).
- **MacIPX** (`[IPXGW]`) — IPX-over-AppleTalk for the classic MacIPX client, DDP
  socket 78. See [`spec/15-macipx-gateway.md`](../spec/15-macipx-gateway.md).

## Client side

The file-client SDK (`client/`, driving `csfs`/`csmount`/the in-process Finder client)
speaks the client half of AFP, SMB (over TCP/NBF/NBIPX/direct-IPX), NCP, and EtherDFS —
the same protocol versions/dialects listed above, from the other end of the wire.
