<div align="center">

<img src="https://raw.githubusercontent.com/ObsoleteMadness/OmniTalk/main/icon256.png" alt="OmniTalk" width="256" height="256"/>

# OmniTalk

### OmniTalk is a all-in-one AppleTalk Phase 2 router, MacIP Router and AFP file server for bridging classic Apple networking into modern environments. 🍏💾

</div>

## Features

- Cross Platform Support: runs on Windows, MacOS and Linux.
- 100% user-mode code, no special kernels or features needed.
- AppleTalk routing across multiple transports.
- EtherTalk support via pcap, plus tap/tun backend options.
- LocalTalk via LToUDP and TashTalk serial adapters.
- AFP file server running over both DDP (ASP/ATP) and TCP (DSI).
- MacIP gateway support with both a bridged mode and NAT mode.
- Zone and routing protocols (RTMP/ZIP/NBP) implemented as router services.



## Quick start

- Copy server.ini.example to server.ini and edit values.
- Run OmniTalk with no flags to auto-load server.ini.
- Or pass a config file explicitly with -config.

Examples:

~~~bash
./omnitalk -config server.ini
~~~

~~~powershell
.\omnitalk.exe -config server.ini
~~~

Config-loading rule:

- -config cannot be combined with other flags.
- If no flags are supplied, OmniTalk auto-loads server.ini if present.

---

## License
 - Currently GPL3. 


## Build instructions

Requirements:

- Go 1.23+
- On Windows for EtherTalk/pcap: [Npcap](https://npcap.com/#download)
- On Linux/macOS for EtherTalk/pcap: libpcap

Build from repository root:

~~~bash
go build ./cmd/omnitalk
~~~

Build with explicit binary name:

~~~bash
go build -o omnitalk ./cmd/omnitalk
~~~

Build with explicit semantic version metadata:

~~~bash
go build -trimpath \
	-ldflags "-X main.BuildVersion=1.2.3 -X main.BuildCommit=$(git rev-parse --short HEAD) -X main.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
	-o omnitalk ./cmd/omnitalk
~~~

Build using the shared local/CI scripts:

~~~bash
bash scripts/ci/build.sh
bash scripts/ci/test.sh
~~~

~~~powershell
./scripts/ci/build.ps1
./scripts/ci/test.ps1
~~~

Print runtime/build version info:

~~~bash
./omnitalk -version
~~~

Run tests:

~~~bash
go test ./...
~~~

## CI and releases

- Pull requests to `main`/`master` run GitHub Actions CI for tests and cross-platform builds.
- Pushes (including merges) to `main` publish a `dev-*` prerelease.
- Pushing a SemVer tag like `v1.2.3` publishes a stable release for that tag.
- GitHub Actions calls the same scripts under `scripts/ci/` that you can run locally.
- Release assets are produced for Linux, macOS, and Windows.
- Release packages include the repository `dist/` content.
- Windows release binaries include icon and file version metadata from `icons/omnitalk.ico`.
- macOS release bundles include app icon metadata from `icons/omnitalk.icns`.
- Go build/test already ignores non-Go folders; additionally `scripts/ci/test.sh` and `scripts/ci/test.ps1` explicitly exclude `dist`, `icon`, and `icons` from the package list.

## Status and provenance
> **Warning:** large parts of this codebase were developed in a "vibe coded" style. It appears to work in real use, but treat behavior as pragmatic rather than formally verified.

## Attribution
- The AppleTalk routing is based on tashrouter by lampmerchant: https://github.com/lampmerchant/tashrouter (in-fact it's basically an LLM port).


## AppleTalk Routing

Route AppleTalk between EtherTalk and LocalTalk ports, with RTMP/ZIP/NBP services provided by the router.

### At a glance

- Ports: EtherTalk (pcap/tap/tun), LToUDP, TashTalk.
- Core keys: `[LToUdp]`, `[TashTalk]`, `[EtherTalk]`.
- Wi-Fi note: use `bridge_mode=wifi` when adapters/APs reject non-host source MACs.

### Listing interfaces on Windows

Use the built-in pcap listing mode:

~~~powershell
.\omnitalk.exe -list-pcap-devices
~~~

This prints available interface names and pcap device IDs. Use the device string in [EtherTalk] device, for example:

~~~ini
device = "\Device\NPF_{YOUR-GUID-HERE}"
~~~

Tip: [install Npcap](https://npcap.com/#download) first, otherwise pcap devices may not appear.

### Example interface configs (Linux, macOS, Windows)

These examples show only relevant keys; merge into your full server.ini.

Linux example:

~~~ini
[LToUdp]
enabled = true
interface = 192.168.1.10

[EtherTalk]
backend = pcap
device = eth0
hw_address = "DE:AD:BE:EF:CA:FE"
seed_network_min = 3
seed_network_max = 5
seed_zone = "EtherTalk Network"
~~~

macOS example:

~~~ini
[LToUdp]
enabled = true
interface = 192.168.1.20

[EtherTalk]
backend = pcap
device = en0
hw_address = "DE:AD:BE:EF:CA:FE"
seed_network_min = 3
seed_network_max = 5
seed_zone = "EtherTalk Network"
~~~

Windows example:

~~~ini
[LToUdp]
enabled = true
interface = 0.0.0.0

[EtherTalk]
backend = pcap
device = "\Device\NPF_{1DFDAA9C-7DD4-40F8-B6D4-9298C273D654}"
hw_address = "DE:AD:BE:EF:CA:FE"
bridge_mode = auto
seed_network_min = 3
seed_network_max = 5
seed_zone = "EtherTalk Network"
~~~

### Configuration reference

### [LToUdp]

| Key | Type | Default | Description |
|---|---|---|---|
| enabled | bool | true | Enables LToUDP LocalTalk port. |
| interface | string | 0.0.0.0 | Local IPv4 address used for multicast join/send. 0.0.0.0 means auto/default interface. |
| seed_network | uint | 1 | Seed network number for this LocalTalk segment. |
| seed_zone | string | LToUDP Network | Seed zone name advertised for LToUDP. |

### [TashTalk]

| Key | Type | Default | Description |
|---|---|---|---|
| port | string | (empty) | Serial port path/name for TashTalk. Empty disables TashTalk. |
| seed_network | uint | 2 | Seed network number for TashTalk segment. |
| seed_zone | string | TashTalk Network | Seed zone name advertised for TashTalk. |

### [EtherTalk]

| Key | Type | Default | Description |
|---|---|---|---|
| backend | string | pcap | Backend type: blank, pcap, tap, or tun. Blank disables EtherTalk. |
| device | string | (empty) | Interface/device identifier. For pcap this is adapter name/device ID. |
| hw_address | string | DE:AD:BE:EF:CA:FE | Router MAC address used by EtherTalk port. |
| bridge_mode | string | auto | Bridge mode: auto, ethernet, or wifi. |
| bridge_host_mac | string | (empty) | Optional host adapter MAC for Wi-Fi bridge shim logic. |
| seed_network_min | uint | 3 | Minimum network in seeded EtherTalk range. |
| seed_network_max | uint | 5 | Maximum network in seeded EtherTalk range. |
| seed_zone | string | EtherTalk Network | Seed zone for EtherTalk. |

#### EtherTalk bridge modes

- `bridge_mode=auto`: Detects medium and picks `ethernet` for wired links or `wifi` for wireless links.
- `bridge_mode=ethernet`: Raw pass-through bridging. Frames are forwarded without MAC rewrite.
- `bridge_mode=wifi`: Enables Wi-Fi bridge shim behavior for adapters/APs that do not allow arbitrary source MACs.

Why `wifi` mode exists:

- Many Wi-Fi adapters and AP paths reject or rewrite frames when the source MAC does not match the host adapter MAC.
- On Windows, the miniport/NDIS path commonly drops transmit frames when source hardware address does not match the host adapter MAC.
- In `wifi` mode OmniTalk rewrites outbound EtherTalk frame source MAC to the host adapter MAC and updates AARP hardware fields accordingly.
- For inbound traffic, OmniTalk reverses destination rewrite using a short-lived peer-to-virtual mapping so the EtherTalk port still sees the expected virtual MAC identity.
- This is effectively an L2 NAT-style shim for MAC identities (not MacIP IP-layer NAT).

Recommended settings:

- On Wi-Fi, set `bridge_mode=wifi` (or leave `auto` and verify it detected Wi-Fi correctly).
- Set `bridge_host_mac` to your actual Wi-Fi adapter MAC when needed; if blank, OmniTalk falls back to `hw_address`.
- On wired Ethernet, prefer `bridge_mode=ethernet` or `auto`.

##### Wi-Fi troubleshooting

Common symptoms:

- You see AppleTalk traffic in one direction only.
- AARP appears unanswered even when peers are present.
- OmniTalk works on wired Ethernet but fails on the same host over Wi-Fi.

Checks and fixes:

- Force `bridge_mode=wifi` instead of relying on `auto` while testing.
- Set `bridge_host_mac` to the real Wi-Fi adapter MAC shown by your OS/NIC tools.
- On Windows, confirm the adapter MAC did not randomize or change after reconnect; update `bridge_host_mac` if it did.
- Ensure your WLAN does not enable client isolation/AP isolation when testing peer-to-peer visibility.
- Verify you selected the intended pcap device (especially when multiple virtual/VPN adapters exist).

## MacIP Gateway

Provide IP connectivity to AppleTalk clients via a MacIP gateway.

### At a glance

- Use `mode=nat` when upstream routers cannot install static routes to your MacIP client subnet.
- Use `mode=pcap` for bridged/static-pool style behavior.
- Use `dhcp_relay=true` to relay/translate DHCP for MacIP clients instead of relying only on static gateway semantics.

Example NAT-oriented configuration:

~~~ini
[MacIP]
enabled = true
mode = nat
zone = "EtherTalk Network"
nat_subnet = 192.168.100.0/24
nat_gw = 192.168.100.1
ip_gateway = 192.168.1.1
nameserver = 192.168.1.1
dhcp_relay = false
lease_file = leases.txt
~~~

### [MacIP]

| Key | Type | Default | Description |
|---|---|---|---|
| enabled | bool | false | Enables MacIP service. |
| mode | string | pcap | MacIP mode: pcap (bridged/static-pool behavior) or nat. |
| zone | string | (empty) | Zone used for MacIP NBP registration. Empty falls back to EtherTalk/first zone. |
| nat_subnet | string | 192.168.100.0/24 | MacIP subnet CIDR for address assignment/NAT pool. |
| nat_gw | string | (empty) | Gateway IP presented to MacIP clients in NAT mode. |
| lease_file | string | (empty in code; example uses leases.txt) | Optional path for lease persistence across restarts. |
| ip_gateway | string | (empty) | Upstream/default gateway IP on IP-side network. |
| dhcp_relay | bool | false | Enables DHCP relay/translation mode for MacIP clients. |
| nameserver | string | (empty) | DNS server advertised to MacIP clients. |

## AFP

OmniTalk includes an AFP file server focused on AFP 2.0-level behavior, with selective AFP 2.1/2.2 calls, exposed over both classic AppleTalk transport and modern TCP transport:

- DDP stack: DDP -> ATP -> ASP -> AFP
- TCP stack: TCP -> DSI -> AFP
- Advertised AFP versions: AFPVersion 2.0 and AFPVersion 2.1

### AFP feature status

Supported:

- Core volume, directory, file, fork, and enumerate operations.
- Desktop database operations (icons, APPL mappings, comments).
- File extension to type/creator fallback via extension map.

Unsupported or limited:

- Catalog search (`FPCatSearch`) is currently not implemented.
- Multi-phase login continuation (`FPLoginCont`) is not implemented.
- `FPChangePassword` and `FPGetUserInfo` return call-not-supported.

### Authentication model

- Server info advertises `No User Authent`.
- Runtime behavior is effectively guest/no-user-auth.
- The internal cleartext-password path exists in code but is not exposed via current runtime config.

### AFP configuration overview

#### Server identity and transports

- Set server display name with `[AFP] name`.
- Select transports with `[AFP] protocols` (`ddp`, `tcp`, or both).
- Set DSI listen address with `[AFP] binding`.

### [AFP]

| Key | Type | Default | Description |
|---|---|---|---|
| enabled | bool | true | Enables AFP service. |
| name | string | Go File Server | NBP-advertised AFP server name. |
| zone | string | (empty) | Zone for AFP registration. Empty uses router-selected default. |
| protocols | string | tcp,ddp | Enabled AFP transports: tcp, ddp, or both comma-separated. |
| binding | string | :548 | TCP listen address for DSI AFP. |
| extension_map | string | (empty) | Path to Netatalk-compatible extension map file. Relative paths are resolved from INI directory. |

#### Filename mapping and encoding

Behavior:

- AFP names are converted between MacRoman (wire) and UTF-8 (host filesystem).
- With `use_decomposed_names=true` (default), host-reserved filename characters are escaped as `0xNN` tokens.
- Reserved-character escaping is platform-aware (Windows has a larger reserved set than POSIX).

#### Extension mapping (extmap.conf)

Use `[AFP] extension_map` to provide Macintosh type/creator metadata for files based on extension.

Example in `server.ini`:

~~~ini
[AFP]
enabled = true
extension_map = extmap.conf
~~~

Format rules:

- One mapping per non-empty line.
- Lines starting with `#` are comments.
- First token is the extension key (typically with leading dot, for example `.txt`).
- Next two quoted fields are required: `"TYPE"` and `"CREA"`.
- `TYPE` and `CREA` must each be exactly 4 bytes.
- A default `.` mapping is required and is used when no specific extension match exists.
- Extension matching is case-insensitive (`ReadMe.TXT` matches `.txt`).

Examples (from the shipped `extmap.conf`):

~~~text
.         "????"  "????"      Unix Binary                    Unix                      application/octet-stream
.txt      "TEXT"  "ttxt"      ASCII Text                     SimpleText                text/plain
.bin      "SIT!"  "SITx"      MacBinary                      StuffIt Expander          application/macbinary
.hqx      "TEXT"  "SITx"      BinHex                         StuffIt Expander          application/mac-binhex40
.sit      "SIT!"  "SITx"      StuffIt 1.5.1 Archive          StuffIt Expander          application/x-stuffit
~~~

Notes:

- In `extmap.conf`, many mappings are disabled by default with `#`; remove `#` to enable a line.
- Extra columns after the first three fields are allowed and treated as descriptive metadata.

### [Volumes.<name>]

Each volume is configured as a separate `[Volumes.<section-name>]` section.

| Key | Type | Default | Description |
|---|---|---|---|
| name | string | section suffix | Display name for the AFP volume (max 31 chars recommended). |
| path | string | none (required) | Host filesystem path to export. |
| read_only | bool | false | Exports the volume as read-only at AFP protocol level. |
| cnid_backend | string | sqlite | CNID backend; currently sqlite or memory depending on build/runtime support. Must not conflict across volumes. |
| use_decomposed_names | bool | true | Encodes host-reserved filename characters as 0xNN tokens in AFP mapping. Must not conflict across volumes. |
| fork_backend | string | (blank/AppleDouble) | Currently only AppleDouble is accepted when set. |
| appledouble_mode | string | modern | Metadata layout mode: modern (._ sidecars) or legacy (.appledouble directory style). |
| rebuild_desktop_db | bool | false | Rebuilds AFP desktop database from resource fork metadata at startup. |

#### Read-only volume behavior

When `read_only=true` is set on a volume:

- `FPGetVolParms` reports the volume read-only flag (VolAttrReadOnly, bit 15).
- Directory access rights are returned as read-only in directory parameter replies.
- File attributes include WriteInhibit (ReadOnly in AFP 2.0 terminology).
- Write and metadata-mutating operations are denied.

Error code behavior by AFP version:

- AFP 2.0 and higher: returns `kFPVolLocked` (`-5031`).
- AFP 1.1 compatibility mode: returns `kFPAccessDenied`.

Example:

~~~ini
[Volumes.Sample]
path = dist/Sample Volume
read_only = true
~~~

Volume naming:

- Volume names are sent as Pascal strings on AFP (1-byte length). Keep names <=255 bytes.
- For classic Finder compatibility and UI quality, keep names short (31 chars recommended).

#### Sidecar metadata

- `fork_backend` currently accepts AppleDouble storage.
- `appledouble_mode=modern` uses `._filename` sidecars beside files.
- `appledouble_mode=legacy` uses `.AppleDouble/filename` sidecars.
- `rebuild_desktop_db=true` rebuilds desktop metadata cache at startup.

#### Netatalk compatibility

- Compatible formats: Netatalk-style extension map syntax and AppleDouble modern/legacy sidecar layouts.
- Known differences: CNID database implementation is OmniTalk-specific (sqlite or memory), not a drop-in Netatalk CNID store.
- OmniTalk does not currently provide a Netatalk-style extended-attribute metadata backend.
- AFP feature coverage is practical but incomplete (for example catalog search is unsupported).

### [Logging]

| Key | Type | Default | Description |
|---|---|---|---|
| level | string | info | Log level: debug, info, warn. |
| parse_packets | bool | false | Decodes and logs inbound DDP packets and upper protocol layers. |
| parse_output | string | (empty) | Optional output file path for parsed packet logs. |
| log_traffic | bool | false | Enables low-level traffic logging at debug level. |

## Command-line quick reference

Common operational flags:

- -config
- -list-pcap-devices
- -log-level and -log-traffic
- -parse-packets and -parse-output
- -afp-volume (repeatable Name:Path)

Use server.ini for repeatable deployments; use flags for quick experiments.

## Rough project layout

- cmd/omnitalk: entrypoint, flag handling, INI loading, runtime wiring.
- router: datagram dispatch, routing table, zone information table.
- port: transport implementations (EtherTalk, LocalTalk variants, rawlink, NAT helpers).
- service: protocol/application services (AEP, RTMP, ZIP, ASP/ATP/DSI, AFP, MacIP, LLAP).
- appletalk and protocol/ddp: packet/datagram and protocol encoding helpers.
- spec: protocol and subsystem design notes.

## Contributing

Contributions are welcome.

Suggested workflow:

1. Open an issue describing the bug, protocol behavior, or enhancement.
2. Keep pull requests focused to one subsystem when possible.
3. Add or update tests near the package you changed.
4. Run go test ./... before opening a PR.
5. Include protocol notes or packet captures when behavior changes are non-obvious.

Practical expectations:

- Preserve existing architecture patterns (ports, router, services).
- Keep platform-specific files separated where they already are (for example *_windows.go vs *_other.go).
- Prefer small, reviewable changes over broad refactors.

## Related docs

- [Inside AppleTalk](https://obsoletemadness.github.io/Inside-AppleTalk/)