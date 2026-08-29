#include <windows.h>
#include <stdio.h>
#include <string.h>
#include <time.h>
#include "win_files.h"
#include "results.h"
#include "status.h"

/* Build "<dir-of-the-running-.EXE>\name" into out. Program Manager / File
 * Manager launch the .EXE with a working directory that is NOT the floppy
 * (typically the .EXE's own dir at best, C:\WINDOWS at worst), so a bare
 * "script.txt"/"results.txt" opened against the current directory can miss the
 * floppy entirely — the app runs but writes results.txt into the wrong place.
 * Anchoring both files to the directory the .EXE itself was loaded from keeps
 * them next to the program, i.e. on the floppy, regardless of the launch CWD.
 *
 * Getting the .EXE path differs by target:
 *   Win32 — GetModuleFileNameA(NULL,...) returns the full path of the current
 *           process's .EXE (the NULL "current module" convention).
 *   Win16 — that convention does NOT exist: GetModuleHandle takes a module-NAME
 *           string and GetModuleHandle(NULL) faults (it dereferences the null
 *           far pointer) — this was a GPF right after the window opened. The C
 *           runtime already hands us the full .EXE path in argv[0] under the
 *           QuickWin startup, so we use that. If argv0 has no directory
 *           component we fall back to the bare name (current directory). */
void WinFilesResolveBesideExe(const char *argv0, const char *name,
                              char *out, int outSize) {
    const char *base;
    const char *p;
    const char *lastSep = NULL;
    int dirLen;
#ifdef WIN32
    char exePath[260];
    DWORD n;

    n = GetModuleFileNameA(NULL, exePath, sizeof(exePath));
    base = (n > 0 && n < sizeof(exePath)) ? exePath : name;
#else
    /* Win16: argv[0] is the full module path; no GetModuleFileName call. */
    base = (argv0 != NULL && argv0[0] != '\0') ? argv0 : name;
#endif

    for (p = base; *p != '\0'; p++) {
        if (*p == '\\' || *p == '/' || *p == ':') {
            lastSep = p; /* keep the last path separator (or drive colon) */
        }
    }

    if (lastSep == NULL) {
        /* No directory component — use the name as-is (current directory). */
        strncpy(out, name, outSize - 1);
        out[outSize - 1] = '\0';
        return;
    }

    /* Copy the directory prefix through and including the separator, then
     * append the file name. A trailing ':' ("A:file") or '\\'/'/' is a valid
     * boundary to append after. */
    dirLen = (int)(lastSep - base) + 1;
    if (dirLen > outSize - 1) {
        dirLen = outSize - 1;
    }
    memcpy(out, base, (size_t)dirLen);
    out[dirLen] = '\0';
    strncat(out, name, (size_t)(outSize - 1 - dirLen));
    out[outSize - 1] = '\0';
}

FILE *WinFilesOpenRead(const char *name) {
    FILE *f = fopen(name, "rb");
    if (f == NULL) {
        char msg[128];
        sprintf(msg, "WinFilesOpenRead: could not open '%s'", name);
        ResultsDebug(msg);
    }
    return f;
}

FILE *WinFilesOpenWrite(const char *name) {
    FILE *f = fopen(name, "wb");
    if (f == NULL) {
        /* No results.txt handle to log into yet (this open often *is* the
         * results.txt open) — stderr is the only fallback. */
        fprintf(stderr, "WinFilesOpenWrite: could not open '%s'\n", name);
    }
    return f;
}

int WinFilesClose(FILE *file) {
    int rc;
    if (file == NULL) {
        return 0;
    }
    rc = fclose(file);
    if (rc != 0) {
        fprintf(stderr, "WinFilesClose: fclose failed (rc=%d)\n", rc);
    }
    return rc;
}

/* Reads one line, treating \r, \n and \r\n all as terminators. Consumes a
 * trailing \n after a \r so \r\n counts as a single line break. Skips a
 * leading UTF-8/BOM byte-order-mark only if it opens the very first read
 * (harmless if a script was saved as UTF-8 with a BOM by a host editor). */
int WinFilesReadLine(void *filePtr, char *buf, int bufSize) {
    FILE *file = (FILE *)filePtr;
    int n = 0;
    int c;

    c = fgetc(file);
    if (c == EOF) {
        return -1; /* nothing left */
    }

    for (;;) {
        if (c == EOF) {
            break;
        }
        if (c == '\r') {
            int next = fgetc(file);
            if (next != '\n' && next != EOF) {
                ungetc(next, file);
            }
            break;
        }
        if (c == '\n') {
            break;
        }
        if (n < bufSize - 1) {
            buf[n++] = (char)c;
        }
        c = fgetc(file);
    }

    buf[n] = '\0';
    return 0;
}

void WinFilesWriteLine(void *filePtr, const char *text) {
    FILE *file = (FILE *)filePtr;
    fputs(text, file);
    fputs("\r\n", file);
    fflush(file);
}

void WinFilesFormatTimestamp(char *buf, int bufSize) {
    time_t now;
    struct tm *lt;

    if (bufSize < 20) {
        if (bufSize > 0) {
            buf[0] = '\0';
        }
        return;
    }

    time(&now);
    lt = localtime(&now);
    if (lt == NULL) {
        strcpy(buf, "0000-00-00 00:00:00");
        return;
    }
    sprintf(buf, "%04d-%02d-%02d %02d:%02d:%02d",
            lt->tm_year + 1900, lt->tm_mon + 1, lt->tm_mday,
            lt->tm_hour, lt->tm_min, lt->tm_sec);
}

void WinFilesLogLine(const char *line) {
    char msg[SCRIPT_MAX_LINE + 8];
    sprintf(msg, "line %s", line);
    ResultsDebug(msg);
    /* Show the command about to run on the console too, so the on-screen trace
     * reads "<command>" then the SMB step(s) it drives — and a GPF leaves the
     * command in flight visible. StatusSet also breadcrumbs it to results.txt. */
    StatusSetf("run: %s", line);
}
