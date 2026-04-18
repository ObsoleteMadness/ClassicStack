# ZIP Service Specification

## Purpose

The Zone Information Protocol (ZIP) service maintains the Zone Information Table (ZIT) by exchanging zone-to-network mappings with neighboring routers. It also answers zone queries from end nodes.

ZIP has two responsibilities:

1. **Responding** — Process incoming ZIP packets (queries, replies, and GetNetInfo requests) and ATP zone requests.
2. **Sending** — Periodically query neighbors for zone information that is missing from the ZIT.

## Identity

| Property | Value |
|---|---|
| Socket | 6 |
| DDP Types | 6 (ZIP), 3 (ATP) |

---

## Part 1: ZIP Packet Format (DDP Type 6)

### ZIP Function Codes

| Value | Name | Direction |
|---|---|---|
| 0x01 | Query | Router → Router (request zone info) |
| 0x02 | Reply | Router → Router (immediate reply) |
| 0x05 | GetNetInfo Request | End node → Router |
| 0x06 | GetNetInfo Reply | Router → End node |
| 0x08 | ExtReply | Router → Router (preferred extended reply) |

---

### ZIP Query (Function 0x01)

Sent by a router that lacks zone information for one or more networks.

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 1 byte | Function | 0x01 |
| 1 | 1 byte | Network count | Number of networks being queried (N) |
| 2 | 2*N bytes | Networks | N network numbers, each 2 bytes big-endian |

---

### ZIP Reply (Function 0x02)

Sent in response to a Query. Contains one or more zone–network mappings.

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 1 byte | Function | 0x02 |
| 1 | 1 byte | Tuple count | Number of (network, zone) tuples |
| 2+ | variable | Tuples | See below |

Each tuple:

| Size | Field | Notes |
|---|---|---|
| 2 bytes | Network min | big-endian; for non-extended, NetworkMin = NetworkMax |
| 1 byte | Zone length | |
| variable | Zone name | |

---

### ZIP ExtReply (Function 0x08)

Preferred form of reply; carries an explicit zone count instead of deriving it from tuple count.

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 1 byte | Function | 0x08 |
| 1 | 1 byte | Zone count | Total number of zones in this packet |
| 2+ | variable | Tuples | Same tuple format as Reply |

---

### ZIP GetNetInfo Request (Function 0x05)

Sent by an end node to discover zone information for its own network.

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 1 byte | Function | 0x05 |
| 1 | 5 bytes | Reserved | Must be 0x00 |
| 6 | 1 byte | Zone name length | |
| 7 | variable | Zone name | The zone the node believes it is in |

---

### ZIP GetNetInfo Reply (Function 0x06)

Sent by the router in response to a GetNetInfo Request.

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 1 byte | Function | 0x06 |
| 1 | 1 byte | Flags | See below |
| 2 | 2 bytes | Network min | Port's network range minimum (big-endian) |
| 4 | 2 bytes | Network max | Port's network range maximum (big-endian) |
| 6 | 1 byte | Given zone length | |
| 7 | variable | Given zone name | Echoed from the request |
| 7+zoneLen | 1 byte | Multicast address length | |
| 8+zoneLen | variable | Multicast address | EtherTalk multicast for zone (may be empty) |

If the given zone name is not valid for this network (Flags bit 0x80 set), append:

| Size | Field |
|---|---|
| 1 byte | Default zone length |
| variable | Default zone name |

GetNetInfo Reply Flags:

| Bit | Mask | Meaning |
|---|---|---|
| 7 | 0x80 | Zone invalid — the given zone is not in this network's range |
| 6 | 0x40 | Use broadcast — no multicast address available |
| 5 | 0x20 | Only one zone in range |

---

## Part 2: ATP Packet Format (DDP Type 3)

ZIP also handles zone queries from end nodes using ATP (AppleTalk Transaction Protocol) over DDP type 3.

### ATP Control Byte (byte 0)

| Bit pattern | Meaning |
|---|---|
| 0x40 | TReq (Transaction Request) |
| 0x80 | TResp (Transaction Response) |
| 0x90 | TResp + EOM (End of Message) |

### ATP Functions Handled by ZIP

| Function | Code | Meaning |
|---|---|---|
| GetMyZone | 7 | Return the zone name for the requesting node's port |
| GetZoneList | 8 | Return a paginated list of all zones |
| GetLocalZoneList | 9 | Return zones only in the requesting port's network range |

### ATP Header Layout

ATP Request (at the start of DDP payload):

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 1 byte | Control | 0x40 = TReq |
| 1 | 1 byte | Bitmap | 0x01 for single-sequence transaction |
| 2 | 2 bytes | TID | Transaction ID (big-endian); echo in response |
| 4 | 1 byte | Function | 7, 8, or 9 |
| 5 | 1 byte | Reserved | 0x00 |
| 6 | 2 bytes | Start index | 1-based starting zone index (for GetZoneList/GetLocalZoneList) |

ATP Response:

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 1 byte | Control | 0x90 = TResp + EOM |
| 1 | 1 byte | Bitmap | 0x00 |
| 2 | 2 bytes | TID | Echo from request |
| 4 | 1 byte | Last flag | 0x01 if this packet is the last in the response, 0x00 otherwise |
| 5 | 1 byte | Reserved | 0x00 |
| 6 | 2 bytes | Zone count | Number of zones in this packet (big-endian) |
| 8+ | variable | Zone list | Each zone: 1-byte length + zone name bytes |

---

## Part 3: Responding Service

The responding service handles all inbound ZIP and ATP packets on socket 6.

### Dispatch

```
if datagram.DDPType == 6:
  switch data[0]:
    0x01: handleQuery(datagram, rxPort)
    0x02: handleReply(datagram, rxPort)
    0x08: handleExtReply(datagram, rxPort)
    0x05: handleGetNetInfo(datagram, rxPort)

if datagram.DDPType == 3:
  if data[0] == 0x40 (ATP TReq):
    switch data[4]:
      7: handleGetMyZone(datagram, rxPort)
      8: handleGetZoneList(datagram, rxPort)
      9: handleGetLocalZoneList(datagram, rxPort)
```

---

### handleReply (Function 0x02)

```
tuple_count = data[1]
offset = 2

for i in 0..tuple_count:
  network_min = data[offset..offset+2] (big-endian)
  zone_len = data[offset+2]
  zone_name = data[offset+3 .. offset+3+zone_len]

  look up route for network_min in RoutingTable
  if route found:
    ZIT.AddNetworksToZone(zone_name, route.NetworkMin, route.NetworkMax)

  offset += 3 + zone_len
```

---

### handleExtReply (Function 0x08)

```
zone_count = data[1]
offset = 2
zones_accumulated = 0

while offset < len(data) and zones_accumulated < zone_count:
  network_min = data[offset..offset+2] (big-endian)
  zone_len = data[offset+2]
  zone_name = data[offset+3 .. offset+3+zone_len]

  look up route for network_min in RoutingTable
  if route found:
    ZIT.AddNetworksToZone(zone_name, route.NetworkMin, route.NetworkMax)

  offset += 3 + zone_len
  zones_accumulated += 1
```

---

### handleQuery (Function 0x01)

```
network_count = data[1]
offset = 2

all_tuples = []

for i in 0..network_count:
  queried_network = data[offset..offset+2] (big-endian)
  offset += 2

  look up route for queried_network in RoutingTable
  if no route: continue

  zones = ZIT.ZonesInNetworkRange(route.NetworkMin, route.NetworkMax)
  for each zone:
    all_tuples.append((route.NetworkMin, zone))

if all_tuples is empty: return

build ExtReply datagram:
  data[0] = 0x08 (ExtReply)
  data[1] = len(all_tuples)
  for each (network_min, zone) in all_tuples:
    append network_min (2 bytes big-endian)
    append len(zone) (1 byte)
    append zone bytes

reply using router.Reply(received, response)
```

---

### handleGetNetInfo (Function 0x05)

```
// parse request
given_zone_len = data[6]
given_zone = data[7 .. 7+given_zone_len]

// look up port's network range
network_min = rx_port.NetworkMin
network_max = rx_port.NetworkMax

// check if given zone is valid for this port
zones = ZIT.ZonesInNetworkRange(network_min, network_max)
zone_valid = (given_zone in zones)  // case-insensitive

// determine flags
flags = 0x00
if not zone_valid: flags |= 0x80
if len(zones) == 1: flags |= 0x20

// get multicast address for zone (EtherTalk only; may be empty)
multicast = get_zone_multicast_address(given_zone)
if multicast is empty: flags |= 0x40

// build reply
reply_data = [
  0x06,          // Function = GetNetInfo Reply
  flags,
  network_min (2 bytes big-endian),
  network_max (2 bytes big-endian),
  len(given_zone),
  given_zone bytes,
  len(multicast),
  multicast bytes
]

if not zone_valid:
  default_zone = first zone in zones (ZIT default)
  reply_data += [len(default_zone), default_zone bytes]

reply using router.Reply(received, response)
```

---

### handleGetMyZone (ATP Function 7)

```
tid = data[2..4] (big-endian)

// find zone for rx_port
zones = ZIT.ZonesInNetworkRange(rx_port.NetworkMin, rx_port.NetworkMax)
if zones is empty: return (no response)
zone = zones[0]  // default zone (first in list)

// build ATP response
response_data = [
  0x90,       // TResp + EOM
  0x00,       // bitmap
  tid[0], tid[1],
  0x00, 0x00, // reserved
  0x00, 0x01, // zone count = 1
  len(zone),
  zone bytes
]

reply using router.Reply(received, response)
```

---

### handleGetZoneList (ATP Function 8)

```
tid = data[2..4]
start_index = data[6..8] (big-endian, 1-based)

all_zones = ZIT.all_zones()  // returns all zones in the ZIT

paginate and respond:
  subset = all_zones[start_index-1 .. start_index-1+max_fit]
  is_last = (start_index-1 + len(subset) >= len(all_zones))

response_data = [
  0x90,
  0x00,
  tid[0], tid[1],
  0x01 if is_last else 0x00,
  0x00,
  len(subset) high byte, len(subset) low byte,
  for each zone:
    len(zone), zone bytes
]

reply using router.Reply(received, response)
```

---

### handleGetLocalZoneList (ATP Function 9)

Same as `handleGetZoneList` but the zone list is restricted to zones in `rx_port.NetworkMin .. rx_port.NetworkMax`.

---

## Part 4: Sending Service

The sending service queries neighbors for zone information that is not yet in the ZIT.

### Timer Behavior

At startup, schedule a recurring 10-second timer. On each tick:

```
send_zone_queries()
```

### Sending Zone Queries

```
send_zone_queries():
  for each port:
    networks_needing_zones = []

    for each entry in RoutingTable:
      if entry.Distance == 0 and entry.Port == port:
        continue  // we should already know our own zones

      zones = ZIT.ZonesInNetworkRange(entry.NetworkMin, entry.NetworkMax)
      if zones is empty:
        networks_needing_zones.append(entry)

    for each group of entries sharing the same next-hop (or direct):
      build ZIP Query datagram:
        data[0] = 0x01 (Query)
        data[1] = count of networks in this query
        for each entry in group:
          append entry.NetworkMin (2 bytes big-endian)

      if entry.Distance == 0:
        broadcast query on entry.Port (DestinationNode = 0xFF)
      else:
        unicast query to entry.NextNetwork, entry.NextNode via entry.Port
```

Note: a single ZIP Query may contain multiple network numbers. Group queries by reachability to keep packet count low, but respect the 586-byte DDP payload limit.

---

## Notes

- Zone names use AppleTalk case folding for comparison. The ZIT stores and retrieves zone names in their canonical form; comparisons should use case-folded keys.
- The default zone for a network range is the first zone added for that range. GetNetInfo replies use the default zone when the requested zone is invalid.
- EtherTalk multicast addresses for zones are computed from the zone name using a standard AppleTalk hash. Non-Ethernet ports may not support multicast.
- The responding and sending services share access to the ZIT; operations on the ZIT must be thread-safe.
