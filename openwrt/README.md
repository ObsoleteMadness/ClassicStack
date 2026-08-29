# ClassicStack on OpenWRT

Artifacts to build and host ClassicStack as an OpenWRT package.

```
openwrt/
  Makefile                     OpenWRT package Makefile (golang-package)
  files/
    classicstack.init          procd init script  -> /etc/init.d/classicstack
    classicstack.config        UCI default config -> /etc/config/classicstack
  README.md                    this file
```

## How ClassicStack reads UCI

There is **no separate config format on the router**. ClassicStack's config
codec is chosen by the `-config` path: any path under an `/etc/config/`
directory (or ending in `.uci`) is parsed as OpenWRT UCI; everything else is
TOML. The init script runs:

```
classicstack -config /etc/config/classicstack
```

so the daemon reads `/etc/config/classicstack` directly. `uci set` / LuCI edits
take effect on `/etc/init.d/classicstack reload` (a procd reload trigger watches
the file). The web-admin UI's Save likewise rewrites the UCI file in place.

`/etc/config/classicstack` mirrors `server.toml.example` field-for-field — see
that file for what each option means. Booleans are `'0'` / `'1'`. The one extra
block is `config classicstack 'init'`, which the **init script** reads (enable
flag, `http_addr` for the web-admin API, respawn) and the daemon ignores.

## Building the package

The Makefile uses the buildroot's `golang-package.mk`, so it cross-compiles
`./cmd/classicstack` for the target arch. Place this directory in a feed:

```
# in your OpenWRT buildroot
cp -r openwrt feeds/<yourfeed>/net/classicstack      # or symlink it
./scripts/feeds update <yourfeed>
./scripts/feeds install classicstack

make menuconfig            # Network -> Services -> classicstack  (set to <M> or <*>)
make package/classicstack/compile V=s
```

The resulting `.ipk` is under `bin/packages/<arch>/<feed>/`. Install on the
router with `opkg install classicstack_*.ipk`.

### Build tags

`GO_PKG_BUILD_TAGS` in the Makefile selects which components are compiled in.
The default is `afp smb netbios ipx netbeui macip pcap` (a full legacy file
server with libpcap capture). Trim it for a smaller image — e.g. `afp pcap` for
an AppleTalk/AFP-only router. `DEPENDS` pulls in `libpcap` for the `pcap` tag;
drop both together if you build without raw-Ethernet transports.

## Running

```
/etc/init.d/classicstack enable      # start at boot
/etc/init.d/classicstack start
logread -e classicstack              # follow logs (procd captures stdout/stderr)
```

The management UI is on **:1984** by default (`config http`). Override the
listen address with `option http_addr ':1984'` in `config classicstack 'init'`,
or set `option enabled '0'` under `config http` to turn it off. HTTP Basic over
that address has no TLS of its own — keep it on the LAN or behind a TLS reverse
proxy, and note the web-admin requires a first-run setup before it serves
anything.

## Privileges & interfaces

The EtherTalk / IPX / NetBEUI transports open raw Ethernet via libpcap and need
to run as root (procd does). Bind them to the router's LAN bridge by setting the
`iface` option (e.g. `br-lan`) and declaring it in a `config interface` block,
or leave `iface` blank to inherit the `config bridge` default.
