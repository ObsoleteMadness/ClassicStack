# NMAKE makefile for the Win32 SMB end-to-end tool (SMBE2E.EXE, PE binary).
#
# Toolchain: Microsoft Visual C++ 1.2 for Windows NT (32-bit), the original
# NT 3.1-era release, shipped under tools/end-to-end/tools/msvc/win32. The
# compiler driver is CL386 and the linker is LINK32; both are 32-bit NT programs
# but the surrounding NMAKE/CL wrapper is 16-bit, so run this build on the host
# through otvdm, or inside an NT/Win32s guest. (MSVC 2.0 is a drop-in
# alternative if a build issue surfaces — same CL/LINK32 invocation.)
#
# The produced SMBE2E.EXE is a console-subsystem Win32 app: it runs script.txt
# and writes results.txt in the current directory. It uses the full WNet
# enumeration API (WNetOpenEnum/WNetEnumResource) for EnumerateServers/Shares,
# WNetAddConnection2 to map the share, and FindFirstFile for LFN + short-name +
# timestamp enumeration. Run it on a Windows NT / 95 / Win32s guest with a
# network client bound to a transport ClassicStack's SMB server speaks
# (NetBEUI / IPX / NBT).
#
# Build (from this directory, win32 BIN on PATH under otvdm):
#     nmake
# Produces SMBE2E.EXE, then `nmake floppy` packs it + basic.txt + lfn.txt onto
# a 1.44 MB image (SMBE2E1.img) for 86Box.

MSVC    = ..\..\tools\msvc\win32
CC      = $(MSVC)\bin\cl386.exe
LINK    = $(MSVC)\bin\link32.exe
FLOPGEN = ..\..\tools\flopgen.exe

SRC     = ..\src
COMMON  = ..\common
SCRIPTS = ..\scripts

# /c    = compile only; /Ox optimize; /W3 warnings.
# /D_X86_ selects the x86 target headers in this SDK's windows.h.
# /I    = SDK + our include dirs.
CFLAGS = /c /Ox /W3 /D_X86_=1 /DWIN32 /I$(MSVC)\include /I$(SRC) /I$(COMMON)

# Console subsystem; link the WNet (mpr) + base (kernel32) import libs. This
# MSVC 1.2 LINK32 has no /LIBPATH (that came later) — it resolves import libs
# via the LIB environment variable, which build.bat sets to the win32 lib dir.
# /ALIGN:0x1000 packs the sections to 4 KiB instead of the default 64 KiB so the
# .EXE is small enough to fit the 1.44 MB floppy image flopgen builds.
LFLAGS = /NOLOGO /SUBSYSTEM:console /ALIGN:0x1000
LIBS   = mpr.lib kernel32.lib user32.lib advapi32.lib libc.lib

OBJS = main.obj script.obj results.obj win_files.obj status.obj \
       smb_common.obj smb_win32.obj commands.obj

all: SMBE2E.EXE

main.obj: $(SRC)\main.c
	$(CC) $(CFLAGS) $(SRC)\main.c

status.obj: $(SRC)\status.c
	$(CC) $(CFLAGS) $(SRC)\status.c

commands.obj: $(SRC)\commands.c
	$(CC) $(CFLAGS) $(SRC)\commands.c

win_files.obj: $(SRC)\win_files.c
	$(CC) $(CFLAGS) $(SRC)\win_files.c

smb_common.obj: $(SRC)\smb_common.c
	$(CC) $(CFLAGS) $(SRC)\smb_common.c

smb_win32.obj: $(SRC)\smb_win32.c
	$(CC) $(CFLAGS) $(SRC)\smb_win32.c

script.obj: $(COMMON)\script.c
	$(CC) $(CFLAGS) $(COMMON)\script.c

results.obj: $(COMMON)\results.c
	$(CC) $(CFLAGS) $(COMMON)\results.c

SMBE2E.EXE: $(OBJS)
	$(LINK) $(LFLAGS) $(OBJS) $(LIBS) /OUT:SMBE2E.EXE
	rem MSVC 1.2 LINK32 zero-pads the image to ~2 MB; trim it so it fits a floppy.
	powershell -NoProfile -ExecutionPolicy Bypass -File trimpe.ps1 SMBE2E.EXE

# --- Floppy image for 86Box ---------------------------------------------------
# The Win32 image carries both scripts so an NT guest can run the LFN tests too.
floppy: SMBE2E.EXE
	copy $(SCRIPTS)\basic.txt script.txt
	$(FLOPGEN) -o SMBE2E -s 1440 SMBE2E.EXE script.txt $(SCRIPTS)\lfn.txt

clean:
	-del *.obj
	-del SMBE2E.EXE
	-del script.txt
	-del SMBE2E1.img
