# NBP Service Specification

## Purpose

The Name Binding Protocol (NBP) service handles name-to-address resolution in an AppleTalk internetwork. The router's role is to forward and distribute NBP lookup queries to the appropriate network segments, based on zone information.

The router does **not** register names itself (end nodes do that). The router's job is to route queries to the correct ports and networks.

## Identity

| Property | Value |
|---|---|
| Socket | 2 |
| DDP Type | 2 |

## Packet Format

All NBP packets are DDP type 2 delivered to socket 2.

### Header (bytes 0–1)

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 1 byte | Control | High nibble = Function, Low nibble = Tuple count |
| 1 | 1 byte | NBP ID | Opaque identifier set by the originating node |

Function codes (high nibble of byte 0):

| Value | Name | Meaning |
|---|---|---|
| 1 | BrRq | Broadcast Request — client sends to router |
| 2 | LkUp | Lookup — router multicasts to local segment |
| 4 | Fwd | Forward — router forwards to another router |

### Name Tuple (follows the 2-byte header)

Each NBP packet carries one name tuple immediately after the header.

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 2 bytes | Reply network | Network number where replies should be sent (big-endian) |
| 2 | 1 byte | Reply node | Node where replies should be sent |
| 3 | 1 byte | Reply socket | Socket where replies should be sent |
| 4 | 1 byte | Enumerator | Set by the originating node; opaque to router |
| 5 | 1 byte | Object length | Length of object name in bytes |
| 6 | Object length | Object name | Name string (not null-terminated) |
| 6+objLen | 1 byte | Type length | Length of type name in bytes |
| 7+objLen | Type length | Type name | Type string |
| 7+objLen+typeLen | 1 byte | Zone length | Length of zone name in bytes |
| 8+objLen+typeLen | Zone length | Zone name | Zone string |

## Zone Name Resolution

Before processing a BrRq or Fwd, the router must resolve the zone name in the tuple:

- If zone name is `*` (a single asterisk byte):
  - On a non-extended port: substitute the zone name from the Zone Information Table if exactly one zone covers the sending port's network range. If zero or more than one zone are present, the query cannot be resolved to a single zone on a non-extended network — see handling below.
  - On an extended port: drop the packet (zone must be specified on extended networks).
- Otherwise: use the zone name as-is.

## Behavior

### BrRq (Broadcast Request, Function = 1)

A BrRq arrives from an end node requesting a name lookup across the internetwork.

```
parse zone name from tuple
resolve zone name (see Zone Name Resolution above)

if zone is "*":
  if port is extended:
    drop packet
    return
  else:
    look up zones for rx_port's network range
    if exactly 1 zone: substitute it
    else:
      broadcast LkUp on rx_port
      return

for each network in zone (from Zone Information Table):
  look up route to that network in Routing Table
  if no route found: skip
  if route.Distance == 0 (directly connected):
    build LkUp datagram (Function=2, same NBP ID, same tuple with zone substituted)
    multicast LkUp on route.Port for zone
  else (route.Distance > 0):
    build Fwd datagram (Function=4, same NBP ID, same tuple with zone substituted)
    unicast Fwd to route.NextNetwork, route.NextNode via route.Port
```

Routing logic note: if the zone maps to multiple networks reachable via the same next-hop router, send only one Fwd to that router (deduplication by next-hop network+node pair).

### Fwd (Forward, Function = 4)

A Fwd arrives from another router, instructing this router to perform a local lookup on its segment.

```
parse zone name from tuple

build LkUp datagram (Function=2, same NBP ID, same tuple with zone as-is)
multicast LkUp on rx_port for the zone name
```

The zone name in a Fwd should already be fully resolved (not `*`); if it is `*`, treat it the same as the BrRq case.

### LkUp (Lookup, Function = 2)

LkUp datagrams are multicast to end nodes on a local segment. The router does not process LkUp packets as a service; end nodes respond directly to the requester. The router should not forward LkUp datagrams.

## Building LkUp and Fwd Datagrams

When building a LkUp or Fwd from a BrRq:

- Copy the NBP ID from the received BrRq.
- Set Function to 2 (LkUp) or 4 (Fwd).
- Set Tuple count to 1.
- Copy the name tuple from the BrRq exactly, substituting the zone name if `*` was resolved.

For a **LkUp** datagram:
- Destination socket: 2 (NBP)
- Destination node: 0xFF (broadcast) or zone multicast address
- DDP type: 2

For a **Fwd** datagram:
- Destination network: route.NextNetwork
- Destination node: route.NextNode
- Destination socket: 2 (NBP)
- DDP type: 2
- Route using router.Route with originating=true

## Notes

- The router increments the hop count on forwarded datagrams (handled by the router's Route method, not the service).
- NBP replies (LkUp-Reply, function 3) are sent directly from the responding end node back to the reply address in the tuple; the router does not process them specially.
- Case folding: zone name comparison uses AppleTalk-specific case folding rules, which differ from ASCII. The Zone Information Table handles this internally.
