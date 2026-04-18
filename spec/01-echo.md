# Echo Service Specification

## Purpose

The Echo service implements AppleTalk's Echo Protocol. It responds to Echo Request datagrams by sending back an identical payload, allowing nodes to test reachability and measure round-trip time.

## Identity

| Property | Value |
|---|---|
| Socket | 4 |
| DDP Type | 4 |

## Packet Format

Echo packets are DDP datagrams with DDP type 4, delivered to socket 4. The payload has the following structure:

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 1 byte | Function | 0x01 = Request, 0x02 = Reply |
| 1 | variable | Echo data | Arbitrary payload bytes |

## Behavior

### On Receiving an Echo Request (Function = 0x01)

1. Construct a reply datagram by copying the received datagram.
2. Set the reply's Function byte (offset 0) to 0x02 (Reply).
3. Leave all other payload bytes unchanged.
4. Send the reply back to the sender using the router's Reply method.

### On Receiving an Echo Reply (Function = 0x02)

Echo replies are not addressed to or handled by the router itself; they are destined for end nodes. The router should not normally receive a reply addressed to its own socket. If one is received, it may be silently ignored.

### On Receiving Unknown Function Codes

Silently ignore the datagram.

## Notes

- The echo payload is reflected verbatim; the service does not inspect or modify bytes beyond offset 0.
- The service is stateless; each request is handled independently with no ongoing state.
- The Reply routing method handles reversing source and destination addresses, so the service does not need to compute the reply address manually.
