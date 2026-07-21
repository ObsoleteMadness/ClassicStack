@echo off
rem ===========================================================================
rem ClassicStack EtherDFS end-to-end workflow for MS-DOS.
rem
rem EtherDFS is not a "net use" redirector: the ETHERDFS.EXE TSR installs an
rem INT 2Fh network redirector that maps a REMOTE drive letter on the server to
rem a LOCAL drive letter, talking raw Ethernet (no IP/IPX) straight to the
rem server's MAC. There is no login and no "mount" command -- once the TSR is
rem resident the local drive is simply present, so this script's "mount" step is
rem loading the TSR and its "unmount" step is unloading it. The file/directory
rem work is delegated to the shared FILEOPS.BAT, traversing
rem ETHERDFS redirector -> raw Ethernet -> ClassicStack's EtherDFS server.
rem
rem Discovery: ETHERDFS.EXE with "::" as the server MAC broadcasts an
rem AL_DISKSPACE probe for the drive being mapped and learns the server MAC from
rem the reply (auto-discovery). Give an explicit MAC as %1 to skip the broadcast.
rem
rem The REMOTE drive letter is [[EtherDFSDrives]].name in server.toml (default
rem "C"); we map it to LOCAL F:. Result lines follow
rem tools/end-to-end/RESULT-FORMAT.md (RESULT v1); raw output goes to OUT.TXT.
rem
rem Requirements: a packet driver loaded for the NIC, ETHERDFS.EXE on the PATH,
rem and the ClassicStack EtherDFS service running on the same segment. Run from a
rem writable LOCAL drive (e.g. C:\E2E) -- NOT from the mapped drive. FILEOPS.BAT
rem must sit in the same directory as this file.
rem
rem Usage:  ETHERDFS                 (auto-discover; remote C -> local F:)
rem         ETHERDFS mac remote local
rem           mac    = server MAC "aa:bb:cc:dd:ee:ff", or "::" to auto-discover
rem           remote = remote drive letter on the server (e.g. C)
rem           local  = local drive letter to map (e.g. F)
rem ===========================================================================

set E2EMAC=::
set E2EREM=C
set E2ELOC=F
if not "%1"=="" set E2EMAC=%1
if not "%2"=="" set E2EREM=%2
if not "%3"=="" set E2ELOC=%3
set E2EDRV=%E2ELOC%:

rem shared FILEOPS.BAT contract:
set E2EHOME=C:
set E2ETAG=ETHERDFS
set RES=RESULTS.TXT
set OUT=OUT.TXT

echo RESULT v1 started="%DATE% %TIME%">%RES%
echo DEBUG etherdfs starting>>%RES%
echo DEBUG env: platform=msdos redirector=etherdfs>>%RES%
echo.>%OUT%

rem --- Discovery + Mount (load the TSR) -------------------------------------
rem "etherdfs <mac> <remote>-<local>" installs the redirector and, with "::",
rem performs auto-discovery. A successful install prints the server MAC and
rem returns errorlevel 0; a failure ("No EtherDFS server found on the LAN")
rem returns nonzero.
echo DEBUG line etherdfs %E2EMAC% %E2EREM%-%E2ELOC%>>%RES%
echo === etherdfs %E2EMAC% %E2EREM%-%E2ELOC% ===>>%OUT%
etherdfs %E2EMAC% %E2EREM%-%E2ELOC%>>%OUT%
if errorlevel 1 goto mountfail
echo PASS EnumerateServers supported=1 detail="auto-discovery via etherdfs ::">>%RES%
echo PASS Mount remote="%E2EREM%" drive="%E2EDRV%" detail="TSR installed">>%RES%
goto mounted

:mountfail
echo FAIL Mount remote="%E2EREM%" drive="%E2EDRV%" detail="etherdfs install failed (server not found?)">>%RES%
goto done

rem --- File / directory tasks (shared) --------------------------------------
:mounted
call FILEOPS.BAT

rem --- Teardown (unload the TSR) -------------------------------------------
rem "etherdfs /u" unloads the redirector and frees the drive letter.
echo DEBUG line etherdfs /u>>%RES%
echo === etherdfs /u ===>>%OUT%
etherdfs /u>>%OUT%
if errorlevel 1 goto unloadfail
echo PASS Unmount drive="%E2EDRV%" detail="TSR unloaded">>%RES%
goto done
:unloadfail
echo FAIL Unmount drive="%E2EDRV%" detail="etherdfs /u failed">>%RES%

:done
echo DEBUG counts computed by harness from PASS/FAIL lines>>%RES%
echo DONE>>%RES%
echo Finished. See %RES% (results) and %OUT% (raw output).
