
<div align="center">

<img src="https://raw.githubusercontent.com/ObsoleteMadness/ClassicStack/main/icon256.png" alt="ClassicStack" width="256" height="256"/>

![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/obsoletemadness/classicstack/release-main.yml)
[![CodeFactor](https://www.codefactor.io/repository/github/obsoletemadness/classicstack/badge)](https://www.codefactor.io/repository/github/obsoletemadness/classicstack)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/obsoletemadness/classicstack)
![GitHub License](https://img.shields.io/github/license/obsoletemadness/classicstack)
![GitHub repo size](https://img.shields.io/github/repo-size/obsoletemadness/classicstack)
[![GitHub Repo stars](https://img.shields.io/github/stars/obsoletemadness/classicstack)](https://github.com/obsoletemadness/classicstack/stargazers)
[![WARN-LLM GENERATED](https://img.shields.io/badge/WARN-LLM%20GENERATED-FF6347)](https://github.com/40ants/ai-badges)

# ClassicStack

ClassicStack is an AppleTalk router and classic LAN services stack that bridges legacy Macintosh and DOS networking into modern environments. Always in beta. 

</div>

## What it does

- AppleTalk Phase 2 routing across EtherTalk and LocalTalk transports.
- AFP file server over both classic DDP and modern TCP transports.
- MacIP gateway for IP-over-AppleTalk clients.
- MacIPX gateway for IPX-over-AppleTalk clients. 
- Optional IPX, NetBEUI, NetBIOS, and SMB1 services (build-tag gated).
- Shared raw-link bridge settings for EtherTalk, MacIP, IPX, and NetBEUI.
- File **client** that mounts remote AFP / SMB / NCP / EtherDFS shares as a host
  filesystem via WinFsp (`csmount` on Windows), macFUSE / libfuse (`csmount` on
  macOS and Linux), plus a cross-platform CLI (`csfs`).

## Releases
Grab the latest release from Github Releases [releases](https://github.com/ObsoleteMadness/ClassicStack/releases/latest).

## Screenshots

![WebUI](./img/webui.png)
The web interface. 

![Doom](./img/doom.png)
Doom running over MacIPX over AppleTalk over LtOUDP through Snow, back to IPX on 86box. 

## Build

Requirements:

- Go 1.23+
- Npcap on Windows for pcap mode: https://npcap.com/#download
- libpcap on Linux/macOS for pcap mode. On macOS, `/dev/bpf*` is root-only unless you install Wireshark's **ChmodBPF** (adds your user to `access_bpf`; log out and back in) or run ClassicStack with `sudo`. Wi‑Fi access points drop Ethernet frames not sourced from the NIC's own MAC — leave `hw_address` empty so the server and Finder client both use the host MAC. Many consumer APs also filter non-IP ethertypes (AppleTalk, IPX, NetBEUI); a wired NIC or an AP that bridges those frames is required for remote clients.

Build default binary (all optional protocol hooks enabled):

~~~bash
go build -tags all -o classicstack ./cmd/classicstack
~~~

Build with a custom protocol tag set:

~~~bash
go build -tags "ipx netbeui netbios smb" -o classicstack ./cmd/classicstack
~~~

or:

~~~bash
go build -tags all -o classicstack ./cmd/classicstack
~~~

Build router-only variant (no optional build-tag services):

~~~bash
go build -o classicstack ./cmd/classicstack
~~~

Run tests:

~~~bash
go test ./...
~~~

## Quick start

1. Copy [server.toml.example](server.toml.example) to server.toml.
2. Edit bridge/device/network values.
3. Run with no flags (auto-loads server.toml) or pass -config.

Examples:

~~~bash
./classicstack -config server.toml
~~~

~~~powershell
.\classicstack.exe -config server.toml
~~~

Config loading rules:

- -config cannot be combined with other flags.
- When no flags are passed, server.toml is loaded automatically if present.

## Shared bridge model

Bridge defaults live in [Bridge] and are reused by EtherTalk, MacIP, IPX, and NetBEUI.

| Key | Type | Default | Description |
|---|---|---|---|
| mode | string | pcap | Raw-link backend: pcap, tap, tun. |
| device | string | (empty) | Interface/device name used by shared raw-link consumers. |
| hw_address | string | (empty) | Shared station MAC. Blank = the NIC's own hardware address (required on WiFi). Set a value only to spoof a distinct station on wired Ethernet. |
| bridge_mode | string | auto | Frame adaptation mode: auto, ethernet, wifi. |

Important: legacy bridge keys under [EtherTalk] are no longer accepted in config files. Use [Bridge] only.

Per-protocol pcap filter overrides:

- [EtherTalk].filter
- [MacIP].filter
- [IPX].filter
- [NetBEUI].filter

These filters apply only in pcap mode.

## Transport and service sections

### [Router]

Declares which transports the AppleTalk router binds to. An enabled transport
that is **not** bound runs *standalone*: it still comes up and receives frames
(and can be captured), but it is not part of the AppleTalk router — no RTMP/ZIP
and no inter-port forwarding. This lets you run, say, TashTalk on its own
segment without it joining the router.

| Key | Default | Notes |
|---|---|---|
| ports | (empty) | Transport section names the router binds to (`"LToUdp"`, `"TashTalk"`, `"EtherTalk"`). Empty (or section omitted) binds every enabled transport; a non-empty list binds only those named, so any enabled-but-unlisted transport runs standalone. |

```toml
[Router]
ports = ["LToUdp", "EtherTalk"]   # TashTalk, if enabled, runs standalone
```

The dashboard shows each port's `routed: on/off` so you can see at a glance
which transports are part of the router. The same allow-list is editable from
the web UI via the "Attach to AppleTalk router" checkbox on each transport.

### [LToUdp]

| Key | Default | Notes |
|---|---|---|
| enabled | true | Enables LocalTalk-over-UDP port. |
| interface | 0.0.0.0 | Local IPv4 bind/join interface. |
| seed_network | 1 | Seed network ID for this segment. |
| seed_zone | LToUDP Network | Seed zone name. |

### [TashTalk]

| Key | Default | Notes |
|---|---|---|
| port | (empty) | Serial device path/name; empty disables. |
| seed_network | 2 | Seed network ID for this segment. |
| seed_zone | TashTalk Network | Seed zone name. |

### [EtherTalk]

| Key | Default | Notes |
|---|---|---|
| bridge_host_mac | (empty) | Optional host adapter MAC for wifi bridge shim. |
| filter | (protocol default) | Optional BPF override in pcap mode. |
| seed_network_min | 3 | Seed network range start. |
| seed_network_max | 5 | Seed network range end. |
| seed_zone | EtherTalk Network | Seed zone name. |

### [MacIP]

| Key | Default | Notes |
|---|---|---|
| enabled | false | Enables MacIP gateway. |
| mode | pcap | pcap or nat. |
| zone | (empty) | Registration zone override. |
| nat_subnet | 192.168.100.0/24 | Subnet/pool for NAT mode. |
| nat_gw | (empty) | Gateway address advertised in NAT mode. |
| lease_file | (empty) | Optional lease persistence file. |
| ip_gateway | (empty) | Upstream gateway address. |
| dhcp_relay | false | Translate/relay DHCP for clients. |
| nameserver | (empty) | DNS server for clients. |
| filter | (protocol default) | Optional BPF override in pcap mode. |

### [IPX]

IPX is optional and requires build tag ipx or all.

| Key | Default | Notes |
|---|---|---|
| enabled | false | Enables IPX router services. |
| interface | (empty) | Raw-link interface; empty reuses bridge device. |
| framing | ethernet_ii | One of ethernet_ii, raw_802_3, llc, snap. |
| internal_network | (empty) | 8 hex digits; empty falls back to default network. |
| filter | ipx (internal default) | Optional BPF override in pcap mode. |

### [NetBEUI]

NetBEUI is optional and requires build tag netbeui or all.

| Key | Default | Notes |
|---|---|---|
| enabled | false | Enables NetBEUI raw-link port. |
| interface | (empty) | Raw-link interface; empty reuses bridge device. |
| filter | llc (internal default) | Optional BPF override in pcap mode. |

### [NetBIOS]

NetBIOS is optional and requires build tag netbios or all.

| Key | Default | Notes |
|---|---|---|
| enabled | false | Enables NetBIOS service. |
| transports | ["tcp"] | Allowed values: tcp, netbeui, ipx. |
| scope_id | (empty) | Optional NetBIOS scope ID. |

NetBIOS server/workgroup identity is derived from SMB server/workgroup values.

### [SMB]

SMB is optional and requires build tag smb or all.

| Key | Default | Notes |
|---|---|---|
| enabled | false | Enables SMB server. |
| nbt_binding | :139 | NetBIOS-over-TCP listener. |
| direct_binding | (empty) | Optional direct SMB listener (for example :445). |
| guest_ok | false | Allows guest sessions. |
| server_name | CLASSICSTACK | Computer/server name. |
| workgroup | WORKGROUP | Workgroup/domain label. |

SMB shares are configured as [SMB.Volumes.<name>] sections.

Example:

~~~toml
[SMB]
enabled = true
nbt_binding = ":139"
guest_ok = true
server_name = "CLASSICSTACK"
workgroup = "WORKGROUP"

[SMB.Volumes.Public]
name = "Public"
path = "./public"
fs_type = "local_fs"
read_only = false
~~~

### [AFP]

AFP runs over ddp, tcp, or both.

| Key | Default | Notes |
|---|---|---|
| enabled | true | Enables AFP service. |
| name | ClassicStack (example) | Advertised AFP server name. |
| zone | (empty) | Registration zone override. |
| protocols | ddp,tcp | AFP transports. |
| binding | :548 | DSI listener. |
| extension_map | (empty) | Extension map file path. |
| cnid_backend | sqlite | sqlite or memory. |
| use_decomposed_names | true | Reserved-character mapping behavior. |
| appledouble_mode | modern | modern or legacy sidecar layout. |

AFP volumes are configured as [AFP.Volumes.<name>] sections.

## Logging and capture

[Logging]:

- level: debug, info, warn
- parse_packets: protocol decode logging
- parse_output: file target for parsed logs
- log_traffic: raw traffic logging

[Capture]:

- localtalk, ethertalk, ipx capture output paths
- snaplen for capture truncation length

## Web UI

A management web UI is available in builds that include `-tags webui` (which
`-tags all` does). Compile it with `make spa` (Node 20 + ClassicStack-web).
The SPA is **Finder-first**: browse this instance’s live volumes and LAN
servers over `/finder` (Go speaks AFP/SMB/NCP/EtherDFS; the browser does not).
Essential admin tabs cover **status** (start/stop/restart), **sharing**
(volumes/shares), **users**, and a live **log viewer**.

It listens by default on **:1984** (`[http]` in server.toml). Set
`enabled = false` to turn it off; `-http :port` overrides the address.

The in-process **file client** (`[Client]` in server.toml) is **off by default**.
When enabled it binds an `[[interface]]` (e.g. `br-lan`), probes the listed
schemes (`afp`, `smb`, `ncp`, `etherdfs`) at startup, tracks connections and
open volumes, and the Finder API (`GET /finder/state`) reads that state.
`max_idle_minutes` (default 10) disconnects unused remote sessions;
`mount = true` allows FUSE/WinFsp host mounts; `log_file` is an optional extra
client log.

**FUSE** (`[FUSE]` in server.toml) sets the host-mount connect timeout
(`mount_timeout_seconds`, default 30). `[[fusevolumes]]` entries are remote
shares auto-mounted at startup (URI, local mount point, optional read-only)
when the client is enabled with `mount = true`. Configure them from Settings →
FUSE → Auto-mounted volumes.

From **Status** you can **start, stop, and restart** services live. **Sharing**
adds, updates, and removes AFP volumes, SMB shares, NCP volumes, and EtherDFS
drives. **Logs** streams the server's log output live with a client-side level
filter. **Save** on the Sharing tab writes `server.toml` (numbered backups).
The same management operations are exposed by the transport-agnostic
`pkg/control` API.

## File client — mount and browse remote shares

ClassicStack is not only a server. It ships a **file client** that connects *out* to
a legacy file server — AFP, SMB (over IPX / NBIPX / NetBEUI / TCP), NetWare NCP, or
EtherDFS — and presents it through one uniform interface. Two commands share that
client:

- **`csmount`** — mount a remote share as a host filesystem: a Windows drive letter
  via [WinFsp](https://winfsp.dev/), a directory via [macFUSE](https://macfuse.github.io/)
  on macOS, or libfuse on Linux, so any application can use it.
- **`csfs`** — a cross-platform CLI over the same client (`ls` / `cp` / `mv` / `rm` /
  `attrib` / `type` / `creator`), plus an interactive REPL.

Both address a server by URI:

~~~
<scheme>://[[user][:pass]@]<server>[,<transport>]/<volume>[/<path>]

afp://classicstack:MyZone/Volume        smb://pete:secret@host,tcp/share
smb://server,nbf/Share                  ncp://SERVER,ipx/SYS
etherdfs://02-1a-4d-11-22-33/C
~~~

Shared flags: `-ifacetype` (`ltoudp` | `tashtalk` | `pcap` | `tcp`, validated against
the scheme), `-iface` (the IPv4 address / pcap device / serial port / host),
`-transport` (SMB pcap carrier: `ipx` | `nbipx` | `nbf`), `-mac` (virtual-station MAC
for raw-Ethernet SMB carriers), `-fork` (fork container, see below), and `-v` (client
wire-trace to stderr).

### csmount (Windows / macOS / Linux)

`csmount [flags] <uri> <mountpoint>` connects with the same client SDK as `csfs` and
mounts the share so Finder, Explorer, or any local program can use it. Ctrl-C unmounts
cleanly. Add the `pcap` build tag for raw-Ethernet carriers (SMB-over-IPX/NBIPX/NBF,
NCP, EtherDFS).

**Windows** needs the [WinFsp runtime](https://winfsp.dev/rel/). It compiles with no cgo:

~~~powershell
go build -tags pcap -o csmount.exe ./cmd/csmount
# or: make build-mount
.\csmount.exe "afp://user@MyServer/My Volume" M:
.\csmount.exe -iface "\Device\NPF_{GUID}" "smb://WIN98,nbf/C-DRIVE" N:
~~~

`<mountpoint>` is a drive letter (`X:`) or an empty directory. `-fork native` (or
`ads` / `hfs`) surfaces the resource fork, Finder info, and comment as NTFS named
streams under the Services-for-Macintosh names (`:AFP_Resource`, `:AFP_AfpInfo`,
`:Comments`).

**macOS** needs [macFUSE](https://macfuse.github.io/) and cgo. **Linux** needs libfuse
(`libfuse-dev`) and cgo; Linux FUSE support is experimental and has not been tested.

~~~bash
go build -tags fuse -o csmount ./cmd/csmount
# or: make build-mount
csmount -ifacetype tcp "afp://user@MyServer/My Volume" /Volumes/Classic
csmount -ifacetype tcp "afp://user@MyServer/OpenRetroSCSI 7.5.3" "/Volumes/OpenRetroSCSI 7.5.3"
~~~

On macOS pass `/Volumes/<name>` (spaces allowed) and do **not** create that folder —
macFUSE's setuid `mount_macfuse` creates a missing `/Volumes` leaf. A regular user
cannot `mkdir` under `/Volumes` (`root:wheel` 0755 since Sierra). Linux still needs
an empty directory. The default `-fork` (AFP `passthrough`, or
`native` / `hfs` / `xattr` / `ads`) maps forks to host xattrs:

- macOS: `com.apple.FinderInfo` (32-byte FInfo+FXInfo) and `com.apple.ResourceFork`,
  plus the virtual path `file/..namedfork/rsrc`.
- Linux: `user.org.netatalk.Metadata` (402-byte Netatalk AppleDouble header, including
  the Finder comment) and `user.org.netatalk.ResourceFork`.

Sidecar `-fork` values (`appledouble`, `derez`, …) still project `._name` / `.rdump`
files into the mount instead.

**DOS attributes** (hidden / system / read-only) and **file dates** are read live from
the server — AFP maps Invisible→hidden, System→system, WriteInhibit→read-only; SMB
uses the server's FileAttributes and timestamps directly.

List pcap device names with `classicstack -list-pcap-devices` (or `csmount -list-ifaces`).

### csfs (cross-platform CLI)

`csfs` runs anywhere and drives the same client. Host↔remote copies are one code path,
so it preserves resource forks, Finder type/creator, and DOS attributes across a copy.

~~~bash
go build -tags pcap -o csfs ./cmd/csfs      # -tags pcap for raw-Ethernet carriers

csfs ls "afp://user@MyServer/My Volume"           # list a directory
csfs ls smb://server/                             # a bare server → list its shares
csfs get "afp://user@server/Vol/file" ./file      # copy remote → host (forks preserved)
csfs put ./file "smb://server,nbf/Share/file"     # copy host → remote
csfs discover afp                                  # find servers (NBP / SAP / broadcast)
csfs "afp://user@server/Vol"                       # no command → interactive REPL
~~~

## Running as a service / daemon

ClassicStack ships a wrapper binary so it can run in the background and start
automatically. It shares the same runtime as `classicstack`, so the config and
behaviour are identical — it just manages the process lifecycle. The wrapper is a
different command per platform:

### Windows service — `classicstack-svc.exe`

Run from an **elevated** (Administrator) prompt:

~~~powershell
# Register the service (auto-start at boot) pointing at a config file:
.\classicstack-svc.exe install -config C:\ProgramData\ClassicStack\server.toml

.\classicstack-svc.exe start      # start it now
.\classicstack-svc.exe status     # query the state
.\classicstack-svc.exe stop       # stop it
.\classicstack-svc.exe uninstall  # remove it
~~~

The service is named `ClassicStack` (visible in `services.msc` and
`Get-Service ClassicStack`) and writes start/stop entries to the Application event
log. `classicstack-svc.exe run -config ...` runs the stack in the current console
for debugging.

### Linux / macOS daemon — `classicstackd`

`classicstackd` self-daemonizes — it needs no systemd or other init system:

~~~bash
# Start detached in the background (writes a PID file and logs to a file):
classicstackd start -config /etc/classicstack/server.toml \
  -pidfile /var/run/classicstack.pid -log /var/log/classicstack.log

classicstackd status   # report whether it is running
classicstackd stop     # stop it gracefully (SIGTERM)
classicstackd run -config /etc/classicstack/server.toml   # foreground (Ctrl-C to stop)
~~~

`-pidfile` and `-log` default to `/var/run/classicstack.pid` and
`/var/log/classicstack.log`. For boot persistence, point your init system's
`ExecStart` at `classicstackd run -config <path>`.

On **macOS**, `install`/`uninstall` additionally manage a LaunchAgent so the daemon
runs as a login item (headless):

~~~bash
classicstackd install -config ~/Library/Application\ Support/ClassicStack/server.toml
# writes ~/Library/LaunchAgents/com.obsoletemadness.classicstack.plist and loads it
classicstackd uninstall   # unload + remove the LaunchAgent
~~~

## Useful commands

List pcap devices:

~~~powershell
.\classicstack.exe -list-pcap-devices
~~~

Print version:

~~~bash
./classicstack -version
~~~

## Status and attribution

Warning: this project is pragmatic and evolving. Validate behavior in your environment before production use.

ClassicStack stands on a lot of prior open-source work. Several subsystems are clean
re-implementations over our storage/transport seams rather than code ports, but they owe
a clear debt to the originals.

- - **tashrouter** — the original inspiration for the AppleTalk routing core by **Tashtari**.
  https://github.com/lampmerchant/tashrouter, released under GPL-3.0. 
- **macresources / rdump (DeRez) format** by **Elliot Nunn** — the resource-fork text
  format and reference implementation behind our `derez` fork backend, ported to Go.
  https://github.com/elliotnunn/macresources
- **mars_nwe** (the MARtin Stover NetWare Emulator), © 1993,1995 Martin Stover, Marburg,
  Germany — the canonical open-source NetWare/NCP reference that inspired our NCP service
  (alongside Linux ncpfs by Volker Lendecke et al).
- **atalk-proxy** by **joshua stein** — the proxy-AARP rule (rewriting AARP Replies'
  sender-hardware to the egress MAC so AppleTalk bridges onto Wi-Fi) behind our
  proxy-AARP Wi-Fi/tunnel bridge, cross-checked against the Linux kernel's
  `net/appletalk/aarp.c` `proxies[]` table. https://github.com/jcs/atalk-proxy
- **NetBoot** by **Elliot Nunn** — the reverse engineering of the classic Mac
  `.netBOOT`/`.ATBOOT` ROM boot protocol (with the mac68k forum), the reference
  Python servers and ChainBoot extension our netboot service re-implements, and
  the Python Snefru-128 port behind `core/hash/snefru` (S-boxes from Ralph C.
  Merkle's Snefru / Xerox). Payload/PRAM groundwork by **Rob Braun (bbraun)**.
  Cross-checked against Apple's SuperMario `os/netboot` source.
- **macipgw** (AppleTalk MacIP Gateway) by **Stefan Bethke** (© 1997, 2013) and
  **Jason King** (© 2015) — the golden reference for our MacIP gateway
  (`core/service/macip`): the ATP config exchange and `struct macip_req` wire layout,
  the `MACIP_ASSIGN`/`SERVER`/`ERROR` functions and error strings, the
  `IPADDRESS`/`IPGATEWAY` NBP naming, source-IP ARP snooping, and the 586-byte MacIP
  MTU. An independent Go reimplementation over our egress seam; macipgw is GPLv2+
  (compatible with our GPLv3).
- **go-winfsp** and **cgofuse** by Bill Zissimopoulos. 
- **EtherDFS** by **Mateusz Viste**, Copyright © 2017-2023 Mateusz Viste — the EtherType
  0xEDF5 DOS file-system protocol our EtherDFS service re-implements.
- **Icons8** — icons used in the SPA / topology UI. https://icons8.com/

## License
This work is released under the terms of the GPL-3.0. 

Some components are based on works licensed differently, see NOTICE for details. 
Those components should be considered derivite works and can be used under their 
original license. Remember, though I'm not a lawyer and this is not legal advise. 


## Additional docs

- Operator / developer manual: [docs/manual.md](docs/manual.md)
- High-level runtime map: [ARCHITECTURE.md](ARCHITECTURE.md)
- Protocol notes: [spec](spec)