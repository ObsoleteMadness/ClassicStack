# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

ClassicStack is a Go-based AppleTalk Phase 2 router and AFP file server. It also supports other legacy protocols such as NetBEUI, NetBIOS and SMB. 
It bridges legacy Apple networking protocols to modern environments, supporting EtherTalk (raw Ethernet), LToUDP (multicast UDP), TashTalk (serial), and virtual LocalTalk transports.

**Module:** `github.com/ObsoleteMadness/ClassicStack`  
**Go version:** 1.23.0

## Remember!
1. Always confirm implementation details with the specifications found in /spec/*.md
2. Use consts rather than hard-coded values, especially for responses, errors, etc. 
3. Use the names from the specification for functions, consts, etc and include a comment with a breif description from the spec for any functions.
4. Captures of protocols can be found in /captures. Use `tshark` to review protocol captures to aid in diagnosing faults. 
5. When the observation from a capture differs from the spec, document it in the code and in `/spec/errata.md`
6. Where we do not have a spec and implementation is from observation, add details on wire format, observed commands, observed responses. Eg, the MacIPX Gateway implementation will be based on observed IPX encapsulation over AppleTalk traffic between a Novell Server and a Macintosh MacIPX client.
7. If code is from 3rd parties, **Always** attribute it to the original authors. 
8. Check for linting errors before committing.
9. Run gofmt before commiting.


## Commands

```bash
# Build
go build -tags all -o classicstack ./cmd/classicstack

# Run all tests
go test ./...

# Run tests for a specific package
go test ./service/afp/...

# Run with TOML config
./classicstack  # auto-loads server.toml if present

# Run with flags (see README.md for full list)
./classicstack -ethertalk eth0 -zone "MyZone"
```

## Architecture

### Core Data Flow

```
cmd/classicstack/main.go  →  Ports  →  Router  →  Services
```

1. **Entry point** (`cmd/classicstack/`) parses CLI flags and `server.toml`, constructs ports, wires them to the router, and starts services.
2. **Router** (`router/`) receives DDP datagrams from all ports, maintains the `RoutingTable` and `ZoneInformationTable`, and dispatches to services by socket number or forwards to other ports.
3. **Ports** (`port/`) abstract network interfaces. All implement `port.Port` (Unicast/Broadcast/Multicast). Implementations: `ethertalk`, `localtalk/ltoudp`, `localtalk/tashtalk`, `localtalk/virtual`.
4. **Services** (`service/`) plug into the router by registering socket numbers. Each implements `service.Service`.

### Key Packages

| Package | Role |
|---|---|
| `appletalk/` | DDP datagram struct, encode/decode, MacRoman codec |
| `router/` | Core routing engine, routing table aging, zone info |
| `port/ethertalk/` | EtherTalk over raw Ethernet using libpcap/Npcap, includes AARP |
| `port/localtalk/` | LocalTalk base; subpackages: LToUDP (UDP multicast 239.192.76.84:1954), TashTalk (serial at 1 Mbit/s), Virtual |
| `service/rtmp/` | Routing Table Maintenance Protocol — `RespondingService` + `SendingService` |
| `service/zip/` | Zone Information Protocol — `RespondingService` + `SendingService` |
| `service/afp/` | Apple Filing Protocol file server (largest subsystem, 35 files) |
| `service/asp/` | AppleTalk Session Protocol — AFP transport over DDP |
| `service/atp/` | AppleTalk Transaction Protocol — reliable messaging |
| `service/dsi/` | Data Stream Interface — AFP transport over TCP |
| `service/macip/` | IP-over-AppleTalk gateway with NAT and DHCP relay |
| `netlog/` | Structured logger with debug/info/warn levels |

### AFP Architecture

AFP supports two transport stacks simultaneously:
- **Classic:** DDP → ATP → ASP → AFP
- **Modern:** TCP → DSI → AFP

AppleDouble metadata is stored either as `._filename` sidecars or in `.appledouble/` folders (Netatalk-compatible). CNID tracking uses SQLite (`modernc.org/sqlite`).

### Configuration

Copy `server.toml.example` to `server.toml`. Format is TOML (parsed via `knadh/koanf` + `pelletier/go-toml`). Sections: `[LToUdp]`, `[TashTalk]`, `[EtherTalk]`, `[MacIP]`, `[AFP]`, `[Volumes.*]`, `[Logging]`. File extension→type/creator mappings live in `extmap.conf` (Netatalk-compatible format).

### Protocol Specifications

The `spec/` directory contains 14 markdown documents describing the internal protocol design. Start with `spec/00-overview.md` for DDP socket assignments and service interface contracts before modifying router or service code.
