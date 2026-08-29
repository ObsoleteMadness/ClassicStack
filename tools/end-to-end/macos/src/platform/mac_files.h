/* macOS implementation of the script/results file I/O seams (script.h
 * PlatformReadLineProc, results.h PlatformWriteLineProc), plus the wall-clock
 * timestamp string for the RESULT header. Uses Retro68's Newlib stdio, which
 * resolves relative paths against the application's own folder — so a script
 * file living next to the .APPL on the same floppy opens with a bare
 * "script.txt". */
#ifndef E2E_MAC_FILES_H
#define E2E_MAC_FILES_H

#include <stdio.h>
#include "../script/script.h"
#include "../results.h"

/* Opens name for reading; returns NULL on failure (caller decides how to
 * report/abort — see main.c). Logs a DEBUG line to results.txt either way
 * (via ResultsDebug) — see mac_files.c for why results.txt, not a console,
 * is this app's diagnostic channel. */
FILE *MacFilesOpenRead(const char *name);

/* Opens name for writing (truncates if it exists); returns NULL on failure.
 * Can't log to results.txt on failure (there's no open results file yet if
 * this is the results.txt open itself) — falls back to stderr. */
FILE *MacFilesOpenWrite(const char *name);

/* Closes file, reporting (via stderr — there may be no results.txt handle
 * left to log into once this file itself is what's being closed) if the
 * underlying fclose/flush failed, e.g. because the volume filled up or was
 * ejected mid-write. Returns 0 on success, non-zero on failure (matches
 * fclose's convention). */
int MacFilesClose(FILE *file);

/* PlatformReadLineProc: reads one line via fgets, strips the trailing
 * newline. Returns 0 on success, non-zero at EOF/error. */
int MacFilesReadLine(void *file, char *buf, int bufSize);

/* PlatformWriteLineProc: writes one line + newline, then fflush()es so
 * partial results survive a crash. */
void MacFilesWriteLine(void *file, const char *text);

/* Formats the current date/time (via the Toolbox GetDateTime, converted with
 * the standard C library) as "YYYY-MM-DD HH:MM:SS" into buf (must be at
 * least 20 bytes). */
void MacFilesFormatTimestamp(char *buf, int bufSize);

/* PlatformLogLineProc: logs the script line as a DEBUG entry in results.txt. */
void MacFilesLogLine(const char *line);

#endif /* E2E_MAC_FILES_H */
