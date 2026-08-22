---
title: "Configuration"
weight: 3
---

# Configuration reference

This is the per-section key reference for `server.toml`. For a guided walkthrough
(mental model, worked examples) see [manual.md §3](manual.md#3-servertoml-configuration);
for the fully commented, always-current source of truth see
[`server.toml.example`](../server.toml.example) at the repo root — every table below is
derived from it and from the config-section structs it round-trips, so if the two ever
disagree, trust the example file and the code, not this page.

Copy `server.toml.example` to `server.toml` and edit it. The interactive binary
auto-loads `./server.toml`; pass `-config` to point elsewhere. A missing file is fine —
the stack boots on built-in defaults. Only sections whose component was compiled in
(see [build.md](build.md#build-tags)) are honoured; unknown sections are ignored.

**Important:** when the web UI (or any control-plane Save) rewrites the file, it keeps
**values only** (comments are dropped) and backs up the previous file as
`server.toml.NNNN`. Treat hand-written comments as reference material, not something the
tool will preserve.

## Section shapes

- **Well-known singletons** (one per server): `[identity]`, `[logging]`, `[http]`,
  `[Client]`, `[FUSE]`, `[router]`, `[adminauth]` — lower-case.
- **The interface namespace**: repeated `[[interface]]` — the uplink bridge(s) only
  (pcap/tap/raw). Serial (TashTalk) and multicast (LToUDP) are **not** interfaces; those
  ports carry their own binding directly.
- **Ports**: repeated `[[ethertalk]]`, `[[ltoudp]]`, `[[tashtalk]]`, `[[ipx]]`,
  `[[netbeui]]` — lower-case array-of-tables. One instance of each by default; repeated
  so a config can name several (e.g. two TashTalk dongles).
- **Service singletons with exact-case keys**: `[MacIP]`, `[IPXGW]`, `[AFP]`, `[SMB]`,
  `[NetBIOS]`, `[NCP]`, `[EtherDFS]`, `[Netboot]`.
- **File-service shares**: repeated `[[afpvolumes]]`, `[[smbshares]]`, `[[ncpvolumes]]`,
  `[[etherdfsdrives]]`, plus auto-mounted client volumes as `[[fusevolumes]]`.

The layering: a bridge/uplink **interface** is just the wire. A **port** is a transport
stack bound to it (EtherTalk → bridge, LToUDP → host multicast, TashTalk → a serial tty,
IPX/NetBEUI → bridge), each opening its own capture stream. **Services** ride ports
(NetBIOS, AFP, SMB, NCP). The router has ports as members.

## `[identity]`

One identity, owned by no single service.

| Key | Default | Notes |
|---|---|---|
| hostname | classicstack | Also the AFP server name and, when NetBIOS/SMB is enabled, the NetBIOS computer name (capped at 15 bytes there). |
| workgroup | WORKGROUP | SMB workgroup / browse domain. |
| description | (empty) | Free-text server description. |

## `[logging]`

| Key | Default | Notes |
|---|---|---|
| Level | info | `debug` \| `info` \| `warn` \| `error`. (Capitalised `Level` — the section carries no TOML tag, so the codec emits the Go field name; both cases are accepted on read.) |

## `[router]`

Declares which transport **ports** join the AppleTalk router (RTMP/ZIP + inter-port
forwarding), by port name. A port not listed still comes up and serves its own segment,
but **standalone** — no RTMP/ZIP, no forwarding.

| Key | Default | Notes |
|---|---|---|
| default_zone | (empty) | Default AppleTalk zone. |
| members | (empty) | Port names that join the router (`"EtherTalk"`, `"LToUDP"`, `"TashTalk"`, …). Empty means **no** port joins — explicit-over-implicit, there is no "bind everything" default. Port names default to the section key unless a port instance sets its own `name`. |

~~~toml
[router]
default_zone = "EtherTalk Network"
members = ["EtherTalk", "LToUDP"]
~~~

The dashboard shows each port's `routed: on/off`; the same list is editable from the web
UI via the "Attach to AppleTalk router" checkbox on each transport.

## `[[interface]]` — the uplink bridge

The one interface concept: a bridge/uplink over a host NIC. An EtherTalk/IPX/NetBEUI
port binds a named bridge via its own `iface`; a port that names none inherits whichever
bridge has `default = true` (at most one).

| Key | Default | Notes |
|---|---|---|
| Name | — | Alias the ports reference (e.g. `br-lan`). |
| Kind | bridge | `bridge` (pcap/tap/raw over a host NIC). |
| Backend | pcap | `pcap` \| `tap` \| `raw`. |
| Device | (empty) | Host adapter (pcap device name; on Windows `\Device\NPF_{GUID}`). Empty resolves at open time. |
| hw_address | (empty) | Station MAC stamped on pcap inject (EtherTalk/IPX/NetBEUI/EtherDFS). Blank = the NIC's own hardware address (required on Wi-Fi — APs drop frames not sourced from the NIC). Set a value only to spoof a distinct station on wired Ethernet. |
| default | false | Marks the bridge that un-bound ports inherit. |

~~~toml
[[interface]]
Name = "br-lan"
Kind = "bridge"
Backend = "pcap"
default = true
# Device = "eth0"
# hw_address = "DE:AD:BE:EF:CA:FE"
~~~

## Ports — `[[ethertalk]]`, `[[ltoudp]]`, `[[tashtalk]]`, `[[ipx]]`, `[[netbeui]]`

Every port shares one section shape; each reads only the fields that apply to it.

| Key | Applies to | Notes |
|---|---|---|
| name | all | Per-instance identity. Blank = the lone default instance (named after the section key). |
| enabled | all | true/false. |
| iface | EtherTalk, IPX, NetBEUI | The bridge to bind. Blank = the default bridge. |
| mac | EtherTalk, IPX, NetBEUI | Station MAC. Blank = the interface's `hw_address`, else the NIC's own. Leave blank on Wi-Fi. |
| seed_network / seed_network_end / seed_zone | EtherTalk, LToUDP, TashTalk | AppleTalk seed config for this segment. `seed_network_end` sets an extended-network range; 0 = single number. Zero range = non-seed. |
| device / baud | TashTalk | The host serial tty + line speed (serial is a port property, not an interface). |
| ipx_frame_type | IPX | Ethernet encapsulation for **outbound** frames: `ethernet_ii` (DIX, default, MacIPX-compatible) \| `802.3` (raw Novell) \| `802.2` (IEEE LLC). Inbound frames are accepted in any framing regardless. |
| ipx_network | IPX | IPX network number for this segment (0 = local/unknown). Same key as `[IPXGW].ipx_network` — set both the same when MacIPX clients should see the Ethernet IPX segment. |
| capture / capture_snaplen | all | pcap file to tee this port's wire traffic to (blank = off), and bytes stored per frame (0 = full frame). Needs `-tags pcap` for NIC transports. |
| pace_ms | LToUDP, TashTalk | Minimum inter-frame gap in ms. 0 = transport default; negative disables pacing. |

~~~toml
[[ethertalk]]
iface = "br-lan"
enabled = true
seed_network = 3
seed_network_end = 5
seed_zone = "EtherTalk Network"

[[ltoudp]]
enabled = true
seed_network = 1
seed_zone = "LToUDP Network"

[[tashtalk]]
enabled = false
device = "/dev/ttyAMA0"
baud = 1000000
seed_network = 2
seed_zone = "TashTalk Network"

[[ipx]]
iface = "br-lan"
enabled = false
ipx_frame_type = "ethernet_ii"

[[netbeui]]
iface = "br-lan"
enabled = false
~~~

LToUDP and TashTalk are distinct AppleTalk segments (own network, zone, node space) —
the router can bridge both at once.

## `[MacIP]`

The IP-over-AppleTalk gateway for MacTCP clients (DDP type 22, socket 72). Rides the
AppleTalk router. Requires build tag `macip` (and `router`). Only one `[MacIP]` section
is allowed. See [`spec/14-macip-gateway.md`](../spec/14-macip-gateway.md).

| Key | Default | Notes |
|---|---|---|
| enabled | false | Gate the gateway on/off. |
| mode | bridge | `bridge` (proxy-ARP onto an existing subnet) or `nat` (hand out a private subnet and NAT upstream). Use `nat` on Wi-Fi — bridge mode injects IP/ARP on the wire and APs drop frames not sourced from the host NIC. |
| zone | (empty) | AppleTalk zone for the `IPGATEWAY` NBP name. Blank = router default. |
| gateway_ip | — | IPv4 identity advertised to clients (also the NBP object name). |
| network | (empty) | Subnet base. Blank = derived from `gateway_ip` + `subnet_mask`. |
| nameserver | (empty) | DNS server advertised to clients. |
| broadcast | (empty) | Subnet broadcast. Blank = derived. |
| subnet_mask | 255.255.255.0 | Mask advertised to clients. |
| host_count | 0 (→254) | Lease-pool slot count, including the network (`.0`) and gateway (`.1`) reserved slots. |
| interface | — | `[[interface]]` name to bridge IP traffic onto. Empty = AppleTalk-only. |
| host_mac / host_ip | (empty) | Ethernet MAC / host IPv4 on the uplink. Blank = auto-detect. |
| default_gateway | (empty) | Upstream router for off-subnet bridge egress. |
| dhcp_relay | false | Relay DHCP for client addresses instead of the static pool (fabricates per-Mac MACs; does not work on Wi-Fi). |

## `[IPXGW]`

The MacIPX gateway — IPX-over-AppleTalk for the classic MacIPX client, DDP socket 78.
Requires build tag `ipxgw` (and `router`). See
[`spec/15-macipx-gateway.md`](../spec/15-macipx-gateway.md).

| Key | Default | Notes |
|---|---|---|
| enabled | false | Gate the gateway on/off. |
| ipx_network | 0 (→0x10) | IPX network number announced to clients. |
| bindings | [] | `"Object:Zone"` NBP names to advertise. Empty = one "IPX Gateway" name per zone the router knows. |

## `[AFP]` / `[[afpvolumes]]`

Requires build tag `afp`. See [protocols.md](protocols.md#afp-apple-filing-protocol) for
which AFP versions this speaks.

`[AFP]` (singleton — advertised identity + transport bindings):

| Key | Default | Notes |
|---|---|---|
| server_name | (empty) | Chooser/NBP name. Blank = `identity.hostname`, then `"ClassicStack"`. |
| zone | (empty) | AppleTalk zone to advertise into. Blank = router default. |
| transports | (empty → all built) | `"ddp"` (classic, ASP/ATP/DDP) and/or `"tcp"` (modern, DSI — see [protocols.md](protocols.md#afp-apple-filing-protocol) and `spec/21-dsi.md`). |
| tcp_addr | (empty) | DSI/TCP listen address (e.g. `:548`). Never binds implicitly — must be set explicitly, same posture as SMB's direct-TCP `tcp_addr`. |
| login_message | (empty) | Opt-in greeting shown when a client mounts a volume (`FPGetSrvrMsg`, max 199 chars). |

`[[afpvolumes]]` (repeated, one per exported volume):

| Key | Default | Notes |
|---|---|---|
| name | — | Volume display name (max 31 chars). |
| path | — | Host directory. |
| fs_type | local_fs | Filesystem backend: `local_fs`, `memfs`, `macgarden` (`-tags macgarden`), `zipfs` (`-tags zipfs`), … |
| fork_backend | (per-platform default) | `appledouble` \| `ads` \| `xattr` \| `hfs` \| `native` \| `auto`. `native` = the host's own layout (ads on Windows, hfs on macOS, xattr on Linux). |
| filename_codec | (default) | Wire↔store name codec. |
| metastore | mem | Where CNIDs/short-name mappings persist: in-memory (default) or `sqlite` for a durable store. |
| meta_backend | (per-platform default) | Where derived names/DOS attributes live: `metastore` \| `xattr` \| `ads`. |
| extmap_path | (global map) | Per-volume type/creator extension map file (Netatalk-style `extmap.conf`). Empty = the global map. |
| read_only | false | Makes the whole volume read-only. |
| allowed_users | (empty → guest/world) | Access allow-list. |
| options | (empty) | Backend-specific `"key=value"` params. |
| size_limit | 0 (→512) | Volume size **reported** to clients, in MiB. Classic Macs derive their allocation-block size from this (≈ size/65536) — it's presentation only, it does not limit what the host stores. |

See [`spec/16-storage-seam.md`](../spec/16-storage-seam.md) for the shared storage seam
(`fs_type`, fork engines, `meta_backend`) all four file services sit on.

## `[SMB]` / `[[smbshares]]`

Requires build tag `smb`. See [protocols.md](protocols.md#smb-server-message-block--cifs)
for the exact dialects negotiated.

`[SMB]` (singleton — not shown in `server.toml.example` today, but a real,
codec-round-tripped section):

| Key | Default | Notes |
|---|---|---|
| enabled | true | Gate the SMB service on/off. |
| transports | (empty → all built) | `netbeui` (NBF), `ipx` (NB-IPX + direct-hosted SMB-over-IPX socket `0x0550`), `nbt` (NetBIOS-over-TCP), `tcp` (direct-hosted SMB-over-TCP, NetBIOS-less). |
| tcp_addr | (empty) | Direct-hosted SMB-over-TCP listen address. Never defaults to `:445` (Windows' own server usually owns it, and it's privileged on Unix) — must be set explicitly, e.g. `:4450`. |

Server identity (name/workgroup) comes from `[identity]`, not from `[SMB]`, so SMB and
NetBIOS can never disagree with each other.

`[[smbshares]]` (repeated) mirrors `[[afpvolumes]]` with an extra `description` (the
`NetServerEnum2` remark). An SMB share and an AFP volume on the same host path share a
mutation bus, so each sees the other's changes.

| Key | Default | Notes |
|---|---|---|
| name, path, fs_type, read_only, allowed_users | — | Same as `[[afpvolumes]]`. |
| description | (empty) | The `NetShareEnum` remark. |
| meta_backend | (per-platform default) | The `MetaEngine` for derived DOS/AFP names, CNIDs, and DOS attributes the host filesystem can't represent — there is no "off": 8.3 name derivation for DOS/Win16 clients is always on. Empty picks per-platform (`xattr` on Linux, `ads` on an NTFS-backed Windows share, else `metastore`); `metastore` is the universal fallback; `xattr`/`ads` fall back to a sidecar when the host doesn't support them. |

## `[NetBIOS]`

Requires build tag `netbios`. Not shown in `server.toml.example` today, but a real
section (`core/service/netbios/section.go`).

| Key | Default | Notes |
|---|---|---|
| transports | (empty → all built) | `netbeui`, `ipx`, and/or `nbt`. |
| scope_id | (empty) | NetBIOS scope appended to names (rarely used). |
| nbt_addr | (empty) | NetBIOS-over-TCP session-service listen address (conventionally `:139`). Never binds implicitly — must be set explicitly, same reasoning as SMB's `tcp_addr`. |

Server/workgroup identity comes from `[identity]`, upper-cased to the NetBIOS name.

## `[NCP]` / `[[ncpvolumes]]`

Requires build tag `ncp` (needs an enabled `[[ipx]]` port). NetWare 3.x-style bindery
emulation, rides IPX (socket `0x0451`) and advertises via SAP (socket `0x0452`). See
[`spec/17-ncp.md`](../spec/17-ncp.md).

| Key | Default | Notes |
|---|---|---|
| server_name | (empty) | `[identity].hostname`, upper-cased. |
| description | (empty) | `[identity].description`. |
| internal_network | 0 (auto) | The NetWare internal IPX network clients learn via SAP then RIP GetLocalTarget. 0 = derive from the station MAC. |

`[[ncpvolumes]]`: `name` (upper-case, e.g. `SYS`), `path`, `fs_type`, `read_only`,
`allowed_users` — same shape as the other file services. Bindery login validates
against the same user store AFP/SMB use (guest if none).

## `[EtherDFS]` / `[[etherdfsdrives]]`

Requires build tag `etherdfs`. DOS file service over raw EtherType `0xEDF5` — no
IP/TCP/NetBIOS, and **no authentication** (any client that can reach the server's MAC
may use any drive, gated only by `read_only`/`allowed_users`). Only one EtherDFS
instance can run per NIC. See [`spec/18-etherdfs.md`](../spec/18-etherdfs.md).

`[EtherDFS]` (singleton — the wire endpoint):

| Key | Default | Notes |
|---|---|---|
| enabled | true | |
| iface | (default bridge) | The bridge/uplink to bind. |
| mac | (NIC's own) | Optional station MAC override. Blank required on Wi-Fi. |
| server_name | (empty) | Advertised in install checks. Blank = `[identity].hostname`. |
| capture / capture_snaplen | (off) | Same as the port capture fields. |

`[[etherdfsdrives]]` (repeated, one per drive letter): `name` (A–Z), `path`, `fs_type`,
`meta_backend`, `read_only`, `allowed_users`.

## `[Netboot]`

See [netboot.md](netboot.md) for the full protocol write-up. Requires build tag
`netboot` (and `router`). Section key is exact-case: `[Netboot]`, not `[netboot]`.

~~~toml
[Netboot]
enabled = true
payload = "/srv/netboot/BootWrapper.bin"
image = "/srv/netboot/system607.dsk"
block_size = 512
disk = "/srv/netboot/system71.dsk"
pace_ms = 2
chain_pace_ms = 10
name = "0000"
zone = "*"
~~~

## `[http]` — web admin UI

| Key | Default | Notes |
|---|---|---|
| enabled | true | Set false to turn the web UI off entirely. |
| addr | :1984 | Listen address. `-http :port` on the command line overrides this and implies `enabled = true`. |

## `[Client]` — in-process file client

Off by default. When enabled, this process also acts as a file *client*: it scans the
LAN at startup for AFP/SMB/NCP/EtherDFS servers and tracks connections/open volumes for
the operator Finder (`GET /finder/state`, `/finder/discover`, `/finder/mounted`).

| Key | Default | Notes |
|---|---|---|
| enabled | false | Gate the client on/off. |
| iface | (default bridge) | `[[interface]]` name to bind. |
| name | (empty → identity.hostname) | NetBIOS/SMB name the outbound client presents. |
| mac | (interface hw_address, or NIC's own) | Ethernet source the outbound client presents. Set this when the client shares an interface with the server's own ports, to give the client a distinct station. |
| services | all four | Schemes to probe/connect: `afp`, `smb`, `ncp`, `etherdfs`. |
| max_idle_minutes | 10 | Unused remote session idle time before disconnect. |
| mount | false | Allow FUSE (macFUSE/libfuse) or WinFsp host mounts of remote volumes the client opens. |
| log_file | (none) | Extra log path for client/Finder traffic. |

## `[FUSE]` / `[[fusevolumes]]`

Host mounts of remote volumes. Auto-mount requires `[Client].enabled = true` and
`[Client].mount = true`, and a binary built with FUSE (`-tags fuse`) or WinFsp on
Windows.

| Key | Default | Notes |
|---|---|---|
| mount_timeout_seconds | 30 | How long to wait to connect to the remote server; auto-mount retries until this deadline. |

`[[fusevolumes]]` (repeated, auto-mounted at startup): `remote` (client URI), `mountpoint`
(host directory or Windows drive letter), `read_only`.

## `[adminauth]`

Written by the **first-run** setup of the web admin UI (username + a salted
PBKDF2-SHA256 hash — never a cleartext password). Do not author it by hand; leaving it
absent is what marks the server "needs setup". HTTP Basic over the listen address —
run it over loopback or behind TLS.

## Supported protocol versions

See [protocols.md](protocols.md) for the exact AFP/SMB/AppleTalk/IPX protocol versions
and dialects each service speaks — and which of the config keys above (`AFP.tcp_addr`)
are currently accepted-but-inert.

## Web UI, control API, netboot

See [web-ui.md](web-ui.md) for the admin UI/control-API architecture and
[netboot.md](netboot.md) for the `[Netboot]` protocol details.
