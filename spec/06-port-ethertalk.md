# EtherTalk Port Specification

## Purpose

The EtherTalk port connects the router to a real (or virtual) Ethernet segment carrying AppleTalk Phase 2 traffic. It handles:

- Raw Ethernet frame I/O via a packet capture library
- The AppleTalk Address Resolution Protocol (AARP) for acquiring and maintaining node addresses
- An Address Mapping Table (AMT) that caches AppleTalk-to-MAC address mappings
- Holding datagrams while waiting for AARP resolution

The port is an **extended network** port: it operates over a range of network numbers (networkMin..networkMax) rather than a single number.

---

## Dependencies

The EtherTalk port requires a packet capture library that can open a network interface in promiscuous mode and provide raw Ethernet frame read/write access. On Linux/macOS this is libpcap; on Windows this is **Npcap** (the successor to WinPcap). The interface name format differs by platform:

- Linux/macOS: `eth0`, `en0`, etc.
- Windows: `\Device\NPF_{GUID}` style names returned by `pcap_findalldevs`

Npcap must be installed separately on Windows before the EtherTalk port can function. WinPcap is not sufficient for modern Windows versions.

---

## Ethernet Frame Format

All EtherTalk and AARP frames use **IEEE 802.3 with LLC/SNAP headers** (not DIX/Ethernet II).

### Common Ethernet + LLC/SNAP header

| Offset | Size | Field | Value |
|---|---|---|---|
| 0 | 6 bytes | Destination MAC | |
| 6 | 6 bytes | Source MAC | |
| 12 | 2 bytes | Length | Total payload length (big-endian) |
| 14 | 3 bytes | LLC | `0xAA 0xAA 0x03` (SNAP SAP, UI frame) |
| 17 | 5 bytes | SNAP OUI + type | See below |

SNAP type values:

| Purpose | SNAP bytes (5 bytes) |
|---|---|
| AARP | `0x00 0x00 0x00 0x80 0xF3` |
| AppleTalk DDP | `0x08 0x00 0x07 0x80 0x9B` |

Minimum Ethernet payload is 46 bytes; pad with zeros if the LLC+SNAP+data is shorter.

### AARP Frame Payload (after LLC/SNAP header)

An AARP frame has exactly 36 bytes of payload (after the LLC/SNAP prefix), for a total frame length of 22 (14 Ethernet + 8 LLC/SNAP) + 36 = 58 bytes minimum.

| Offset (from AARP start) | Size | Field | Notes |
|---|---|---|---|
| 0 | 2 bytes | Hardware type | `0x00 0x01` (Ethernet) |
| 2 | 2 bytes | Protocol type | `0x80 0x9B` (AppleTalk) |
| 4 | 1 byte | HW address length | `0x06` (6 bytes) |
| 5 | 1 byte | Protocol address length | `0x04` (4 bytes) |
| 6 | 2 bytes | Function | `0x00 0x01` Request, `0x00 0x02` Response, `0x00 0x03` Probe |
| 8 | 6 bytes | Sender hardware address | Sender's MAC |
| 14 | 1 byte | Reserved | `0x00` |
| 15 | 2 bytes | Sender network | big-endian |
| 17 | 1 byte | Sender node | |
| 18 | 6 bytes | Target hardware address | Zeros for Request/Probe; target MAC for Response |
| 24 | 1 byte | Reserved | `0x00` |
| 25 | 2 bytes | Target network | big-endian |
| 27 | 1 byte | Target node | |

The bytes at offset 0–5 of the AARP payload are a fixed validation header: `0x00 0x01 0x80 0x9B 0x06 0x04`. Frames not matching this should be discarded.

### AppleTalk DDP Frame Payload (after LLC/SNAP header)

The DDP payload follows directly after the 8-byte LLC/SNAP prefix, as a **long-header DDP datagram** (see the overview document). Checksum calculation is enabled.

---

## Addressing

### MAC Addresses

- **EtherTalk broadcast:** `09:00:07:FF:FF:FF`
- **EtherTalk multicast prefix:** `09:00:07:00:00:xx` where `xx` is 0x00–0xFC

### Zone Multicast Address Calculation

The EtherTalk multicast address for a zone is derived from the zone name:

```
uppercase_zone = appletalk_uppercase(zone_name)
checksum = ddp_checksum(uppercase_zone)
multicast = [0x09, 0x00, 0x07, 0x00, 0x00, checksum % 253]
```

The DDP checksum algorithm is a 16-bit rotating-left-one-bit-and-add checksum. AppleTalk uppercase uses a specific character table for accented characters (distinct from ASCII).

> **Note:** Although zone-specific multicast addresses can be computed, the implementation sends all multicast datagrams to the EtherTalk-wide broadcast address (`09:00:07:FF:FF:FF`) instead. This is because many VM-based AppleTalk stacks do not join zone-specific multicast groups, so they would miss zone-targeted frames. All Phase 2 nodes are required to accept the EtherTalk broadcast. Implementors targeting real hardware may choose to use zone-specific multicasts, but should be aware of this compatibility concern.

---

## Address Acquisition (AARP Probe)

Before the port can send or receive DDP datagrams, it must acquire a valid AppleTalk network.node address. This is done using AARP Probe frames.

### Initial State

At construction time, the implementor may supply:
- `seedNetworkMin`, `seedNetworkMax` — the network range this port should use
- `desiredNetwork`, `desiredNode` — a preferred address hint (e.g. from a saved config)

If a desired network/node is provided and falls within the seed range, it is tried first. Otherwise addresses are chosen at random from the range.

### Probe Algorithm

```
build candidate network list: all networks in [networkMin..networkMax], shuffled
  (prepend desiredNetwork if supplied and in range)
build candidate node list: nodes 1..253, shuffled
  (prepend desiredNode if supplied and in range 1..253)

probeNetwork = pop first from candidate network list
probeNode = pop first from candidate node list
probeAttempts = 0

on 200ms timer tick:
  if address already claimed: do nothing
  if probeAttempts >= 10:
    claim address: network = probeNetwork, node = probeNode
    log "claiming address network.node"
    stop timer
  else:
    send AARP Probe for (probeNetwork, probeNode)
    probeAttempts += 1

on receiving AARP Response addressed to us (or matching probe address):
  if address not yet claimed and response.targetNetwork == probeNetwork and response.targetNode == probeNode:
    collision detected: reroll
    probeNode = pop next from candidate node list
    if candidate node list exhausted:
      probeNetwork = pop next from candidate network list
      rebuild candidate node list (1..253, shuffled)
      probeNode = pop next from candidate node list
    probeAttempts = 0
```

An AARP Probe frame is sent to the EtherTalk broadcast MAC. The sender hardware address is this port's MAC. The sender protocol address is set to the **desired** (probe) address. The target protocol address is also set to the desired address (both sender and target are the candidate address).

Collision detection: if any node responds to our probe address (sends an AARP Response or any AARP frame identifying itself at that network.node), we reroll to another address.

### Seed Zone Registration

If seed zone names are provided, register them in the Zone Information Table at startup (before probing begins), associated with the port's network range.

### SetNetworkRange

When the RTMP service learns the network range from a neighbor, it may call `SetNetworkRange(min, max)`. This:

1. Updates networkMin and networkMax.
2. Resets the currently claimed address to zero (clears network and node).
3. Restarts address acquisition from scratch using the new range.
4. Updates the routing table's directly connected entry for this port.

---

## Address Mapping Table (AMT)

The AMT caches the Ethernet MAC addresses of known AppleTalk nodes.

### Entry Format

| Field | Type | Description |
|---|---|---|
| Key | (network uint16, node uint8) | AppleTalk address |
| hw | 6 bytes | Ethernet MAC address |
| timestamp | time | When the entry was last updated |

### Aging

Entries expire after **10 seconds** of inactivity. An aging goroutine runs every 1 second and removes entries older than the limit.

### Populating the AMT

AMT entries are added from two sources:

1. **AARP Responses:** When an AARP Response is received (any response, even ones not directly solicited), add the sender's AppleTalk address → MAC mapping.
2. **Zero-hop DDP frames:** When an AppleTalk datagram is received with HopCount == 0, the sending node is directly attached. Add its source address → source MAC to the AMT.

---

## Held Datagrams

When `Unicast()` is called for an AppleTalk address not in the AMT, the datagram cannot be sent immediately. Instead:

1. Add the datagram to the held queue for that destination.
2. If this is the first datagram held for this destination, send an AARP Request immediately.
3. A retry goroutine re-sends AARP Requests every **250ms** for all destinations with held datagrams.
4. When an AMT entry is added (from AARP Response), flush all held datagrams for that destination by sending them.
5. Held datagrams expire after **10 seconds**. An aging goroutine runs every 1 second and removes stale held datagrams.

### AARP Request

An AARP Request is broadcast to `09:00:07:FF:FF:FF`. The sender fields contain this port's own address. The target protocol fields contain the desired destination; the target hardware address is all zeros.

---

## Inbound Frame Processing

```
receive raw Ethernet frame

if frame length < 22: discard
if bytes[14..17] != [0xAA, 0xAA, 0x03]: discard (not LLC SNAP)

length = frame[12..14] as big-endian uint16
if length > (frame_length - 14): discard

dstMAC = frame[0..6]

if frame[17..22] == SNAP_AARP:
  if length != 36: discard
  if frame[22..28] != AARP_VALIDATION_HEADER: discard

  fn = frame[28..30] as big-endian uint16
  srcHW = frame[30..36]
  srcNetwork = frame[37..39] as big-endian uint16
  srcNode = frame[39]
  targetNetwork = frame[47..49] as big-endian uint16
  targetNode = frame[49]

  if dstMAC == own_mac:
    process AARP frame (respond or detect collision)
  else if (fn == Request OR fn == Probe) AND dstMAC == elapBroadcast
       AND own_network != 0 AND targetNetwork == own_network AND targetNode == own_node:
    process AARP frame (respond — protects our address from being claimed by others)
  else if fn == Response:
    add to AMT silently (promiscuous AARP learning)
  return

if frame[17..22] == SNAP_APPLETALK:
  parse DDP long header from frame[22 .. 14+length]
  if HopCount == 0:
    add srcNetwork.srcNode → frame[6..12] to AMT
  if dstMAC == own_mac OR dstMAC == elapBroadcast OR (dstMAC matches multicast prefix and dstMAC[5] <= 0xFC):
    deliver datagram to router via router.Inbound(datagram, this_port)
```

### Processing an AARP Frame

```
switch fn:
  case Request:
    if we have a claimed address: send AARP Response to srcHW
  case Probe:
    if we have a claimed address: send AARP Response to srcHW
    // Note: do NOT trigger collision detection on probes from others
  case Response:
    add srcNetwork.srcNode → srcHW to AMT
    if we have not yet claimed an address:
      if srcNetwork == probeNetwork and srcNode == probeNode:
        collision: reroll probe address
```

> **Note on Probe responses:** The implementation responds to both AARP Requests and AARP Probes with an AARP Response — this tells the probing node that the address is already in use. Probes from other nodes (where we have already claimed our address) should trigger a response. Probes should not trigger collision-detection on our own probe state — only AARP Responses indicating our candidate address is already taken should do that.

---

## Outbound Frame Sending

### Unicast

```
look up (network, node) in AMT
if found:
  send datagram as long-header DDP frame to the cached MAC
else:
  add to held queue
  send AARP Request if not already pending
```

### Broadcast

Send as long-header DDP frame to EtherTalk broadcast MAC `09:00:07:FF:FF:FF`. Set DestinationNetwork and DestinationNode to their correct broadcast values before serializing.

### Multicast

Send to EtherTalk broadcast MAC `09:00:07:FF:FF:FF` (not to the zone-specific multicast address). See the note above about VM compatibility.

---

## Pcap I/O Layer

The EtherTalk port core (address acquisition, AMT, AARP, frame dispatch) is transport-agnostic. The Pcap I/O layer wraps it with a specific transport.

### Pcap Configuration

| Parameter | Value |
|---|---|
| Snapshot length | 65535 bytes |
| Promiscuous mode | Yes |
| Read timeout | 250ms |
| Immediate mode | Yes (deliver packets as received, not batched) |

### Goroutines

Two goroutines handle I/O:

**Reader goroutine:**
```
loop:
  read one packet from pcap handle (blocking with 250ms timeout)
  if timeout: check stop signal, continue
  if error: log and continue
  call port.InboundFrame(data)
```

**Writer goroutine:**
```
loop:
  wait for frame on write queue channel OR stop signal
  if stop: return
  call pcap_handle.WritePacketData(frame)
  if error: log warning and continue
```

Outbound frames are queued in a channel (capacity 1024). If the write queue is full, the frame is dropped with a warning log. This prevents a slow Ethernet from blocking the router's inbound processing.

### Goroutine Summary

The port runs 6 goroutines total:

| Goroutine | Purpose |
|---|---|
| acquireAddressRun | AARP probe and address claiming |
| amtAgeRun | Expire old AMT entries (1s interval) |
| heldAgeRun | Expire old held datagrams (1s interval) |
| aarpRetryRun | Resend AARP requests for held datagrams (250ms interval) |
| readRun | Read frames from pcap |
| writeRun | Write frames to pcap from queue |

---

## Windows-Specific Notes

- **Npcap required:** The packet capture library on Windows is Npcap (not WinPcap). Npcap must be installed separately before the router can use EtherTalk.
- **Interface name format:** Under Npcap, interface names are device paths like `\Device\NPF_{XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX}`. Use `pcap_findalldevs` (or the equivalent API) to enumerate interfaces and their human-readable descriptions.
- **Promiscuous mode:** Npcap supports promiscuous mode on most adapters. Some virtual adapters (Hyper-V virtual switch, VirtualBox host-only) may silently ignore or block promiscuous mode; datagrams from other nodes may not be visible.
- **Multicast reception:** Some Windows network adapters and drivers do not pass multicast frames to the application even in promiscuous mode. The implementation uses the EtherTalk broadcast address for all outbound multicast/broadcast traffic to maximize compatibility, but inbound multicast filtering at the driver level is outside the router's control.
- **Loopback interface:** The Windows loopback adapter (`\Device\NPF_Loopback`) does not support standard Ethernet frame formats. It should not be used as an EtherTalk port.

---

## TODO / Known Limitations

- **Zone-specific multicast not used:** The implementation sends all multicast/broadcast datagrams to `09:00:07:FF:FF:FF` rather than zone-specific multicast addresses. This is correct per AppleTalk Phase 2 (all nodes must accept the broadcast), but zone-specific multicast would reduce traffic on busy segments. Future implementations targeting real hardware could use the computed multicast address and rely on the NICs joining the appropriate groups.
- **No AARP table persistence:** The AMT is entirely in-memory. If the process restarts, all address mappings must be re-learned. A future implementation could optionally persist and re-seed the AMT across restarts.
- **Desired address hint not persisted:** The desired network.node address hint is only used at startup. A future implementation could save the successfully claimed address and use it as the first candidate on the next start, reducing probe time.
