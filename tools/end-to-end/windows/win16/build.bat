@echo off
rem ---------------------------------------------------------------------------
rem Build SMBE2E.EXE (Win16, NE) with MSVC 1.5, then optionally pack a 1.44 MB
rem floppy image for 86Box.  Runs on the host: NMAKE.EXE and the CL.EXE driver in
rem this kit are native win32, and otvdm executes the TNT compiler passes CL
rem spawns.  Usage:
rem     build.bat            -> SMBE2E.EXE
rem     build.bat floppy     -> SMBE2E.EXE + SMBE2E1.img
rem
rem Two things make this build succeed on the host, learned the hard way:
rem   1. SUBST W: onto tools/end-to-end so every path the 16-bit tools see is
rem      short (W:\tools\msvc\win16\...).  Deep paths overflow their buffers.
rem   2. BLANK the INCLUDE/LIB/CL env vars and trim PATH to just the win16 BIN.
rem      A long inherited PATH/INCLUDE overflows the tools' fixed env buffers and
rem      shows up as a spurious "Out of memory" from the compiler.  Includes and
rem      libs are passed explicitly via /I and full-path LIBS in the makefile, so
rem      no INCLUDE/LIB is needed.
rem The QuickWin C2 pass Q23.EXE must exist in the win16 BIN dir (CL looks it up
rem by name for /Mq; if missing it prompts interactively and hangs otvdm).
rem ---------------------------------------------------------------------------
setlocal

set OTVDM=c:\otvdm\otvdm.exe

rem The end-to-end root is two levels up from windows\win16.
set E2E=%~dp0..\..
set SUBSTDRV=W:

subst %SUBSTDRV% "%E2E%" >nul 2>&1
if errorlevel 1 (
    echo Could not subst %SUBSTDRV% onto "%E2E%" -- is it already in use?
    echo Try: subst %SUBSTDRV% /d
    exit /b 1
)

set BIN=%SUBSTDRV%\tools\msvc\win16\bin
set FLOPGEN=%SUBSTDRV%\tools\flopgen.exe
set SCRIPTS=%SUBSTDRV%\windows\scripts

rem --- Minimal environment for the 16-bit tools (see note above) -------------
set INCLUDE=
set LIB=
set CL=
set PATH=%BIN%;%SystemRoot%\System32

pushd %SUBSTDRV%\windows\win16
"%OTVDM%" %BIN%\NMAKE.EXE /f makefile
set BUILDERR=%ERRORLEVEL%
popd

if not "%BUILDERR%"=="0" goto :done
if not exist %SUBSTDRV%\windows\win16\SMBE2E.EXE (
    echo Build reported success but SMBE2E.EXE is missing.
    set BUILDERR=1
    goto :done
)

rem --- Floppy image (host-side; flopgen is native win32) --------------------
if /I "%1"=="floppy" (
    pushd %SUBSTDRV%\windows\win16
    copy /Y "%SCRIPTS%\basic.txt" script.txt >nul
    "%FLOPGEN%" -o SMBE2E -s 1440 SMBE2E.EXE script.txt
    set BUILDERR=%ERRORLEVEL%
    popd
)

:done
subst %SUBSTDRV% /d >nul 2>&1
endlocal & exit /b %BUILDERR%
