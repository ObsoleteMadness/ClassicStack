# Routing Table Aging Service Specification

## Purpose

The Routing Table Aging service periodically advances routing table entries through an aging state machine, eventually expiring entries that have not been refreshed by RTMP. This prevents stale routes from persisting after a neighboring router disappears.

## Identity

This service has no socket or DDP type. It does not send or receive datagrams. It operates purely as a background timer.

## Routing Table Entry State Machine

Each routing table entry is in one of the following states:

| State | Description |
|---|---|
| Good | Entry is fresh (recently confirmed or newly added) |
| Suspicious | Entry has not been refreshed in one aging interval |
| Bad | Entry has not been refreshed in two aging intervals |
| Worst | Entry has not been refreshed in three aging intervals; will be removed next cycle |

State transitions happen only for entries with Distance > 0 (i.e., entries learned from neighbor routers). Entries with Distance == 0 represent directly connected networks and do not age.

### State Transition Table

| Current State | Condition | Next State |
|---|---|---|
| Good | Distance > 0 | Suspicious |
| Suspicious | (any) | Bad |
| Bad | (any) | Worst |
| Worst | (any) | Deleted |

When an entry is deleted from the routing table, its associated network range is also removed from the Zone Information Table.

### Refreshing Entries

When RTMP receives a routing announcement for a network, the routing table resets that entry to the Good state. This prevents the entry from aging out as long as the neighbor continues sending RTMP broadcasts.

## Timer Behavior

At startup, schedule a recurring 20-second timer. On each tick:

```
RoutingTable.Age()
```

The `Age()` operation:

```
for each entry in routing_table:
  if entry.Distance == 0:
    continue  // directly connected; never ages

  switch entry.state:
    case Good:
      entry.state = Suspicious
    case Suspicious:
      entry.state = Bad
    case Bad:
      entry.state = Worst
    case Worst:
      remove entry from routing_table
      ZIT.RemoveNetworks(entry.NetworkMin, entry.NetworkMax)
```

## Relationship to RTMP

RTMP broadcasts routing table updates every 10 seconds. A route must be refreshed within approximately 20 seconds (one aging cycle) to remain in the Good state. After 60 seconds without a refresh (three aging cycles: Good → Suspicious → Bad → Worst → Deleted), the route is removed entirely.

Timeline for a route whose neighbor goes silent:

| Time after last refresh | State |
|---|---|
| 0–20 s | Good |
| 20–40 s | Suspicious |
| 40–60 s | Bad |
| 60–80 s | Worst |
| 80 s | Deleted |

## Notes

- The aging interval (20 seconds) and the RTMP sending interval (10 seconds) are chosen so that two RTMP broadcasts can be missed before an entry goes Suspicious, and a total of ~6 missed broadcasts before deletion.
- Directly connected routes (Distance == 0) are maintained by the port itself, not by RTMP, and should never expire.
- Removing a network from the Zone Information Table when a route is deleted ensures that zone queries no longer reference unreachable networks.
