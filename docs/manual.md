---
title: "Full Manual"
weight: 8
---

# ClassicStack Manual

First-pass operator and developer guide. For wire-level protocol notes see [`spec/`](../spec/); for the runtime map see [`ARCHITECTURE.md`](../ARCHITECTURE.md). Configuration field detail lives in [`server.toml.example`](../server.toml.example).

Focused documents split out of this manual: [quickstart.md](quickstart.md),
[build.md](build.md) (build tags), [config.md](config.md) (full config key reference),
[protocols.md](protocols.md) (supported protocol versions), [netboot.md](netboot.md),
[testing.md](testing.md), and [web-ui.md](web-ui.md) (control API + `classicstack-web`
reuse).

---

## 1. What ClassicStack does

ClassicStack is an **AppleTalk Phase 2 router** and a **classic LAN services stack**. It bridges legacy Macintosh and DOS networking into modern hosts (Linux, macOS, Windows), and can also run as a file *client* against the same protocols it serves.

In practice it does:

| Role | What you get |
|---|---|
| **Router** | AppleTalk Phase 2 across EtherTalk (raw Ethernet), LocalTalk-over-UDP (LToUDP), and TashTalk (serial LocalTalk). RTMP/ZIP keep routes and zones coherent between ports you attach to the router. |
| **File server** | AFP (classic DDP and modern TCP/DSI), SMB1 (over TCP, NetBEUI, IPX/NBIPX), Novell NCP (NetWare 3.x-style bindery), and EtherDFS (DOS over EtherType `0xEDF5`). Shares can back the same host path so AFP/SMB/NCP see each other’s changes. |
| **Gateways** | **MacIP** — IP-over-AppleTalk for MacTCP clients (bridge or NAT). **MacIPX** — IPX-over-AppleTalk for the classic MacIPX client. |
| **LAN presence** | NetBIOS name service / browser, WinPopup-style Messenger, optional AppleTalk **Netboot** (`.netBOOT` / ChainBoot). |
| **File client** | Connect *out* to remote AFP / SMB / NCP / EtherDFS shares; browse them in the web Finder, mount them with FUSE/WinFsp (`csmount`), or drive them from the CLI (`csfs`). |
| **Operator UI** | HTTPS/HTTP management SPA (default `:1984`) — Finder-first, plus status, sharing, settings, topology, and live logs. |

Optional protocol hooks are gated by Go build tags (`afp`, `smb`, `ipx`, `netbeui`, `macip`, `webui`, `pcap`, `fuse`, …). A typical full desktop build is:

```bash
go build -tags all -o classicstack ./cmd/classicstack
```

The project is pragmatic and evolving — validate behaviour in your environment before relying on it in production.

---

## 2. Command-line tools

Binaries live under `cmd/`. File-client tools share the **client SDK** (`client/`) and the same URI grammar (see §5).

### Server and lifecycle

| Command | Purpose |
|---|---|
| **`classicstack`** | Interactive server. Loads `server.toml` (or `-config`), starts ports/services, optional web UI. Flags: `-config`, `-http`, `-version`, `-list-pcap-devices`. |
| **`classicstack-svc`** | Windows service wrapper (`install` / `uninstall` / `start` / `stop` / `status` / `run`). Same runtime as `classicstack`. |
| **`classicstackd`** | Unix/macOS daemon (`start` / `stop` / `status` / `run`). On macOS, `install` / `uninstall` manage a LaunchAgent. |

Quick start:

```bash
cp server.toml.example server.toml
# edit bridges, ports, volumes…
./classicstack                    # auto-loads ./server.toml
./classicstack -config /path/to/server.toml
./classicstack -http :1984        # override web listen; implies http enabled
```

### File client

| Command | Purpose |
|---|---|
| **`csfs`** | Cross-platform CLI over the client SDK (`cmd/csclient`). `ls` / `cp` / `get` / `put` / `mv` / `rm` / `attrib` / `type` / `creator` / `discover`, or a bare URI for an interactive REPL. Preserves resource forks, Finder type/creator, and DOS attributes across host↔remote copies. |
| **`csmount`** | Mount a remote share as a host filesystem: WinFsp (Windows), macFUSE (macOS), libfuse (Linux). Same URI/flags as `csfs`. Ctrl-C unmounts cleanly. |

URI examples:

```text
afp://classicstack:MyZone/Volume
smb://pete:secret@host,tcp/share
smb://server,nbf/Share
ncp://SERVER,ipx/SYS
etherdfs://02-1a-4d-11-22-33/C
```

Shared flags (see `csfs -h` / `csmount -h`):

- `-ifacetype` — `ltoudp` | `tashtalk` | `pcap` | `tcp` (validated against the scheme)
- `-iface` — bind address / pcap device / serial port / TCP host
- `-transport` — SMB pcap carrier: `ipx` | `nbipx` | `nbf`
- `-mac` — virtual-station MAC for raw-Ethernet carriers
- `-fork` — host fork container (`appledouble`, `native`, `hfs`, `ads`, …)
- `-v` — wire trace to stderr

Build hints:

```bash
go build -tags pcap -o csfs ./cmd/csclient
go build -tags "pcap fuse" -o csmount ./cmd/csmount   # macOS/Linux need fuse
# Windows: go build -tags pcap -o csmount.exe ./cmd/csmount
```

### AppleTalk diagnostics

| Command | Purpose |
|---|---|
| **`csecho`** | AEP echo (AppleTalk “ping”). Default transport LToUDP; `-transport tashtalk` or `pcap` for others. |
| **`csnbp`** | NBP lookup (`object:type@zone`), like netatalk `nbplkup`. Wildcards: `=` for object/type, `*` for this zone. |
| **`csgetzones`** | ZIP zone list (`GetZoneList` / `-local` / `-my`), like netatalk `getzones`. |

### IPX / NetBIOS / NetWare helpers

| Command | Purpose |
|---|---|
| **`csipxping`** | IPX Diagnostic request/response over Ethernet (needs `-tags pcap`). |
| **`csncpinfo`** | SAP “file server” discovery (SLIST-style); `-frametype` must match the server’s IPX encapsulation. |
| **`csnetview`** | SMB workgroup “net view” via master browser + `NetServerEnum2` (not just broadcast sniff). Carriers: NBF / NB-IPX. |
| **`csnetsend`** | NetBIOS Messenger / WinPopup datagram to `name,nbf` or `name,nbipx`. |

### Embedded / experimental

| Command | Purpose |
|---|---|
| **`cs-tinygo`** | TinyGo-oriented build smoke for the memory-constrained / embedded ring (ports, router, core codecs). Not a desktop operator tool. |

---

## 3. `server.toml` configuration

Copy [`server.toml.example`](../server.toml.example) to `server.toml` and edit. The interactive binary auto-loads `./server.toml`; use `-config` to point elsewhere. A missing file is fine — the stack boots on built-in defaults.

**Important:** when the web UI (or any control-plane Save) rewrites the file, it keeps **values only** (comments are dropped) and backs up the previous file as `server.toml.NNNN`. Treat hand-written comments as reference material.

### Mental model

```text
[[interface]]     → uplink bridge (pcap / tap / raw over a host NIC)
[[ethertalk]] …   → ports (transports) bound to a bridge, or carrying their own bind (LToUDP, TashTalk)
services          → AFP, SMB, NCP, EtherDFS, MacIP, … riding those ports
[router]          → which AppleTalk ports join RTMP/ZIP + inter-port forwarding
```

Only sections whose component was compiled in are honoured; unknown sections are ignored.

The one piece worth internalising before anything else: **router membership is explicit,
not implicit.** A port (`[[ethertalk]]`, `[[ltoudp]]`, `[[tashtalk]]`) can be `enabled`
and still run *standalone* — its own segment, reachable, capturable — without joining
the AppleTalk router's RTMP/ZIP and inter-port forwarding. Only ports named in
`[router].members` actually route:

```toml
[router]
default_zone = "EtherTalk Network"
members = ["EtherTalk", "LToUDP"]   # empty = no port joins (explicit-over-implicit)
```

Everything else — the interface/bridge namespace, every port's fields, every service
singleton and its shares, the web UI/client/FUSE keys — is documented section-by-section
in **[config.md](config.md)**, generated from the same `server.toml.example` linked
above. This manual won't repeat it.

---

## 4. Web UI (including Finder)

Available in builds with `-tags webui` (included in `-tags all`); open
`http://127.0.0.1:1984/` (or whatever `[http].addr` / `-http` you set) once running. For
how the SPA is built (`make spa`) and how it's put together — the transport-agnostic
`core/control.Plane` contract, its `http`/`ubus`/`inproc` adapters, and how the UI reuses
components with the standalone LocalTalk PWA via `classicstack-web` — see
[web-ui.md](web-ui.md). This section is the operator's tour of what's on screen.

### First run

If no `[adminauth]` exists, the UI enters **setup** (HTTP 409 from the status probe) and asks you to create an admin user. Afterwards, HTTP Basic auth gates the control API.

### Finder (primary surface)

The SPA is **Finder-first**. The browser does not speak AFP/SMB/NCP/EtherDFS; the Go process does, over `/finder`.

With `[Client]` enabled, ClassicStack:

- Probes the LAN for servers in the configured schemes
- Tracks connections and open volumes
- Exposes catalog I/O (`children`, `get`, `mkdir`, `rename`, transfers, …) to the SPA

Local shares on *this* instance appear alongside remote servers. Volume chrome (icons, Get Info fields, path punctuation) follows each volume’s **capabilities** (`shareKind`, `addressBy` CNID vs path, fork/metadata flags) — see [`spec/20-finder-catalog.md`](../spec/20-finder-catalog.md).

Useful Finder affordances:

- Browse folders, copy/move across volumes (native CNID or path refs)
- Get Info, resource-fork explorers (Macintosh / Windows)
- Extension map editor (AFP type/creator by extension)
- Login dialogs for authenticated remotes; idle sessions disconnect after `max_idle_minutes`

### Admin windows (app menu)

| Window | Role |
|---|---|
| **Status / control plane** | Start, stop, restart services live |
| **Settings** | Server identity, transports, client/FUSE, protocol options |
| **Sharing** | Add/update/remove AFP volumes, SMB shares, NCP volumes, EtherDFS drives; **Save** writes `server.toml` |
| **Topology** | Port/router membership at a glance (“Attach to AppleTalk router”) |
| **Logs** | Live log stream with client-side level filter |
| **MacIP leases** | Lease table when MacIP is running |
| **About** | Version / attribution |

Notifications (bell) surface AFP login/server messages and NetBIOS Messenger pop-ups when the stack receives them.

The same operations are available through the transport-agnostic control API (`core/control.Plane`, driven here by the HTTP adapter under `adapter/control/http`) — see [web-ui.md](web-ui.md).

---

## 5. Extending ClassicStack — the client SDK

The **file-client SDK** lives in the top-level `client/` package. It is the client-side mirror of `core/fs.BuildShare`: you address a legacy server with a URI and get back an `fs.ForkFS` — the same interface servers implement — so remote and local volumes look alike to copy tools, mounts, and the Finder adapter.

### Package map

| Package | Role |
|---|---|
| `client` | `RegisterClient` / `Connect` — scheme registry + fork/meta wrap |
| `client/uri` | URI parse → `Target` |
| `client/link` | Transport opener (`pcap`, `ltoudp`, `tashtalk`, `tcp`, …) |
| `client/afp`, `smb`, `ncp`, `etherdfs` | Scheme factories (blank-import to register) |
| `client/atalk` | AppleTalk endpoint (NBP, AEP, ZIP helpers) |
| `client/netbios`, `client/browse` | Messenger + SMB browse |
| `client/xfer` | Host↔remote copy preserving forks/attrs |
| `client/fuse`, `client/winfsp` | Host mount adapters |
| `cmd/internal/csconnect` | Shared CLI flag/URI plumbing used by `csfs` / `csmount` |

### URI grammar

```text
<scheme>://[[user][:pass]@]<server>[,<transport>]/<volume>[/<path>]
```

`<server>` and `<volume>` are **protocol-native** (opaque to the parser): AFP may use `name:zone` or `net.node`; EtherDFS uses dash- or bare-hex MACs (never colon-separated).

### Minimal Go example

```go
package main

import (
	"context"
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/client"
	"github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"

	_ "github.com/ObsoleteMadness/ClassicStack/client/afp" // register scheme
)

func main() {
	target, err := uri.Parse("afp://guest@MyServer:MyZone/Public")
	if err != nil {
		panic(err)
	}
	opener := &link.Opener{ /* Kind, Device/Addr from your flags */ }
	forkFS, err := client.Connect(context.Background(), target, client.Options{
		Opener: opener,
		// ForkBackend: "passthrough", // AFP default
	})
	if err != nil {
		panic(err)
	}
	defer fs.CloseFS(forkFS)

	ents, err := forkFS.ReadDir("")
	if err != nil {
		panic(err)
	}
	for _, e := range ents {
		fmt.Println(e.Name())
	}
}
```

### Adding a scheme

1. Implement a `client.Factory` that dials through `opts.Opener`, authenticates, opens the volume, and returns a `fs.FileSystem` (optionally `fs.ForkEngine` for native forks).
2. Call `client.RegisterClient("myscheme", defaultFork, transports, factory)` from an `init()` in your package.
3. Blank-import the package from your binary (same pattern as `cmd/csclient`).
4. Prefer DTOs that marshal/unmarshal wire formats; reuse codecs under `core/protocol/` where they exist.

`csfs`, `csmount`, the in-process `[Client]`, and the web Finder are all consumers of this SDK — extend once, every front-end benefits.

---

## 6. Credits

ClassicStack is released under **GPL-3.0**. Some components are based on differently licensed works; see [`NOTICE`](../NOTICE) for full license text. (This is not legal advice.)

The project stands on a lot of prior open-source work. Several subsystems are clean re-implementations over our storage/transport seams rather than line-for-line ports, but they owe a clear debt to the originals:

| Project / author | Contribution |
|---|---|
| **tashrouter** by **Tashtari** ([lampmerchant/tashrouter](https://github.com/lampmerchant/tashrouter), GPL-3.0) | Inspiration for the AppleTalk routing core |
| **macresources / rdump (DeRez)** by **Elliot Nunn** | Resource-fork text format behind the `derez` fork backend |
| **mars_nwe** (Martin Stover) and **ncpfs** (Volker Lendecke et al.) | Canonical open-source NetWare/NCP references for our NCP service |
| **atalk-proxy** by **joshua stein** | Proxy-AARP rule for bridging AppleTalk onto Wi‑Fi / tunnels |
| **NetBoot** by **Elliot Nunn**, with payload/PRAM groundwork by **Rob Braun (bbraun)** | Classic Mac ROM netboot / ChainBoot; Snefru-128 port behind `core/hash/snefru` |
| **macipgw** by **Stefan Bethke** and **Jason King** (GPLv2+) | Golden reference for the MacIP gateway ATP/config wire layout |
| **go-winfsp** / **cgofuse** by **Bill Zissimopoulos** | Windows / FUSE host mounts |
| **EtherDFS** by **Mateusz Viste** | EtherType `0xEDF5` DOS file-system protocol |
| **Icons8** | Icons used in the SPA / topology UI |

Thanks also to everyone who captured traffic, filed bugs, and kept vintage gear on the wire.

---

*Document status: first pass. Corrections and deeper sections (auth model, capture/debug workflow, per-protocol operator recipes) welcome.*
