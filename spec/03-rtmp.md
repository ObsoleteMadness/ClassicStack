# RTMP Service Specification

## Purpose

The Routing Table Maintenance Protocol (RTMP) service maintains the router's routing table by exchanging routing information with neighboring routers. It has two responsibilities:

1. **Responding** — Process incoming RTMP Data and Request packets from neighbors.
2. **Sending** — Periodically broadcast this router's routing table to all neighbors.

## Identity

| Property | Value |
|---|---|
| Socket | 1 |
| DDP Types | 1 (RTMP Data), 5 (RTMP Request) |

---

## Part 1: RTMP Data Packet Format (DDP Type 1)

RTMP Data packets are broadcast by routers to announce their routing tables.

### Header

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 2 bytes | Sender network | The sending router's network number (big-endian) |
| 2 | 1 byte | ID length | Always 8 for AppleTalk Phase 2 |
| 3 | 1 byte | Sender node | The sending router's node number on this segment |

### Sender's Own Network Tuple (immediately after header)

For a **non-extended** sender:

| Offset | Size | Field | Notes |
|---|---|---|---|
| 4 | 2 bytes | Network number | Always 0 for non-extended sender's own tuple |
| 6 | 1 byte | Distance | Always 0 |

For an **extended** sender:

| Offset | Size | Field | Notes |
|---|---|---|---|
| 4 | 2 bytes | Network min | Start of sender's network range (big-endian) |
| 6 | 1 byte | Extended marker | 0x80 (marks this as an extended tuple) |
| 7 | 2 bytes | Network max | End of sender's network range (big-endian) |
| 9 | 1 byte | Version | RTMP version; 0x82 for Phase 2 |

### Neighbor Network Tuples (repeated, follow sender's own tuple)

Each tuple describes a network reachable via the sender.

**Non-extended tuple (3 bytes):**

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 2 bytes | Network number | big-endian |
| 2 | 1 byte | Distance | Hops from the sender; bit 7 NOT set |

**Extended tuple (6 bytes):**

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 2 bytes | Network min | big-endian |
| 2 | 1 byte | Distance + extended marker | bits 0–4 = distance, bit 7 = 1 (0x80 set) |
| 3 | 2 bytes | Network max | big-endian |
| 5 | 1 byte | Version | 0x82 for Phase 2 |

Detection: a tuple is extended if bit 7 of the third byte is set.

### Distance Encoding

The raw distance value in a neighbor tuple is the distance **from the sender**. When the receiver processes a tuple, it adds 1 to convert to distance from itself.

A distance of 31 (the constant `NotifyNeighborDistance`) is a special sentinel meaning "I used to know this route but it is now unreachable." The receiver should mark the corresponding entry as Bad.

---

## Part 2: RTMP Request Packet Format (DDP Type 5)

RTMP Request packets ask a neighbor router for routing information.

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 1 byte | Function | See below |

Function codes:

| Value | Name | Meaning |
|---|---|---|
| 0x01 | RReq | Request the receiver's own network range |
| 0x02 | RDR Split Horizon | Request full routing table, excluding the sender's network |
| 0x03 | RDR No Split Horizon | Request full routing table without exclusions |

---

## Part 3: Responding Service

The responding service processes inbound RTMP Data and Request packets.

### Processing RTMP Data (DDP Type 1)

```
parse sender_network, id_length, sender_node from header
(id_length must be 8; otherwise drop)

parse sender's own tuple:
  if byte[6] has bit 7 set (extended marker):
    sender_network_min = bytes[4–5]
    sender_network_max = bytes[7–8]
    is_extended = true
    next_tuple_offset = 10  // skip 4-byte header + 6-byte extended own tuple
  else:
    sender_network_min = sender_network
    sender_network_max = sender_network
    is_extended = false
    next_tuple_offset = 7

if rx_port has no network range assigned yet:
  call rx_port.SetNetworkRange(sender_network_min, sender_network_max)
  (this sets the port's operating network range)

add routing table entry for sender's network range:
  entry.ExtendedNetwork = is_extended
  entry.NetworkMin = sender_network_min
  entry.NetworkMax = sender_network_max
  entry.Distance = 0
  entry.Port = rx_port
  entry.NextNetwork = 0
  entry.NextNode = 0
  call RoutingTable.Consider(entry)

for each neighbor tuple starting at next_tuple_offset:
  if tuple has bit 7 of third byte set (extended):
    tuple_network_min = bytes[0–1]
    raw_distance = bytes[2] & 0x1F  (mask off extended bit)
    tuple_network_max = bytes[3–4]
    tuple_is_extended = true
    advance by 6 bytes
  else:
    tuple_network = bytes[0–1]
    tuple_network_min = tuple_network
    tuple_network_max = tuple_network
    raw_distance = bytes[2]
    tuple_is_extended = false
    advance by 3 bytes

  if raw_distance >= 15 (NotifyNeighborDistance sentinel):
    call RoutingTable.MarkBad(tuple_network_min, tuple_network_max)
    continue

  entry.ExtendedNetwork = tuple_is_extended
  entry.NetworkMin = tuple_network_min
  entry.NetworkMax = tuple_network_max
  entry.Distance = raw_distance + 1
  entry.Port = rx_port
  entry.NextNetwork = sender_network
  entry.NextNode = sender_node
  call RoutingTable.Consider(entry)
```

### Routing Table Consideration Rules

When `RoutingTable.Consider(entry)` is called, the table updates if any of the following:

- No existing entry covers this network range.
- The new entry has a strictly lower distance.
- The existing entry is in Bad or Worst state.
- The new entry uses the same next-hop (NextNetwork + NextNode) but arrives via a different port (port change for same route).

Otherwise the entry is ignored. An existing entry that already equals the new entry is refreshed in-place (reset to Good state).

### Processing RTMP Request (DDP Type 5)

```
function = data[0]

if function == 0x01 (RReq):
  build response datagram:
    DDP type = 1 (RTMP Data)
    payload = sender header only (network, 8, node)
    for rx_port:
      if extended: append sender's extended tuple (networkMin, 0x80, networkMax, 0x82)
      else: append (0, 0, 0) non-extended tuple
  send response via router.Reply(received, response)

else if function == 0x02 (RDR Split Horizon):
  build full routing table response, excluding routes whose Port == rx_port
  send response via router.Reply(received, response)

else if function == 0x03 (RDR No Split Horizon):
  build full routing table response, no exclusions
  send response via router.Reply(received, response)
```

See "Building a Routing Table Datagram" in Part 4 below for response construction.

---

## Part 4: Sending Service

The sending service broadcasts the routing table to all ports on a 10-second interval.

### Timer Behavior

At startup, schedule a recurring 10-second timer. On each tick:

```
for each port in router:
  broadcast_routing_table(port)
```

### Broadcasting the Routing Table

```
broadcast_routing_table(port):
  datagrams = build_routing_table_datagrams(port, split_horizon=true)
  for each datagram:
    broadcast datagram on port
    (set DestinationNode = 0xFF, DestinationSocket = 1, DDPType = 1)
```

### Building Routing Table Datagrams

A single routing table may require multiple datagrams if it exceeds the 586-byte DDP payload limit.

```
build_routing_table_datagrams(port, split_horizon):
  result = []
  current_payload = []

  header = build_sender_header(port)
  current_payload = header

  for each entry in RoutingTable:
    if split_horizon and entry.Port == port and entry.Distance > 0:
      continue  // skip routes learned from this port

    tuple = build_tuple(entry)

    if len(current_payload) + len(tuple) > 586:
      result.append(current_payload)
      current_payload = header + []  // start new datagram with fresh header

    current_payload += tuple

  if len(current_payload) > len(header):
    result.append(current_payload)

  return result
```

### Building the Sender Header

For an **extended** port:

```
header = [
  port.NetworkMin (2 bytes, big-endian),
  8,
  port.Node,
  port.NetworkMin (2 bytes, big-endian),
  0x80,
  port.NetworkMax (2 bytes, big-endian),
  0x82
]
```

For a **non-extended** port:

```
header = [
  port.Network (2 bytes, big-endian),
  8,
  port.Node,
  0x00, 0x00,    // network = 0
  0x00,          // distance = 0
]
```

### Building a Network Tuple

For each routing table entry:

Determine the distance to report:

```
if entry state is Bad or Worst:
  reported_distance = 31  // NotifyNeighborDistance
else:
  reported_distance = entry.Distance
```

For an **extended** entry:

```
tuple = [
  entry.NetworkMin (2 bytes, big-endian),
  (reported_distance & 0x1F) | 0x80,  // set bit 7 = extended marker
  entry.NetworkMax (2 bytes, big-endian),
  0x82  // version
]
```

For a **non-extended** entry:

```
tuple = [
  entry.NetworkMin (2 bytes, big-endian),
  reported_distance & 0x1F
]
```

### Split Horizon

Split horizon prevents routing loops: when broadcasting on port P, omit entries that were learned from port P (i.e., entries where `entry.Port == P` and `entry.Distance > 0`). Directly connected routes (Distance == 0) are always included.

---

## Notes

- RTMP Data packets are broadcast (DestinationNode = 0xFF) by the sending service.
- Datagrams from the router itself use DDP type 1 and socket 1 as both source and destination.
- The routing table entries for directly connected routes (Distance = 0) are set by the port startup process, not by RTMP; RTMP only updates entries for routes learned from neighbors.
- If two routers announce the same network with different distances, the lower distance always wins (better route).
- A router should not announce routes back to the port they were learned from (split horizon rule).
