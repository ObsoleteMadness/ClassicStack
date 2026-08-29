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

The gateway registers **only its own `IPGATEWAY` name**. It does **NOT** register an
`IPADDRESS` NBP name for a client's leased address: per draft §3.2.2.4 the MacIP
**host** registers `<ip>:IPADDRESS@*` for its OWN address, and a gateway registration
would *shadow* it.

> **Wire-confirmed regression (fixed).** An earlier build DID register
> `<leasedIP>:IPADDRESS` per lease. On the client's first boot it worked, but after the
> Mac **rebooted** and re-leased the same address, the Mac's own NBP name-registration
> conflict check (a `LkUp` for `<ip>:IPADDRESS` before it registers) was answered by our
> stale name — so the Mac saw its address as already-in-use, aborted MacTCP
> initialisation, and looped `ASSIGN → SERVER → ASSIGN` forever. The capture
> `ltoudp-netboot.pcap` shows two `192.168.100.2:IPADDRESS` entries (the Mac's and
> ours). It also violated §3.8 ("NBP Proxy ARP MUST NOT respond to wildcard `IPADDRESS`
> lookups"), since a real registered name answers `=:IPADDRESS@*`. *Our own bug, not
> errata.*

The gateway's legitimate NBP-ARP roles do not need this registration: the Confirm loop
(§3.8.2) and the startup reregistration search (§3a / §3.7) both **probe** for the
HOSTS' own registrations, and NBP Proxy ARP answers only SPECIFIC delivery lookups. See
[core/service/macip](../core/service/macip) — `registerLeaseName` /
`unregisterLeaseName` are now no-ops.

---

## 3. Configuration exchange (ATP, DDP type 3)

Address assignment and server liveness use **ATP** (AppleTalk Transaction
Protocol) request/response on socket 72. The Mac is the ATP *requester*; the
gateway is the *responder*.

### 3.1 ATP framing

Only the single-packet transaction form is used. The DDP payload of an ATP
datagram begins with the **8-byte ATP header** (control, bitmap/seq, a 16-bit
transaction id, and **4 ATP user bytes**). The MacIP `macip_req_control` struct
**straddles the ATP header/data boundary**: its `mipr_version`/`_mipr_pad1` half
occupies the ATP **user bytes** (`Data[4:8]`), and `mipr_function` is the first 4
bytes of the ATP **data** (`Data[8:12]`):

```
DDP.Data layout (DDP type 3):
  +0  ATP control byte    (1)   function in top 2 bits: TReq=0x40, TResp=0x80; EOM=0x10
  +1  ATP bitmap / seq    (1)
  +2  ATP transaction id  (2)   big-endian; echoed in the response
  +4  ATP user bytes      (4)   = mipr_version(2) + _mipr_pad1(2)
  +8  ATP data ...              ← mipr_function(4), then mipr_ipaddr(4), …
```

> **Wire-verified (a real MacTCP client).** The captured request bytes place
> `mipr_function` at the START of the ATP data (`Data[8:12]` — e.g. `00 00 00 01`
> = MACIP_ASSIGN), with `mipr_version`/`_mipr_pad1` in the ATP user bytes
> (`Data[4:8]`). An earlier reading assumed the WHOLE control struct
> (`version+pad+function`) sat in the ATP data and read `function` at `Data[12:16]`
> — that mis-parsed every real request as an unknown function (e.g. `0x00010000`),
> so no client could ever get a configuration and MacTCP would not start. *Our own
> bug is not errata.*
>
> **The REPLY always sets `mipr_version` = 1 and `_mipr_pad1` = 0** in its user bytes
> (`Data[4:8] = 00 01 00 00`), matching `macipgw` (which sets `macip_req.version` on
> every reply). It does **not** echo the request's user bytes: a real MacTCP client
> sends arbitrary bytes there (observed `00 1a dd fc`) and reads the version back FROM
> the reply — echoing the client's junk (`0x001a`) made MacTCP treat the config as a
> version mismatch and refuse to bring up its stack (also our own bug, not errata).

```c
struct macip_req_control {        // big-endian on the wire
    int16  mipr_version;          // ATP user bytes +0 : protocol version (1)
    int16  _mipr_pad1;            // ATP user bytes +2 : reserved / zero
    int32  mipr_function;         // ATP data       +0 : MACIP_ASSIGN(1) | MACIP_SERVER(3)
};
```

On an **assign** request the Mac may append a *data* block (in the ATP data,
after `mipr_function`) requesting a specific IP (other fields normally zero):

```c
struct macip_req_data {           // ATP data, following mipr_function
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
    if len(DDP.Data) < 8 + 4: drop           # need 8-byte ATP header + mipr_function(4)
    hdr = atp.Decode(DDP.Data)               # control, bitmap, tid, 4 user bytes
    if hdr.FuncCode() != TReq: drop           # only requests are processed here
    tid       = hdr.TransID                    # echoed in the response
    userBytes = hdr.UserData                   # version/pad; echoed in the response (§3.1)
    macReq    = DDP.Data[8:]                    # ATP data: mipr_function then mipr_ipaddr …
    function  = be32(macReq[0:4])              # mipr_function (first 4 bytes of ATP data)
    requested = (len(macReq) >= 8) ? IPv4(macReq[4:8]) : 0.0.0.0

    atNet, atNode = source of the datagram   # see §3.5 for net-0 normalisation
    if not valid_unicast(atNet, atNode): drop
```

### 3.3 Function handling

```
switch function:
  case MACIP_SERVER (3):           # "are you still there?" / re-bind probe
      refresh the lease for (atNet,atNode) if one exists   # a liveness signal (§4.2)
      reply TResp: function=MACIP_SERVER, first IP address = 0.0.0.0

  case MACIP_ASSIGN (1):           # "give me an IP"
      ip, ok = assign_address(requested, atNet, atNode)    # §4
      if ok:  reply TResp: function=MACIP_ASSIGN, first IP address = ip
      else:   reply TResp: function=MACIP_ERROR, first IP = 0, error = "No Address Available."

  default:                         # unrecognised function code
      reply TResp: function=MACIP_ERROR, first IP = 0, error = "Unknown Operation."
```

**The only wire difference between an ASSIGN response and a SERVER/ERROR response
is the first IP address:** ASSIGN carries the assigned value there; SERVER and
ERROR leave it `0.0.0.0` (issue #17, observed of Shiva Fastpath 5 / K-STAR and
Apple IP Gateway). All three carry the *full* config data block (§3.4) — the
nameserver and broadcast are the only fields the client actually uses; the rest can
be anything, and Apple IP Gateway sets the 5th address to the subnet mask.

### 3.4 Response packet (ATP TResp, DDP type 3)

The response reuses socket 72 and DDP type 3, reverses the DDP source/dest, echoes
the transaction id and the ATP user bytes (§3.1), and carries the MacIP control
struct (version = 1, `function`) followed by the **full config data block with
space for all eight IP addresses** — the same length in every reply type. This
mirrors `macipgw` after njroadfan's "send back a complete config packet" fix
(`sizeof(struct macip_req) - 21` = 41 bytes of MacIP data on success):

Like the request (§3.1), `mipr_version`/`_pad1` ride the echoed ATP **user bytes**
and `mipr_function` is the first 4 bytes of the ATP **data** — so the ATP-data
portion is 37 bytes (`function(4) + 32-byte address block + 1 NUL`) and the wire
packet is `8-byte ATP header + 37 = 45 bytes` on success:

```
TResp DDP.Data layout (8-byte ATP header + 37-byte MacIP data = 45 bytes on success):
  +0  ATP control byte = TResp(0x80) | EOM(0x10)
  +1  ATP seq          = 0
  +2  ATP tid          = echoed request tid                 (be16)
  +4  ATP user bytes   = echoed; version stamped if 0        (4)   ← mipr_version/_pad1 (§3.1)
  +8  mipr_function    = MACIP_ASSIGN(1)/SERVER(3)/ERROR(-1) (be32)   # first bytes of ATP data
  +12 assigned IP      (4)   # the value for ASSIGN; 0.0.0.0 for SERVER and ERROR
  +16 nameserver       (4)   # the client actually uses this
  +20 broadcast        (4)   # …and this
  +24 _pad2            (4)   # 4th address (unused by the client)
  +28 subnet mask      (4)   # 5th address (Apple IP Gateway convention)
  +32 _pad3/_pad4/_pad5 (12) # 6th–8th addresses (unused)
  +44 error[]          (≤22) # first byte NUL on success; NUL-terminated string on ERROR
```

On an **ERROR** response the NUL-terminated error string is written into the
`error[]` field and the packet is lengthened by `len(str)` beyond the 45-byte base
(so `MACIP_ERROR` replies run longer than success/SERVER replies).

> **Draft vs. reference.** The MacIP-02 draft (§3.8.8) draws the data field as
> Assigned IP + Name Server + Broadcast + File Server + 16 bytes of "Other" (a
> 32-byte / eight-address block) followed by a 128-byte error field, giving a
> nominal 64-byte data length. ClassicStack follows the `macipgw` reference
> struct instead — the same eight-address block but a compact 22-byte `error[]`
> field (`sizeof(struct macip_req) - 21 = 41` bytes of MacIP data on success) —
> because that is what real Netatalk/`macipgw`-interoperating clients expect. The
> eight-address block and the "first IP only in ASSIGN" rule are identical either
> way; only the error-field capacity differs. Any config field
the assignment did not override (nameserver / broadcast / subnet mask) falls back to
the gateway's configured defaults before being written. All IPv4 values are in
network byte order.

```
build_TResp(reqHdr, fn, cfg):
    ns   = cfg.nameserver or DEFAULT_nameserver
    bc   = cfg.broadcast  or DEFAULT_broadcast
    mask = cfg.subnet     or DEFAULT_subnet
    emit ATP header (TResp|EOM, tid=reqHdr.tid, user bytes per §3.1)
    emit macip_req: version=1, function=fn, full 32-byte data block
    if fn == MACIP_ASSIGN: first IP = cfg.ip   # SERVER/ERROR leave it 0.0.0.0
    if fn == MACIP_ERROR:  append NUL-terminated error string into error[]
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

## 3a. Startup reregistration (draft §3.2.4.4 / §3.7)

The gateway is responsible for handing out **unique** IP addresses. If it restarts
or crashes while clients hold leases, it must not reassign those addresses to other
hosts. On startup — before it begins assigning — it therefore searches NBP for the
addresses already registered by live MacIP hosts (and by any peer gateway) and
seeds its pool with the ones in its range:

```
on gateway start (once NBP is available):
    ents = NBP_lookup(object="=", type="IPADDRESS", zone=configured-zone)   # a BrRq; collect LkUp-Rply over a window
    for ent in ents:
        ip = parse_dotted(ent.object)          # the NBP object name IS the IP (§2)
        if ip is not parseable: continue
        if ip == GatewayIP: continue            # our own IPGATEWAY identity, not a client lease
        if ip is inside the assignable pool range and currently free:
            claim ip for ent.(network,node)     # pool.assign(ip, atNet, atNode) — reserve it
            # do NOT register the name: the HOST that answered owns "<ip>:IPADDRESS" (§3.2.2.4)
```

Because it needs an NBP **requester** (broadcast a BrRq, collect replies over a
fixed window — NBP has no "no more" signal), this rides the core NBP service's
`Lookup()`; the MacIP gateway runs the search on its own goroutine so `Start`
does not block for the collection window. A discovered address outside the pool
range, or the gateway's own IP, is ignored; an address already leased to the same
endpoint is a no-op.

> **What answers the search.** On a live network the `=:IPADDRESS@*` lookup is
> answered by the **MacIP hosts themselves** (each registers `<ip>:IPADDRESS@zone`
> per §3.2.2.4) and by any peer gateway that registers its range (§3.2.4.3) — and,
> after this change, by our own gateway for the leases it re-publishes. Note the
> draft (§3.7) says a *synthetic* NBP-proxy-ARP responder MUST NOT answer wildcard
> `IPADDRESS` lookups (it would flood the whole range); that restriction is on the
> proxy responder, not on answering with the concrete, individually-registered
> names, which is what the core NBP responder does.

> **Node-address stability (draft §3.2.4.4).** The draft also asks that the gateway
> keep the **same AppleTalk node** across restarts, since hosts cache it. In
> ClassicStack the AppleTalk node is owned by the LLAP/AARP node-claim of the
> underlying port, not by MacIP; node-address persistence is a transport concern
> tracked there, not in this service.

---

## 4. Address assignment & the lease pool

The gateway owns the **Dynamic Range** (draft §3.2.3 / §3.8.2): a contiguous pool
of IPv4 addresses on its IP-side subnet it can ASSIGN to clients. Slot 0 is the
gateway's own IP and is never handed out; slots 1..N are client leases. Each entry
mirrors the draft's table row — `{ IP address; timer; flags; AppleTalk address }`:

```
pool.base   = Network base address (uint32)
pool[0]     = gateway IP (reserved, ASSIGN_FIXED)
pool[1..N]  = client slots; each = { used, atNet, atNode, lastSeen, freedAt, missed }
IP(slot i)  = pool.base + i
```

### 4.1 Static assignment algorithm

Assignment follows draft §3.8.2: reuse the same IP for the same AppleTalk address
if possible; otherwise pick the **oldest unused** entry; and **resolve the chosen
address via NBP ARP** before handing it out, so a live host already using it is not
double-assigned.

```
assign_address(requested, atNet, atNode):
    # 1. Reuse: same AppleTalk endpoint already has a lease → return it (refresh timer). No probe.
    for i in 1..N:
        if pool[i].used and pool[i].atNet==atNet and pool[i].atNode==atNode:
            pool[i].lastSeen = now; return IP(i), ok         # reuse — the client already owns it

    loop (bounded retries):
        # 2. Honour a specific in-range free requested IP, else…
        # 3. …allocate the OLDEST-freed free slot (draft: "locate the oldest unused table entry";
        #    a never-used slot sorts oldest). Skip slots a prior probe flagged in-use (conflicts).
        cand = pick_requested_or_oldest_free()
        if none: return 0.0.0.0, FAIL                        # Dynamic Range exhausted → MACIP_ERROR

        # 4. NBP-ARP probe (draft §3.8.2 "registered and resolved using NBP ARP"):
        #    look up "<cand>:IPADDRESS@zone". A reply from a node OTHER than the requester means
        #    a live host holds it → record a conflict, free the tentative slot, and retry.
        if nbp_lookup(cand, IPADDRESS, zone) answered by some node != (atNet,atNode):
            note_conflict(cand); continue
        register(cand, IPADDRESS, zone, 72); return cand, ok  # publish + hand out
```

Assignment is **deterministic and sticky**: a given Mac keeps the same IP across
re-binds as long as its lease survives, and the oldest-unused pick maximises the
chance a returning Mac finds its previous address still free. The NBP-ARP probe
blocks briefly, so the gateway runs each assignment on its own goroutine (like the
DHCP path) rather than on the datagram read loop.

### 4.2 Lease lifetime — active NBP ARP Confirm (draft §3.8.2)

Once a lease is handed out, the gateway keeps it alive by the draft's **Confirm
Period** echo rather than by passive aging alone:

- Every **Confirm Period** (60 s) the gateway sends an **NBP ARP Confirm** — an NBP
  lookup of the lease's `<ip>:IPADDRESS@zone`. A reply **from the lease's own
  AppleTalk node** restarts its timer and clears its miss counter.
- After **5** consecutive Confirm Periods with no reply (~300 s), the entry is
  reclaimed and its `IPADDRESS` name withdrawn — the slot becomes the oldest-freed
  candidate for the next assignment.
- Inbound **IP data** (§5) and a `MACIP_SERVER` probe are *also* liveness signals:
  each refreshes the timer and clears the miss counter, so a chatty client is never
  reclaimed by a lost Confirm.

> The active NBP-ARP model needs the NBP service wired (to probe). Without it the
> gateway falls back to **passive last-seen aging**: a 30 s sweep evicts any lease
> unseen for 5 minutes. External/DHCP-relayed leases always age passively. The
> reference `macipgw` uses ICMP-echo probing instead of NBP ARP; all three
> reclaim a vanished client on roughly the same horizon.

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

Bridge mode injects Ethernet frames (proxy-ARP replies and IP datagrams) sourced
from `host_mac`. That works on wired Ethernet. **On WiFi use NAT mode** (§6.2):
access points drop frames not sourced from the associated NIC MAC, and extra ARP
identities for leased client IPs are not reliable. ClassicStack logs a warning
when bridge mode starts.

### 6.2 NAT mode

Off-subnet client traffic is forwarded through the **host OS network stack**
(real sockets) so the host's own IP is the NAT source — no host route needed.
ICMP echo to the *gateway IP itself* is answered locally. Replies are reassembled
and delivered back through the inbound callback.

NAT-only (`mode = "nat"` and `dhcp_relay = false`) does **not** open a pcap
handle: there is nothing to inject. That is the WiFi path (Mac laptop, Npcap).
`dhcp_relay` still needs pcap and fabricates per-Mac MACs (`02:00:00:…`) that
WiFi APs will drop — leave it false on wireless.

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
server hands back the same lease across reconnects. Those fabricated MACs are
dropped by WiFi APs — do not enable `dhcp_relay` on wireless (use NAT mode with
the static pool instead). The resulting lease is
recorded in an "external" table (it may fall outside the static range) so inbound
IP for it still routes to the right Mac.

---

## 7. Implementation notes

- One socket (72) serves both roles; the DDP type byte (3 vs 22) is the
  demultiplexer. Drop other DDP types on socket 72.
- The config response is an ATP TResp with `EOM` set, sequence 0, the echoed
  transaction id and ATP user bytes, and the full 33-byte config data block (space
  for all eight IP addresses + a leading NUL error byte, 41 bytes of MacIP data on
  success); unset config fields fall back to the gateway defaults. SERVER and ERROR
  responses zero the first IP address; ERROR responses append a NUL-terminated
  string. See issue #17.
- Leases are keyed by AppleTalk (network, node); normalise a net-0 source to the
  receiving port's network before keying.
- Intra-pool traffic (one leased Mac to another) is delivered directly over
  AppleTalk without an IP-side round trip.
- The core never opens a socket or links libpcap; the IP side is injected through
  an `IPEgress` seam, and DHCP-relay assignment through an optional
  `AddressAssigner` capability on that seam. An AppleTalk-only build (no egress)
  still answers discovery and assignment — data simply has nowhere to go.
- Each lease is published as an `IPADDRESS@zone` NBP name on socket 72 and withdrawn
  on expiry (§2); on startup the gateway runs the reregistration search (§3a) via the
  core NBP service's `Lookup()` requester to reclaim addresses live hosts still hold.
- The core NBP responder/requester decodes only single-tuple NBP packets, so the
  reregistration search sees one address per responder (our own responders and MacIP
  hosts emit single-tuple replies). A foreign gateway that packs several `IPADDRESS`
  tuples into one LkUp-Rply would be under-read — an existing whole-service limitation,
  not specific to reregistration.

## 8. References

- IPv4 datagram format: standard (RFC 791); the DDP payload is a verbatim IPv4 packet.
- ATP: [10-asp.md](10-asp.md) (ASP rides ATP; same ATP framing) and DDP type 3.
- NBP: [02-nbp.md](02-nbp.md).
- DDP fields & well-known sockets/types: [00-overview.md](00-overview.md).
- Protocol draft: *A Standard for the Transmission of Internet Packets over
  AppleTalk Networks* — `draft-ietf-appleip-MacIP-02` (see
  [draft-ietf-appleip-MacIP-02.txt](draft-ietf-appleip-MacIP-02.txt) in this
  folder), §3.8 for the address-assignment (MacIPGP) packet format.
- Reference C implementation: Stefan Bethke's `macipgw`
  (https://github.com/jasonking3/macipgw), including njroadfan's Netatalk fix
  "macipgw: Send back a complete config packet"
  (https://github.com/Netatalk/netatalk/commit/77c587e1523aef1179d5c9b34752754eb3665914),
  which corrected the config reply to always carry space for eight IP addresses.
  Thanks to **njroadfan** for the wire-format observations behind issue #17.
- ClassicStack: [core/service/macip](../core/service/macip) (AppleTalk + pool),
  [adapter/macipgw](../adapter/macipgw) (IP-side egress).
