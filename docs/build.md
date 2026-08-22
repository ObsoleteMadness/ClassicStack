---
title: "Building"
weight: 2
---

# Building ClassicStack

For a five-minute path from zero to a running server, see [quickstart.md](quickstart.md).
This document covers building from source in full, including what every build tag does.

## Clone

The web admin UI lives in a separate repo, consumed as a git submodule
([ClassicStack-web](https://github.com/ObsoleteMadness/ClassicStack-web); see
[web-ui.md](web-ui.md#the-classicstack-web-submodule)):

~~~bash
git clone --recurse-submodules https://github.com/ObsoleteMadness/ClassicStack.git
# already cloned without it:
git submodule update --init --recursive
~~~

## Requirements

- Go 1.23+
- Node 20+ if you build the web UI (`-tags webui` or `all`)
- Npcap on Windows for pcap mode: https://npcap.com/#download
- libpcap on Linux/macOS for pcap mode. On macOS, `/dev/bpf*` is root-only unless you
  install Wireshark's **ChmodBPF** (adds your user to `access_bpf`; log out and back in)
  or run ClassicStack with `sudo`. Wi-Fi access points drop Ethernet frames not sourced
  from the NIC's own MAC — leave `hw_address` empty so the server and Finder client both
  use the host MAC. Many consumer APs also filter non-IP ethertypes (AppleTalk, IPX,
  NetBEUI); a wired NIC or an AP that bridges those frames is required for remote clients.
- WinFsp / macFUSE / libfuse if you'd like to mount volumes with `csmount`.

## Build commands

Build the default binary (all optional protocol hooks enabled):

~~~bash
go build -tags all -o classicstack ./cmd/classicstack
~~~

Build every desktop command at once (server, daemon, `csmount`, and the diagnostic
tools) into `./bin`, with the full desktop tag set — `all,pcap,netboot,fuse` on
macOS/Linux, minus `fuse` on Windows:

~~~bash
make build-local
# or a subset / a different output directory:
./scripts/build-local.sh classicstack csmount
BIN_DIR=/tmp/cs ./scripts/build-local.sh
~~~

Build with a custom protocol tag set:

~~~bash
go build -tags "ipx netbeui netbios smb" -o classicstack ./cmd/classicstack
~~~

Build the router-only variant (no optional build-tag services):

~~~bash
go build -o classicstack ./cmd/classicstack
~~~

Run tests:

~~~bash
go test ./...
~~~

See [testing.md](testing.md) for the end-to-end test suites (in-process protocol
harness plus the native vintage-client tools).

## Build tags

Every optional subsystem is compiled in only when its tag (or `all`) is passed to
`go build`/`go test`. The pattern throughout the tree is `//go:build <tag> || all`, so
`-tags all` is a superset that turns everything on; a production build should instead
pick only the tags it needs to keep the binary small and the attack surface narrow.

A few tags additionally require `router` (they only make sense wired into the
AppleTalk router) — `all` already implies this, so it only matters when you're
hand-picking tags.

| Tag | Enables | Config section |
|---|---|---|
| `all` | Every tag below at once. The default full-desktop build. | — |
| `afp` | AFP file service (classic DDP/ASP and modern TCP/DSI transports) | `[AFP]`, `[[afpvolumes]]` |
| `smb` | SMB1 file service (NBT, NetBEUI, IPX/NBIPX, direct-TCP carriers) | `[SMB]`, `[[smbshares]]` |
| `ncp` | Novell NCP file service (NetWare 3.x-style bindery emulation over IPX) | `[NCP]`, `[[ncpvolumes]]` |
| `etherdfs` | EtherDFS DOS file service (raw EtherType `0xEDF5`) | `[EtherDFS]`, `[[etherdfsdrives]]` |
| `ipx` | IPX router services (RIP/SAP) | `[[ipx]]` |
| `ipxgw` | MacIPX gateway — IPX-over-AppleTalk for the classic MacIPX client (needs `router`) | `[IPXGW]` |
| `ipxdiag` | IPX Diagnostic responder service | — |
| `netbeui` | NetBEUI raw-link port | `[[netbeui]]` |
| `netbios` | NetBIOS name/session service (backs SMB over NBF/NBIPX/NBT) | `[NetBIOS]` |
| `browser` | NetBIOS browser (`\MAILSLOT\BROWSE`: HostAnnounce/Election/master browser) | — |
| `messenger` | NetBIOS Messenger / WinPopup (`net send`, `\MAILSLOT\MESSNGR`) | — |
| `macip` | MacIP gateway — IP-over-AppleTalk for MacTCP clients (needs `router`) | `[MacIP]` |
| `netboot` | AppleTalk Netboot (ABP + ChainBoot EBP) — see [netboot.md](netboot.md) (needs `router`) | `[Netboot]` |
| `macgarden` | `macgarden` `fs_type`: a virtual share backed by a live scrape of macintoshgarden.org | per-volume `fs_type = "macgarden"` |
| `zipfs` | `zipfs` `fs_type`: a read-write filesystem backed by a single `.zip` archive | per-volume `fs_type = "zipfs"` |
| `xattr` | Maps DOS/AFP attributes onto host extended attributes on Linux/macOS | — |
| `sqlite` | SQLite-backed CNID/metastore instead of the in-memory default | `cnid_backend = "sqlite"` |
| `webui` | Embeds the web admin SPA (built from the `classicstack-web` submodule) | `[http]` |
| `pcap` | Real device links via libpcap/Npcap (EtherTalk, MacIP, IPX, NetBEUI over a real NIC) | `[[interface]]` |
| `fuse` | Host filesystem mounts via WinFsp/macFUSE/libfuse (`csmount`) | `[FUSE]`, `[Client]` |
| `fswatch` | Host filesystem change notifications surfaced to AFP/SMB/NCP/EtherDFS clients | — |
| `perfcounters` | Extra `expvar` performance counters | — |

Tags outside this table (`tinygo`, `pico`, `picow`, `esp32`, `wt32eth01`,
`registrytag`, `driverint`, …) select embedded targets or internal test
configurations rather than desktop features — see [testing.md](testing.md) for
`driverint` and the `.refactor/00-DESIGN.md` charter for the embedded rings.

## Running as a service / daemon

ClassicStack ships wrapper binaries so it can run in the background and start
automatically. They share the same runtime as `classicstack` — config and behaviour are
identical, they just manage the process lifecycle.

### Windows service — `classicstack-svc.exe`

Run from an **elevated** (Administrator) prompt:

~~~powershell
.\classicstack-svc.exe install -config C:\ProgramData\ClassicStack\server.toml
.\classicstack-svc.exe start      # start it now
.\classicstack-svc.exe status     # query the state
.\classicstack-svc.exe stop       # stop it
.\classicstack-svc.exe uninstall  # remove it
~~~

The service is named `ClassicStack` (visible in `services.msc` and
`Get-Service ClassicStack`) and writes start/stop entries to the Application event log.
`classicstack-svc.exe run -config ...` runs the stack in the current console for
debugging.

### Linux / macOS daemon — `classicstackd`

`classicstackd` self-daemonizes — it needs no systemd or other init system:

~~~bash
classicstackd start -config /etc/classicstack/server.toml \
  -pidfile /var/run/classicstack.pid -log /var/log/classicstack.log
classicstackd status   # report whether it is running
classicstackd stop     # stop it gracefully (SIGTERM)
classicstackd run -config /etc/classicstack/server.toml   # foreground (Ctrl-C to stop)
~~~

`-pidfile` and `-log` default to `/var/run/classicstack.pid` and
`/var/log/classicstack.log`. For boot persistence, point your init system's `ExecStart`
at `classicstackd run -config <path>`.

On **macOS**, `install`/`uninstall` additionally manage a LaunchAgent so the daemon runs
as a login item (headless):

~~~bash
classicstackd install -config ~/Library/Application\ Support/ClassicStack/server.toml
# writes ~/Library/LaunchAgents/com.obsoletemadness.classicstack.plist and loads it
classicstackd uninstall   # unload + remove the LaunchAgent
~~~

### Menu bar / system tray app — `cmd/classicstack-tray`

A small status-item app (macOS menu bar, Windows system tray) that shows Running/Stopped
status, opens the web admin UI, and can start/restart/shut down the stack. Once an admin
password is set via the web UI's first-run setup, restart/shutdown prompt for it once and
remember it (macOS Keychain / Windows Credential Manager). It also watches the control
API's event stream and raises native notifications for incoming Messenger/AFP messages
and error-level log lines.

**macOS:** `make app-darwin` builds `dist/ClassicStack.app`, a menu-bar-only bundle (no
Dock icon) wrapping `classicstackd`. Local/manual build, unsigned, not part of CI release
packaging.

**Windows:** `classicstack-tray.exe` drives `classicstack-svc.exe` — see
`packaging/windows` for the installer, which can register it to start at sign-in.
