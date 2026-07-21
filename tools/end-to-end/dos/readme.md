# DOS End-to-End Tests

Unlike the macOS (AFP) and Windows (SMB) trees, which are compiled C programs
that interpret a `script.txt`, the DOS tests are plain **batch files**. DOS has
no interpreter of ours, so native commands (`net`, `login`/`map`, `etherdfs`,
`dir`, `copy`, ...) do the work and their output is redirected to the result
files. The batch files hand-emit `RESULT v1` lines (see
[../RESULT-FORMAT.md](../RESULT-FORMAT.md)) so one harness still parses every
platform's output; the raw command text lands alongside in `OUT.TXT` for the
cases where a step's pass/fail can't be told from `errorlevel` alone.

Each client runs the same four phases as the compiled tools — **discovery →
mount → file/directory tasks → unmount** — and writes:

- `RESULTS.TXT` — the `RESULT v1` PASS/FAIL/DEBUG lines.
- `OUT.TXT` — raw output of every `net`/`dir`/`copy`/... command.

## Scripts (`scripts/`)

| File           | Redirector | Discovery → mount → unmount |
|----------------|------------|------------------------------|
| `MSCLIENT.BAT` | SMB (MS Network Client / Workgroup Connection) | `net view` / `net view \\srv` → `net use F: \\CLASSICSTACK\Foo` → `net use F: /delete` |
| `NETWARE.BAT`  | NCP (Novell NETX/VLM over IPX) | `slist` → `login CLASSICSTACK/GUEST` + `map F:=CLASSICSTACK/Foo:` → `map del` + `logout` |
| `ETHERDFS.BAT` | EtherDFS (raw-Ethernet TSR) | `etherdfs :: C-F` (auto-discovery + install) → `etherdfs /u` |
| `FILEOPS.BAT`  | *(shared)* | the create/write/stat/rename/move/copy/delete file+directory body every client `CALL`s |

`FILEOPS.BAT` is the DOS analogue of the file/directory section every compiled
tool runs; the three clients differ only in how they discover/mount/unmount, so
they each `CALL FILEOPS.BAT` for the identical middle. Before the call a client
sets the contract variables:

- `E2EDRV`  — the mapped drive to exercise, incl. colon (e.g. `F:`)
- `E2EHOME` — the local drive to return to afterwards (e.g. `C:`)
- `E2ETAG`  — a short label written into the test file's contents
- `RES` / `OUT` — the results and raw-output filenames

## Running

Copy the `scripts/` directory to a **writable local drive** on the DOS client
(e.g. `C:\E2E`) — never run from the network drive itself. Load the appropriate
stack first (SMB redirector, or IPXODI+NETX, or a packet driver for EtherDFS),
make sure the matching ClassicStack service is running, then:

```
C:
CD \E2E
MSCLIENT              rem or NETWARE, or ETHERDFS
```

All three take positional overrides (server / share|volume / drive, etc.) — see
the header comment in each `.BAT`.

For SMB, both **NetBEUI and IPX** transports should be exercised (bind one at a
time in `PROTOCOL.INI` / `SYSTEM.INI`); this overlaps with the Win16 SMB tests.

## DOS-safety notes

The batch files stick to real-mode `COMMAND.COM` syntax so they run on genuine
MS-DOS, not just NT `cmd.exe`: no `( ... ) else ( ... )` blocks (each check is a
`if exist`/`if not exist` pair), no `/Q` on `del` (a `*.*` delete answers its
confirmation prompt with `echo Y|`), no `goto :eof`, and CRLF line endings.
