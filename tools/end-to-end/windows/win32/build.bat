@echo off
rem ---------------------------------------------------------------------------
rem Build SMBE2E.EXE (Win32) with MSVC 1.2 for Windows NT, SUBST-ing a drive
rem onto tools/end-to-end so the (16-bit-wrapped) toolchain sees short paths.
rem
rem Run on the host under otvdm, or inside an NT/Win32s guest. Pass "floppy" to
rem also build the 86Box disk image:   build.bat floppy
rem ---------------------------------------------------------------------------
setlocal

set E2E=%~dp0..\..
set SUBSTDRV=W:

subst %SUBSTDRV% "%E2E%" >nul 2>&1
if errorlevel 1 (
    echo Could not subst %SUBSTDRV% onto "%E2E%" -- is it already in use?
    echo Try: subst %SUBSTDRV% /d
    goto :end
)

set MSVC=%SUBSTDRV%\tools\msvc\win32
set INCLUDE=%MSVC%\include
set LIB=%MSVC%\lib
set PATH=%MSVC%\bin;%PATH%

pushd %SUBSTDRV%\windows\win32
nmake /f makefile MSVC=%MSVC% %1 %2 %3
set BUILDERR=%ERRORLEVEL%
popd

subst %SUBSTDRV% /d >nul 2>&1

:end
endlocal
exit /b %BUILDERR%
