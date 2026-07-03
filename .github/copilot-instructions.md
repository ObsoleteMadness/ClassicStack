
ClassicStack **bridges contemporary systems and legacy systems** by implementing legacy
network protocols and file/print services in **modern, self-contained code** — with **no
dependency on legacy kernel extensions or OS services** (no `AF_APPLETALK` stack, no Windows
Services-for-Macintosh, no kernel IPX). We meet users where they are — Windows, macOS, Linux,
or embedded — and serve as many legacy clients as possible (Classic Mac OS, DOS, early
Windows). The four pillars below drive every design decision in this document.

**Cross-platform — first-class on each target.**
- *Desktop* (Windows / macOS / Linux): behaves natively — Windows Service, launchd, systemd.
- *OpenWRT*: lives in the ecosystem — UCI configuration, ubus control, procd integration.
- *Embedded* (primarily ESP32 via **TinyGo**): the same core runs on microcontrollers.
  TinyGo is also a **size lever on desktop** — e.g. a TinyGo build inside an Alpine image to
  keep a Linux container tiny.

**Flexible.**
- Components are **selectable at build time** so binaries stay small (only what you ship is
  linked).
- Ports and services **start, stop, and reconfigure at runtime** (§11).
- **No leaky abstractions** — adapters absorb each environment's capabilities (§1).

**Small.** Clear separation of concerns; prefer the smaller implementation; delete code.

**Adaptable.** **No hard-coded assumptions about the physical interface.** A port works over
pcap, TAP, PPP/SLIP, a kernel datagram socket, or an in-memory pipe **without rewriting the
port** — the link is an adapter (§2).

### Compatibility over correctness (a deliberate stance)

We target long-obsolete systems that predate modern best practice (no/weak encryption, no
bounds discipline, quirky clients). Our goal is **compatibility — including bug-for-bug
compatibility with real clients — not abstract spec correctness.** Concretely:

- **Observed client behaviour outranks the spec.** Where a real Classic Mac / DOS client
  disagrees with the written protocol, we match the client and **document the deviation**
  (per CLAUDE.md: code comment + `spec/errata.md`).
- **Quirk handling is a feature, not debt.** Working around known client bugs is expected.
- **Where we have no spec, the wire is the spec** — observed framing/commands/responses are
  documented and become the contract (e.g. MacIPX from captures).

### Security posture — host-side modern, wire-side faithful

The governing split: **apply modern good practice to everything on *our* side of the bridge,
and faithfully speak the legacy client's insecure dialect on the *wire* side.** Insecurity is
scoped to the wire-facing edge by necessity — it is never leaked inward to the host.

**Host side (we hold ourselves to current practice):**
- **Robust, safe Go**: no memory-unsafety, careful parsing of hostile input, fail closed on
  our own bugs, sanitise everything that crosses into the host (paths, names, sizes).
- **Credentials at rest are protected with modern primitives** even when the protocol that
  *uses* them is weak — e.g. a user store keeps salted-hashed passwords, regardless of the
  fact that the legacy auth handshake will compare them against a cleartext or
  weakly-hashed value off the wire.

**Wire side (compatibility is the requirement, weakness is intentional):**
- We **implement the legacy protocol's auth and crypto exactly as the client expects** —
  cleartext passwords, obsolete ciphers, broken hashes — because that is the only thing the
  client can speak. Refusing defeats the project's purpose.
- **Deliberately-obsolete cryptography is a feature, not a defect.** Example: a future HTTP
  proxy may MITM and **re-encrypt with SSL 3.0 and dead ciphers** so a Netscape 2.0 client
  can connect over "SSL." A modern browser will (correctly) reject it; the legacy client
  works. A security scanner flagging the SSL 3.0 code path **must not "fix" it** — it is
  there on purpose, behind a feature/build flag, and annotated as intentional.


## Remember!
1. Always confirm implementation details with the specifications found in /spec/*.md
2. Use consts rather than hard-coded values, especially for responses, errors, etc. 
3. Use the names from the specification for functions, consts, etc and include a comment with a breif description from the spec for any functions.
4. Captures of protocols can be found in /captures. Use `tshark` to review protocol captures to aid in diagnosing faults. 
5. When the observation from a capture differs from the spec, document it in the code and in `/spec/errata.md`
6. Where we do not have a spec and implementation is from observation, add details on wire format, observed commands, observed responses. Eg, the MacIPX Gateway implementation will be based on observed IPX encapsulation over AppleTalk traffic between a Novell Server and a Macintosh MacIPX client.
7. If code is from 3rd parties, **Always** attribute it to the original authors. 
8. Check for linting errors before committing.
9. Run gofmt before commiting.
10. Every function, const must have a comment
11. every code file should have a comment