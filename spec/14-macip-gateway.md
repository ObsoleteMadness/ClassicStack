# MacIP Gateway — IP-over-AppleTalk (DDP type 22, socket 72)

MacIP (also "IPTalk" / KIP / "AppleTalk-IP") carries IPv4 traffic for Macintosh
clients that have no native IP stack on their network medium (classic LocalTalk,
EtherTalk, LToUDP, TashTalk). A MacIP **gateway** sits on the AppleTalk side and
on a real IP network; it leases each Mac an IPv4 address, then tunnels the Mac's
IP datagrams to/from the wider IP world.

> **Sources.** The AppleTalk-facing wire format here matches Apple's original
> MacIP and is interoperable with Stefan Bethke's `macipgw` (the reference C
> implementation, https://github.com/jasonking3/macipgw) and Netatalk's
> `papd`/`macipgw`. Where a detail is observed rather than published it is called
> out. ClassicStack's implementation splits cleanly into two halves: the
> **AppleTalk protocol + lease pool** ([core/service/macip](../core/service/macip))
> and the **IP-side egress** (proxy-ARP / NAT / DHCP-relay,
> [adapter/macipgw](../adapter/macipgw)). An implementor only needs the first half
> to be wire-compatible with Mac clients; the IP side is a local engineering
> choice.

> **Not to be confused with** [15-macipx-gateway.md](15-macipx-gateway.md) — that
> is *MacIPX* (AppleTalk ↔ Novell IPX, DDP type 0x4E). This document is *MacIP*
> (AppleTalk ↔ IPv4, DDP type 22).

---

## 1. Reference data

| Item | Value |
|---|---|
| AppleTalk socket (config + data) | **72** |
| DDP type — configuration | **3** (ATP) |
| DDP type — IP data | **22** (`DDPTYPE_MACIP`) |
| MacIP protocol version | **1** |
| NBP object name | the gateway's own IP, dotted-decimal (e.g. `192.168.1.1`) |
| NBP type | `IPGATEWAY` |
| NBP zone | operator-configured (defaults to the router's first zone) |
| Max IP payload per DDP datagram | **586** bytes (`ddp.MaxDataLength`) |
| MacIP function — assign | **1** (`MACIP_ASSIGN`) |
| MacIP function — server check | **3** (`MACIP_SERVER`) |
| Default lease pool size | 254 host slots (incl. reserved gateway slot) |
| Lease idle timeout (this impl.) | 5 minutes since last seen |

Both configuration and data use **the same socket, 72**, distinguished only by the
DDP type byte (3 = ATP config, 22 = IP data). A datagram arriving on socket 72
with any other DDP type is dropped.

---

## 2. Discovery (NBP)

A Mac finds the gateway by an NBP lookup for type `IPGATEWAY` in its zone. The
gateway has registered its IP-as-name there:

```
NBP BrRq   =:IPGATEWAY@<zone>            (client → broadcast)
NBP LkReply <gw-ip-string>:IPGATEWAY@<zone>   (gateway → client)
            └ carries the gateway's DDP address + socket 72
```

The object **name** is the gateway IP rendered as dotted-decimal text
(`"192.168.1.1"`), so the client learns the gateway's IP identity and its DDP
address (network/node/socket) in one reply.

```
register at startup:    NBP_register(name = ipv4_string(GatewayIP),
                                     type = "IPGATEWAY",
                                     zone = configured-or-first-zone,
                                     socket = 72)
unregister at shutdown: NBP_unregister(same name/type/zone)
```

> The reference `macipgw` additionally answers NBP lookups for type `IPADDRESS`
> to discover individual client mappings. ClassicStack does not need this:
> client → IP mappings are learned directly from the ATP assignment exchange.

---

## 3. Configuration exchange (ATP, DDP type 3)

Address assignment and server liveness use **ATP** (AppleTalk Transaction
Protocol) request/response on socket 72. The Mac is the ATP *requester*; the
gateway is the *responder*.

### 3.1 ATP framing

Only the single-packet transaction form is used. The DDP payload of an ATP
datagram begins with a 4-byte ATP header, followed by an 8-byte ATP *user-bytes*
field which here carries the MacIP control struct:

```
DDP.Data layout (DDP type 3):
  +0  ATP control byte    (1)   function in top 2 bits: TReq=0x40, TResp=0x80; EOM=0x10
  +1  ATP bitmap / seq    (1)
  +2  ATP transaction id  (2)   big-endian; echoed in the response
  +4  ATP user bytes ...        ← MacIP control struct starts here
```

The MacIP control struct (the ATP user-bytes, 8 bytes minimum) is:

```c
struct macip_req_control {        // big-endian on the wire
    int16  mipr_version;          // +0  protocol version (1)
    int16  _mipr_pad1;            // +2  reserved / zero
    int32  mipr_function;         // +4  MACIP_ASSIGN(1) | MACIP_SERVER(3)
};
```

On an **assign** request the Mac may append a *data* block requesting a specific
IP (and echoing the other config fields, normally zero):

```c
struct macip_req_data {           // optional; follows the control struct
    int32 mipr_ipaddr;            // requested IP (0 = "any")
    int32 mipr_nameserver;
    int32 mipr_broadcast;
    int32 _mipr_pad2;
    int32 mipr_subnet;
};
```

### 3.2 Request parsing (pseudo-code)

```
on ATP datagram (DDP type 3) on socket 72:
    if len(DDP.Data) < 4 + 8: drop          # need ATP header + control struct
    if (DDP.Data[0] & 0xC0) != TReq: drop    # only requests are processed here
    tid       = be16(DDP.Data[2:4])
    userData  = DDP.Data[4:]
    function  = be32(userData[4:8])          # control struct mipr_function
    requested = (len(userData) >= 12) ? IPv4(userData[8:12]) : 0.0.0.0

    atNet, atNode = source of the datagram   # see §3.5 for net-0 normalisation
    if not valid_unicast(atNet, atNode): drop
```

### 3.3 Function handling

```
switch function:
  case MACIP_SERVER (3):           # "are you still there?" / re-bind probe
      if an existing lease for (atNet,atNode) exists:
          reply TResp with that leased IP
          return
      # else fall through and treat like an assign

  case MACIP_ASSIGN (1):           # "give me an IP"
      ip, ok = assign_address(requested, atNet, atNode)   # §4
      reply TResp with ip (0.0.0.0 + function=fail if pool exhausted)
```

A request whose function is neither 1 nor 3 is answered with a failure response
(version 1, function = 0) by the reference gateway; ClassicStack simply does not
recognise it.

### 3.4 Response packet (ATP TResp, DDP type 3)

The response reuses socket 72 and DDP type 3, reverses the DDP source/dest, and
echoes the transaction id. The ATP user-bytes carry the same control struct
(version = 1, function = `MACIP_ASSIGN`), followed by the full **config data**
block. ClassicStack emits a fixed 28-byte data block (`configDataLen`):

```
TResp DDP.Data layout (32 bytes total):
  +0  ATP control byte = TResp(0x80) | EOM(0x10)
  +1  ATP seq          = 0
  +2  ATP tid          = echoed request tid (be16)
  +4  mipr_version     = 1                 (be16)
  +6  _pad1            = 0                 (2 bytes)
  +8  mipr_function    = MACIP_ASSIGN(1)   (be32)    # 0 on failure
  +12 assigned IP      (4)                            # 0.0.0.0 if assignment failed
  +16 nameserver       (4)
  +20 broadcast        (4)
  +24 _pad2            (4)
  +28 subnet mask      (4)
```

Any field the assignment did not override (nameserver / broadcast / subnet mask)
falls back to the gateway's configured defaults before being written. All IPv4
values are in network byte order.

```
build_TResp(tid, cfg):
    ns   = cfg.nameserver or DEFAULT_nameserver
    bc   = cfg.broadcast  or DEFAULT_broadcast
    mask = cfg.subnet     or DEFAULT_subnet
    emit bytes per the table above with cfg.ip, ns, bc, mask
    router.Reply(received, ddpType=3, data=...)   # Reply reverses src/dst
```

### 3.5 Source-address normalisation

A Mac that has not yet learned its AppleTalk network number may send with
`SrcNetwork = 0`. The gateway substitutes the **receiving port's** network number
so the lease is keyed to a real (network, node):

```
atNet = DDP.SrcNetwork
if atNet == 0 and rxPort.Network() != 0:
    atNet = rxPort.Network()
atNode = DDP.SrcNode
valid_unicast := atNet != 0 and atNode != 0 and atNode != 0xFF
```

---

## 4. Address assignment & the lease pool

The gateway owns a contiguous pool of IPv4 addresses on its IP-side subnet. Slot
0 is the gateway's own IP and is never handed out; slots 1..N are client leases.

```
pool.base   = Network base address (uint32)
pool[0]     = gateway IP (reserved, ASSIGN_FIXED)
pool[1..N]  = client slots; each = { used, atNet, atNode, lastSeen }
IP(slot i)  = pool.base + i
```

### 4.1 Static assignment algorithm

```
assign_address(requested, atNet, atNode):
    # 1. Reuse: same AppleTalk endpoint already has a lease → return it (refresh lastSeen)
    for i in 1..N:
        if pool[i].used and pool[i].atNet==atNet and pool[i].atNode==atNode:
            pool[i].lastSeen = now; return IP(i), ok

    # 2. Honour a specific requested IP if it is in range and free
    if requested != 0.0.0.0:
        idx = requested - pool.base
        if 1 <= idx < N and not pool[idx].used:
            pool[idx] = {used, atNet, atNode, now}; return requested, ok

    # 3. Otherwise allocate the next free slot
    for i in 1..N:
        if not pool[i].used:
            pool[i] = {used, atNet, atNode, now}; return IP(i), ok

    return 0.0.0.0, FAIL          # pool exhausted → failure TResp
```

Assignment is **deterministic and sticky**: a given Mac keeps the same IP across
re-binds as long as its lease has not expired.

### 4.2 Lease lifetime

- Every inbound **IP data** packet (§5) from a Mac refreshes its lease's
  `lastSeen`.
- A periodic sweep (every 30 s here) evicts any lease unseen for longer than the
  idle timeout (5 minutes), returning its slot to the pool.
- A `MACIP_SERVER` (function 3) request also reuses/refreshes the existing lease.

> The reference `macipgw` instead validates leases with periodic ICMP echo probes
> (30 s timeout, ~10 retries before release). ClassicStack uses passive
> last-seen aging, which avoids generating probe traffic. Either is conformant;
> the choice only affects how quickly a vanished client's address is reclaimed.

### 4.3 Pool sizing

The default pool is 254 host slots. The reference derives the size from the
subnet: `hosts = ~mask - 1` (all host addresses except network/broadcast/gateway).
Either is acceptable; the only invariant is that slot 0 (gateway) is never leased.

---

## 5. IP data transport (DDP type 22)

Once a Mac has a lease it tunnels raw IPv4 packets to the gateway and receives
them back, each wrapped in one DDP datagram of type 22 on socket 72.

```
DDP (type 22, socket 72→72): [ complete IPv4 packet, header + payload ]
```

There is **no MacIP header on data packets** — the DDP payload *is* the IP
packet, starting at the IPv4 version/IHL byte. The IPv4 total-length field is
authoritative.

### 5.1 Outbound (Mac → IP world)

```
on DDP type 22 on socket 72:
    if len(DDP.Data) < 20: drop            # not even an IPv4 header
    dstIP = IPv4(DDP.Data[16:20])
    refresh lease lastSeen for (SrcNetwork, SrcNode)

    if dstIP is leased to another Mac:      # intra-pool: deliver over AppleTalk
        route_ip_to_mac(thatMac, DDP.Data)  # §5.3, no IP-side trip
        return
    if egress is wired:
        egress.SendIP(DDP.Data)             # hand to the IP-side adapter (§6)
    else:
        drop                                 # AppleTalk-only mode: nowhere to go
```

### 5.2 Inbound (IP world → Mac)

The IP-side adapter captures packets destined for a leased client IP and calls
back into the gateway:

```
on inbound IPv4 packet from egress:
    if len < 20: drop
    dstIP = IPv4(packet[16:20])
    (atNet, atNode) = lease owner of dstIP   # static or external table
    if none: drop                            # not one of ours
    route_ip_to_mac((atNet,atNode), packet)
```

### 5.3 Wrapping an IP packet for a Mac

```
route_ip_to_mac(atNet, atNode, pkt):
    if not valid_unicast(atNet, atNode): drop
    router.Route(DDP{
        DestNetwork=atNet, DestNode=atNode, DestSocket=72,
        SrcSocket=72, DDPType=22, Data=pkt
    }, originating=true)
```

### 5.4 Fragmentation

An IPv4 packet larger than the 586-byte DDP MTU must be IP-fragmented before it
is wrapped (one fragment per DDP datagram), unless the DF bit is set (then it is
dropped, and ideally an ICMP "fragmentation needed" is returned). In
ClassicStack this is an **egress (adapter) concern** performed before the inbound
callback; the core forwards whatever it is handed unchanged. An implementor that
keeps both halves together must fragment here.

---

## 6. IP-side egress (engineering reference, not wire protocol)

How the gateway connects the leased addresses to the real IP network is a local
choice; nothing here is visible to the Mac. ClassicStack offers three modes over
a raw-Ethernet (libpcap) link. They are summarised so an implementor knows the
problem space.

### 6.1 Bridge mode (proxy ARP)

Clients get IPs on an **existing** subnet. The gateway:

- answers ARP requests for any leased client IP with its **own** MAC (proxy ARP),
  so the segment sends the client's traffic to the gateway;
- sends a **gratuitous ARP** when a lease is created, priming peers' caches;
- injects the Mac's outbound IP straight onto the wire (next hop = dest if
  on-subnet, else the default gateway), resolving the next-hop MAC via ARP;
- captures inbound frames whose dest IP is a leased client and tunnels them back.

Return routing requires the rest of the network to reach the MacIP subnet (proxy
ARP handles the local segment; off-segment needs a host route).

### 6.2 NAT mode

Off-subnet client traffic is forwarded through the **host OS network stack**
(real sockets) so the host's own IP is the NAT source — no host route needed.
ICMP echo to the *gateway IP itself* is answered locally. Replies are reassembled
and delivered back through the inbound callback.

### 6.3 DHCP-relay mode

Instead of a static pool, the gateway obtains each client's address by performing
DHCP on the IP-side network on the Mac's behalf:

```
AssignIP(atNet, atNode, requested):           # called in place of the static pool
    fabMAC = 02:00:00 : atNet_hi : atNet_lo : atNode    # stable per-Mac MAC
    DISCOVER (xid, fabMAC, requested-ip-option?)  → broadcast UDP 68→67
    on OFFER  (matched by xid): REQUEST (offered, serverID)
    on ACK:    extract yiaddr + options (mask, router, DNS, broadcast, lease)
    gratuitous-ARP the assigned IP; adopt any DHCP-supplied default gateway
    return { ip, nameserver, broadcast, subnet } to fill the TResp (§3.4)
    on NAK / timeout (10 s): no reply → the Mac retries
```

The fabricated MAC uses the locally-administered OUI `02:00:00` followed by the
3-byte AppleTalk address, giving each Mac a **stable** identity so the DHCP
server hands back the same lease across reconnects. The resulting lease is
recorded in an "external" table (it may fall outside the static range) so inbound
IP for it still routes to the right Mac.

---

## 7. Implementation notes

- One socket (72) serves both roles; the DDP type byte (3 vs 22) is the
  demultiplexer. Drop other DDP types on socket 72.
- The config response is an ATP TResp with `EOM` set, sequence 0, the echoed
  transaction id, and the 28-byte config data block; unset config fields fall
  back to the gateway defaults.
- Leases are keyed by AppleTalk (network, node); normalise a net-0 source to the
  receiving port's network before keying.
- Intra-pool traffic (one leased Mac to another) is delivered directly over
  AppleTalk without an IP-side round trip.
- The core never opens a socket or links libpcap; the IP side is injected through
  an `IPEgress` seam, and DHCP-relay assignment through an optional
  `AddressAssigner` capability on that seam. An AppleTalk-only build (no egress)
  still answers discovery and assignment — data simply has nowhere to go.

## 8. References

- IPv4 datagram format: standard (RFC 791); the DDP payload is a verbatim IPv4 packet.
- ATP: [10-asp.md](10-asp.md) (ASP rides ATP; same ATP framing) and DDP type 3.
- NBP: [02-nbp.md](02-nbp.md).
- DDP fields & well-known sockets/types: [00-overview.md](00-overview.md).
- Reference C implementation: Stefan Bethke's `macipgw`
  (https://github.com/jasonking3/macipgw).
- ClassicStack: [core/service/macip](../core/service/macip) (AppleTalk + pool),
  [adapter/macipgw](../adapter/macipgw) (IP-side egress).
