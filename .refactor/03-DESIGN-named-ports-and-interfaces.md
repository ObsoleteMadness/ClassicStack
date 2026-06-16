# Design: Named Port Instances over an Interface Namespace

> Status: **proposed** (design agreed, not yet implemented). Extends
> [00-DESIGN.md](00-DESIGN.md) §2 (the link adapter / `FrameLink` seam) and §4/§9d
> (the shared Bridge). Supersedes the singleton-port + single-`Model.Bridge` shape
> that M3–M10 built on.

## 1. Motivation

Today each transport is a **singleton**: one `[EtherTalk]`, one `[LToUDP]`, one
`[TashTalk]`, one `[IPX]` section, each building exactly one component under a
fixed key. That cannot express the configurations a real router serves:

- **Multiple TashTalk dongles**, each on its own serial port, each bridging a
  *different* physical LocalTalk segment into the router.
- **Multiple EtherTalk interfaces**, each bound to a different NIC, each its own
  AppleTalk network, all joined to the one AppleTalk router.
- The same for **IPX** — several interfaces, each its own segment — except IPX
  ports join the **IPX router**, not the AppleTalk router.

The unifying observation (from the design steer): **a serial port is an interface
too — just not a *network* interface.** "A port instance, with a name, bound to an
interface, that is a member of a router" is the general concept. The interface may
be a NIC, a serial device, a named bridge, or nothing (a multicast/tunnel
segment). Which router it joins is a property of the port *type*, not the
instance.

This is squarely the **Adaptable** pillar — "no hard-coded assumptions about the
physical interface" — taken to its conclusion: not only is the link an adapter,
but *which* link and *how many* are config, and the interface a port binds to is a
first-class named entity.

## 2. The two structural changes

### 2a. Ports become named, repeated instances

A transport section is no longer a singleton in `Model.Sections`; it is a
**repeated (named-instance) section** in `Model.Lists`, exactly like AFP volumes
and SMB shares already are. The machinery exists today:
`config.NamedSection` (adds `InstanceName()`), `Model.Lists`,
`Model.AddInstance`, and the codec's TOML array-of-tables / UCI repeated-block
round-trip.

```toml
[[EtherTalk]]
name = "et-lab"            # InstanceName(): the router member + UI/control handle
iface = "eth0"            # references an interface by NAME (see §3)
seed_network = 10
seed_zone = "Lab"

[[EtherTalk]]
name = "et-dmz"
iface = "eth1"
seed_network = 20

[[TashTalk]]
name = "tt-printer"
iface = "ttyS-printer"    # a SERIAL interface, by name
seed_network = 30

[[TashTalk]]
name = "tt-attic"
iface = "ttyUSB-attic"

[[IPX]]
name = "ipx-lan"
iface = "eth0"            # IPX member — joins the IPX router, not AppleTalk
```

`port.Section` gains an instance `name` and implements `NamedSection`; `Key()`
keeps returning the shared schema key (`"EtherTalk"`), `InstanceName()` returns
the per-instance name. The runport already derives `Component.Name()` — today from
`sec.SKey`; it will derive from the instance name so each instance is an
independently addressable component (start/stop/restart/stats per instance).

### 2b. Interfaces become a named namespace (bridges included)

Today there is one global `Model.Bridge InterfaceSection`, and
`EffectiveInterface` folds it in as the inherited default. The steer:

> we also have the concept of a shared bridge. the bridge could have a name too
> and that's the iface referenced?

So generalise: there is a **namespace of named interfaces**, and a port's `iface`
field references one *by name*. Members of the namespace:

- **NIC** — a physical/virtual network interface (`eth0`). pcap/tap/rawsock/etc.
- **Serial** — a UART/serial device (`COM3`, `/dev/ttyUSB0`). tashtalk/ppp/slip.
- **Bridge** — a *named* virtual interface aggregating one or more NICs (the
  current shared Bridge, now one named entry among possibly several).
- **(none)** — multicast/tunnel segments (LToUDP) that bind no host interface;
  `iface` is then a transport-specific address (the IPv4 bind addr), not a
  namespace reference.

```toml
[[Interface]]
name = "br-lan"
kind = "bridge"
members = ["eth0", "eth1"]   # bridge-specific

[[Interface]]
name = "ttyUSB-attic"
kind = "serial"
device = "/dev/ttyUSB0"
baud = 1000000

[[Interface]]
name = "eth0"
kind = "nic"
```

A port's `iface = "br-lan"` then means "bind to the interface named br-lan",
whatever kind it is. The current bridge-inheritance behaviour (empty iface →
inherit the global bridge) is re-expressed as: a port with no `iface` inherits a
configured **default interface** (which may be a bridge); a port that names one
overrides. `EffectiveInterface` keeps that resolution but resolves against the
namespace rather than a single `Model.Bridge`.

### 2c. Interface kind is explicit (not inferred)

Decision (agreed): the interface carries an explicit **`kind`** field
(`nic | serial | bridge`, with multicast/none implied by a port that references no
interface) rather than inferring it from the port type. Rationale: a port type no
longer implies a single medium (EtherTalk could in principle run over a bridge or
a raw NIC; the namespace entry is the authority), and an explicit kind lets the
compose layer pick the right **opener** (pcap vs serial vs rawsock) from the
*interface*, not the port. It also future-proofs new transports.

This means the per-port `Bindable`/`InterfaceProvider` story shifts: the port
declares *which interface name* it wants; the **interface** declares its kind and
the parameters an opener needs (device path, baud, bridge members). The opener
selection moves from "the EtherTalk factory always uses pcap" to "look up the
named interface, dispatch on its kind."

## 3. How this lands on the existing seams

### 3a. `FrameLink` is unchanged — it is already the right abstraction

Nothing about `core/link.FrameLink` (`Read`/`Write`/`Close`) changes. The backends
already exist as `adapter/link/*` packages presenting it (pcap real; ltoudp,
tashtalk real; tap/ppp/slip/driversnet stubs). What changes is **who picks which
opener**: instead of the EtherTalk factory hard-wiring pcap and the LToUDP/TashTalk
factories hard-wiring their adapter, an **interface-kind → opener** table maps a
resolved interface to the right `adapter/link/*` opener. The `LinkOpener` seam
(injected at the cmd edge, keeping cgo out of compose) stays; it grows from "the
pcap opener" into "the opener registry keyed by interface kind."

### 3b. Shared serial opener (the original UART question, now subsumed)

`tashtalk`, `ppp`, `slip` each open their own UART today. With interfaces named and
typed, a `kind = "serial"` interface owns the device parameters (device, baud,
parity) and a single `adapter/serial` opener returns the `io.ReadWriteCloser`;
`tashtalk`/`ppp`/`slip` become **framers over that byte stream** (each supplying
its escape rules), not device owners. This is the clean home for the
serial-opener split — it falls out of treating serial as an interface kind.

### 3c. Registry: one factory → N components

Today `registry.Build(name, ctx)` builds one component. Repeated ports need the
supervisor to enumerate instances (`Model.Lists[key]`) and build **one component
per instance**, addressed by instance name. Options to settle at implementation
time:

- a per-instance `Build(key, instanceName, ctx)`, or
- a factory that returns a *slice* of components for its key, or
- the supervisor iterating instances and calling the existing factory with the
  instance's section selected in the context.

The AFP-volume / SMB-share path already solved "N instances from one schema key"
at the *service* level; this applies the same pattern to *ports*.

### 3d. Router membership by instance name, per router type

Each port instance joins a router. Which one is a property of the port type:

- EtherTalk / LToUDP / TashTalk → the **AppleTalk router**.
- IPX → the **IPX router** (mini-router).
- NetBEUI → the **NetBEUI mini-router**.

Membership is by the instance's name. The existing router-port wiring (generic
today) keys on the component name, so multiple named instances slot in without the
router learning about transports.

## 4. Migration / compatibility

- The legacy `internal/app` config (`[LToUdp]`, `[TashTalk]`, `[EtherTalk]`
  singletons) and the legacy web UI use the old keys; this is the **new** stack's
  config and does not have to match byte-for-byte (00-DESIGN §"greenfield"). A
  one-instance array-of-tables is the natural upgrade of a singleton section.
- A singleton with no `name` can default its instance name to the schema key
  (`EtherTalk` → instance `"EtherTalk"`), so a minimal config still works and the
  conformance harness keeps a deterministic name.

## 5. Decisions captured

| # | Decision | Rationale |
|---|---|---|
| D1 | Ports are **named repeated instances** (`Model.Lists` + `NamedSection`) | Real routers have several drops per transport; machinery already exists (AFP/SMB). |
| D2 | Applies to **IPX** too, but IPX joins the **IPX router** | "Named port bound to an interface, member of a router" is general; the router differs by type. |
| D3 | Interfaces are a **named namespace**; a port's `iface` references one by name | Generalises the single `Model.Bridge`; a bridge is just one named interface. |
| D4 | A **bridge is a named interface** | Lets several bridges exist and ports reference any of them by name. |
| D5 | Interface **kind is explicit** (`nic`/`serial`/`bridge`) | Port type no longer implies one medium; the interface drives opener selection; future-proof. |
| D6 | `FrameLink` and the `LinkOpener` seam are **unchanged**; opener selection moves to an **interface-kind → opener** table | The abstraction is already correct; only the dispatch generalises. |
| D7 | Serial becomes an **interface kind** with a shared `adapter/serial` opener; tashtalk/ppp/slip are framers over the byte stream | Removes per-adapter `serial.Open` duplication; the right home for the UART split. |

## 6. Out of scope (here)

- Node-claim (LLAP ENQ/ACK on LocalTalk, AARP on EtherTalk) remains deferred as in
  M3/M10; named instances do not change that.
- The IPX/NetBEUI device-link injection (TODO follow-on (2)) should be implemented
  **on top of** this model (named IPX instances) rather than against the singleton
  shape, to avoid building something we immediately re-shape.
