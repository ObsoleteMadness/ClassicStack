
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

Grab the latest build from [GitHub Releases](https://github.com/ObsoleteMadness/ClassicStack/releases/latest).

## Screenshots

![WebUI](./img/webui.png)
The web interface. 

![Doom](./img/doom.png)
Doom running over MacIPX over AppleTalk over LtOUDP through Snow, back to IPX on 86box. 

## Basic usage

~~~bash
cp server.toml.example server.toml   # edit bridge/ports/shares, then:
./classicstack                       # auto-loads ./server.toml
./classicstack -config /path/to/server.toml
~~~

The file **client** connects out to a remote AFP/SMB/NCP/EtherDFS share and mounts it
(`csmount`) or drives it from the CLI (`csfs`):

~~~bash
csfs ls "afp://user@MyServer/My Volume"
csmount "afp://user@MyServer/My Volume" M:      # Windows drive letter (WinFsp)
csmount "afp://user@MyServer/My Volume" /Volumes/Classic   # macFUSE / libfuse
~~~

That's the fast path — building from source, every build tag, the full `server.toml`
reference, the web admin UI, and the rest of the command-line tools (service/daemon
wrappers, the tray app, AppleTalk/IPX/NetBIOS diagnostics) are in [docs/](docs), see the
list at the bottom of this file.

## Status and attribution

Warning: this project is pragmatic and evolving. Validate behavior in your environment before production use.

ClassicStack stands on a lot of prior open-source work. Several subsystems are clean
re-implementations over our storage/transport seams rather than code ports, but they owe
a clear debt to the originals.

- **tashrouter** — the original inspiration for the AppleTalk routing core by **Tashtari**.
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

- [Quick start](docs/quickstart.md) — get running in five minutes
- [Building from source](docs/build.md) — requirements, build commands, every build tag
- [Configuration reference](docs/config.md) — the full `server.toml` key-by-key guide
- [Supported protocol versions](docs/protocols.md) — exact AFP/SMB/AppleTalk/IPX/NCP versions and dialects
- [Web UI & control API](docs/web-ui.md) — the API split and how the SPA reuses `classicstack-web` components
- [AppleTalk Netboot & ChainBoot](docs/netboot.md) — how diskless classic Mac boot works
- [Testing](docs/testing.md) — the in-process protocol harness and the native vintage-client test tools
- [Operator / developer manual](docs/manual.md) — the full guide (CLI tools, config, web UI, client SDK)
- [High-level runtime map](ARCHITECTURE.md)
- [Protocol notes](spec)