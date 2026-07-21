@echo=off
if EXIST SDLLCEW.LIB echo Patching small-model dll library.
if EXIST SDLLCEW.LIB ..\bin\lib /NOLOGO sdllcew.lib -+fastadd.obj,,
if EXIST CDLLCEW.LIB echo Patching compact-model dll library.
if EXIST CDLLCEW.LIB ..\bin\lib /NOLOGO cdllcew.lib -+fastadd.obj,,
if EXIST MDLLCEW.LIB echo Patching medium-model dll library.
if EXIST MDLLCEW.LIB ..\bin\lib /NOLOGO mdllcew.lib -+fastadd.obj,,
if EXIST LDLLCEW.LIB echo Patching large-model dll library.
if EXIST LDLLCEW.LIB ..\bin\lib /NOLOGO ldllcew.lib -+fastadd.obj,,
