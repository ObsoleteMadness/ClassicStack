# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

ClassicStack is a Go-based AppleTalk Phase 2 router and AFP file server. It also supports other legacy protocols such as NetBEUI, NetBIOS and SMB. 
It bridges legacy Apple networking protocols to modern environments, supporting EtherTalk (raw Ethernet), LToUDP (multicast UDP), TashTalk (serial), and virtual LocalTalk transports.

**Module:** `github.com/ObsoleteMadness/ClassicStack`  
**Go version:** see `go.mod` (`go` directive) — kept current there, not duplicated here

## Remember!
1. Always confirm implementation details with the specifications found in /spec/*.md
2. Use consts rather than hard-coded values, especially for responses, errors, etc. Prefer grouping them in in a const file per package/area rather than throughout the source code.
3. Use the names from the specification for functions, consts, etc and include a comment with a breif description from the spec for any functions.
4. Captures of protocols can be found in /captures. Use `tshark` to review protocol captures to aid in diagnosing faults. Under windows `tshark` is in `c:\Program Files\Wireshark\tshark.exe`
5. When the observation from a capture differs from the **spec**, document it in the code and in `/spec/errata.md`. REMEMBER: OUR OWN BUGS ARE NOT ERRATA. Captures of "real" clients/servers should be prioritised as "golden" implementations. 
6. Where we do not have a spec and implementation is from observation, add details on wire format, observed commands, observed responses. Eg, the MacIPX Gateway implementation will be based on observed IPX encapsulation over AppleTalk traffic between a Novell Server and a Macintosh MacIPX client.
7. If code is from 3rd parties or based on it, **Always** attribute it to the original authors. Our code base is licensed under the GPL3 - make sure code used is compatible with the license. If the upstream code is say MIT/Apache/BSD licensed, that code can be explictly used under dual license of either GPL3 or the original MIT/Apache/BSD license. Always respect the intent of the author: eg MIT must attribute the author in documentation, or readme files, or about boxes. Include license details in the code headers.
8. Check for linting errors before committing.
9. Run gofmt before commiting.
10. Use DTOs for protocol level representations. Ie rather than manipulating bytes in protocol function calls, each struct should be self serialising/deserialising.  Eg a `processRequest(data []byte)` method should call request.Unmarshal(data []byte) rather than attempt to decode the request in the function body. 
11. While we run on desktop (Linux, MacOS, Windows), the project aims to run on memory constrained devices. Where possible, use zero-copy, sync.pool, etc. 
12. Fuctions handling data should always emit a debug log. Eg DHCP relay log a debug log with the request/response. MacTCP should log debug logs when a session is established/renewed/ended. Errors must always be logged to error log, not silently ignored. 


## Commands

```bash
# Build
go build -tags all -o classicstack ./cmd/classicstack

# Run all tests
go test -tags all ./...

# Run tests for a specific package
go test -tags all ./core/service/afp/...

# Run with TOML config
./classicstack  # auto-loads server.toml if present

# Run with flags (see README.md for full list)
./classicstack -ethertalk eth0 -zone "MyZone"
```

## Architecture

ClassicStack is a hexagonal (ports-and-adapters) architecture split into five
rings — `core/`, `adapter/`, `compose/`, `client/`, `cmd/` — plus a `hardware/`
tree for embedded targets. **[`ARCHITECTURE.md`](ARCHITECTURE.md) is the
authoritative, maintained description of this layout** (the ring diagram, the
per-ring import rules, the full package map, and how a request moves through
the system); read it before orienting in the tree. Summary:

- **`core/`** — protocol-pure logic: wire codecs (`core/protocol/*`), the
  router (`core/router`), file/network services (`core/service/*`), the
  storage seam (`core/fs`, `core/share`, `core/metastore`). Imports stdlib
  only (enforced by `core/internal/archtest`), so it stays TinyGo-safe for the
  embedded targets under `hardware/`.
- **`adapter/`** — the real world: `adapter/link` (pcap/ltoudp/tashtalk NICs),
  `adapter/control` (http/ubus/inproc management front-ends), `adapter/config`,
  `adapter/store`, `adapter/metastore`, `adapter/fork`. May import `core/`.
- **`compose/`** — wiring only: `compose/registry` (name → factory),
  `compose/runtime` (build + cross-wire), `compose/supervisor` (lifecycle,
  dependency-ordered start/stop). Turns a `config.Model` into a running,
  supervised stack. May import `core/` and `adapter/`.
- **`client/`** — the outbound mirror of the server: `client/afp`,
  `client/smb`, `client/ncp`, `client/etherdfs` dial a remote server and
  present it as an `fs.FileSystem` via `client/link`'s transport `Opener`.
- **`cmd/`** — thin entry points only. `cmd/classicstack/main.go` hands off
  immediately to `cmd/internal/cli`, the shared run-core that parses flags/
  `server.toml`, builds the `compose/runtime` stack, optionally serves the
  web-admin control API (`-tags webui`), and runs until SIGINT/SIGTERM.
  `cmd/classicstack-svc` (Windows service) and `cmd/classicstackd` (Unix/macOS
  daemon) wrap the same run-core for background operation. `cmd/cs-tinygo` is
  the embedded compile-smoke target (blank-imports the TinyGo-safe `core/`
  subset); `cmd/csfs`/`cmd/csmount`/`cmd/csclient` are file-client CLIs over
  the `client/` SDK; the rest (`cmd/csecho`, `cmd/csnbp`, …) are AppleTalk/IPX
  diagnostic probes.

### AFP Architecture

AFP supports two transport stacks simultaneously:
- **Classic:** DDP → ATP → ASP → AFP
- **Modern:** TCP → DSI → AFP

AppleDouble metadata is stored either as `._filename` sidecars or in `.appledouble/` folders (Netatalk-compatible). CNID tracking (`core/metastore`) defaults to an in-memory store; the `sqlite` build tag swaps in a SQLite-backed one (`modernc.org/sqlite`) instead.

### Configuration

Copy `server.toml.example` to `server.toml`. Format is TOML (`pelletier/go-toml`), loaded into `core/config.Model` (see `core/config/config.go`). Section keys mirror the example file: singletons like `[identity]`, `[router]`, `[MacIP]`, `[http]`; repeated (named-instance) sections like `[[ethertalk]]`, `[[ltoudp]]`, `[[afpvolumes]]`, `[[smbshares]]`. File extension→type/creator mappings live in `extmap.conf` (Netatalk-compatible format). Full key-by-key reference: [`docs/config.md`](docs/config.md).

### Protocol Specifications

The `spec/` directory contains markdown documents describing the internal protocol design (e.g. `spec/17-ncp.md` for the Novell NCP file service). Start with `spec/00-overview.md` for DDP socket assignments and service interface contracts before modifying router or service code.


### Bridge
Bridge in this project represents the up-link interface for the clients. It's the bridge between our internal network stack and the outside world. It's members are ClassicStack and the specified interface. 