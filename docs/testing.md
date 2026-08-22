---
title: "Testing"
weight: 7
---

# Testing

ClassicStack is tested at two layers: a fast in-process Go harness that runs in CI on
every push, and a set of native period-correct client applications that drive a real
(emulated) vintage OS against a real ClassicStack server. The second layer exists
because a Go test can assert wire correctness, but it cannot prove that Windows 3.11's
actual redirector, or a real 68k Mac ROM, is happy with what we sent.

## In-process protocol × transport matrix (`go test`)

`test/e2e` is the consolidated end-to-end gate for the client SDK: for every
protocol × transport combination it stands up a **real** in-process ClassicStack server,
connects a **real** client, and runs the same file-operation battery (create a file with
a data fork + resource fork + Finder type/creator → list → copy out → copy back →
rename → delete → directory create/delete) through it. A single failing subtest names
exactly which protocol × transport and which operation broke.

Covered combinations (`test/e2e/e2e_test.go`):

| Case | What it exercises |
|---|---|
| `afp/ddp` | AFP over ASP/DDP (models AFP over LToUDP and EtherTalk — DDP payload is transport-agnostic) |
| `smb/direct` | The message-level SMB command core (direct-hosted family) |
| `smb/tcp` | Real client TCP/NBT framing over a `net.Pipe` |
| `smb/nbipx` | Real IPX port + NBIPX session engine over an in-memory link pair |
| `smb/nbf` | Real NetBEUI port + LLC2 responder + NBF session engine over an in-memory link pair |
| `ncp/ipx` | NCP over an IPX-datagram bridge |
| `etherdfs/eth` | EtherDFS over a raw-Ethernet in-memory link pair |

Run it with everything else:

~~~bash
go test -tags all ./...
# or just this package:
go test -tags all ./test/e2e/...
~~~

Live raw-Ethernet transports on a real segment and the WinFsp drive mount need a
physical NIC, Npcap, two L2 stations, and (for the mount) the WinFsp kernel driver —
none of which a unit test can provide in CI. Those are covered by the `driverint`
build-tagged Windows tests in the same package (`driver_live_windows_test.go`,
`driver_mount_windows_test.go`, `driver_segment_windows_test.go`), which are excluded
from the normal `-tags all` run and only compile under `windows && driverint`:

~~~powershell
go test -tags "driverint pcap" -run TestDriver ./test/e2e/ -v
~~~

Per-protocol focused tests also live alongside each client package
(`client/*/e2e_test.go`) — `test/e2e` is the cross-cutting peer that proves every
protocol × transport combination through one shared harness.

## Native end-to-end tools (`tools/end-to-end/`)

These are real client applications for real (or accurately emulated) operating
systems, each driving ClassicStack over the actual OS network stack rather than a Go
client. They run under an emulator (86Box for DOS/Windows/OS2, Mini vMac / "Snow" for
classic Mac OS) against a live ClassicStack instance, and every tool writes results in
the same shared format so one harness can parse all of them.

| Platform | Location | Protocol | Approach |
|---|---|---|---|
| Classic Mac OS (68k) | `tools/end-to-end/macos` | AFP | A native System 7.1 app (built with [Retro68](https://github.com/autc04/Retro68)) drives the real AppleShare client, so every operation traverses AppleShare → AFP → ClassicStack. |
| Windows 3.1 / WfW 3.11 (Win16) | `tools/end-to-end/windows/win16` | SMB | A 16-bit NE app (MSVC 1.5) drives the real Windows network redirector (WNet API + `_dos_findfirst`/`fopen`), so every op traverses the MS redirector → SMB → ClassicStack. |
| Windows NT/95/98 (Win32) | `tools/end-to-end/windows/win32` | SMB | A 32-bit PE app (MSVC 1.2 for NT) drives the same redirector via `WNetOpenEnum`/`FindFirstFile`, plus long-file-name tests. |
| DOS | `tools/end-to-end/dos` | SMB, NCP, EtherDFS | Plain batch files (`net`/`login`+`map`/`etherdfs`) — DOS has no interpreter of ours, so native commands do the work and their output is redirected into the result files. |
| OS/2 | `tools/end-to-end/os2` | SMB | Placeholder — planned REXX/batch script plus a WPS shell exercise. |

Both compiled tools (macOS, Windows) share one design: a portable command-parser/
result-writer core, a plain-text `script.txt` of commands to run, and a `results.txt`
of `PASS`/`FAIL`/`DEBUG` lines plus a final `DONE total=<n> pass=<n> fail=<n>` summary.
The DOS batch suite hand-emits the same `RESULT v1` lines so one fixture-diffing harness
parses every platform's output identically — see
[`tools/end-to-end/RESULT-FORMAT.md`](../tools/end-to-end/RESULT-FORMAT.md), the shared
contract every tool (including future ones) must honour.

### Running a native tool

The general shape (see each tool's own readme for exact commands):

1. Build the tool for its target (cross-toolchain instructions are in each
   `readme.md` — e.g. the Windows tree builds natively on a Windows 11 host with
   period MSVC compilers, no emulator needed for the build step itself).
2. Pack the compiled binary plus its `script.txt` onto a floppy image
   (`tools/end-to-end/tools/flopgen.exe` for Windows/DOS).
3. Boot the target OS under emulation (86Box for DOS/Windows, Mini vMac/Snow for
   classic Mac OS) with networking bridged to a host running ClassicStack, configured
   with a share matching the script (commonly `\\CLASSICSTACK\Foo` / `Foo` volume).
4. Run the tool from the floppy; it writes `results.txt` back to the same floppy.
5. Shut the guest down, read `results.txt` off the image, and diff it against the
   expected fixture.

Automating the emulator launch + result extraction + fixture diff into one harness is a
planned follow-up, shared across platforms.

## Adding a test

- A new protocol × transport combination for the file-operation battery: add a case to
  `test/e2e/e2e_test.go` and a server builder in `test/e2e/servers_test.go`.
- A new native OS/protocol combination: follow the `RESULT-FORMAT.md` contract so it
  plugs into the same eventual harness, and share the command-parser/result-writer core
  with the closest existing tool rather than inventing a new one.
