/* Windows implementation of the script/results file I/O seams (script.h
 * PlatformReadLineProc, results.h PlatformWriteLineProc), plus the wall-clock
 * timestamp string for the RESULT header. Uses the MSVC C runtime, so relative
 * paths resolve against the process's current directory — script.txt and
 * results.txt are expected to live next to the .EXE (or in whatever directory
 * the harness launches it from).
 *
 * Compiles unchanged under both MSVC 1.5 (Win16) and MSVC 2.0/4.0 (Win32):
 * only ANSI C stdio and <time.h> are used here.
 */
#ifndef E2E_WIN_FILES_H
#define E2E_WIN_FILES_H

#include <stdio.h>
#include "script.h"
#include "results.h"

/* Resolves `name` to an absolute path in the directory the running .EXE was
 * loaded from (derived from argv[0]) into `out` (outSize bytes). Ensures
 * script.txt/results.txt are read/written next to the program — i.e. on the
 * floppy — even when Program Manager / File Manager launched the app with a
 * different current directory. Falls back to the bare name if argv[0] has no
 * path component. */
void WinFilesResolveBesideExe(const char *argv0, const char *name,
                              char *out, int outSize);

/* Opens name for reading; returns NULL on failure (caller decides how to
 * report/abort — see main.c). Opened in binary mode so WinFilesReadLine sees
 * raw bytes and can treat \r, \n and \r\n uniformly regardless of how the
 * script file was authored. */
FILE *WinFilesOpenRead(const char *name);

/* Opens name for writing (truncates if it exists); returns NULL on failure.
 * Can't log to results.txt on failure (there's no open results file yet if
 * this is the results.txt open itself) — falls back to stderr. */
FILE *WinFilesOpenWrite(const char *name);

/* Closes file, reporting on stderr if the underlying fclose/flush failed
 * (e.g. the volume filled up mid-write). Returns 0 on success, non-zero on
 * failure (matches fclose's convention). */
int WinFilesClose(FILE *file);

/* PlatformReadLineProc: reads one line byte-by-byte, treating \r, \n and \r\n
 * all as line terminators (see the macOS tool's line-ending note — a plain
 * fgets misses bare \r and silently swallows several commands). Returns 0 on
 * success, non-zero at EOF/error. */
int WinFilesReadLine(void *file, char *buf, int bufSize);

/* PlatformWriteLineProc: writes one line + "\r\n", then fflush()es so partial
 * results survive a crash. CRLF keeps results.txt readable in Notepad on the
 * guest and in any modern editor on the host. */
void WinFilesWriteLine(void *file, const char *text);

/* Formats the current local date/time as "YYYY-MM-DD HH:MM:SS" into buf
 * (must be at least 20 bytes). */
void WinFilesFormatTimestamp(char *buf, int bufSize);

/* PlatformLogLineProc: logs the script line as a DEBUG entry in results.txt. */
void WinFilesLogLine(const char *line);

#endif /* E2E_WIN_FILES_H */
