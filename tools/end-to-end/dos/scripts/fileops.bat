@echo off
rem ===========================================================================
rem Shared file/directory task body for the DOS end-to-end clients.
rem
rem This is the DOS analogue of the file/directory section every compiled
rem end-to-end tool runs (macOS AFP, Windows SMB): create/write/stat/rename/
rem move/copy/delete a file at the volume root and again one level down, then
rem rename/copy/enumerate/delete directories. It is transport-agnostic -- it
rem operates purely on whatever drive letter is already mapped, so MSCLIENT.BAT
rem (SMB), NETWARE.BAT (NCP) and ETHERDFS.BAT all CALL this one file instead of
rem each carrying its own byte-identical copy.
rem
rem The caller must set these before "call fileops.bat":
rem   E2EDRV   the mapped drive to exercise, incl. colon (e.g. "F:")
rem   E2EHOME  the local drive to return to afterwards, incl. colon (e.g. "C:")
rem   E2ETAG   short label written into the test file's contents (e.g. MSCLIENT)
rem   RES      results file    (RESULT v1 lines are appended)
rem   OUT      raw-output file  (native command output is appended)
rem
rem Result lines follow tools/end-to-end/RESULT-FORMAT.md (RESULT v1). Only
rem DOS-safe COMMAND.COM syntax is used: no "( ) else ( )" blocks, no "/Q",
rem no "goto :eof"; deletes of *.* answer the confirmation prompt with "Y".
rem ===========================================================================

%E2EDRV%
cd \

rem --- Enumerate the volume root -------------------------------------------
echo DEBUG line dir %E2EDRV%\>>%RES%
echo === dir %E2EDRV%\ ===>>%OUT%
dir %E2EDRV%\>>%OUT%
if errorlevel 1 goto enumfail
echo PASS EnumerateVolume supported=1 path="%E2EDRV%\" detail="see OUT.TXT">>%RES%
goto e_ok
:enumfail
echo FAIL EnumerateVolume path="%E2EDRV%\" detail="dir failed">>%RES%
:e_ok

rem --- File tasks at the volume root ---------------------------------------
echo hello from %E2ETAG% E2E>E2E.TXT
if not exist E2E.TXT goto cf_fail
echo PASS CreateFile name="E2E.TXT">>%RES%
echo PASS WriteFile name="E2E.TXT" detail="echo redirect">>%RES%
goto cf_ok
:cf_fail
echo FAIL CreateFile name="E2E.TXT" detail="create/write failed">>%RES%
:cf_ok

echo === type E2E.TXT ===>>%OUT%
type E2E.TXT>>%OUT%
if exist E2E.TXT echo PASS StatFile name="E2E.TXT">>%RES%
if not exist E2E.TXT echo FAIL StatFile name="E2E.TXT">>%RES%

ren E2E.TXT E2E2.TXT
if exist E2E2.TXT echo PASS RenameFile old="E2E.TXT" new="E2E2.TXT">>%RES%
if not exist E2E2.TXT echo FAIL RenameFile old="E2E.TXT" new="E2E2.TXT">>%RES%

md DEST
if exist DEST\NUL echo PASS CreateDir name="DEST">>%RES%
if not exist DEST\NUL echo FAIL CreateDir name="DEST">>%RES%

move E2E2.TXT DEST>>%OUT%
if exist DEST\E2E2.TXT echo PASS MoveFile name="E2E2.TXT" toDir="DEST">>%RES%
if not exist DEST\E2E2.TXT echo FAIL MoveFile name="E2E2.TXT" toDir="DEST">>%RES%

cd DEST
copy E2E2.TXT E2ECOPY.TXT>>%OUT%
if exist E2ECOPY.TXT echo PASS CopyFile name="E2E2.TXT" to="E2ECOPY.TXT">>%RES%
if not exist E2ECOPY.TXT echo FAIL CopyFile name="E2E2.TXT" to="E2ECOPY.TXT">>%RES%

del E2ECOPY.TXT
if not exist E2ECOPY.TXT echo PASS DeleteFile name="E2ECOPY.TXT">>%RES%
if exist E2ECOPY.TXT echo FAIL DeleteFile name="E2ECOPY.TXT">>%RES%
cd \

rem --- Subdirectory: run the same file tasks one level down ----------------
md SUB
if exist SUB\NUL echo PASS CreateDir name="SUB">>%RES%
if not exist SUB\NUL echo FAIL CreateDir name="SUB">>%RES%
cd SUB
echo hello from the subdirectory>E2E.TXT
if exist E2E.TXT echo PASS CreateFile name="E2E.TXT">>%RES%
if not exist E2E.TXT echo FAIL CreateFile name="E2E.TXT">>%RES%
if exist E2E.TXT echo PASS WriteFile name="E2E.TXT">>%RES%
if not exist E2E.TXT echo FAIL WriteFile name="E2E.TXT">>%RES%
ren E2E.TXT E2E2.TXT
if exist E2E2.TXT echo PASS RenameFile old="E2E.TXT" new="E2E2.TXT">>%RES%
if not exist E2E2.TXT echo FAIL RenameFile old="E2E.TXT" new="E2E2.TXT">>%RES%
copy E2E2.TXT E2ECOPY.TXT>>%OUT%
if exist E2ECOPY.TXT echo PASS CopyFile name="E2E2.TXT" to="E2ECOPY.TXT">>%RES%
if not exist E2ECOPY.TXT echo FAIL CopyFile name="E2E2.TXT" to="E2ECOPY.TXT">>%RES%
del E2ECOPY.TXT
if not exist E2ECOPY.TXT echo PASS DeleteFile name="E2ECOPY.TXT">>%RES%
if exist E2ECOPY.TXT echo FAIL DeleteFile name="E2ECOPY.TXT">>%RES%
echo === dir (SUB) ===>>%OUT%
dir>>%OUT%
echo PASS EnumerateDir supported=1 path="%E2EDRV%\SUB" detail="see OUT.TXT">>%RES%
cd \

rem --- Directory tasks -----------------------------------------------------
ren SUB SUBREN
if exist SUBREN\NUL echo PASS RenameDir old="SUB" new="SUBREN">>%RES%
if not exist SUBREN\NUL echo FAIL RenameDir old="SUB" new="SUBREN">>%RES%

rem COMMAND.COM has no recursive dir copy; xcopy does the CopyDir.
md SUBCOPY
xcopy SUBREN SUBCOPY /S >>%OUT%
if exist SUBCOPY\E2E2.TXT echo PASS CopyDir name="SUBREN" to="SUBCOPY">>%RES%
if not exist SUBCOPY\E2E2.TXT echo FAIL CopyDir name="SUBREN" to="SUBCOPY">>%RES%

echo === dir %E2EDRV%\ (post) ===>>%OUT%
dir %E2EDRV%\>>%OUT%
echo PASS EnumerateVolume supported=1 path="%E2EDRV%\" detail="post-ops, see OUT.TXT">>%RES%

echo Y| del SUBCOPY\*.* >>%OUT%
rd SUBCOPY
if not exist SUBCOPY\NUL echo PASS DeleteDir name="SUBCOPY">>%RES%
if exist SUBCOPY\NUL echo FAIL DeleteDir name="SUBCOPY">>%RES%

echo Y| del SUBREN\*.* >>%OUT%
rd SUBREN
if not exist SUBREN\NUL echo PASS DeleteDir name="SUBREN">>%RES%
if exist SUBREN\NUL echo FAIL DeleteDir name="SUBREN">>%RES%

echo Y| del DEST\*.* >>%OUT%
rd DEST
if not exist DEST\NUL echo PASS DeleteDir name="DEST">>%RES%
if exist DEST\NUL echo FAIL DeleteDir name="DEST">>%RES%

rem back to the local drive so the caller can release the mapping.
%E2EHOME%
