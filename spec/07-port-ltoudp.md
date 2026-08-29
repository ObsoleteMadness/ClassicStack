# LToUDP Port Specification

## Purpose

The LToUDP port tunnels LocalTalk frames over UDP multicast, allowing multiple router instances (or AppleTalk nodes) running on different machines — or different processes on the same machine — to communicate as if they were connected to the same LocalTalk segment. It is commonly used for development, testing, and bridging where physical LocalTalk hardware is unavailable.

The LToUDP port wraps the base LocalTalk port, which handles LLAP framing and node address acquisition. See [08-port-localtalk-base.md](08-port-localtalk-base.md) for the base port specification.

---

## Transport

| Parameter | Value |
|---|---|
| Protocol | UDP over IPv4 |
| Multicast group | `239.192.76.84` (IPv4 multicast, administratively scoped) |
| Port | `1954` |
| Multicast TTL | 1 (link-local only; does not cross routers) |
| Bind address | `0.0.0.0:1954` (all interfaces) |

The multicast group `239.192.76.84` falls in the administratively scoped range (239.192.0.0/14), meaning it is intended for local use and will not propagate beyond the local site.

---

## Frame Format

Each UDP datagram sent or received on the multicast group consists of:

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 4 bytes | Sender ID | Identifies the sending process; used to filter own frames |
| 4 | variable | LocalTalk frame | Raw LLAP frame (see base LocalTalk port spec) |

The minimum valid UDP payload is 7 bytes (4-byte sender ID + 3-byte minimum LLAP frame).

### Sender ID

The Sender ID is a 4-byte big-endian value derived from the process ID (PID) of the sender. Its sole purpose is self-filtering: a receiver that receives a UDP datagram whose Sender ID matches its own will silently discard it. This prevents a process from processing its own multicast traffic.

> **Note on loopback and self-filtering:** Because UDP multicast loopback is enabled (see below), the socket will receive its own outbound packets. The Sender ID filter is therefore essential for correct operation. Without it, the process would process every frame it sends as an inbound frame from another node.

---

## Socket Configuration

The UDP socket requires specific configuration to work reliably across platforms.

### Socket Options

| Option | Value | Reason |
|---|---|---|
| `SO_REUSEADDR` | 1 (enabled) | Allows reuse of the local port (Windows: sufficient to share `0.0.0.0:1954`) |
| `SO_REUSEPORT` | 1 (enabled; POSIX) | Required on Darwin (and recommended by [ltoudp.md](ltoudp.md)) so Mini vMac / a second ClassicStack can bind UDP 1954 at the same time |
| `IP_ADD_MEMBERSHIP` | Join `239.192.76.84` on every host LAN interface | Enables reception of multicast traffic |
| `IP_MULTICAST_IF` | Default-route LAN NIC (else first LAN NIC) | Pins outbound TTL-1 packets to Ethernet/Wi-Fi, not VPN/AirDrop |
| `IP_MULTICAST_TTL` | 1 | Prevents traffic from escaping the local network |
| `IP_MULTICAST_LOOP` | 1 (enabled) | Ensures the socket receives its own outbound multicast packets (required for self-filtering to work) |

The socket is bound to `0.0.0.0:1954`, not to the multicast group address. This is the correct POSIX behavior for receiving multicast: bind to the wildcard address, then join the multicast group.

When no bind address is configured, the group is joined on every up, multicast-capable IPv4 **LAN** interface (Wi-Fi, Ethernet, bridges). VPN/point-to-point (`utun`), Apple Wireless Direct Link (`awdl`/`llw`), and tunnel (`gif`/`stf`) interfaces are skipped: TTL 1 packets sent there never reach other machines on the operator's LAN. Outbound multicast is pinned with `IP_MULTICAST_IF` to the default-route LAN NIC when that NIC is in the join set, otherwise the first LAN NIC. Loopback is still joined so two processes on one host share the segment.

A configured `Interface` IPv4 address still joins and pins that one interface only.

### Windows-Specific: SO_REUSEADDR

On Windows, the file descriptor type passed to `setsockopt` for `SO_REUSEADDR` is `syscall.Handle` (which is `uintptr`), while on POSIX platforms it is `int`. These are binary-incompatible and must be handled with platform-specific code. Specifically:

- **Windows:** `setsockopt(syscall.Handle(fd), SOL_SOCKET, SO_REUSEADDR, 1)`
- **Non-Windows:** `setsockopt(int(fd), SOL_SOCKET, SO_REUSEADDR, 1)`

### Windows-Specific: Multicast Join

On Windows, `net.ListenMulticastUDP` does not reliably enable multicast reception. Instead, the correct approach is:

1. Create a raw UDP socket with `SO_REUSEADDR`.
2. Bind it to `0.0.0.0:1954`.
3. Use `IP_ADD_MEMBERSHIP` via the `ipv4.PacketConn` API to join the multicast group.
4. Explicitly set `IP_MULTICAST_LOOP` to enabled — Windows defaults may differ from POSIX.

Failure to explicitly enable `IP_MULTICAST_LOOP` on Windows may cause the process to never receive its own packets, breaking the loopback scenario (multiple processes on the same Windows machine sharing a simulated LocalTalk segment).

---

## Inbound Processing

```
loop:
  set read deadline = now + 250ms  (allows checking stop signal)
  n, addr, err = conn.ReadFromUDP(buf)
  if error (including deadline timeout):
    if stop signal received: exit
    continue

  if n < 7: discard (too short for 4-byte sender ID + 3-byte LLAP minimum)

  if buf[0..4] == own_sender_id: discard (own frame)

  llap_frame = buf[4..n]
  call base_port.InboundFrame(llap_frame)
```

---

## Outbound Sending

All outbound frames (from Unicast, Broadcast, and Multicast) pass through the base LocalTalk port's send function, which calls the configured `sendFrame` callback:

```
sendFrame(frame):
  payload = sender_id_bytes + frame
  conn.WriteToUDP(payload, multicast_group_addr)
```

The sender ID bytes are the 4-byte big-endian PID, computed once at construction.

---

## Loopback Behavior (Same Machine)

Two router processes on the same machine can communicate over LToUDP:

1. Both bind to `0.0.0.0:1954`.
2. Both join `239.192.76.84`.
3. `SO_REUSEADDR` + `SO_REUSEPORT` allow both to bind to the same port (Darwin needs `SO_REUSEPORT`; `SO_REUSEADDR` alone yields EADDRINUSE).
4. `IP_MULTICAST_LOOP` ensures each socket receives frames sent by the other.
5. Sender ID filtering ensures each socket discards its own frames.

This is the primary use case for LToUDP: running two router instances on a single development machine to simulate a multi-segment AppleTalk internetwork.

> **Limitation:** Because `IP_MULTICAST_TTL` is set to 1, LToUDP traffic does not cross IP routers. All participants must be on the same IP subnet. This is intentional — LToUDP simulates a shared LocalTalk segment, which is inherently local.

---

## Relationship to Base LocalTalk Port

LToUDP wraps the base LocalTalk port:

- The base port handles LLAP framing, LLAP ENQ/ACK node acquisition, and routing of inbound frames to the router.
- LToUDP provides the transport: UDP send/receive replaces the serial or other physical medium.
- LToUDP sets `respondToEnq = true` on the base port, meaning it will respond to LLAP ENQ frames targeting its claimed node. This is appropriate for a simulated shared medium where multiple participants need to coordinate node addresses.
- The default desired node is `0xFE`.

---

## Construction Parameters

| Parameter | Description |
|---|---|
| `intfAddr` | Optional: local IP address hint for display purposes. Not used for socket binding (always binds to 0.0.0.0). Shown in log output. |
| `seedNetwork` | The LocalTalk network number this port should use (0 = learn from RTMP neighbor). |
| `seedZoneName` | Zone name to register in the ZIT at startup (may be empty). |

---

## Windows-Specific Notes

- **Multicast on Windows:** Windows requires explicit `IP_ADD_MEMBERSHIP` and `IP_MULTICAST_LOOP` configuration. Using high-level multicast APIs like `net.ListenMulticastUDP` may not work reliably — use raw socket configuration with platform-specific `SO_REUSEADDR`.
- **Multiple processes on one machine:** `SO_REUSEADDR` on Windows does **not** have the same semantics as on Linux. On Windows, `SO_REUSEADDR` allows any process (including potentially malicious ones) to bind to the same port and receive a copy of the traffic. `SO_EXCLUSIVEADDRUSE` prevents this but prevents multiple legitimate processes from sharing the port. For this application (simulated LocalTalk), the shared-port behavior of `SO_REUSEADDR` is the desired one, so `SO_EXCLUSIVEADDRUSE` should not be set.
- **Windows Firewall:** Outbound multicast UDP to `239.192.76.84:1954` may be blocked by Windows Firewall. An inbound firewall rule for UDP port 1954 may need to be added to receive multicast from other machines.
- **Virtual network adapters:** If the machine has multiple network adapters (e.g. Ethernet + WiFi + Hyper-V virtual switch), the OS may select a non-obvious default multicast interface. On Windows, the default route's interface is used for multicast. LToUDP enumerates adapters, skips those that are not `Up` or are point-to-point, and pins outbound multicast to the default-route LAN adapter when it is in the join set.

---

## macOS Local Network privacy

Sending or receiving LToUDP multicast is a local-network operation ([TN3179](https://developer.apple.com/documentation/technotes/tn3179-understanding-local-network-privacy)). On macOS 15+:

- A command-line tool started from Terminal or SSH is **auto-allowed**.
- A process spawned by another app (IDE, Finder) uses **that app's** Local Network privilege. If that parent was denied — or never prompted — outbound multicast is dropped with no error and other machines will not see this router.
- `classicstack` binaries built on Darwin embed an `Info.plist` (`NSLocalNetworkUsageDescription`) in the Mach-O `__TEXT,__info_plist` section so a launched binary can present a usage string. Opening the LToUDP socket also `connect()`s UDP to the group (discard port) to trigger the system prompt (TN3179: there is no explicit API).
- The Application Firewall (System Settings → Network → Firewall) is a separate gate: it can block **incoming** UDP 1954. Other machines seeing **us** is outbound multicast (Local Network); us seeing **them** also needs the firewall to allow incoming datagrams.

If peers on the same L2 segment cannot see ClassicStack: allow Local Network for the responsible app (the IDE if you `go run` from it, or ClassicStack if you launch a built binary), and allow incoming connections if the firewall dialog appeared.

---

## TODO / Known Limitations

- **Sender ID is process PID:** This works correctly as long as each process has a unique PID, which is always true on a single machine. However, if two machines happen to have processes with the same PID, their sender IDs will collide and one will discard frames from the other. Using a random 4-byte sender ID generated at startup would be more robust.
- **No authentication or encryption:** LToUDP transmits LocalTalk frames in plaintext UDP. Any machine on the local network can join the multicast group and inject or observe traffic.
