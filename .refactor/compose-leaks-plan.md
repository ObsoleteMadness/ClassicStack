# Plan — declarative deps + transports.go config-emission + control.go leak fix

Three related concerns about the composition root reaching across component
boundaries. Severity and fix differ per concern; they're independent and can land as
separate commits. All follow the EXISTING optional-capability pattern in
`core/component/component.go` (Enableable/Bindable/Describable/Attachable…) — the runtime
type-asserts a capability rather than hardcoding per-component knowledge.

---

## Concern A — `hardDeps` is a centralized static map (runtime.go:54-74)

**Verdict: real, already flagged in-code as a deferred follow-on.** Each component's
start-order edges are declared in a static map in the root, not by the component, and the
"list every possible edge then filter to built-both-ends" approach can't vary cleanly by
configuration.

**Fix — `component.DependsOn` capability:**

1. `core/component/component.go`: add
   ```go
   // DependsOn lets a component declare the names that must be RUNNING before it starts
   // (and stop after it). Optional: a component with no edges omits it. The result may
   // depend on how the component was configured (e.g. SMB lists "NetBEUI" only when its
   // NetBEUI transport binding is on), so it is a method on the constructed component,
   // not static metadata. The runtime filters to edges whose target was also built.
   type DependsOn interface{ Dependencies() []string }
   ```
2. `compose/runtime/runtime.go`: `builtDeps(name)` (line ~361) consults the BUILT
   component (`comps[name].(component.DependsOn)`) first; fall back to `hardDeps[name]`
   for components that don't implement it. Keep the built-both-ends filter.
3. Give each service with edges a `Dependencies()` method, config-aware where it matters:
   - `afp`: `{"Router"}` (always)
   - `rtmp`/`zip`/`nbp`/`aep`: `{"Router"}`
   - `macip`: `{"Router","NBP"}`; `ipxgw`: `{"Router","NBP"}`
   - `smb`: `{"NetBEUI"}` ONLY when the SMB server section binds the NetBEUI transport
     (today the edge is unconditional + filtered) — this is the "varies by config" win
   - `smbtcp`: `{"SMB"}`
   The component returns dep NAMES (strings already used as component names); no new
   import of the dependency package is required (names are consts the component already
   knows or can be string literals matching the registry name).
4. Once every edged component implements it, `hardDeps` shrinks to empty / is deleted.
   Land step 1-2 with `hardDeps` as fallback FIRST (zero behaviour change), then move
   edges into components incrementally.

**Files:** `core/component/component.go`, `compose/runtime/runtime.go`, +`Dependencies()`
on afp/smb/smbtcp/macip/ipxgw/rtmp/zip/nbp/aep services.

**Test:** a fake component implementing DependsOn drives `builtDeps`; an SMB section with
NetBEUI binding off yields no NetBEUI edge (config-varying). Supervisor start-order
unchanged for the existing set.

---

## Concern B — transports.go does config-driven wiring that components should emit

**Verdict: partly real, but more nuanced than A.** transports.go is the legitimate
composition root for CROSS-package bridges (smbSessionBridge, smbBrowseBridge — two
structurally-identical interfaces in packages that must not import each other; the root is
the only correct home for these). That part stays.

What DOESN'T belong here is the **config interrogation** the root currently does on each
component's behalf — it reads `smb.ServerSectionFromModel(m)` / `netbios.SectionFromModel(m)`
and calls `.Binds(transport)` to decide which transports to wire. The component already
holds its section; it should EMIT which transports it wants bound, rather than the root
re-deriving it from the model. Same for MacIP egress params: the root calls
`sec.EgressParams()` and `sec.Enabled` — the component should expose "do I want egress, and
with what params."

**Fix — components emit their transport/wiring intent:**

1. Add a capability for transport-binding intent (NetBIOS-family + SMB):
   ```go
   // TransportBinder lets a service declare which named transports it wants bound, so the
   // compose root wires only those without re-reading the service's config section.
   // Returns the lower-cased transport family names (e.g. "ipx","netbeui","nbt","tcp").
   type TransportBinder interface{ BoundTransports() []string }
   ```
   `smb.Service` and `netbios.Service` implement it from their already-held section, so
   transports.go asks the COMPONENT (`sm.BoundTransports()`) instead of
   `smb.ServerSectionFromModel(m).Binds(...)`. This removes `*config.Model` interrogation
   for transport decisions from the root.
2. MacIP egress: add to the macip service (or a small capability) a method exposing its
   egress intent + params from its own section, so `wireMacIP` asks the service rather
   than calling `macip.SectionFromModel(m)` + `sec.EgressParams()` in the root. The
   pcap/cgo egress OPENER stays injected at the cmd edge (that seam is already correct);
   only the "should I, and with what params" moves into the component.
3. The mini-router construction (NewRouter, AddPort, RegisterSocket) and the cross-package
   bridges STAY in transports.go — those are genuinely composition concerns (they wire two
   packages together and own objects with no component lifecycle). Do NOT move those.

**Scope caution:** this is the largest of the three and risks over-reach. Recommend
landing it AFTER A, and only the config-interrogation part (B1+B2). The bridge/mini-router
wiring is correctly placed and should not move.

**Files:** `core/component/component.go` (+TransportBinder), `core/service/smb`,
`core/service/netbios` (+BoundTransports), `core/service/macip` (egress-intent accessor),
`compose/runtime/transports.go` (ask components, drop model interrogation).

**Test:** SMB/NetBIOS BoundTransports reflect their section bindings; transports.go wires
the same set as today given the same config (no behaviour change, just relocated source
of truth).

---

## Concern C — core/control/control.go leaks NetBIOS + NBPName (real leak)

**Verdict: real, and the worst of the three because it's a CORE package.**
`core/control` is the transport-agnostic management contract. It currently:
  - declares `Diagnostics.RegisteredNames() ([]NBPName, error)` + the `NBPName` DTO
    (AppleTalk-NBP-specific) — control.go:157-176, 438
  - hardcodes `netbiosComponentName = "NetBIOS"` and a `netbiosEnabled()` helper that
    string-matches the component in Status() to gate hostname validation — control.go:329-342
  - threads `config.ValidateOptions{NetBIOSEnabled: ...}` — control.go:301

The NBPName one is a genuine abstraction leak: a protocol-specific DTO in the neutral
control contract. The NetBIOS-gate is subtler — it's already string-matched (no import),
WITH a comment admitting it's a workaround "to keep core/control free of service
dependencies." But it still encodes service-specific knowledge (the rule that NetBIOS
gates a hostname constraint) in core/control.

**Fix:**

C1 — **NBPName / RegisteredNames:** these are AppleTalk diagnostics. Two options:
   - (preferred) Keep `Diagnostics` generic: replace the typed `NBPName`/`MacIPLease`
     DTOs with a neutral, protocol-agnostic shape the diagnostics IMPL fills — e.g. a
     generic `DiagTable{ Columns []string; Rows [][]string }` or `[]map[string]string`,
     so control declares "a diagnostics probe returns rows" without knowing they're NBP
     names. The AppleTalk-specific decoding stays in the diagnostics impl (compose/cmd
     edge), not in core/control's type vocabulary.
   - (alt) Move the protocol-specific diagnostics surface OUT of core/control into an
     adapter-side extension, leaving core/control with only `ListZones` (already generic)
     or nothing protocol-named.
   Recommend the generic-table approach — smallest blast radius, keeps the existing
   plane/HTTP/ubus wiring, removes the protocol vocabulary from core.

C2 — **NetBIOS hostname-validation gate:** the rule "an over-length hostname is invalid
   WHEN NetBIOS is enabled" is a CONSUMER-GATED config rule. core/control shouldn't know
   it's NetBIOS. Generalize: the validation should ask the live components "does any of you
   impose a hostname constraint?" via a capability, rather than control string-matching
   "NetBIOS". Options:
   - Add `config.ValidateOptions` a generic set of active constraint sources the SUPERVISOR
     supplies (it already enumerates components), e.g. a `[]string` of constraint keys or a
     `HostnameConstraints` set; control passes whatever the supervisor reports without
     naming NetBIOS.
   - Or a `component.HostnameConstrainer` capability the supervisor aggregates, so the
     decision lives with the component that imposes it (NetBIOS), the supervisor collects
     it, and core/control just forwards the aggregate to Validate.
   Recommend the capability route (consistent with A/B): NetBIOS declares the constraint,
   the supervisor aggregates, control forwards — control loses the `netbiosComponentName`
   const and `netbiosEnabled()` entirely.

**Files:** `core/control/control.go` (drop NBPName/NetBIOS specifics), `core/config`
(generalize ValidateOptions if C2 via options), `core/component` (+HostnameConstrainer if
C2 via capability), the diagnostics impl + supervisor (fill the generic shapes), the HTTP/
ubus/inproc adapters (track the Diagnostics signature change — keep conformance green).

**Test:** Diagnostics returns generic rows; the NBP/MacIP front-end renders them the same.
Hostname validation still rejects an over-length name when NetBIOS is enabled, driven by
the capability/aggregate, with core/control naming no service.

---

## Concern C — REFINED (user direction 2026-06-26)

A and B are DONE. C splits:

**C2 (NetBIOS validation gate) — DOING NOW, clear leak.** Replace
`netbiosComponentName`/`netbiosEnabled()` + `ValidateOptions.NetBIOSEnabled` with the
`component.HostnameConstrainer` capability (already added): NetBIOS DECLARES the
constraint, the supervisor aggregates active constraints across the live component set,
the plane forwards the aggregate to `Model.Validate` WITHOUT naming any service.
- `core/component`: `HostnameConstrainer{ HostnameConstraint() (constraint string, active bool) }` (added).
- `core/service/netbios`: Service implements it → `("netbios", enabled)`.
- supervisor: aggregate — a method the plane calls, e.g. `HostnameConstraints() []string`
  (the active constraint keys across components implementing HostnameConstrainer).
- `core/config.ValidateOptions`: replace `NetBIOSEnabled bool` with
  `HostnameConstraints []string` (or a set); `Validate` applies the ≤15-byte rule when
  "netbios" is present. core/config already owns `Identity.ValidateForNetBIOS` — keep the
  rule there, gate it on the constraint key, not a bool named NetBIOS at the call site.
- `core/control`: `persist()` passes `ValidateOptions{HostnameConstraints: p.sup.HostnameConstraints()}`;
  drop the const + helper. control names no service.

**C1 (diagnostics DTOs) — per user: move diagnostics INTO the protocols.** Each
protocol/service exposes its OWN Diag model + a small read interface; core/control stops
declaring `NBPName`/`MacIPLease` and the protocol-specific `Diagnostics` methods. Not
every service has one.
- Each service owns its diagnostic view + getter:
  - `nbp`: a `nbp.NameTableEntry` (decoded display strings: object/type/zone/socket) +
    `Service` method returning `[]NameTableEntry` (or a `nbp.Diagnostician` interface).
  - `macip`: a `macip.LeaseView` (ip string + at-net/node + source) + getter.
- `core/control.Diagnostics`: keep ONLY genuinely-neutral probes. `ListZones` is
  AppleTalk-specific too, but it returns `[]string` (no protocol DTO), so it can stay as a
  generic "zones" probe OR also move — decide during impl; minimum is removing NBPName/
  MacIPLease + RegisteredNames/MacIPLeases from the core interface.
  Option: core/control keeps a small registry of named diagnostic probes
  (`map[string]func(ctx)(any,error)` is reflection-y — avoid); better, core/control
  exposes a slim `Diagnostics` with only neutral methods, and the protocol-specific
  drill-downs are reached through the SERVICE's own interface, surfaced by the adapters
  that already special-case those routes (HTTP /registered_names, /macip_leases) by
  type-asserting the service from the supervisor's component set.
- `compose/diag`: shrinks — it no longer maps service rows into `control.NBPName`/
  `control.MacIPLease`; the decoding lives in the service's own diag getter, and the
  HTTP/ubus handlers call the service interface (resolved from the supervisor) for those
  two routes.
- Adapters (HTTP/ubus/inproc): the two protocol routes move from
  `plane.Diagnostics().RegisteredNames/MacIPLeases` to a per-protocol diagnostics
  accessor; conformance assertions for the GENERIC Diagnostics stay green, the two
  protocol probes become service-typed. Keep all three adapters in lockstep.
- The exact seam for "adapter reaches the service's diag" needs care: the adapters today
  only hold `control.Plane`. Add a neutral accessor on the plane/supervisor to fetch a
  named diagnostic probe by capability (a service implementing `nbp.Diagnostician` is
  found in the component set), WITHOUT core/control importing nbp/macip — the adapter (in
  adapter/ ring) may import the service packages, so the type-assertion lives there.

C1 risk: medium-high (3 adapters + conformance + the diag wiring). C2: low. Do C2 first.

## C1 — REVISED AGAIN (user direction): a dedicated diagnostics ADAPTER package

Drop the generic-capability-in-core approach (the `component.Diagnosable` +
supervisor `ListDiagProbes/RunDiag` + generic `control.DiagProbe/DiagResult` churn).
Instead: a NEW `adapter/control/diag` package (adapter ring, build-tag gated) that
IMPORTS the service packages directly and bridges them to the web/ubus front-ends. The
adapter layer is allowed to know NBP/MacIP/etc.; core/control carries NOTHING diagnostic.

**Revert (the C1-only pieces of the in-flight work; KEEP A, B, C2):**
- `core/component/component.go`: remove `Diagnosable`, `DiagProbe`, `DiagResult`,
  `ErrNoSuchProbe`. KEEP `DependsOn`, `TransportBinder`, `HostnameConstrainer`.
- `compose/supervisor/supervisor.go`: remove `ListDiagProbes`/`RunDiag`. KEEP
  `HostnameConstraints`.
- `core/service/nbp`, `core/service/macip`: remove the `DiagProbes`/`RunDiag` methods +
  the `_ component.Diagnosable` assertions + the `DiagProbe*` consts + the sort/strconv
  imports they added. KEEP their pre-existing typed getters `Names() []RegisteredName`
  and `Leases() []LeaseInfo` (the adapter calls these).

**New end-state:**
- `core/control.Diagnostics` shrinks to ONLY `ListZones(ctx) ([]string, error)` (router
  zones — neutral, returns []string, no protocol DTO). Remove `NBPName`, `MacIPLease`,
  `RegisteredNames`, `MacIPLeases`, and the generic `DiagProbe/DiagResult/ListDiagProbes/
  RunDiag` that the in-flight work added. The `control.Client`/AdapterClient surface and
  the inproc `Client` interface lose the registered-names/macip-leases methods (and do NOT
  gain the generic ones) — keep only `ListZones`. Conformance shrinks accordingly.
- NEW `adapter/control/diag` (build tag e.g. `afp || smb || ncp || all`, or its own —
  match what pulls nbp/macip): a `Provider` that takes the runtime (or supervisor +
  router) and resolves services from the live component set:
    rt.Component(nbp.Name).(*nbp.Service) -> decode Names() to typed rows here
    rt.Component(macip.Name).(*macip.Service) -> decode Leases() here
  It exposes typed accessors (RegisteredNames, MacIPLeases) returning DTOs OWNED BY THIS
  PACKAGE (or by the services). The decode (bytes->string, IPv4->string) lives here/at the
  service, NOT in core.
- The web front-end (`adapter/control/http`): the `/registered_names` + `/macip_leases`
  routes are served by THIS package's provider (the http.Server gains an optional diag
  provider field set at the cmd edge), NOT via control.Plane.Diagnostics(). ubus likewise
  if it carries those methods. ListZones stays on control.Plane (it is router-sourced and
  neutral) OR also moves — simplest: ListZones stays (it's a []string, no leak).
- `compose/diag`: retire (its router ListZones moves back to a tiny core/control-wired
  impl, or stays as the ListZones-only impl). The NBP/MacIP shims in cmd/internal/cli/
  diag.go are removed.
- Wiring: the cmd edge (cmd/internal/cli) builds the diag provider over the runtime and
  hands it to the http/ubus servers — the same place that injects pcap openers etc.

This mirrors compose/runtime/transports.go (the write-side composition layer that imports
every service); the diag adapter is its read-only sibling. Build-tag gating is natural
(the package imports nbp/macip only under their tags). core/control names no protocol and
the SPA's existing /registered_names + /macip_leases routes keep working (served by the
new provider) — minimal SPA churn, unlike the earlier generic-probe route plan.

Open point to confirm while building: does ListZones stay on control.Plane.Diagnostics
(neutral []string — recommended, least churn) or also move to the diag adapter? Default:
stays.

## Sequencing & risk

1. **A** (declarative deps) — cleanest, matches an in-code TODO, smallest. Land first with
   `hardDeps` fallback (zero behaviour change), then migrate edges into components.
2. **C** (control.go leak) — core-purity fix; C1 (NBPName) is mechanical, C2 (NetBIOS gate)
   needs the supervisor/adapter touch. Medium.
3. **B** (transports.go config emission) — largest, highest over-reach risk; do the
   config-interrogation relocation only, leave bridges/mini-routers in place. Last.

Each is independently shippable and behaviour-preserving. Verify per the standard gate:
`go build -tags all ./...` + headless, `go test -tags all ./core/... ./compose/...`,
archtest green, control-adapter conformance (HTTP/ubus/inproc) green, gofmt/vet.
