---
title: "Quick Start"
weight: 1
---

# Quick start

## 1. Get a binary

Either grab a build from [GitHub Releases](https://github.com/ObsoleteMadness/ClassicStack/releases/latest),
or build from source — see [build.md](build.md):

~~~bash
git clone --recurse-submodules https://github.com/ObsoleteMadness/ClassicStack.git
cd ClassicStack
go build -tags all -o classicstack ./cmd/classicstack
~~~

## 2. Configure

~~~bash
cp server.toml.example server.toml
~~~

Edit `server.toml`:

- Point `[[interface]]` (the shared bridge) at your NIC (`device = "eth0"`, or leave
  `hw_address` blank on Wi-Fi).
- Enable the ports/services you want (`[[ethertalk]]`, `[[ltoudp]]`, `[AFP]`, `[SMB]`,
  …) and list which of them should join `[router].members`.
- Add at least one share (`[[afpvolumes]]` or `[[smbshares]]`) if you want file service.

Full key-by-key reference: [config.md](config.md). Fully commented example:
[`server.toml.example`](../server.toml.example).

## 3. Run

~~~bash
./classicstack                    # auto-loads ./server.toml
./classicstack -config /path/to/server.toml
~~~

~~~powershell
.\classicstack.exe -config server.toml
~~~

Config-loading rules: `-config` cannot be combined with other flags; with no flags,
`server.toml` is loaded automatically if present.

## 4. Open the web UI

If built with `-tags webui` (implied by `all`), the admin UI is at
`http://127.0.0.1:1984/` by default (`[http].addr`, or `-http :port` on the command
line). First run walks you through creating an admin user, then shows live status,
sharing, and the Finder-first file browser. See [web-ui.md](web-ui.md).

## 5. Connect a client

- **Classic Mac**: point AppleShare at the zone/server you configured; it should show
  up over EtherTalk/LocalTalk once a router-joined port is up.
- **Windows/DOS**: `net view \\CLASSICSTACK` (SMB, needs `[SMB]` + a NetBIOS transport).
- **File client from this host**: `csfs ls "afp://user@MyServer/My Volume"` or
  `csmount "afp://user@MyServer/My Volume" M:` — see
  [manual.md §2](manual.md#2-command-line-tools).

## Next steps

- [build.md](build.md) — building from source, every build tag explained
- [config.md](config.md) — full `server.toml` reference
- [protocols.md](protocols.md) — exactly which protocol versions/dialects are supported
- [netboot.md](netboot.md) — booting a diskless classic Mac over AppleTalk
- [testing.md](testing.md) — the end-to-end test suites, including the native vintage
  client tools
- [web-ui.md](web-ui.md) — the admin API and how the web UI is put together
- [manual.md](manual.md) — the full operator/developer manual
