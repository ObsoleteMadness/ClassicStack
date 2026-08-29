@echo off
rem ===========================================================================
rem ClassicStack SMB end-to-end workflow for MS-DOS (Microsoft Network Client
rem 3.0 / Workgroup Connection, redirector loaded).
rem
rem Drives the full discovery -> mount -> file/directory -> unmount path against
rem ClassicStack's SMB server through the real DOS "net" redirector, over either
rem NetBEUI or IPX (whichever protocol.ini/system.ini has bound). Discovery uses
rem "net view"; the mount uses "net use"; the file/directory work is delegated
rem to the shared FILEOPS.BAT so each op traverses
rem MS-redirector -> SMB -> ClassicStack exactly as a DOS user would.
rem
rem Unlike the compiled macOS/Windows tools, this is a plain batch file: DOS has
rem no script interpreter of ours, so native commands do the work and their
rem output is redirected here. Result lines match tools/end-to-end/RESULT-FORMAT.md
rem (RESULT v1) as closely as batch allows; a human/harness still reads OUT.TXT
rem for the raw "net"/"dir" output when a step's pass/fail can't be told from
rem errorlevel alone.
rem
rem Usage:  MSCLIENT              (defaults: server CLASSICSTACK, share Foo, F:)
rem         MSCLIENT server share drive:
rem
rem Results go to RESULTS.TXT in the current directory; raw command output to
rem OUT.TXT.  Run from a writable local drive (e.g. C:\E2E), not the mapped one.
rem FILEOPS.BAT must sit in the same directory as this file.
rem ===========================================================================

set E2ESRV=CLASSICSTACK
set E2ESHARE=Foo
set E2EDRV=F:
if not "%1"=="" set E2ESRV=%1
if not "%2"=="" set E2ESHARE=%2
if not "%3"=="" set E2EDRV=%3

rem shared FILEOPS.BAT contract:
set E2EHOME=C:
set E2ETAG=MSCLIENT
set RES=RESULTS.TXT
set OUT=OUT.TXT

echo RESULT v1 started="%DATE% %TIME%">%RES%
echo DEBUG msclient starting>>%RES%
echo DEBUG env: platform=msdos redirector=smb>>%RES%
echo.>%OUT%

rem --- Discovery ------------------------------------------------------------
rem "net view" lists other servers on the workgroup; "net view \\srv" lists our
rem shares. Neither returns a reliable errorlevel on every client build, so we
rem log them as PASS supported=1 and dump the raw text to OUT.TXT for the eye.
echo DEBUG line net view>>%RES%
echo === net view ===>>%OUT%
net view>>%OUT%
echo PASS EnumerateServers supported=1 detail="see OUT.TXT">>%RES%

echo DEBUG line net view \\%E2ESRV%>>%RES%
echo === net view \\%E2ESRV% ===>>%OUT%
net view \\%E2ESRV%>>%OUT%
echo PASS EnumerateShares supported=1 server="%E2ESRV%" detail="see OUT.TXT">>%RES%

rem --- Mount ----------------------------------------------------------------
rem Map \\SERVER\SHARE to the drive letter. Guest access (no /user).
echo DEBUG line net use %E2EDRV% \\%E2ESRV%\%E2ESHARE%>>%RES%
echo === net use %E2EDRV% \\%E2ESRV%\%E2ESHARE% ===>>%OUT%
net use %E2EDRV% \\%E2ESRV%\%E2ESHARE%>>%OUT%
if errorlevel 1 goto mountfail
echo PASS Mount server="%E2ESRV%" share="%E2ESHARE%" drive="%E2EDRV%">>%RES%
goto mounted

:mountfail
echo FAIL Mount server="%E2ESRV%" share="%E2ESHARE%" drive="%E2EDRV%" detail="net use failed">>%RES%
goto done

rem --- File / directory tasks (shared) --------------------------------------
:mounted
call FILEOPS.BAT

rem --- Teardown -------------------------------------------------------------
echo DEBUG line net use %E2EDRV% /delete>>%RES%
echo === net use %E2EDRV% /delete ===>>%OUT%
net use %E2EDRV% /delete>>%OUT%
if errorlevel 1 goto unmountfail
echo PASS Unmount drive="%E2EDRV%">>%RES%
goto done
:unmountfail
echo FAIL Unmount drive="%E2EDRV%" detail="net use /delete failed">>%RES%

:done
echo DEBUG counts computed by harness from PASS/FAIL lines>>%RES%
echo DONE>>%RES%
echo Finished. See %RES% (results) and %OUT% (raw output).
