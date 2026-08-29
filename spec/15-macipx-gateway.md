# MacIPX Gateway — DDP Protocol 0x4E

> **OBSERVED, not specified.** Novell never published the wire format used between the Macintosh MacIPX control panel and `MACIPXGW.NLM`. The protocol described here was reverse-engineered from packet captures of Mac OS MacIPX clients (LocalTalk and EtherTalk) against NetWare 3.x and 4.x servers running `MACIPXGW.NLM`. Any future deviation should be added below and noted in [errata.md](errata.md).

## Discovery

The gateway advertises itself in NBP with type `IPX Gateway`. The zone is operator-configurable; deployments observed in the wild use `Novell Network` or `Netware Network` interchangeably. The object name is the gateway server's name (e.g. the NetWare bindery server name).

```
NBP BrRq   =:IPX Gateway@<zone>           (client → broadcast)
NBP LkReply <server>:IPX Gateway@*        (gateway → client)
```

The NBP reply carries the gateway's DDP address with port **78 (0x4E)** — the same number as the DDP protocol type the gateway uses (see below).

## DDP framing

| Field | Value |
|---|---|
| DDP protocol type | **0x4E (78)** |
| Source socket | **78 (0x4E)** |
| Destination socket | **78 (0x4E)** |
| Long DDP | required |

Both sides use socket 78; there is no asymmetric well-known socket pairing. The DDP protocol type byte and the socket number coincide; both are 0x4E.

## Sub-protocol

The first byte of the DDP payload is an opcode. Subsequent bytes depend on the opcode.

| Opcode | Direction | Payload after opcode | Meaning |
|---|---|---|---|
| `0x00` | both | full 30-byte IPX header + IPX payload | Encapsulated IPX datagram. |
| `0x10` | client → gw | one or more 8-byte `(node 6B, socket 2B)` pairs | Register broadcast listens. The client tells the gateway which IPX sockets it wants broadcast traffic forwarded for; the node field is always the IPX broadcast address (`FF:FF:FF:FF:FF:FF`) in observed traffic. A single 0x10 frame may carry multiple pairs. |
| `0x20` | client → gw | 6-byte request blob (observed value `00 02 00 00 00 01`) | Address-assignment request. |
| `0x23` | gw → client | request blob echo (6B) + assigned IPX node low 3 bytes | Address-assignment reply. The full IPX node is `MacIPXNodePrefix` (`7A:00:00`) concatenated with the 3 reply bytes. |

### Address-assignment handshake

Both NetWare 3.x and 4.x gateways perform the same `0x20` / `0x23` exchange before a Mac starts emitting IPX. The 6-byte request blob has not been fully reverse-engineered; the gateway echoes it back unchanged in the reply.

```
client (DDP a.b:78) → gw (DDP c.d:78)  DDP-type 0x4E
    payload: 20 00 02 00 00 00 01
             ^^ opcode 0x20 (request)
                ^^^^^^^^^^^^^^^^^ 6-byte request blob (echoed in reply)

gw (DDP c.d:78) → client (DDP a.b:78)  DDP-type 0x4E
    payload: 23 00 02 00 00 00 01 NN NN NN
             ^^ opcode 0x23 (reply)
                ^^^^^^^^^^^^^^^^^ request blob, echoed
                                  ^^^^^^^^ low 3 bytes of the assigned
                                           IPX node; the full node is
                                           7A:00:00:NN:NN:NN
```

The assigned low 3 bytes encode the client's DDP address:

```
assigned-low-3 = 00 : (AT_network_low_byte) : (AT_node)
full IPX node  = 7A : 00 : 00 : 00 : (AT_network_low_byte) : (AT_node)
```

Examples:

| AT source | Assigned IPX node |
|---|---|
| net 1, node 1 | `7a:00:00:00:01:01` |
| net 3, node 0x3E | `7a:00:00:00:03:3e` |

Because the encoding is deterministic, ClassicStack can derive the same IPX node from the DDP source address without consulting any per-client table; see `macipx.AssignedNodeForDDP`.

> ⚠️ Only the **low byte** of the AT network is encoded, so two MacIPX clients on different AT networks whose numbers share their low byte would collide on the IPX side. Real NetWare appears to live with this; the practical impact is small (most deployments expose MacIPX on a single AT network).

The IPX network number is **not** carried in the reply. The Mac learns it from RIP/SAP traffic relayed by the gateway. In observed deployments the IPX network served by `MACIPXGW.NLM` is `0x00000010`, but this is configured on the NetWare side and the gateway must not assume any particular value.

### Encapsulated IPX (opcode 0x00)

The remainder of a 0x00 frame is a complete standard IPX datagram — checksum, length, hops, type, addresses, payload — exactly as it would appear on a raw IPX wire. There is no extra length prefix, no padding; the IPX header's own length field is authoritative.

```
DDP payload: 00 | <30-byte IPX header> | <IPX payload>
             ^^ opcode 0x00
                ^^^^^^^^^^^^^^^^^^^^^^^ standard IPX datagram
```

Example — a RIP request a Mac sends right after the handshake:

```
00  ff ff           IPX checksum (none)
    00 28           IPX length 40
    00 01           hops 0, type 1 (RIP)
    00 00 00 00     dst net 0 (local, unknown)
    ff ff ff ff ff ff  dst node (broadcast)
    04 53           dst sock 0x0453 (RIP)
    00 00 00 00     src net 0
    7a 00 00 00 01 01  src node (assigned by handshake)
    40 00           src sock 0x4000 (dynamic)
    00 01 ff ff ff ff ff ff ff ff  RIP request body
```

The client may emit IPX with `src net = 0` until it has learnt the real network number from a RIP reply. The gateway must forward this unchanged — overwriting `src net` would confuse the conversation.

### Network discovery via RIP — GOLDEN (captures/nw41-macipxgw.pcapng)

> **Golden**, captured from a real **NetWare 4.1** server running `MACIPXGW.NLM` against a Mac OS MacIPX client (`captures/nw41-macipxgw.pcapng`). Per CLAUDE.md #5 this observed behaviour is authoritative over the mars_nwe-derived assumptions where they differ.

Immediately after the `0x20`/`0x23` register handshake the Mac broadcasts a **RIP Request for the wildcard network** (`0xFFFFFFFF`) from `src net = 0`, and the gateway answers with a **RIP Response**. The Mac adopts the network number from the **IPX header source network of that response** (not from any single RIP entry) — every frame the Mac sends afterwards carries that network. This is the mechanism by which the operator's configured IPX network reaches the client; the register reply never carries it.

Golden exchange (client assigned `7a:00:00:00:03:3e`, gateway MacIPX network `0x00000010`):

```
Mac → gw   RIP Request (frame 18)
  00 01                 RIP op = Request
  ff ff ff ff           entry network = wildcard (ask for all routes)
  ...                   IPX src net = 0, src node = 7a:00:00:00:03:3e, src sock = 0x4000

gw → Mac   RIP Response (frame 19)   IPX src NET = 00 00 00 10, src node = 00:00:00:00:00:01, src sock = 0x0453
  00 02                 RIP op = Response
  be ef ca fe 00 02 00 07   network 0xbeefcafe  hops 2  ticks 7   (one hop further)
  00 00 00 03 00 01 00 06   network 0x00000003  hops 1  ticks 6   (directly served)
  05 29 48 c3 00 01 00 06   network 0x052948c3  hops 1  ticks 6
  b3 e0 b8 70 00 01 00 06   network 0xb3e0b870  hops 1  ticks 6
  6a 09 18 3d 00 01 00 06   network 0x6a09183d  hops 1  ticks 6
```

Observations that shape the implementation:

- **The response is addressed FROM the gateway's MacIPX network (`0x10`) and node `00:00:00:00:00:01`.** The Mac keys on this source network, so the gateway MUST stamp the reply's IPX source network with the network it announces. (ClassicStack does this by giving the IPX mini-router that network as its wire identity, so `router.Send` fills the source network on the responder's reply.)
- **The gateway answers the wildcard with its full route table** (every network it knows), not just its own. A minimal gateway that returns only its own network still lets the Mac learn its network (the source-network field is what matters), but a complete table matches real MACIPXGW.
- **Metric over MacIPX is hops 1 / ticks 6 for a directly-served network** — note the *native* RIP on the same wire advertised the same networks at ticks 2 (see the `ipxrip` frames); the larger tick count over MacIPX reflects the added LToUDP/DDP hop cost. The Mac does not key on the exact tick value. ClassicStack's RIP responder advertises hops 1 / ticks 2 (mars_nwe `ins_rip_buff`), which the client accepts.
- The gateway answers **every** RIP request, not just the first (the golden capture shows 4 request/response pairs during one session).

### Listen / register-socket (opcode 0x10)

Used by the client to tell the gateway which IPX sockets it wants *broadcast* traffic forwarded for. Unicast IPX addressed to the client's assigned node already reaches the gateway via per-node dispatch, so 0x10 is only relevant for broadcasts.

```
DDP payload: 10 | <pair 1> | <pair 2> | ...
             ^^ opcode 0x10
each pair = <6-byte node> <2-byte big-endian IPX socket>
```

Examples:

```
Single registration (NetWare diagnostic, socket 0x0456):
    10 ff ff ff ff ff ff 04 56

Two registrations in one frame (diagnostic + Duke3D on 0xDEAD):
    10 ff ff ff ff ff ff 04 56 ff ff ff ff ff ff de ad
```

ClassicStack records the listened sockets per client and **fans out** every inbound broadcast IPX whose destination socket is in any client's listen set. The originating client (if it is itself a MacIPX peer) is skipped so a client's own broadcast is not reflected back to it.

## Implementation notes

- The gateway listens on DDP socket 78 with DDP type 0x4E. Other DDP types arriving on socket 78 are dropped.
- Outgoing encapsulated IPX is wrapped exactly as opcode `0x00` followed by the raw IPX bytes — no extra length, no padding.
- The gateway derives a deterministic IPX node from each client's DDP address via `macipx.AssignedNodeForDDP`. It learns the `IPX node → DDP addr` mapping from whichever event happens first — a `0x20` register request or a `0x00` data frame — and claims the IPX node on the native IPX router so inbound unicast IPX for that node is dispatched here and tunnelled back over DDP.
- The gateway is also registered as the IPX router's broadcast handler. Inbound broadcasts are fanned out only to MacIPX clients whose `0x10` listen set includes the destination socket.
- The gateway does **not** rewrite IPX source addresses on outbound traffic.

## References

- IPX datagram format: standard 30-byte header (see [protocol/ipx/datagram.go](../protocol/ipx/datagram.go)).
- NBP: see [03-nbp.md](03-nbp.md).
