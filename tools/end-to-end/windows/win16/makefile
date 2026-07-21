# NMAKE makefile for the Win16 SMB end-to-end tool (SMBE2E.EXE, NE binary).
#
# Toolchain: Microsoft Visual C++ 1.5 for Windows (16-bit), shipped under
# tools/end-to-end/tools/msvc/win16. NMAKE.EXE and the CL.EXE *driver* in that
# kit are actually native NT win32 programs, so nmake runs directly on Windows
# 11; CL then spawns the Phar Lap TNT-extended compiler passes (C1/C2/Q23), which
# otvdm executes. The produced SMBE2E.EXE is a QuickWin (/Mq) Windows app: it
# opens a text window, runs script.txt, and writes results.txt next to itself.
# It needs Windows for Workgroups 3.11 (or Windows 3.1 + a network client) so
# WNetAddConnection can map the share.
#
# Build with build.bat (recommended): it SUBSTs W: for short paths and — the key
# part — BLANKS the INCLUDE/LIB/CL env vars and trims PATH to just the win16 BIN.
# A long PATH/INCLUDE overflows the 16-bit tools' fixed env buffers and surfaces
# as a spurious "Out of memory" from the compiler; a clean minimal environment
# is what makes this build succeed on the host.
#     build.bat            -> SMBE2E.EXE
#     build.bat floppy     -> SMBE2E.EXE + SMBE2E1.img (for 86Box)
#
# The /Mq QuickWin C2 pass is Q23.EXE and MUST be present in the win16 BIN dir;
# the base kit ships C1/C2/C3 passes but the QuickWin codegen pass is a separate
# executable CL looks up by name (missing -> CL prompts for it, hanging otvdm).

MSVC15 = ..\..\tools\msvc\win16
CC     = $(MSVC15)\bin\cl.exe

SRC    = ..\src
COMMON = ..\common

# /AL  = LARGE memory model. REQUIRED and load-bearing: /Mq only selects the
#        QuickWin flavour, it does NOT set a model — without /AL, CL defaults to
#        SMALL model (near code/data pointers). We link the LARGE-model QuickWin
#        runtime LLIBCEWQ.LIB (the leading "L"), so small-model objects calling
#        it make the CRT read a far pointer where a near one was pushed: printf's
#        `les si,[bp+0Ah]` loads a garbage selector → #GP at startup, before a
#        single character prints (a blank QuickWin window then an app fault).
#        The model letter of the objects (/A?) MUST match the runtime lib's
#        leading letter (L*LIBC*). See tools/end-to-end/windows/readme.md.
# /Mq  = QuickWin app (text window + stdio + main()).
# /Od  = no optimization; these test binaries are tiny, so it buys nothing.
# /W3  = reasonable warning level.
# /I   = SDK + our own include dirs.
CFLAGS = /AL /Mq /Od /W3 /I$(MSVC15)\include /I$(SRC) /I$(COMMON)

# QuickWin large-model runtime + Windows import library, named explicitly with
# full paths so the build does not depend on the LIB environment variable being
# set. LLIBCEWQ = Large model, LIBC, Emulator math, Windows, Quickwin.
LFLAGS = /NOLOGO /NOD
LIBS   = $(MSVC15)\lib\llibcewq.lib $(MSVC15)\lib\libw.lib

OBJS = main.obj script.obj results.obj win_files.obj status.obj \
       smb_common.obj smb_win16.obj commands.obj

all: SMBE2E.EXE

# --- Compile each translation unit (sources live in sibling src/common dirs) --
main.obj: $(SRC)\main.c
	$(CC) $(CFLAGS) /c $(SRC)\main.c

status.obj: $(SRC)\status.c
	$(CC) $(CFLAGS) /c $(SRC)\status.c

commands.obj: $(SRC)\commands.c
	$(CC) $(CFLAGS) /c $(SRC)\commands.c

win_files.obj: $(SRC)\win_files.c
	$(CC) $(CFLAGS) /c $(SRC)\win_files.c

smb_common.obj: $(SRC)\smb_common.c
	$(CC) $(CFLAGS) /c $(SRC)\smb_common.c

smb_win16.obj: $(SRC)\smb_win16.c
	$(CC) $(CFLAGS) /c $(SRC)\smb_win16.c

script.obj: $(COMMON)\script.c
	$(CC) $(CFLAGS) /c $(COMMON)\script.c

results.obj: $(COMMON)\results.c
	$(CC) $(CFLAGS) /c $(COMMON)\results.c

# --- Link ---------------------------------------------------------------------
# 16-bit LINK syntax:  objs, exe, map, libs, deffile
SMBE2E.EXE: $(OBJS) smbe2e.def
	$(MSVC15)\bin\link.exe $(LFLAGS) $(OBJS), SMBE2E.EXE, NUL, $(LIBS), smbe2e.def

# The floppy image (SMBE2E1.img) is built by build.bat, not nmake: flopgen is a
# native win32 tool and packaging is a host-side step, so it lives outside the
# 16-bit toolchain's make run.

clean:
	-del *.obj
	-del SMBE2E.EXE
	-del script.txt
	-del SMBE2E1.img
