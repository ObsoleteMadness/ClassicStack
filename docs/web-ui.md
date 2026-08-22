---
title: "Web UI & API"
weight: 5
---

# Web UI and control API

ClassicStack's web admin UI is two separately-versioned pieces talking one contract: a
Go control API (this repo) and a TypeScript SPA
([ClassicStack-web](https://github.com/ObsoleteMadness/ClassicStack-web), pulled in as a
git submodule). Neither embeds protocol knowledge the other needs to duplicate — the Go
side speaks AFP/SMB/NCP/EtherDFS/AppleTalk; the browser only ever speaks HTTP/JSON and
SSE to it.

## The API split

### `core/control` — the transport-agnostic contract

`core/control.Plane` is the single management contract every front-end drives: request/
response methods (status, config stage/apply/save, service start/stop/restart,
diagnostics, share CRUD) plus a topic-based `Subscribe` for push updates (status, stats,
log lines, messages). It lives in the `core` ring — stdlib plus `core/bus` and
`core/config` only, no `net/http`, no transport types — precisely so it can be driven by
more than one kind of front-end without any of them being "the real one":

| Adapter | Transport | Used by |
|---|---|---|
| `adapter/control/http` | HTTP/JSON + Server-Sent Events | The web admin SPA, `csfs`/`csmount`'s remote-control mode |
| `adapter/control/ubus` | OpenWRT `ubus` | Router-firmware builds (procd/init.d) |
| `adapter/control/inproc` | Direct in-process call | The tray app and CLI tools running alongside the server |

Because all three sit over the same `Plane`, a feature (say, a new share-config field)
implemented once at the `core/control` layer is immediately available through HTTP,
`ubus`, and in-process callers — there is no "the HTTP API has this but ubus doesn't"
drift by construction. `adapter/control/parity_test.go` asserts this: the same operation
driven through `inproc` and `http` must produce the same result.

### `adapter/control/finder` — the catalog/session layer

The Finder-style browsing surface (list a volume's children, get/put files, mount
tracking, login sessions, per-scheme catalog adapters for AFP/SMB/NCP/EtherDFS/local
shares) is a distinct package from the general `Plane`. It is the server-side backend
for `/finder`: the browser never speaks AFP/SMB/NCP/EtherDFS itself, it asks this layer
to do so and gets back a protocol-agnostic catalog view.

### `adapter/control/http` — the HTTP surface

Exposes the `Plane` and the Finder catalog as JSON endpoints plus one `/subscribe` SSE
stream (status/stats/log/message topics — this is what feeds the live log viewer and the
tray app's notification bell). Also serves the compiled SPA itself
(`adapter/control/http/spa`, gated behind the `webui` build tag — see
[build.md](build.md#build-tags)) and holds the first-run setup gate (`/setup`, 409 until
an admin user exists) and HTTP Basic auth once one does.

## The `classicstack-web` submodule

The SPA's source does not live in this repository — it's a separate repo,
[ClassicStack-web](https://github.com/ObsoleteMadness/ClassicStack-web), pinned as a git
submodule at `third_party/classicstack-web`. Vite and `tsc` alias `classicstack-web/*`
directly into that tree's `src/*` (see `adapter/control/http/ui/vite.config.ts` and its
`tsconfig.json`) — there's no npm publish step in between, so both repos typecheck
against the same TypeScript sources at build time.

### Why a separate repo, and what gets reused

`ClassicStack-web`'s modules build **two** different things over **one** shared
`FinderHost` interface:

1. This project's admin SPA — a `FinderHost` implementation that talks to the Go
   `adapter/control/http` API described above.
2. A standalone LocalTalk PWA, distributed independently, that implements the same
   `FinderHost` interface by speaking AFP **directly from the browser** over TashTalk
   (no ClassicStack server involved at all).

Splitting the UI into its own repo is what makes that reuse possible: every Finder
component, the catalog UI, Get Info panels, resource-fork explorers, and the extension
map editor are written once against `FinderHost` and used by both consumers. A change to
how, say, resource-fork icons render benefits both without either repo vendoring the
other's code.

### Source resolution (`make spa`)

`make spa` (`scripts/ci/spa.sh`) resolves which checkout of `classicstack-web` to build
against, in this order:

| | Source |
|---|---|
| 1 | `$WEB_DIR`, if set — point it at a local checkout to develop against uncommitted UI changes without touching the pin |
| 2 | The `third_party/classicstack-web` submodule |
| 3 | `git submodule update --init`, if the clone skipped submodules |
| 4 | A sibling `../ClassicStack-web` checkout |
| 5 | A shallow clone of `$WEB_REF` (default `main`) into this directory |

CI always takes path 2 — every workflow job that builds the SPA checks out with
`submodules: recursive`, and `.github/actions/setup-spa` fails with a clear message if
the submodule is empty.

Day-to-day commands:

~~~bash
# Populate it (first clone, or after a clone without --recurse-submodules):
git submodule update --init --recursive

# Update to the latest upstream main and record the new pin:
git submodule update --remote third_party/classicstack-web
git add third_party/classicstack-web && git commit -m "chore: bump ClassicStack-web"

# Move an existing checkout to the commit this branch pins (after a pull):
git submodule update --recursive

# Build the SPA against whatever the above resolved:
make spa

# Develop against a local ClassicStack-web checkout without touching the pin:
WEB_DIR=../ClassicStack-web make spa
~~~

Full resolution-order detail: [`third_party/README.md`](../third_party/README.md).

## Using the UI

See [manual.md §4](manual.md#4-web-ui-including-finder) for the operator's tour of the
admin windows (Status, Settings, Sharing, Topology, Logs, MacIP leases) and the Finder
browsing experience, and [config.md](config.md#http--web-admin-ui) for the
`[http]`/`[Client]`/`[FUSE]` config keys.
