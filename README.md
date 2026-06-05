<div align="center">

<img src="https://raw.githubusercontent.com/ObsoleteMadness/ClassicStack/main/icon256.png" alt="ClassicStack" width="256" height="256"/>

# ClassicStack

ClassicStack is an AppleTalk router and classic LAN services stack that bridges legacy Macintosh networking into modern environments.

</div>

## What it does

- AppleTalk Phase 2 routing across EtherTalk and LocalTalk transports.
- AFP file server over both classic DDP and modern TCP transports.
- MacIP gateway for IP-over-AppleTalk clients.
- Optional IPX, NetBEUI, NetBIOS, and SMB1 services (build-tag gated).
- Shared raw-link bridge settings for EtherTalk, MacIP, IPX, and NetBEUI.

## Build

Requirements:

- Go 1.23+
- Npcap on Windows for pcap mode: https://npcap.com/#download
- libpcap on Linux/macOS for pcap mode

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
| hw_address | string | DE:AD:BE:EF:CA:FE | Shared host MAC identity. |
| bridge_mode | string | auto | Frame adaptation mode: auto, ethernet, wifi. |

Important: legacy bridge keys under [EtherTalk] are no longer accepted in config files. Use [Bridge] only.

Per-protocol pcap filter overrides:

- [EtherTalk].filter
- [MacIP].filter
- [IPX].filter
- [NetBEUI].filter

These filters apply only in pcap mode.

## Transport and service sections

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
`-tags all` does). It serves a dashboard showing per-service status, bindings,
and live (SSE-streamed) statistics, plus a configuration editor, read-only
diagnostics (zone/network enumeration), and a live **log viewer**.

[WebUI]:

- enabled: turn the listener on (default off)
- bind: `IP:PORT` to listen on (default `127.0.0.1:8080`, loopback)
- tls: serve HTTPS (default true); a self-signed certificate is generated at
  startup when `cert_pem`/`key_pem` are blank
- cert_pem / key_pem: paths to a PEM certificate and key (supply both, or
  leave both blank for the self-signed certificate)

Equivalent flags: `-webui-enabled`, `-webui-bind`, `-webui-tls`,
`-webui-cert-pem`, `-webui-key-pem`.

From the dashboard you can **start, stop, and restart** the standalone services
(IPX, NetBEUI, NetBIOS, SMB) live; stops are dependency-aware (stopping NetBIOS
also stops SMB). The configuration editor can edit scalar settings, **add /
update / remove AFP volumes and SMB shares**, and toggle **packet-dump and pcap
capture** options (parse-packets, traffic logging, and per-transport capture
file paths). The **Logs** tab streams the server's log output live (recent
history is replayed on open, then new lines append) with a client-side level
filter. Edits stage in memory; **Apply** re-wires the running stack (the web
UI server is preserved across the rebuild), **Save** writes `server.toml`
(backing up the prior file to `server.toml.NNNN` and dropping comments), and
**Download backup** exports the current config. The same operations are exposed
by the transport-agnostic `pkg/control` API, so a future text/telnet UI can
reuse them.

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

AppleTalk routing was originally inspired by tashrouter:
https://github.com/lampmerchant/tashrouter

## License

GPL-3.0.

## Additional docs

- High-level runtime map: [ARCHITECTURE.md](ARCHITECTURE.md)
- Protocol notes: [spec](spec)