---
title: "CLI Tools Reference"
weight: 9
---

# CLI Tools Reference

Full flag, subcommand, and exit-code reference for every binary under [`cmd/`](../cmd/). For
the short "what does each tool do" tour see [manual.md §2](manual.md#2-command-line-tools); this
page is the exhaustive version, extracted from each tool's source and `-h`/`usage()` text.

Every binary shares one `-version` output format (from `cmd/internal/buildinfo`):

```text
<tool> <version>
commit: <commit>
built: <date>
go: <go runtime version>
```

---

## 1. Server and lifecycle

### `classicstack`

Interactive entry point: the AppleTalk Phase 2 router and AFP/SMB/NCP/EtherDFS file server. Loads
`server.toml` into the config model, builds and supervises the compose runtime, optionally serves
the web-admin control API, and runs until interrupted (SIGINT/SIGTERM). A second interrupt forces
`os.Exit(1)` immediately ("`classicstack: second interrupt received, forcing exit`").

```text
classicstack [-config <path>] [-http <addr>] [-version] [-list-ifaces]
```

| Flag | Default | Meaning |
|---|---|---|
| `-config` | `server.toml` | Path to the config file (TOML, or UCI for an `/etc/config` path or `*.uci` file). |
| `-http` | *(empty)* | Override `[http]` listen address (empty = `server.toml`'s value, default `:1984`). |
| `-version` | `false` | Print version information and exit. |
| `-list-ifaces` | `false` | List the capturable pcap NICs (the names an `[EtherTalk]`/`[MacIP]`/… interface accepts) and exit. Requires a `pcap`-tagged build; otherwise prints a build-tag hint. |

No subcommands, no positional arguments. Which protocols/ports are actually compiled in (AFP, SMB,
NCP, EtherDFS, MacIP, MacIPX, NetBIOS/Messenger, EtherTalk/LToUDP/TashTalk/NetBEUI/IPX ports) is
decided at **build time** by Go build tags (`-tags all`, or a narrower list like `-tags "afp smb
pcap"`) — a config section for a component that wasn't compiled in is simply inert.

```bash
go build -tags all -o classicstack ./cmd/classicstack
./classicstack                    # auto-loads ./server.toml
./classicstack -config /etc/classicstack/server.toml
./classicstack -http :1984        # override web listen; implies http enabled
./classicstack -list-ifaces
```

**Exit status:** `0` on a clean shutdown after the context is cancelled; `1` on any startup/runtime
error (printed as `classicstack: <err>`).

A web-admin "restart" request re-execs the same binary with the original `os.Args` and exits `0`;
the supervising process (shell, service manager, LaunchAgent) is expected to relaunch it.

---

### `classicstackd`

Unix/macOS background daemon wrapper around the same run-core as `classicstack` — no init-system
dependency required, though macOS gets an optional LaunchAgent.

```text
classicstackd <command> [flags]
```

| Command | Flags | Behavior |
|---|---|---|
| `start` | `-config <path>` (required), `-pidfile <p>`, `-log <p>` | Daemonizes: re-execs itself as `run -config <cfg>` in a new session, stdout/stderr redirected to `-log`, PID written to `-pidfile`. Errors if a live PID is already on file. |
| `stop` | `-pidfile <p>` | Sends `SIGTERM` to the recorded PID, polls up to 20s for exit, then removes a stale pidfile. |
| `status` | `-pidfile <p>` | Reports `running (pid N)`, `not running`, or `not running (stale PID N)`. |
| `run` | `-config <path>` (required) | Runs in the foreground with a signal-cancelled context — the same runtime as `classicstack -config <path>`. |
| `install` | `-config <path>` (required), `-log <p>` | **macOS:** writes and loads a per-user LaunchAgent (`~/Library/LaunchAgents/com.obsoletemadness.classicstack.plist`, `RunAtLoad`/`KeepAlive`). **Other Unix:** prints guidance to use `start`/a systemd-style unit instead — installs nothing. |
| `uninstall` (alias `remove`) | — | **macOS:** unloads and removes the LaunchAgent. **Other Unix:** prints guidance to use `stop` instead. |
| `version` | — | Prints version information. |
| `help` / `-h` / `--help` | — | Prints usage. |

| Flag | Default | Meaning |
|---|---|---|
| `-config` | *(empty)* | Path to the TOML config file. Required for `start`/`install`/`run`. |
| `-pidfile` | `/var/run/classicstack.pid` | Path to the PID file. |
| `-log` | `/var/log/classicstack.log` | Path to the daemon log file. |

```bash
sudo classicstackd start -config /etc/classicstack/server.toml
classicstackd status
sudo classicstackd stop
classicstackd install -config /etc/classicstack/server.toml   # macOS login item
```

**Exit status:** `0` on success; `1` on an operational error (e.g. "already running", "not
running", timeout waiting for stop); `2` when no command is given or the command is unrecognized.
On Windows this binary is a stub that prints `classicstackd is a Unix daemon; use classicstack-svc
on Windows` and exits `1` — use `classicstack-svc` there instead.

---

### `classicstack-svc` *(Windows only)*

Windows Service Control Manager wrapper: `install`/`uninstall`/`start`/`stop`/`status`/`run`/
`version` against a service named `ClassicStack`. Takes `-config <path>` for `install`/`run`. See
`classicstack-svc -h` or [manual.md](manual.md) for details. On non-Windows this binary is a stub.

---

### `classicstack-tray` *(macOS and Windows only)*

Menu bar / system tray status app: reports whether ClassicStack is running and offers **Open
Interface**, **Start**, **Restart**, and **Shutdown** against the web-admin control API
(`adapter/control/http`). **Quit** closes only the tray app — the server keeps running; use
**Shutdown** to actually stop it. Also raises native notifications for incoming Messenger/AFP
messages and error-level log lines (the same feed the web admin's notification bell reads).

```text
classicstack-tray [-http <addr>]
```

| Flag | Default | Meaning |
|---|---|---|
| `-http` | *(empty)* | Control API address to monitor (empty = `server.toml` default, `:1984`). A bare `:port` is treated as `http://127.0.0.1:port`. |

Not a CLI in the usual sense — it's a GUI event loop with a fixed menu, not subcommands. Polls
`/status` every 5 seconds; a `401` from Start/Restart/Shutdown prompts for admin credentials
(cached in the OS credential store). There is no Linux build of this tool (built only under
`GOOS=darwin` or `GOOS=windows`).

---

## 2. File client

### `csfs` (package `cmd/csclient`)

Cross-platform file client CLI over the client SDK: one-shot subcommands or an interactive REPL,
against AFP, SMB, NCP, and EtherDFS servers. Preserves resource forks, Finder type/creator, and DOS
attributes across host↔remote copies.

```text
csfs [flags] <command> [args]
csfs [flags] <uri>                 open an interactive session, or browse a bare server root
```

#### Subcommands

| Subcommand | Args | Behavior |
|---|---|---|
| `discover <scheme>` | `afp \| smb \| ncp \| etherdfs` | Probes the LAN for servers of that scheme (NBP + Bonjour `_afpovertcp._tcp` for AFP; SAP for NCP; master-browser sweep for SMB; broadcast `AL_INSTALLCHK` for EtherDFS). |
| `ls <uri>` | 1 | Lists a directory. A server-root URI (no volume/path) instead prints server info and the volume/share list. |
| `cp <src> <dst>` | 2 | Copies; either side may be a URI or a host path. |
| `get <uri> <host-path>` | 2 | Alias of `cp` for remote → host. |
| `put <host-path> <uri>` | 2 | Alias of `cp` for host → remote. |
| `mv <uri> <newpath>` | 2 | Renames/moves on the server. |
| `rm <uri>` | 1 | Deletes. |
| `attrib <uri> [+r\|-r\|+h\|-h\|+s\|-s\|+a\|-a]` | 1–2 | Shows (no extra arg) or sets DOS attributes. |
| `type <uri> [CODE]` | 1–2 | Shows/sets the 4-character Finder type. |
| `creator <uri> [CODE]` | 1–2 | Shows/sets the 4-character Finder creator. |
| *(bare `<uri>`)* | — | Browses a server root, or opens an interactive REPL against a share/path. |
| `help` / `-h` / `--help` | — | Prints usage. |

Inside the REPL: `ls [path]`, `cd <path>`, `pwd`, `get`, `put`, `cp`, `mv`, `rm`, `attrib`, `type`,
`creator`, `help`, `quit`/`exit`. Prompt is `<scheme>:/<cwd>> `. Arguments may be quoted (`"`/`'`)
with backslash-escapes.

#### Flags

These are shared verbatim with `csmount` (both parse them via `cmd/internal/csconnect`, a
hand-rolled parser so flags can precede the subcommand token):

| Flag | Default | Meaning |
|---|---|---|
| `-ifacetype` | *(auto)* | Transport: `ltoudp \| tashtalk \| pcap \| tcp`, validated against the URI's scheme. |
| `-iface` | *(empty)* | Interface: IPv4 address (ltoudp), pcap device name, `COM3`/`/dev/tty*` (tashtalk), or host (tcp). Pcap: omit to auto-detect the primary NIC. |
| `-transport` | `ipx` | SMB pcap sub-carrier: `ipx \| nbipx \| nbf`. |
| `-frametype` (alias `-framing`) | *(empty)* | IPX Ethernet encapsulation: `ethernet_ii \| 802.3 \| 802.2` (empty = learn from server). |
| `-mac` | *(random)* | Virtual-station MAC for raw-Ethernet carriers. |
| `-fork` | *(empty)* | Host fork container: `appledouble \| applesingle \| macbinary \| derez \| native \| nofork`. See [forks.md](forks.md) for what each one stores. |
| `-v` (alias `-verbose`) | `false` | Print the client wire-trace (NBP/ATP/ASP) to stderr. |
| `-list-ifaces` | `false` | List the capturable pcap NICs and exit. |
| `-version` | `false` | Print version information and exit. |

#### URI grammar

```text
<scheme>://[[user][:pass]@]<server>[,<transport>]/<volume>[/<path>]

afp://classicstack:MyZone/Volume
smb://pete:secret@host,tcp/share
ncp://SERVER,ipx/SYS
etherdfs://02-1a-4d-11-22-33/C
```

`<server>`/`<volume>` are protocol-native and opaque to the parser (AFP may use `name:zone` or
`net.node`; EtherDFS uses dash- or bare-hex MACs, never colon-separated).

```bash
go build -tags pcap -o csfs ./cmd/csclient
csfs discover afp
csfs ls afp://classicstack:MyZone/Volume
csfs get afp://classicstack:MyZone/Volume/README.txt ./README.txt
csfs -ifacetype tcp afp://server/Volume        # open a REPL
```

**Exit status:** `0` success, `2` usage error, `1` operational failure (printed as `csfs: <err>`).

---

### `csmount`

Mounts a remote AFP/SMB/NCP/EtherDFS share as a host filesystem: WinFsp on Windows, macFUSE on
macOS, libfuse on Linux. Ctrl-C unmounts cleanly.

```text
csmount [flags] <uri> <mountpoint>
```

Shares the exact same flag set as `csfs` (see above), plus:

| Flag | Default | Meaning |
|---|---|---|
| `-cache-ms` | *(WinFsp default, ~1000)* | WinFsp `FileInfoTimeout` in ms. `0` disables the FSD metadata cache; `-1` is infinite (also enables kernel data caching). Windows-specific; accepted but not documented on other platforms. |

`-fork` accepts different values per platform (see [forks.md](forks.md) for the full storage
model behind each one):

- **Windows:** `appledouble \| applesingle \| macbinary \| derez \| passthrough \| native \| ads \|
  nofork` — `native` (= `ads`) exposes resource fork / Finder info / comment as NTFS SFM streams
  (`:AFP_Resource`, `:AFP_AfpInfo`, `:Comments`).
- **macOS/Linux (built with `-tags fuse`, needs cgo):** `appledouble \| applesingle \| macbinary \|
  derez \| passthrough \| native \| hfs \| xattr \| ads \| nofork` — `passthrough`/`native`/`hfs`/
  `xattr`/`ads` (and the empty default) map to host extended attributes (`com.apple.FinderInfo` +
  `com.apple.ResourceFork` on macOS; `user.org.netatalk.Metadata` + resource fork on Linux).
  Anything else falls back to `._name`/`.rdump` sidecar files.
- **macOS/Linux built *without* `fuse`:** mounting always fails with a rebuild hint
  (`go build -tags fuse -o csmount ./cmd/csmount`, requires macFUSE or `libfuse-dev` + cgo).

Mountpoint is a drive letter (`"X:"`) or empty directory on Windows, an empty directory on Linux,
or (macOS) a path like `/Volumes/<name>` that you must **not** pre-create — macFUSE creates that
leaf itself.

```bash
go build -tags "pcap fuse" -o csmount ./cmd/csmount   # macOS/Linux
go build -tags pcap -o csmount.exe ./cmd/csmount      # Windows

csmount -ifacetype tcp afp://server/Volume /Volumes/Classic          # macOS
csmount -fork appledouble afp://vmac1/System\ 7.5.3 /mnt/sys75        # Linux
csmount smb://server,nbf/Share M:                                     # Windows
csmount ncp://SERVER/SYS N:                                           # Windows
```

**Exit status:** `2` on a flag-parse/usage error; `1` on connect/mount failure; `0` normal exit
after Ctrl-C unmount (prints `unmounted`). On Linux, always prints a one-line "FUSE support is
experimental" notice regardless of outcome. On any other OS, this binary is a stub that exits `1`.

---

## 3. AppleTalk diagnostics

These four share the `atlink` transport flags (`cmd/internal/atlink`):

| Flag | Default | Meaning |
|---|---|---|
| `-transport` | `ltoudp` | AppleTalk transport: `ltoudp \| tashtalk \| pcap`. |
| `-iface` | *(empty)* | ltoudp: local IPv4 interface address (default: all multicast interfaces); pcap: NIC device name. |
| `-device` | *(empty)* | tashtalk: serial device path (e.g. `COM3` or `/dev/ttyUSB0`). |
| `-baud` | `0` | tashtalk: serial line speed (`0` → adapter default). |
| `-list-ifaces` | `false` | List the capturable pcap NICs and exit. |
| `-claim` | `true` | ltoudp/tashtalk: run a real LLAP ENQ/ACK node-claim for `-src` instead of asserting it directly. `-claim=false` restores the old static-assert behavior (requires an explicit `-src 1..254`). |

### `csecho`

AEP echo — an AppleTalk "ping" (netatalk `aecho` equivalent). DDP type 4, socket 4.

```text
csecho [flags]
```

| Flag | Default | Meaning |
|---|---|---|
| `-net` | `0` | AppleTalk network number we claim as our source (`0` = the AppleTalk "startup range" placeholder — a strict peer may ignore requests from it; pass the segment's real network number if a peer that answers a real client doesn't answer this probe). |
| `-src` | `0` | Our LocalTalk source node. `0` (default) picks a random workstation-range candidate (1–127) for the LLAP node-claim (`-claim`, on by default); `1`–`254` requests a specific candidate instead — the node actually used may still differ if it's taken. Requires `-claim` when `0`. |
| `-dst` | `0xFF` | Destination node (`0xFF` = broadcast to every node). |
| `-count` | `1` | Number of echo requests to send. |
| `-timeout` | `2s` | Per-request reply timeout. |
| `-data` | `"ClassicStack csecho"` | Echo payload string. |
| `-v` | `false` | Verbose wire trace to stderr. |
| `-version` | `false` | Print version information and exit. |

```bash
csecho -dst 0xFF -count 5
csecho -transport tashtalk -device /dev/ttyUSB0 -dst 12
```

**Exit status:** `1` if *no* replies were received across all attempts, or on any other error
(printed as `csecho: <err>`); `0` otherwise.

---

### `csnbp`

NBP (Name Binding Protocol) lookup — like netatalk's `nbplkup`. DDP type 2, socket 2.

```text
csnbp [flags] [object:type@zone]
```

Resolves an NBP name to its registered addresses. Omitted fields wildcard: `=` for object/type,
`*` for zone. Default pattern when no argument is given: `=:=@*` (every name in this zone).

| Flag | Default | Meaning |
|---|---|---|
| `-net` | `0` | AppleTalk network number we claim as our source (`0` = the AppleTalk "startup range" placeholder — a strict peer may ignore requests from it; pass the segment's real network number if a peer that answers a real client doesn't answer this probe). |
| `-src` | `0` | Our LocalTalk source node. `0` (default) picks a random workstation-range candidate (1–127) for the LLAP node-claim (`-claim`, on by default); `1`–`254` requests a specific candidate instead — the node actually used may still differ if it's taken. Requires `-claim` when `0`. |
| `-timeout` | `2s` | How long to collect replies. |
| `-v` | `false` | Verbose wire trace to stderr. |
| `-version` | `false` | Print version information and exit. |

```bash
csnbp "=:AFPServer@*"
csnbp                       # everything in this zone
```

**Exit status:** `1` on error (printed as `csnbp: <err>`); `0` otherwise (a "no replies" result is
still exit `0`).

---

### `csgetzones`

ZIP zone-list query — like netatalk's `getzones`. ATP-carried, DDP type 3 socket 6.

```text
csgetzones [flags]
```

| Flag | Default | Meaning |
|---|---|---|
| `-net` | `0` | AppleTalk network number (`0` = local segment). csgetzones queries routers, which must accept network-0 requests — that's the startup negotiation — so unlike csecho/csnbp there's no strict-peer caveat here. |
| `-src` | `0` | Our LocalTalk source node. `0` (default) picks a random workstation-range candidate (1–127) for the LLAP node-claim (`-claim`, on by default); `1`–`254` requests a specific candidate instead — the node actually used may still differ if it's taken. Requires `-claim` when `0`. |
| `-dst` | `0xFF` | Router node to query (`0xFF` = broadcast to any router). |
| `-timeout` | `2s` | Per-request reply timeout. |
| `-local` | `false` | `GetLocalZones` — only zones on our own network. |
| `-my` | `false` | `GetMyZone` — just the responding router's own zone. `-my` takes priority over `-local` if both are given. |
| `-v` | `false` | Verbose wire trace to stderr. |
| `-version` | `false` | Print version information and exit. |

```bash
csgetzones
csgetzones -my -dst 12
```

**Exit status:** `1` on error (printed as `csgetzones: <err>`); `0` otherwise.

---

### `cspap`

Minimal PAP (Printer Access Protocol) client: enumerates printer shares by NBP type and
reports each one's status string — the same text the Classic Mac Chooser shows next to a
LaserWriter icon. ATP-carried (DDP type 3); no PAP connection is opened, only a single
SendStatus/Status transaction per printer.

```text
cspap [flags] [object]
```

| Flag | Default | Meaning |
|---|---|---|
| `-net` | `0` | AppleTalk network number we claim as our source (`0` = the AppleTalk "startup range" placeholder — a strict peer may ignore requests from it; pass the segment's real network number if a peer that answers a real client doesn't answer this probe). |
| `-src` | `0` | Our LocalTalk source node. `0` (default) picks a random workstation-range candidate (1–127) for the LLAP node-claim (`-claim`, on by default); `1`–`254` requests a specific candidate instead. Requires `-claim` when `0`. |
| `-type` | `LaserWriter` | NBP type to browse. Pass `=` to browse every type in the zone — every match then gets a status query, including non-PAP entries, which will just time out. |
| `-zone` | *(empty → this zone)* | AppleTalk zone to search. |
| `-timeout` | `2s` | How long to collect NBP replies, and the per-printer PAP status timeout. |
| `-status` | `true` | Query each printer's PAP status (`SendStatus`) after finding it. `-status=false` only enumerates names/addresses. |
| `-v` | `false` | Verbose wire trace to stderr. |
| `-version` | `false` | Print version information and exit. |

```bash
cspap                       # every LaserWriter-type share in this zone, with status
cspap -type = -status=false # every NBP name in the zone, no status query
```

**Exit status:** `1` on error (printed as `cspap: <err>`); `0` otherwise (finding no printer
shares is not an error).

---

## 4. IPX / NetBIOS / NetWare helpers

These four talk raw Ethernet directly and need a `pcap`-tagged build plus privilege to open the
NIC (`sudo`/`setcap cap_net_raw` on Linux, Administrator on Windows, or the Local Network
permission prompt on macOS).

```bash
go build -tags pcap -o csipxping ./cmd/csipxping
go build -tags pcap -o csncpinfo ./cmd/csncpinfo
go build -tags pcap -o csnetsend ./cmd/csnetsend
go build -tags pcap -o csnetview ./cmd/csnetview
```

### `csipxping`

IPX Diagnostic request/response (Novell IPXPING equivalent). Socket `0x0456`, Ethernet II
etherType `0x8137`.

```text
csipxping [flags]
```

| Flag | Default | Meaning |
|---|---|---|
| `-iface` | *(auto)* | Interface to send on (pcap device name; omit to auto-detect the primary NIC). |
| `-dst` | `broadcast` | Target node as a MAC address (`aa:bb:cc:dd:ee:ff`) or `"broadcast"`. |
| `-net` | `00000000` | IPX network number, 8 hex digits (`0` = local segment). |
| `-count` | `3` | Number of diagnostic requests to send. |
| `-timeout` | `2s` | Per-request reply timeout. |
| `-interval` | `500ms` | Delay between requests. |
| `-mac` | *(random)* | Source MAC for our virtual station (default: random locally-administered). |
| `-list-ifaces` | `false` | List the capturable pcap NICs and exit. |
| `-version` | `false` | Print version information and exit. |

```bash
csipxping -iface eth0 -dst broadcast -count 5
csipxping -iface en0 -dst 00:1a:2b:3c:4d:5e -net 00000001
```

Prints a per-reply line, per-timeout line, and a final `--- <iface> IPX diagnostic statistics ---`
summary with sent/replies/loss%. **Exit status:** `1` if zero replies were received, or on any
other error (`csipxping: <err>`); `0` otherwise.

---

### `csncpinfo`

NetWare file-server discovery (SAP "General"/"Nearest" service query — like netatalk-era `slist`).
IPX socket `0x0452`.

```text
csncpinfo [flags]
```

| Flag | Default | Meaning |
|---|---|---|
| `-iface` | *(auto)* | Interface to send on (pcap device name; omit to auto-detect the primary NIC). |
| `-net` | `00000000` | IPX network number, 8 hex digits (`0` = local segment). |
| `-timeout` | `2s` | How long to collect SAP responses. |
| `-nearest` | `false` | Send a Get-Nearest-Server query instead of a general query. |
| `-frametype` | *(ethernet_ii)* | IPX Ethernet encapsulation: `ethernet_ii \| 802.3 \| 802.2`. **Must match** the server's IPX encapsulation, or it won't be seen. |
| `-mac` | *(random)* | Source MAC for our virtual station. |
| `-list-ifaces` | `false` | List the capturable pcap NICs and exit. |
| `-version` | `false` | Print version information and exit. |

```bash
csncpinfo -iface eth0
csncpinfo -iface eth0 -frametype 802.3 -nearest
```

**Exit status:** `1` if zero servers were found, or on any other error (`csncpinfo: <err>`); `0`
otherwise.

---

### `csnetview`

Enumerates SMB servers via the master browser (`NetServerEnum2`), not a broadcast-announcement
sniff — like a real Windows "net view". Shares carrier code with `csfs discover smb`.

```text
csnetview [flags]
```

| Flag | Default | Meaning |
|---|---|---|
| `-iface` | *(auto)* | Interface to browse on (pcap or TUN/TAP device name; omit to auto-detect the primary NIC). |
| `-ifacetype` | `pcap` | Interface type: `pcap \| tap` (Linux TUN/TAP). |
| `-timeout` | `4s` | How long to listen per carrier after soliciting. |
| `-v` | `false` | Verbose wire trace to stderr. |
| `-list-ifaces` | `false` | List the capturable pcap NICs and exit. |
| `-version` | `false` | Print version information and exit. |

Runs three discovery passes per carrier (NBF, NB-IPX): solicit+sniff, find-master
(`__MSBROWSE__`/workgroup `<1D>` + `GetBackupList`), then `NetServerEnum2` against the elected
master. Prints a per-carrier header, a results table (SERVER / CARRIERS / SOURCE / ROLE-COMMENT),
and a final count.

```bash
csnetview -iface eth0
```

**Exit status:** `1` on error (`csnetview: <err>`); `0` otherwise — an empty result set is reported
inline, not as a failure.

---

### `csnetsend`

Sends a NetBIOS Messenger ("net send" / WinPopup) pop-up datagram over a raw interface.

```text
csnetsend -iface <dev> -to <name>,<protocol> -text <msg> [flags]
```

| Flag | Default | Meaning |
|---|---|---|
| `-iface` | *(auto)* | Interface to send from (pcap or TUN/TAP device name; omit to auto-detect). |
| `-ifacetype` | `pcap` | Interface type: `pcap \| tap`. |
| `-to` | *(required)* | Recipient as `<name>,<protocol>` — protocol is `nbf` (NetBEUI) or `nbipx` (NetBIOS-over-IPX). |
| `-from` | `CLASSICSTACK` | Sender name (the From field). |
| `-text` | *(required)* | Message text. |
| `-mac` | *(random)* | Source MAC for our virtual station. |
| `-v` | `false` | Verbose wire trace to stderr. |
| `-list-ifaces` | `false` | List the capturable pcap NICs and exit. |
| `-version` | `false` | Print version information and exit. |

`-iface`, `-to`, and `-text` are effectively required — missing any of them prints usage and
exits with an error.

```bash
csnetsend -iface eth0 -to WORKSTATION,nbf -text "Server rebooting in 5 minutes"
```

**Exit status:** `1` on error (`csnetsend: <err>`); `0` on successful send.

---

## 5. Embedded / experimental

### `cs-tinygo`

**Not an operator tool.** Its sole purpose is to give the TinyGo amd64 build gate something real
to compile: it blank-imports the TinyGo-safe subset of `core/` so that a forbidden import or a
reflection-using package in that subset makes `tinygo build` fail, proving the
no-reflection/no-forbidden-import discipline without ESP32 hardware. No flags, no subcommands. The
real interactive entry point remains `classicstack`.

---

## 6. Full flags-by-package map

For readers extending or auditing the CLI surface, this is which shared package backs which
tool's flags:

| Package | Consumers |
|---|---|
| `cmd/internal/cli` | `classicstack`, `classicstack-svc run`, `classicstackd run` (`-config`, `-http`, `-version`, `-list-ifaces`) |
| `cmd/internal/buildinfo` | Every tool's `-version` output |
| `cmd/internal/atlink` | `csecho`, `csnbp`, `csgetzones`, `cspap` (`-transport`, `-iface`, `-device`, `-baud`, `-list-ifaces`, `-claim`) |
| `cmd/internal/csconnect` | `csfs` (`cmd/csclient`), `csmount` (`-ifacetype`, `-iface`, `-fork`, `-mac`, `-transport`, `-frametype`/`-framing`, `-v`, `-list-ifaces`, `-version`, `-cache-ms`) |

`csipxping`, `csncpinfo`, `csnetsend`, and `csnetview` declare their flags directly (no shared
FlagSet helper), though they reuse `csconnect.ResolveIface`/`csconnect.StationMAC` internally for
interface auto-detection and MAC generation.

---

## 7. Compiling in the diagnostic tools

The router/server (`classicstack`, `classicstack-svc`, `classicstackd`) chooses its compiled-in
protocol/port set via build tags on the whole binary. The small client tools instead each need
just enough tags to reach the NIC:

| Need | Tag |
|---|---|
| Raw Ethernet capture (any of csecho/csnbp/csgetzones/cspap/csipxping/csncpinfo/csnetsend/csnetview, csfs, csmount) | `pcap` |
| FUSE mount support in `csmount` on macOS/Linux | `fuse` (needs cgo + macFUSE/libfuse headers) |
| Full desktop build (everything at once) | `all` |

```bash
bash scripts/build-local.sh   # builds every desktop command into ./bin with the full tag set
```

---

## 8. See also

- [manual.md](manual.md) — the short operator tour these tools live in.
- [build.md](build.md) — the full build-tag reference.
- [config.md](config.md) — `server.toml` field reference.
- [forks.md](forks.md) — what each `-fork` backend actually stores, server-side and client-side.
- [filename-encoding.md](filename-encoding.md) — how filenames are transcoded between the host
  filesystem and each protocol's own wire charset.
