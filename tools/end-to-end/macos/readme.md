# macOS End-to-End Tests

A native System 7.1 (68k) application, built with [Retro68](https://github.com/autc04/Retro68),
that exercises ClassicStack's AFP server end-to-end: server/zone discovery,
login, volume mount, directory enumeration, file and resource-fork I/O, and
teardown/shutdown — driven by a plain-text script file and reporting results
to a results file, both read/written next to the app itself (intended to live
on a floppy image alongside the app when run under an emulator).

See the top-level plan for the full command set and multi-platform design —
this app shares its script grammar and command-parser core design with the
planned Win16/Win32 SMB test tools under `tools/end-to-end/windows`.

## Status

Stage 2 (server discovery): the app opens `script.txt`, runs it through the
command parser, and supports `EnumerateServers` (NBP lookup for AFP servers)
and `GetServerInfo` (session-less `ASPGetStatus`/`FPGetSrvrInfo`). Results are
written to `results.txt` with a `RESULT v1` header (including a timestamp),
`PASS`/`FAIL` lines per command, and a `DONE` summary line. Later stages add
login/mount, directory enumeration, file/fork I/O, and shutdown.

## Building (WSL / Ubuntu, with Retro68 already built)

```sh
cd tools/end-to-end/macos
mkdir -p build && cd build
cmake .. -DCMAKE_TOOLCHAIN_FILE=$HOME/Retro68-build/toolchain/m68k-apple-macos/cmake/retro68.toolchain.cmake
make
```

This produces `AFPE2E.APPL` (and a raw `.bin`) plus a ready-to-run
`AFPE2E.dsk` disk image in `build/`. The build automatically copies
`scripts/basic.txt` onto `AFPE2E.dsk` as `script.txt` (via the `hmount`/
`hcopy`/`hattrib` tools that ship alongside the Retro68 cross-compiler) — no
manual disk-image assembly step is needed to smoke-test the app.

If `hmount`/`hcopy`/`hattrib` aren't found next to the compiler, CMake emits a
warning and skips this step; copy `scripts/basic.txt` onto the disk image by
hand in that case (e.g. `hmount build/AFPE2E.dsk && hcopy -t scripts/basic.txt
:script.txt && hattrib -t TEXT -c ttxt :script.txt && humount`).

### Diagnostics

All diagnostics (startup, file open/close outcomes, every script line as it's
read, command dispatch counts) are written as `DEBUG` lines straight into
`results.txt` — not to a console. An earlier version of this app used
`-DAFPE2E_CONSOLE=ON` to link [RetroConsole](https://github.com/autc04/Retro68/tree/master/Console)
for a live text window, but that proved unreliable (no window appeared at
all in one Mini vMac test), so `results.txt` is the one channel this app
consistently produces and is what the diagnostics now target. The
`AFPE2E_CONSOLE` option still exists and still builds (useful if you want to
experiment with it again later), but isn't part of the normal debugging
workflow.

```sh
mkdir -p build-console && cd build-console
cmake .. -DCMAKE_TOOLCHAIN_FILE=$HOME/Retro68-build/toolchain/m68k-apple-macos/cmake/retro68.toolchain.cmake -DAFPE2E_CONSOLE=ON
make
```

The console build is much larger (~1MB `.bin` vs. ~103KB for the default
build).

### Binary size / link flags

Only `-Os` is applied. `-ffunction-sections`/`-Wl,-gc-sections`/
`-Wl,--mac-single` were tried for a smaller binary (got the default build
down to ~62KB) and disabled again after an illegal-instruction crash in
Mini vMac with the RetroConsole build (`--gc-sections` can discard a C++
virtual function only reachable through a vtable). A second crash — an
"Uncaught CPU stepping error" in Snow on the plain build — looked at first
like a second instance of the same class of problem, but turned out to be
an unrelated stack buffer overflow in `afp/atalk.c` (see below/git history),
not caused by these flags. They remain off for now since re-enabling wasn't
worth the debugging churn while more important bugs were open, not because
they're proven unsafe — revisit as a deliberate, individually-tested pass
once the app is functionally complete.

### Isolating a crash to the AFP/AppleTalk code

If the app crashes in an emulator, `-DAFPE2E_AFP_COMMANDS=OFF` excludes
`afp/atalk.c`, `afp/asp.c`, and `afp/afp_client.c` from the build entirely
and registers zero script commands — an exact rebuild of the original
stage-1 skeleton. This is how a non-deterministic crash (different PC each
run of the identical binary — a strong stack/heap corruption signal) was
bisected to a real bug: `AtalkFindAFPServers` in `atalk.c` built an
`AtalkServerMatch.name` Pascal string by concatenating NBP's `Object`,
`Type`, and `Zone` fields (up to 32 bytes each) into a 33-byte buffer with
an unchecked `memcpy` loop — a stack smash whose exact effect depended on
the real server/zone name content and whatever else was in memory that run.
Fixed by widening the buffer to `ATALK_MAX_NAME_LEN` (99 bytes, sized for
the real worst case) and adding a bounds-checked `AtalkAppendPStr` helper.
**Lesson for future additions:** audit every fixed-size buffer that
concatenates multiple variable-length Toolbox-returned fields for a real
worst-case size before writing the copy loop — and if an emulator crash is
non-deterministic across identical runs, suspect a buffer overflow in
recently-added code and bisect with a feature-gate before chasing
linker/toolchain theories.

### Line endings

Script files are plain text but get copied onto the disk image with
`hcopy -t` (text-translation mode), which converts line endings to the
classic Mac convention (bare `\r`) — so `script.txt` on the floppy has `\r`
line endings even though `scripts/basic.txt` in the repo uses `\n`.
`MacFilesReadLine` reads the file itself byte-by-byte and treats `\r`, `\n`,
and `\r\n` all as line terminators for exactly this reason — do not swap it
for a plain `fgets`, which only recognizes `\n` and will silently read
straight through `\r`-terminated lines up to the buffer limit, corrupting
the first several commands in any script (this happened once; see git
history / the DEBUG raw-byte trail that diagnosed it if you need the receipt).

### Shutdown on exit

By default the app just quits back to the Finder when the script finishes
(or fails) — it does **not** power the machine off. This lets you reuse the
same booted emulator session across repeated test runs during development
(rebuild, re-copy `script.txt`/re-mount the disk, re-launch the app) without
a full boot cycle each time, and avoids accidentally testing against a stale
disk image left over from an earlier run. Pass `-DAFPE2E_SHUTDOWN_ON_EXIT=ON`
to make the app call the Shutdown Manager at the end instead — intended for
an eventual unattended harness run where nothing else needs the machine
afterwards:

```sh
cmake .. -DCMAKE_TOOLCHAIN_FILE=... -DAFPE2E_SHUTDOWN_ON_EXIT=ON
```

On any failure the app pauses for ~10 seconds before quitting/shutting down.

## Running under Snow / Mini vMac

1. Launch the emulator with `System7.1-base.hda` as the boot disk and
   `build/AFPE2E.dsk` (or `build-console/AFPE2E.dsk`) as a floppy.
2. Double-click `AFPE2E` — it runs to completion, then quits (or shuts the
   machine down if built with `AFPE2E_SHUTDOWN_ON_EXIT=ON`).
3. Read `results.txt` back off the floppy image (e.g. with `hcopy`).

Automating steps 1–3 (headless launch, completion detection, result
extraction, and diffing against expected fixtures) is planned as a follow-up
Go-based harness, not yet built.

## Source layout

```
src/
  main.c              # entry point: open script, run it, write results, quit
                       # (or shut down if built with AFPE2E_SHUTDOWN_ON_EXIT)
  script/             # portable command parser core (script.c/h, commands.c/h)
  results.c/.h        # result-line writer
  platform/
    mac_files.c/.h    # Toolbox/stdio file I/O + timestamp formatting
  afp/                # AFP client: atalk.c/.h (.MPP/NBP discovery), asp.c/.h
                       # (.XPP/ASP session + commands), afp_client.c/.h (FP*
                       # command parsing), afp_const.h (kFP* result codes)
```

The `script`/`results` split is deliberately platform- and protocol-agnostic;
only `platform/` and `afp/` touch Mac Toolbox or AFP specifics. See the plan
for how this core is intended to be reused by the Win16/Win32 SMB tools.
