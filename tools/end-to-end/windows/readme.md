# Windows End-to-End Tests (Win16 / Win32 SMB)

Native Windows applications — one 16-bit (NE) and one 32-bit (PE) — that
exercise ClassicStack's **SMB** file server end-to-end: server/share discovery,
share mount, directory enumeration, and file/directory operations, driven by a
plain-text `script.txt` and reporting to `results.txt` next to the app. These
are the Windows counterparts of the macOS/AFP tool under `../macos`; they share
the same script grammar, the same portable command-parser/result-writer core,
and the same output contract (`../RESULT-FORMAT.md`).

Where the macOS tool drives the real AppleShare client so every op traverses
`AppleShare → AFP → ClassicStack`, these drive the real **Windows network
redirector**: enumeration and mount go through the WNet API, and once a drive
letter is mapped, ordinary file APIs (`FindFirstFile`/`CreateFile` on Win32,
`_dos_findfirst`/`fopen` on Win16) carry every op through
`MS-redirector → SMB → ClassicStack`. The tool speaks no SMB itself and is
transport-agnostic — it asks the redirector for `\\CLASSICSTACK` and whatever
transport the guest has bound (NetBEUI and/or IPX, matching
`[SMB].transports` in `server.toml`) carries it.

There are **no resource-fork operations** — SMB has no fork concept — so the
Windows `basic.txt` is the macOS workflow minus the `WriteFork` steps. Win32
additionally runs long-file-name (`lfn.txt`) tests.

## Layout

```
windows/
  common/            portable core, kept identical to ../macos/src/script + results
    script.c/.h        command tokenizer + dispatch
    results.c/.h       PASS/FAIL/DEBUG/DONE writer (see ../RESULT-FORMAT.md)
  src/
    win_files.c/.h     file I/O + timestamp seam (MSVC CRT; both targets)
    smb.h              the SMB/net client seam (one interface, two back-ends)
    smb_common.c       shared standardized directory-entry formatter
    smb_win32.c        Win32 back-end: WNet enum/mount + FindFirstFile (LFN)
    smb_win16.c        Win16 back-end: WNetAddConnection + _dos_findfirst (8.3)
    commands.c/.h      the SMB command vocabulary → dispatch table
    main.c             entry point (console/QuickWin)
  scripts/
    basic.txt          discovery → mount → file/dir → unmount (both targets)
    lfn.txt            long-file-name tests (Win32 only)
  win16/  makefile, build.bat, smbe2e.def   (MSVC 1.5, → NE binary)
  win32/  makefile, build.bat               (MSVC 1.2 NT, → PE binary)
```

`common/` is deliberately a copy of the macOS core, not a shared checkout: the
two trees build under completely different toolchains. Keep them in sync by hand
— a fix in one belongs in the other.

## Toolchains

Both compilers live under `../tools/msvc` and are period 16-bit-hosted tools:

| Target | Compiler                    | Output | Runs on                         |
|--------|-----------------------------|--------|---------------------------------|
| Win16  | MSVC **1.5** (`win16/`)     | NE     | Windows 3.1 / WfW 3.11          |
| Win32  | MSVC **1.2** for NT (`win32/`) | PE  | Windows NT 3.1+/95/Win32s       |

Win32 is the original NT 3.1-era MSVC 1.2 (compiler driver `CL386`, linker
`LINK32`). MSVC 2.0 is a drop-in alternative if a build problem surfaces — same
invocation. A Win32 PE and a Win16 NE are different formats; neither linker
produces the other, so both toolchains are genuinely required.

## Building

The compiled `.EXE`s are always **run under 86Box** (an emulated NT/95/WfW
guest). Both toolchains, however, **build on the Windows 11 host** — their
`NMAKE`/`CL` drivers are native win32 — with one difference in how the compiler
back-end runs:

- **Win32 (MSVC 1.2)** — the `CL386`/`LINK32`/`NMAKE` binaries are **native NT
  PE32** programs that run **directly on Windows 11 x64**. No emulator needed to
  build. Verified end-to-end below.
- **Win16 (MSVC 1.5)** — this kit *also* builds on the Win11 host. `NMAKE.EXE`
  and the `CL.EXE` **driver** are native win32 programs, so nmake runs directly;
  `CL` then spawns the Phar Lap TNT-extended compiler passes (`C1`/`C2`/`Q23`),
  which **otvdm** executes. The one trap: the "Out of memory" the compiler used
  to report was **not** the TNT extender — it was a long inherited `PATH`/
  `INCLUDE` overflowing the 16-bit tools' fixed env buffers. `build.bat` fixes it
  by **blanking `INCLUDE`/`LIB`/`CL` and trimming `PATH` to just the win16 BIN**
  (includes/libs are passed explicitly via `/I` and full-path libs). With that
  clean environment the Win16 NE builds end-to-end on this host (verified below).
  One kit note: the QuickWin `/Mq` codegen pass `Q23.EXE` must be present in the
  win16 BIN dir — `CL` looks it up by name and, if it's missing, prompts for its
  path interactively (which hangs otvdm).

Both toolchains dislike long paths (and overflow their fixed command-line/
`INCLUDE` buffers with a deep path — the real source of the "Out of memory"), so
`build.bat` `SUBST`s a drive letter (`W:`) onto `tools/end-to-end`, giving the
tools short paths like `W:\tools\msvc\win32\...`, runs `nmake`, then removes the
subst.

### Win32 (native, Windows 11)

```bat
cd tools\end-to-end\windows\win32
build.bat            :: -> SMBE2E.EXE (native CL386/LINK32; auto-trimmed)
build.bat floppy     :: -> SMBE2E.EXE + SMBE2E1.img
```

Verified: compiles all 7 objects, links a PE32 console `.EXE`, trims it (see
below), and produces a 1.44 MB `SMBE2E1.img` containing `SMBE2E.EXE`,
`SCRIPT.TXT`, and `LFN.TXT`.

**EXE trimming.** The MSVC 1.2 `LINK32` zero-pads the image file to its full
in-memory size (~2 MB for a ~40 KB tool), which would not fit a 1.44 MB floppy.
The `SMBE2E.EXE` build step runs `trimpe.ps1`, which truncates the file to the
end of the last PE section's raw data (rounded to `FileAlignment`) — the padding
is unreferenced by any PE structure, so this is safe. The trimmed tool is ~42 KB.

### Win16 (native, Windows 11)

```bat
cd tools\end-to-end\windows\win16
build.bat            :: -> SMBE2E.EXE (MSVC 1.5 CL/LINK; nmake native, passes via otvdm)
build.bat floppy     :: -> SMBE2E.EXE + SMBE2E1.img
```

Verified: compiles all 7 objects with the 16-bit `CL`, links a genuine NE
(`MZ`…`NE` — 16-bit New Executable, confirmed at `e_lfanew`), and packs a
1.44 MB `SMBE2E1.img` with `SMBE2E.EXE` + `SCRIPT.TXT`. Unlike Win32, the NE
needs no trim — the 16-bit `LINK` writes a tight file (~66 KB). The floppy is
packed by `build.bat` itself (flopgen is native win32), not by nmake.

### Floppy image (86Box)

The `floppy` target packs the built `SMBE2E.EXE` and the script(s) onto a
1.44 MB FAT image with `../tools/flopgen.exe`:

```
flopgen -o SMBE2E -s 1440 SMBE2E.EXE script.txt [lfn.txt]
```

producing `SMBE2E1.img`. `basic.txt` is copied to `script.txt` (the fixed name
the app opens). The Win32 image also carries `lfn.txt`; to run the LFN suite,
rename it to `script.txt` on the image (or build a second image from it).

## Running under 86Box

1. Boot a guest with a network client and a redirector bound to a transport
   ClassicStack serves SMB on (`[SMB].transports = ['netbeui', 'ipx', 'nbt']`):
   - **Win16**: Windows for Workgroups 3.11 (built-in NetBEUI/IPX redirector),
     or Windows 3.1 + the MS Network Client.
   - **Win32**: Windows NT 3.1+/95 with NetBEUI or NWLink (IPX) installed.
   The guest's workgroup should be `WORKGROUP` (matches `[identity].workgroup`).
2. Mount `SMBE2E1.img` as floppy A: and, from the guest,
   `A:` then run `SMBE2E` (Win16: a QuickWin text window opens; Win32: a console
   window). It runs `script.txt` to completion and writes `results.txt` onto the
   floppy (A: is writable).
3. Shut the guest down, then read `results.txt` back off `SMBE2E1.img` on the
   host (any FAT image tool, or re-`subst`/loop-mount) and diff against the
   expected fixture.

The share the scripts target is `\\CLASSICSTACK\Foo`
(`[[smbshares]] name = 'Foo'` in `server.toml`), mapped to `N:`, guest access.

### Win16 vs Win32 differences you'll see in `results.txt`

- **Discovery**: `EnumerateServers`/`EnumerateShares` report `supported=0` on
  Win16 (the Windows 3.1 WNet API has no `WNetOpenEnum`) — reported as PASS, not
  a failure, exactly like the macOS MacIPX placeholders. On Win32 they run for
  real via `WNetOpenEnum`/`WNetEnumResource`.
- **Names**: Win32 entries carry the long name in `name=` and the 8.3 alias in
  `short=`; Win16 sees only 8.3 (`short=` == `name=`).
- **Timestamps**: Win32 fills `created`/`modified`/`accessed`; Win16 (DOS) fills
  only `modified` and emits `-` for the other two. See `../RESULT-FORMAT.md`.

## Status

- **Win32**: builds end-to-end on this host with the native MSVC 1.2 tools —
  `build.bat floppy` produces the trimmed ~42 KB `SMBE2E.EXE` and a ready
  `SMBE2E1.img`. Not yet *run*: that needs an NT/95 guest in 86Box against a
  live ClassicStack.
- **Win16**: also builds end-to-end on this host — `build.bat floppy` compiles
  all 7 objects with MSVC 1.5, links a ~66 KB NE `SMBE2E.EXE`, and packs a
  1.44 MB `SMBE2E1.img`. The build's minimal-env / `Q23.EXE` requirements are
  baked into `build.bat` (see Building). Not yet *run*: that needs a WfW 3.11 /
  Win 3.1 guest in 86Box against a live ClassicStack.

Next step for both: boot the respective guest, run `SMBE2E` off the floppy, and
capture the first `results.txt`.

Automating the 86Box launch + result extraction + fixture diff is a planned
follow-up harness, shared with the macOS tool.
