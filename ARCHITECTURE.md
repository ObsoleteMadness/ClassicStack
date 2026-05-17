# ClassicStack Runtime Map

This document is intentionally high-level and operational. It describes
what currently runs in a ClassicStack process and how major subsystems
connect. For protocol-level details, use [spec](spec).

## Purpose

ClassicStack can run as a mixed classic networking stack with:

- AppleTalk routing (EtherTalk + LocalTalk transports)
- AFP file service
- MacIP gateway
- Optional IPX, NetBEUI, NetBIOS, and SMB1 services

## Runtime topology

At startup, [cmd/classicstack/main.go](cmd/classicstack/main.go) builds
and wires components in this shape:

~~~text
Config (TOML or flags)
  -> Bridge/raw-link setup
  -> Port setup (LToUDP, TashTalk, EtherTalk)
  -> AppleTalk router + core AppleTalk services
  -> Optional protocol hooks (MacIP, IPX, NetBEUI, NetBIOS, SMB)
  -> Optional AFP service wiring
~~~

Each optional subsystem is controlled by both config enable flags and
Go build tags.

## Build-tag gated subsystems

| Subsystem | Build tag | Primary config section |
|---|---|---|
| IPX | ipx or all | [IPX] |
| NetBEUI | netbeui or all | [NetBEUI] |
| NetBIOS | netbios or all | [NetBIOS] |
| SMB | smb or all | [SMB] |
| AFP extras (project-specific variants) | afp/all/macgarden/etc | [AFP] |

If a tag is not present, enable flags/keys for that subsystem are
ignored by a disabled stub implementation.

## Shared raw-link bridge model

Raw-link protocols share one bridge identity and backend selection via
[Bridge] in config:

- mode: pcap, tap, tun
- device: selected interface/device
- hw_address: shared host MAC identity
- bridge_mode: auto, ethernet, wifi

Consumers that can use shared bridge defaults:

- EtherTalk
- MacIP
- IPX
- NetBEUI

In pcap mode, each of those protocols can also apply a protocol-specific
BPF override filter.

## Protocol groups

### AppleTalk group

- Ports: EtherTalk, LToUDP, TashTalk
- Router: AppleTalk datagram dispatch and routing
- Services: RTMP, ZIP, NBP, AEP, LLAP, ATP, ASP, AFP transport hooks

### File services group

- AFP service (DDP and/or TCP depending on [AFP].protocols)
- SMB service (if built and enabled)
- SMB shares are configured under [SMB.Volumes.*]
- AFP volumes are configured under [AFP.Volumes.*]

### Legacy LAN interop group

- MacIP gateway (pcap or nat mode)
- IPX router + RIP/SAP services
- NetBEUI port
- NetBIOS service over selected transports (tcp, netbeui, ipx)
- SMB can use NetBIOS and optional direct IPX transport path when IPX is active

## Configuration flow

1. [server.toml](server.toml) is loaded when no flags are passed.
2. When -config is used, it cannot be mixed with other flags.
3. Config is resolved into a cmd-level appConfig.
4. Bridge settings are synchronized into raw-link consumers.
5. Components are wired and started.

Important policy:

- Legacy EtherTalk bridge identity keys in file config are rejected.
- Use [Bridge] as the only config-file source for backend/device/MAC/frame mode.

## Logging and captures

- Runtime logging is configured by [Logging].
- Optional frame capture output is configured by [Capture].
- Capture outputs currently support LocalTalk/EtherTalk/IPX streams.

## Where to look next

- Operator quickstart and config tables: [README.md](README.md)
- Protocol notes and behavior references: [spec](spec)
- Runtime wiring entrypoint: [cmd/classicstack/main.go](cmd/classicstack/main.go)
