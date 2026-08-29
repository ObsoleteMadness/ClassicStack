# TashTalk Port Specification

## Purpose

The TashTalk port connects the router to a physical LocalTalk segment via a TashTalk hardware adapter. TashTalk is an open-source microcontroller-based device that bridges between LocalTalk (RS-422 serial at 230.4 kbaud) and a USB serial port on the host. The host communicates with the TashTalk hardware over a serial connection at 1 Mbit/s.

The TashTalk port wraps the base LocalTalk port, which handles LLAP framing and node address acquisition. See [08-port-localtalk-base.md](08-port-localtalk-base.md) for the base port specification.

---

## Serial Connection Parameters

| Parameter | Value |
|---|---|
| Baud rate | 1,000,000 (1 Mbit/s) |
| Data bits | 8 |
| Parity | None |
| Stop bits | 1 |
| Read timeout | 250ms |

The serial port name is platform-dependent:

- Linux: `/dev/ttyUSB0`, `/dev/ttyACM0`, etc.
- macOS: `/dev/cu.usbserial-*`, `/dev/cu.usbmodem*`, etc.
- Windows: `COM3`, `COM4`, etc. (check Device Manager for the assigned port)

---

## Initialization Sequence

After opening the serial port, the host sends an initialization sequence to reset the TashTalk hardware:

```
send: [0x00] × 1024, then [0x02] followed by [0x00] × 32, then [0x03] [0x00]
```

That is: 1024 null bytes to flush any partial device state, then a **complete 33-byte set-node-address command** (`0x02` plus a 32-byte all-zero node bitmap), then `0x03 0x00`.

> **`0x02` is NOT a bare reset byte.** It is the opcode of a 33-byte command whose 32-byte payload is a 256-bit bitmap of the node addresses the hardware should receive (see "Node Address Filter" below). Sending `0x02` on its own leaves the firmware consuming the *next 32 bytes on the wire* as bitmap data, desynchronising the command stream. An earlier revision of this document described `0x02` as a standalone reset; that was wrong, and an implementation written to it transmitted normally but received nothing.

After sending the initialization sequence, the host starts the base LocalTalk port (which begins LLAP node acquisition) and then starts the serial read goroutine.

---

## Node Address Filter (required for inbound traffic)

TashTalk filters inbound frames **in hardware** against a 256-bit node-address bitmap. The bitmap starts **empty**, so until the host arms it the device forwards no frames at all — the host sees a completely silent line while its own transmits go out normally.

The host must therefore send a set-node-address command as soon as LLAP node acquisition claims a node:

```
byte 0:      0x02                (set-node-address opcode)
bytes 1..32: node bitmap         (bit `node` = byte node/8, bit node%8)
```

For the default LocalTalk node `0xFE` (254) this sets bit 254 — byte 31, bit 6 — and clears the rest. A node value of 0 writes an all-zero bitmap, disabling reception. Valid node addresses are 1..254 (255 is the LLAP broadcast address and is not assignable).

The command must be re-sent whenever the claimed node changes.

---

## Wire Protocol (Host ↔ TashTalk)

Communication between the host and the TashTalk hardware uses a simple framing protocol with escape sequences, because the raw LLAP data can contain any byte value including control bytes.

### Special Bytes

| Byte | Meaning |
|---|---|
| `0x01` | Frame start marker |
| `0x00` | Escape prefix — the next byte is a data escape |
| `0x02` | Port reset (host → TashTalk only; used during initialization) |

### Escape Sequences

When the byte `0x00` is received, the next byte is interpreted as:

| Escape byte | Meaning |
|---|---|
| `0xFF` | Data byte `0x00` (escaped null) |
| `0xFD` | End of frame |
| Anything else | Discard accumulated frame; reset to idle |

These escape sequences allow `0x00` to appear in frame data (as `0x00 0xFF`) and provide an unambiguous end-of-frame marker (`0x00 0xFD`).

### Outbound Frame Format (host → TashTalk)

To send a LocalTalk LLAP frame:

```
byte 0:     0x01              (frame start marker)
bytes 1..N: frame_bytes       (raw LLAP frame, NOT escaped)
```

Note: the outbound direction does **not** apply escape encoding in the current implementation. The TashTalk firmware handles raw frame bytes after the `0x01` start marker.

### Inbound Frame Format (TashTalk → host)

Frames arrive as a stream of bytes on the serial port. The receiver maintains a state machine:

```
state = IDLE
frame_buffer = []

for each byte b received:
  if state == IDLE:
    if b == 0x01:
      state = IN_FRAME
      frame_buffer = []
    // else: ignore

  else if state == IN_FRAME:
    if b == 0x00:
      state = ESCAPED
    else:
      frame_buffer.append(b)

  else if state == ESCAPED:
    state = IN_FRAME
    if b == 0xFF:
      frame_buffer.append(0x00)  // escaped null byte
    else if b == 0xFD:
      if len(frame_buffer) >= 5:
        dispatch frame_buffer as inbound LLAP frame
      frame_buffer = []          // reset for next frame
      state = IDLE
    else:
      frame_buffer = []          // protocol error: discard
      state = IDLE
```

The minimum valid LLAP frame is 5 bytes (3-byte LLAP header + at least 2 bytes of content). Frames shorter than 5 bytes are silently discarded.

**The `0x01` start marker is HOST→DEVICE ONLY.** The device does NOT prefix its frames with it; inbound frames are delimited solely by the `0x00 0xFD` end-of-frame escape. The receiver must therefore accumulate bytes **unconditionally** from the first byte received, and MUST NOT wait for a start marker.

> Do not "tighten" this by enforcing a start marker inbound. A refactor did exactly that — adding an IDLE state that waited for `0x01` before accumulating — and the port then transmitted normally while receiving **nothing at all**, because the state machine sat in IDLE discarding every inbound byte forever.

---

## Inbound Frame Dispatch

After a complete frame is extracted from the serial stream, it is passed to `base_port.InboundFrame(frame)`. The base port parses the LLAP header and dispatches:

- LLAP type `0x01` → short-header DDP datagram
- LLAP type `0x02` → long-header DDP datagram
- LLAP type `0x81` → LLAP ENQ (node address probe)
- LLAP type `0x82` → LLAP ACK (collision response)

See the base LocalTalk port specification for details.

---

## Relationship to Base LocalTalk Port

TashTalk wraps the base LocalTalk port:

- The base port handles LLAP framing, LLAP ENQ/ACK node acquisition, and delivery to the router.
- TashTalk provides the serial transport: serial read/write replaces a direct shared-medium connection.
- TashTalk sets `respondToEnq = false` on the base port. Because the TashTalk hardware is the only participant that directly sees the LocalTalk segment, the host software does not respond to ENQ frames — the hardware may handle this, or it is simply not needed for the router's operation.
- The default desired node is `0xFE`.

---

## Construction Parameters

| Parameter | Description |
|---|---|
| `serialPort` | Serial port path (e.g. `COM3`, `/dev/ttyUSB0`) |
| `seedNetwork` | LocalTalk network number to use (0 = learn from RTMP neighbor) |
| `seedZoneName` | Zone name to register in the ZIT at startup (may be empty) |

---

## Goroutines

The TashTalk port runs one I/O goroutine in addition to the base port's `nodeRun` goroutine:

| Goroutine | Purpose |
|---|---|
| readRun | Reads bytes from serial port and dispatches complete inbound frames |
| nodeRun (base) | Sends LLAP ENQ frames and claims a node address |

There is no separate writer goroutine; outbound frames are written synchronously to the serial port from whatever goroutine calls `Unicast`, `Broadcast`, or `Multicast`.

---

## Shutdown

On `Stop()`:

1. Signal the read goroutine to exit (via a stop channel).
2. Close the serial port — this causes any blocked serial `Read()` to return an error, unblocking the read goroutine.
3. Call the base port's `Stop()`, which closes the node acquisition goroutine.

---

## Windows-Specific Notes

- **Serial port naming:** On Windows, serial ports are named `COMn`. For port numbers above 9 (e.g. `COM10`), some APIs require the prefix `\\.\` (e.g. `\\.\COM10`). The serial library used should handle this transparently, but check if `COM10` does not open correctly.
- **USB CDC latency:** Windows USB serial drivers often buffer incoming bytes for up to 16ms by default (the "latency timer"). At 1 Mbit/s this is unlikely to be a bottleneck, but for low-latency behavior the latency timer can be reduced in Device Manager → Ports → Advanced settings.
- **Driver selection:** TashTalk hardware appears as a USB CDC serial device. Windows 10/11 includes a built-in CDC driver (usbser.sys) that should work without additional installation. Older systems may require a driver from the TashTalk project or a generic CDC driver.
- **Baud rate support:** The 1 Mbit/s baud rate is non-standard. Most USB CDC virtual COM port drivers support arbitrary baud rates (the baud rate is a hint to the USB device, not an OS-enforced constraint). If the port fails to open at 1,000,000 baud, try the serial library's raw baud-rate setting mechanisms.

---

## TODO / Known Limitations

- **No outbound escape encoding:** Outbound frames are sent as raw bytes after a `0x01` start marker, without escape encoding. This works because the TashTalk firmware is designed to receive frames this way, but it means the host-to-device protocol is asymmetric with the device-to-host protocol. Future hardware revisions or alternative firmware could require escape encoding in both directions.
- **No reconnection logic:** If the TashTalk hardware is unplugged while the router is running, the serial port will return errors and the read goroutine will spin on errors. A future implementation could detect hardware disconnection and attempt to reopen the port.
- **Hardware flow control (RTS/CTS) is enabled.** The host link runs at 1 Mbit/s while TashTalk clocks frames onto LocalTalk at 230.4 kbaud, so the adapter must be able to throttle the host or its receive buffer overruns mid-frame — and a truncated LLAP frame simply fails FCS and disappears. The reference implementation, `tashrouter`, opens its port with `rtscts=True` for the same reason. It can be disabled per-port with `no_flow_control = true` for an adapter whose CTS line is not wired.

  Note this is the **serial line's** RTS/CTS, which is distinct from the **LLAP protocol's** RTS/CTS handshake that precedes a directed LocalTalk frame. The latter is handled by the TashTalk hardware, so the host never synthesises it — see [09-port-localtalk-base.md](09-port-localtalk-base.md).
- **Shared medium visibility:** Unlike LToUDP (where all participants see all frames), TashTalk only delivers to the host the frames that the TashTalk hardware decides to pass up. Specifically, frames on the physical LocalTalk bus addressed to other nodes may or may not be visible depending on the firmware. The AARP-equivalent process (LLAP ENQ/ACK) depends on the hardware forwarding those control frames to the host.
