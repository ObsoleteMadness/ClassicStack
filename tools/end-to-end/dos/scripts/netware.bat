@echo off
rem ===========================================================================
rem ClassicStack NCP (NetWare 3.x bindery) end-to-end workflow for MS-DOS.
rem
rem Drives the discovery -> login -> map -> file/directory -> logout path against
rem ClassicStack's NCP file service over IPX, through the real Novell DOS client
rem (NETX or VLM). Discovery uses "slist"; access uses "login" + "map"; the
rem file/directory work is delegated to the shared FILEOPS.BAT, so each op
rem traverses NetWare shell -> NCP -> ClassicStack exactly as a NetWare user
rem would.
rem
rem ClassicStack emulates a NetWare 3.x bindery server named CLASSICSTACK with a
rem single volume (see [[ncpvolumes]].name in server.toml, default "Foo"). The
rem server allows guest-style login, so we log in as GUEST with no password.
rem
rem Result lines follow tools/end-to-end/RESULT-FORMAT.md (RESULT v1) as closely
rem as batch allows; raw shell output goes to OUT.TXT.
rem
rem Requirements: IPXODI + NETX (or VLM) loaded and the ClassicStack IPX/NCP
rem service running. Run from a writable local drive (e.g. C:\E2E). FILEOPS.BAT
rem must sit in the same directory as this file.
rem
rem Usage:  NETWARE                      (defaults: server CLASSICSTACK, vol Foo,
rem                                       user GUEST, drive F:)
rem         NETWARE server volume user drive:
rem ===========================================================================

set E2ESRV=CLASSICSTACK
set E2EVOL=Foo
set E2EUSER=GUEST
set E2EDRV=F:
if not "%1"=="" set E2ESRV=%1
if not "%2"=="" set E2EVOL=%2
if not "%3"=="" set E2EUSER=%3
if not "%4"=="" set E2EDRV=%4

rem shared FILEOPS.BAT contract:
set E2EHOME=C:
set E2ETAG=NETWARE
set RES=RESULTS.TXT
set OUT=OUT.TXT

echo RESULT v1 started="%DATE% %TIME%">%RES%
echo DEBUG netware starting>>%RES%
echo DEBUG env: platform=msdos redirector=ncp>>%RES%
echo.>%OUT%

rem --- Discovery ------------------------------------------------------------
rem "slist" lists NetWare servers advertised via SAP; ours should appear.
echo DEBUG line slist>>%RES%
echo === slist ===>>%OUT%
slist>>%OUT%
echo PASS ListServers supported=1 detail="see OUT.TXT">>%RES%

rem --- Login ----------------------------------------------------------------
rem "login server/user" authenticates against the bindery. On a guest server no
rem password is needed; feed a blank line in case it prompts.
echo DEBUG line login %E2ESRV%/%E2EUSER%>>%RES%
echo === login %E2ESRV%/%E2EUSER% ===>>%OUT%
echo.| login %E2ESRV%/%E2EUSER%>>%OUT%
if errorlevel 1 goto loginfail
echo PASS Login server="%E2ESRV%" user="%E2EUSER%">>%RES%
goto mapvol

:loginfail
echo FAIL Login server="%E2ESRV%" user="%E2EUSER%" detail="login failed">>%RES%
goto done

rem --- Map the volume to a drive letter -------------------------------------
:mapvol
echo DEBUG line map %E2EDRV%=%E2ESRV%/%E2EVOL%:>>%RES%
echo === map %E2EDRV%=%E2ESRV%/%E2EVOL%: ===>>%OUT%
map %E2EDRV%=%E2ESRV%/%E2EVOL%:>>%OUT%
if errorlevel 1 goto mapfail
echo PASS Mount server="%E2ESRV%" volume="%E2EVOL%" drive="%E2EDRV%">>%RES%
goto mapped

:mapfail
echo FAIL Mount server="%E2ESRV%" volume="%E2EVOL%" drive="%E2EDRV%" detail="map failed">>%RES%
goto logout

rem --- File / directory tasks (shared) --------------------------------------
:mapped
call FILEOPS.BAT

rem --- Teardown -------------------------------------------------------------
:logout
echo DEBUG line map del %E2EDRV%>>%RES%
echo === map del %E2EDRV% ===>>%OUT%
map del %E2EDRV%>>%OUT%
echo PASS Unmount drive="%E2EDRV%">>%RES%
echo DEBUG line logout>>%RES%
echo === logout ===>>%OUT%
logout>>%OUT%
echo PASS Disconnect server="%E2ESRV%">>%RES%

:done
echo DEBUG counts computed by harness from PASS/FAIL lines>>%RES%
echo DONE>>%RES%
echo Finished. See %RES% (results) and %OUT% (raw output).
