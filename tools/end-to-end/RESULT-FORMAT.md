# End-to-End Result Format (v1)

This is the shared, cross-platform contract for the `results.txt` file produced
by every ClassicStack end-to-end test tool (macOS/AFP under `macos/`, Win16/Win32
SMB under `windows/`, and future DOS/OS2 tools). Each tool reads a `script.txt`
of commands and appends result lines here as it runs; a harness diffs these
files against expected fixtures. Keeping the format identical across operating
systems is the whole point of the split-core design — the platform- and
protocol-specific bits live behind seams, but the output every tool emits is the
same so one harness parses them all.

The reference implementation of the writer is `results.c` (shared verbatim
between the macOS and Windows trees) and the directory-entry formatter is
`SmbFormatEntry` in `windows/src/smb_common.c`; the macOS tool emits the same
entry fields from its AFP enumerator. When you change this spec, change both.

## File shape

Plain text, one record per line. Line endings are whatever the platform writes
(`\r` classic Mac, `\r\n` DOS/Windows); the harness must accept `\r`, `\n`, and
`\r\n`. UTF-8/ASCII bytes; no BOM required. The file is flushed after every line
so a mid-run crash still leaves every completed line readable.

A run is exactly:

```
RESULT v1 started="YYYY-MM-DD HH:MM:SS"
<zero or more DEBUG / PASS / FAIL lines, in execution order>
DONE total=<n> pass=<n> fail=<n>
```

### Header line

```
RESULT v1 started="2026-07-20 14:03:11"
```

- `v1` is the format version (this document). Bump it only on a breaking change.
- `started` is the local wall-clock time the run began, quoted,
  `YYYY-MM-DD HH:MM:SS`.

### DONE line

```
DONE total=12 pass=11 fail=1
```

Emitted once, last. `total` counts every PASS+FAIL line (not DEBUG). A run that
reached `DONE` completed; its absence means the tool crashed or hung mid-script.

## Record lines

Every non-header, non-DONE line is one of three kinds:

| Kind    | Shape                              | Counts toward total? |
|---------|------------------------------------|----------------------|
| `PASS`  | `PASS <Command> [detail...]`       | yes                  |
| `FAIL`  | `FAIL <Command> [detail...]`       | yes                  |
| `DEBUG` | `DEBUG <free text>`                | no                   |

`<Command>` is the script command name that produced the line (e.g. `Mount`,
`EnumerateVolume`). The remaining `detail` is a space-separated list of
`key=value` or `key="quoted value"` pairs — free-form per command, but values
containing spaces MUST be quoted so the harness can tokenize with the same
`key=value`/`key="..."` rules the script parser uses. Common keys:

- `err=<n>` — a platform/OS error code on failure.
- `detail="..."` — a human-readable explanation.
- `name=`, `old=`, `new=`, `to=`, `toDir=`, `path=` — operands echoed back.
- `bytes=<n> verified=1` — a write that was read back and byte-compared.
- `supported=0` — a command that isn't available on this platform/build but ran
  cleanly (reported as PASS, not FAIL — e.g. share enumeration on Win16, or the
  MacIPX placeholders on classic Mac). `supported=1` when it did run for real.

### DEBUG conventions

- `DEBUG <tool> starting` — first line after the header.
- `DEBUG env: platform=<banner>` — records the client build (e.g.
  `win32 LFN=1`, `win16 LFN=0`, or the AppleTalk driver version on Mac).
- `DEBUG line <raw script line>` — each dispatched script line, for a trail.

## Standardized directory entry

Directory/volume enumeration (`EnumerateVolume`, `EnumerateDir`) and single-item
stat (`StatFile` on Windows) emit **one PASS line per entry**, followed by a
summary PASS line with the counts. Every entry line uses this exact field set
and order so entries diff cleanly regardless of which OS produced them:

```
PASS EnumerateVolume kind=<file|dir> name="<name>" short="<8.3 name>" attrs=<RHSA> created="<ts>" modified="<ts>" accessed="<ts>" size=<bytes>
```

Fields:

| Field      | Meaning                                                                 |
|------------|-------------------------------------------------------------------------|
| `kind`     | `file` or `dir`.                                                        |
| `name`     | The full name. Long file name on Win32; the plain name on Win16/Mac.    |
| `short`    | The 8.3 short/alternate name. Equals `name` when there is no distinct short name. |
| `attrs`    | Fixed 4-char field `RHSA`: **R**eadonly, **H**idden, **S**ystem, **A**rchive; a dash `-` where the bit is clear (e.g. `--A-`). |
| `created`  | Creation time `YYYY-MM-DD HH:MM:SS`, or `-` if the platform can't supply it. |
| `modified` | Last-modified time, same format.                                       |
| `accessed` | Last-access time, or `-` if unavailable.                               |
| `size`     | File size in bytes (decimal). Directories report their own reported size (often 0). |

The summary line that follows the entries:

```
PASS EnumerateVolume entries=<n> files=<n> dirs=<n> path="<path>"
```

### Per-platform field availability

The format is identical everywhere; what differs is which fields a platform can
populate. Unavailable timestamps are emitted as `-`, never omitted, so the
column layout is stable:

| Field      | Win32 (FindFirstFile) | Win16 (DOS _dos_findfirst) | macOS (AFP/CInfoPBRec) |
|------------|-----------------------|----------------------------|------------------------|
| `name`     | long file name        | 8.3 name                   | HFS name (up to 31)    |
| `short`    | `cAlternateFileName`  | same as `name`             | same as `name`         |
| `attrs`    | full R/H/S/A          | full R/H/S/A               | mapped from Finder/lock flags |
| `created`  | yes                   | `-` (DOS has none)         | yes                    |
| `modified` | yes                   | DOS write date/time        | yes                    |
| `accessed` | yes                   | `-` (DOS has none)         | typically `-`          |
| `size`     | 64-bit                | 32-bit (`long`, ≤2 GiB)    | data-fork length       |

A harness comparing across platforms should therefore assert on `kind`, `name`,
`attrs`, and `size`, and treat `-` timestamps as "not provided by this client"
rather than a mismatch.
