# ClassicStack Service Specifications — Overview

This directory contains implementation-level specifications for each service in the OmniRouter AppleTalk router. These documents are intended to provide sufficient detail for an independent implementor to create a conformant implementation.

## Context

OmniRouter is an AppleTalk Phase 2 router. MacIP Gateway and AFP Server. 
It receives DDP (Datagram Delivery Protocol) datagrams on one or more network ports, routes them to other ports or delivers them to local services, and implements the AppleTalk routing and zone protocols.

## Common Concepts

### DDP Datagram

All services receive and send AppleTalk DDP datagrams. A datagram has:

| Field | Size | Notes |
|---|---|---|
| HopCount | 4 bits | Incremented by each forwarding router; max 15 |
| DestinationNetwork | 16 bits | 0 = same network as sender |
| SourceNetwork | 16 bits | 0 = same network as sender |
| DestinationNode | 8 bits | 0 = network, 255 = broadcast |
| SourceNode | 8 bits | 1–254 valid unicast |
| DestinationSocket | 8 bits | Upper-layer demux |
| SourceSocket | 8 bits | Upper-layer demux |
| DDPType | 8 bits | Protocol type |
| Data | 0–586 bytes | Payload |

Well-known socket numbers referenced by services:

| Socket | Service |
|---|---|
| 1 | RTMP |
| 2 | NBP |
| 4 | Echo |
| 6 | ZIP |

Well-known DDP type numbers:

| DDP Type | Protocol |
|---|---|
| 1 | RTMP Data |
| 2 | NBP |
| 3 | ATP (AppleTalk Transaction Protocol) |
| 4 | Echo |
| 5 | RTMP Request |
| 6 | ZIP |

### Port Interface

A port represents a physical or logical network interface. Services interact with the router through a router interface, and the router interacts with ports. Key port properties relevant to services:

- **Network** — the port's node's network number
- **NetworkMin / NetworkMax** — the range of network numbers on this port's segment
- **ExtendedNetwork** — whether the port is on a Phase 2 extended network (has a range vs. a single number)
- **Node** — the port's node number on its segment

### Router Interface (used by services)

Services call back into the router to:

- **Route(datagram, originating)** — Forward a datagram. If `originating=true`, the router fills in SourceNetwork/SourceNode from the appropriate port.
- **Reply(received, response)** — Send a response datagram. The router reverses source/destination from the received datagram and calls Route.
- **RoutingTable** — Access to the routing table (for route lookups).
- **ZoneInformationTable** — Access to the zone table (for zone lookups).

### Service Lifecycle

Each service implements:

- **Start(router)** — Called once at startup. The service should launch any background goroutines or timers it needs.
- **Stop()** — Called at shutdown. The service must terminate all background goroutines.
- **Inbound(datagram, rxPort)** — Called by the router when a datagram arrives on the service's socket.

### Routing Table Entry

| Field | Type | Notes |
|---|---|---|
| ExtendedNetwork | bool | True if this is a Phase 2 extended network |
| NetworkMin | uint16 | Start of network range |
| NetworkMax | uint16 | End of network range (= NetworkMin if non-extended) |
| Distance | uint8 | Hops to reach network; 0 = directly connected |
| Port | Port | The port this route is reachable via |
| NextNetwork | uint16 | Network of next-hop router (if Distance > 0) |
| NextNode | uint8 | Node of next-hop router (if Distance > 0) |

Routing table entries age through states: Good → Suspicious → Bad → Worst → deleted.

### Zone Information Table

Maps zone names (case-insensitive, AppleTalk case folding) to sets of network ranges, and network ranges to sets of zone names. Also tracks the default zone for each range (the first zone added for that range).

## Services

| Spec File | Service | Socket | DDP Types |
|---|---|---|---|
| [01-echo.md](01-echo.md) | Echo | 4 | 4 |
| [02-nbp.md](02-nbp.md) | Name Binding Protocol | 2 | 2 |
| [03-rtmp.md](03-rtmp.md) | Routing Table Maintenance Protocol | 1 | 1, 5 |
| [04-zip.md](04-zip.md) | Zone Information Protocol | 6 | 3, 6 |
| [05-aging.md](05-aging.md) | Routing Table Aging | (timer only) | — |

## Port Implementations

| Spec File | Port | Transport |
|---|---|---|
| [06-port-ethertalk.md](06-port-ethertalk.md) | EtherTalk | Raw Ethernet via pcap/Npcap |
| [07-port-ltoudp.md](07-port-ltoudp.md) | LToUDP | UDP multicast (239.192.76.84:1954) |
| [08-port-tashtalk.md](08-port-tashtalk.md) | TashTalk | Serial (1 Mbit/s, TashTalk framing) |
| [09-port-localtalk-base.md](09-port-localtalk-base.md) | LocalTalk base | Shared logic for LToUDP and TashTalk |
