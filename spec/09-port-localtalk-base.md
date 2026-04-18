# Base LocalTalk Port Specification

## Purpose

The base LocalTalk port implements the common logic shared by all LocalTalk transport implementations (LToUDP and TashTalk). It handles:

- LLAP (LocalTalk Link Access Protocol) frame parsing and construction
- Node address acquisition via LLAP ENQ/ACK
- Routing inbound frames to the router
- Sending outbound DDP datagrams as LLAP frames

The base port is never used directly; it is always wrapped by a concrete transport (LToUDP wraps it with UDP; TashTalk wraps it with serial). The transport provides a `sendFrame(frame []byte)` callback.

---

## Network Characteristics

LocalTalk is a **non-extended network**: it has a single network number (not a range). Correspondingly:

- `NetworkMin == NetworkMax == Network` (all three return the same value)
- `ExtendedNetwork()` returns `false`
- `SetNetworkRange(min, max)` only accepts `min == max`; ranges are silently ignored

---

## LLAP Frame Format

All LocalTalk frames use the LLAP header:

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 1 byte | Destination node | 0xFF = broadcast |
| 1 | 1 byte | Source node | Sending node's address |
| 2 | 1 byte | LLAP type | See below |
| 3+ | variable | Payload | Present for DDP types only |

### LLAP Type Codes

| Value | Name | Payload |
|---|---|---|
| `0x01` | Short-header DDP | DDP short-header datagram |
| `0x02` | Long-header DDP | DDP long-header datagram |
| `0x81` | LLAP ENQ | None (3-byte frame only) |
| `0x82` | LLAP ACK | None (3-byte frame only) |

Minimum frame length is 3 bytes (header only, for ENQ/ACK). Minimum DDP frame is longer.

---

## Node Address Acquisition

LocalTalk nodes use a probe-and-claim algorithm to acquire a unique node address on the segment. The valid unicast range is 1–253 (0xFE); 0 is reserved and 0xFF is broadcast.

### Initial State

At construction:

- `desiredNode` is set to the preferred starting address (default `0xFE`).
- `desiredNodeList` is a shuffled list of all other addresses (1..253 minus `desiredNode`), used as fallbacks on collision.
- `node` (the claimed address) starts at 0 (unclaimed).

### Acquisition Algorithm

```
on 250ms timer tick (nodeRun goroutine):
  if nodeAttempts >= 8:
    claim node: node = desiredNode
    log "claiming node address N"
    stop timer, exit goroutine
  else:
    send LLAP ENQ frame:
      destination = desiredNode
      source = desiredNode  (self-addressed, a convention for ENQ)
      type = 0x81 (LLAP ENQ)
    nodeAttempts += 1
```

The node is claimed after 8 consecutive ENQs with no collision response (~2 seconds).

### Collision Detection

Collisions are detected in `InboundFrame()`:

**On receiving LLAP ENQ (type 0x81):**
```
if respondToEnq AND node != 0 AND frame.destination == node:
  // Someone is probing our claimed address: respond
  send LLAP ACK: destination = node, source = node, type = 0x82
else:
  // Someone probing while we haven't claimed yet
  if node == 0 AND frame.destination == desiredNode:
    // Collision: another node is probing our desired address
    rerollDesiredNode()
```

**On receiving LLAP ACK (type 0x82):**
```
if node == 0 AND frame.destination == desiredNode:
  // A node responded to our ENQ — collision
  rerollDesiredNode()
```

### Reroll Algorithm

```
rerollDesiredNode():
  nodeAttempts = 0
  if desiredNodeList is empty:
    rebuild: desiredNodeList = [1..253], shuffled
  desiredNode = pop last from desiredNodeList
```

The node address list is drawn from a shuffled pool to minimize deterministic conflicts between multiple routers starting simultaneously.

### respondToEnq Flag

- **LToUDP sets this to `true`:** On a shared simulated segment, all participants see all ENQ frames. A node that has already claimed an address must respond to ENQs for that address so new participants know it is taken.
- **TashTalk sets this to `false`:** The physical LocalTalk medium and hardware handle collision responses at the hardware level; the host does not need to respond.

---

## Inbound Frame Processing

```
InboundFrame(frame):
  if len(frame) < 3: discard

  dst = frame[0]
  src = frame[1]
  typ = frame[2]

  switch typ:
    case 0x01 (short-header DDP):
      parse DDP short-header datagram from frame[3..], with dst and src as node addresses
      deliver to router via router.Inbound(datagram, this_port)

    case 0x02 (long-header DDP):
      parse DDP long-header datagram from frame[3..], optionally verifying checksum
      deliver to router via router.Inbound(datagram, this_port)

    case 0x81 (LLAP ENQ):
      if respondToEnq AND node != 0 AND dst == node:
        send LLAP ACK (destination=node, source=node, type=0x82)
      else if node == 0 AND dst == desiredNode:
        rerollDesiredNode()

    case 0x82 (LLAP ACK):
      if node == 0 AND dst == desiredNode:
        rerollDesiredNode()
```

### DDP Short Header vs Long Header

**Short header** is used for frames where both source and destination are on the same network (network number is implicit). The LLAP dst/src bytes provide the node addresses; no network numbers are present in the DDP header.

**Long header** is used for frames crossing network boundaries. Network numbers are present in the DDP header and a checksum may be present.

The base port verifies checksums on inbound long-header frames and computes checksums on outbound long-header frames. Both behaviors can be configured at construction time.

---

## Outbound Frame Sending

### Unicast

```
Unicast(network, node, datagram):
  if network is specified AND network != port.network: drop
  if port.node == 0 (not yet claimed): drop

  if datagram.DestinationNetwork == datagram.SourceNetwork
     AND (DestinationNetwork == 0 OR DestinationNetwork == port.network):
    // Intra-network: use short header
    frame = [node, port.node, 0x01] + datagram.AsShortHeaderBytes()
  else:
    // Inter-network: use long header
    frame = [node, port.node, 0x02] + datagram.AsLongHeaderBytes(calcChecksums)

  sendFrameFunc(frame)
```

### Broadcast

```
Broadcast(datagram):
  if port.node == 0: drop
  frame = [0xFF, port.node, 0x01] + datagram.AsShortHeaderBytes()
  sendFrameFunc(frame)
```

LocalTalk broadcasts always use the short-header format, as they are segment-local by definition.

### Multicast

```
Multicast(zoneName, datagram):
  // LocalTalk has no multicast; use broadcast
  Broadcast(datagram)
```

LocalTalk does not support multicast addresses. All multicast traffic is sent as broadcast.

---

## SetNetworkRange

Called by RTMP when the network number is learned from a neighbor:

```
SetNetworkRange(networkMin, networkMax):
  if networkMin != networkMax: ignore (LocalTalk is non-extended)
  if port.network != 0: ignore (already set)
  log "assigned network number N"
  network = networkMin
  networkMin = networkMin
  networkMax = networkMax
  update routing table: RoutingSetPortRange(this_port, networkMin, networkMax)
```

The network number can only be set once. After it is claimed, subsequent calls are ignored.

---

## Startup

```
Start(router):
  store router reference

  if seedNetwork != 0:
    register in routing table: RoutingSetPortRange(this_port, seedNetwork, seedNetwork)

  if seedNetwork != 0 AND seedZoneName != empty:
    register in Zone Information Table: AddNetworksToZone(seedZoneName, seedNetwork, seedNetwork)

  launch nodeRun goroutine
```

The seed network and zone are registered synchronously before the goroutine starts, so that the routing table is populated immediately upon `Start()` returning — important for the router to be able to forward datagrams to this port before node acquisition completes.

---

## Construction Parameters

| Parameter | Description |
|---|---|
| `seedNetwork` | Initial network number (0 = unknown, will be learned from RTMP) |
| `seedZoneName` | Zone name to pre-populate in the ZIT (may be empty) |
| `respondToEnq` | Whether to respond to LLAP ENQ frames for the claimed node (true for LToUDP, false for TashTalk) |
| `desiredNode` | First node address to try during acquisition (default 0xFE) |

---

## Thread Safety

The base port uses a single mutex (`mu`) protecting:

- `node` (claimed node address)
- `desiredNode` (current probe candidate)
- `nodeAttempts` (probe count)
- `desiredNodeList` (fallback candidates)

The `nodeRun` goroutine and `InboundFrame` (called from the transport's receive goroutine) both access this state and must hold the mutex when doing so.

`network`, `networkMin`, `networkMax` are written only during initialization and `SetNetworkRange`, which is called from the RTMP service goroutine. In the current implementation these are read without a mutex; an implementor should ensure either that writes are complete before concurrent reads begin, or add appropriate synchronization.

---

## TODO / Known Limitations

- **Node acquisition does not persist:** The claimed node address is lost on restart. A future implementation could save the last-used node and use it as the first candidate on the next start.
- **No re-acquisition after conflict:** Once a node is claimed, conflicts are not re-detected. If another node boots with the same address after this node has claimed it, both will operate with the same node address on the segment. AppleTalk relies on the acquisition phase for uniqueness; post-claim conflict detection is not implemented.
- **SetNetworkRange ignores updates after first set:** Once the network number is assigned, it cannot be changed without restarting the port. This matches the behavior of physical LocalTalk, where a segment has a fixed network number, but a future implementation could support reconfiguration.
