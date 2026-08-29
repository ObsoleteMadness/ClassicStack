---
title: "ClassicStack"
type: docs
---

# ClassicStack

ClassicStack is an AppleTalk router and classic LAN services stack that bridges legacy
Macintosh and DOS networking into modern environments — AFP, SMB1, NetBIOS, IPX, MacIP,
NetBoot, and a file client that mounts remote shares on a modern host.

This site is generated straight from the [ClassicStack](https://github.com/ObsoleteMadness/ClassicStack)
repository's `docs/` and `spec/` directories — nothing here is duplicated by hand, so it
never drifts from what's actually committed.

- **[Quick Start](docs/quickstart/)** — get running in five minutes
- **[Building](docs/build/)** — requirements, build commands, every build tag
- **[Configuration](docs/config/)** — the full `server.toml` reference
- **[Protocol Support](docs/protocols/)** — exact AFP/SMB/AppleTalk/IPX versions and dialects
- **[Web UI & API](docs/web-ui/)** — the control API and how the SPA is put together
- **[Netboot & ChainBoot](docs/netboot/)** — booting a diskless classic Mac over AppleTalk
- **[Testing](docs/testing/)** — the protocol test harness and native vintage-client tools
- **[Full Manual](docs/manual/)** — the complete operator/developer guide
- **[Architecture](architecture/)** — the runtime map
- **[Protocol Notes](spec/)** — wire-level protocol specifications

Grab a build from [GitHub Releases](https://github.com/ObsoleteMadness/ClassicStack/releases/latest),
or see [Building](docs/build/) to build from source.
