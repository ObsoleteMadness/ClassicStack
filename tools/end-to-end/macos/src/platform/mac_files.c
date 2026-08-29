#include <string.h>
#include <stdio.h>
#include <errno.h>
#include <DateTimeUtils.h>
#include <Files.h>   /* FlushVol */
#include "mac_files.h"
#include "../results.h"

/* Diagnostics in this file go into results.txt (via ResultsDebug) rather
 * than stderr/a console: a RetroConsole window has proven unreliable to see
 * in some emulator configs (didn't appear at all in Mini vMac testing), and
 * results.txt is the one output channel this app always reliably produces
 * and we can always read back. Two call sites can't self-report this way —
 * MacFilesOpenWrite("results.txt") failing (there's no results.txt to write
 * the diagnostic into) and MacFilesWriteLine failing (it IS the write path)
 * — those keep a bare fprintf(stderr, ...) as a last resort; it costs
 * nothing if no console is attached. */

FILE *MacFilesOpenRead(const char *name) {
    FILE *f = fopen(name, "r");
    char msg[64];

    if (f == NULL) {
        sprintf(msg, "OpenRead FAILED: \"%s\" errno=%d", name, errno);
    } else {
        sprintf(msg, "OpenRead ok: \"%s\"", name);
    }
    ResultsDebug(msg);
    return f;
}

FILE *MacFilesOpenWrite(const char *name) {
    FILE *f = fopen(name, "w");
    if (f == NULL) {
        fprintf(stderr, "OpenWrite FAILED: \"%s\" errno=%d\n", name, errno);
    }
    return f;
}

int MacFilesClose(FILE *file) {
    int rc;

    if (file == NULL) {
        return 0;
    }

    rc = fclose(file);
    if (rc != 0) {
        fprintf(stderr, "Close FAILED errno=%d\n", errno);
    }
    return rc;
}

/* Reads one line ourselves, byte at a time, treating CR, LF, and CRLF all
 * as a line terminator. We can't rely on fgets here: fgets only recognizes
 * '\n', but scripts.txt gets copied onto the disk image with hcopy -t,
 * which (correctly, for a classic Mac text file) translates line endings
 * to bare '\r' — so a plain fgets never finds a line boundary and instead
 * reads clean through multiple lines up to the buffer limit. Confirmed via
 * the fgets-raw-bytes diagnostic this replaced: it returned
 * "...ShutdownMachine.\r\rEnumer" (255 bytes, mid-word) followed by
 * "ateServers zone=*\rGetServerInfo\r" on the next call. */
int MacFilesReadLine(void *file, char *buf, int bufSize) {
    FILE *f = (FILE *)file;
    int n = 0;
    int c;

    c = fgetc(f);
    if (c == EOF) {
        return -1;
    }

    while (c != EOF && c != '\n' && c != '\r') {
        if (n < bufSize - 1) {
            buf[n++] = (char)c;
        }
        c = fgetc(f);
    }
    buf[n] = '\0';

    /* Swallow a paired LF after a CR (CRLF) so a CRLF-ending file doesn't
     * produce a spurious empty line on the next read. */
    if (c == '\r') {
        c = fgetc(f);
        if (c != '\n' && c != EOF) {
            ungetc(c, f);
        }
    }

    return 0;
}

void MacFilesWriteLine(void *file, const char *text) {
    if (fputs(text, (FILE *)file) == EOF) {
        fprintf(stderr, "WriteLine FAILED (fputs) errno=%d: \"%s\"\n", errno, text);
        return;
    }
    fputc('\n', (FILE *)file);
    if (fflush((FILE *)file) != 0) {
        fprintf(stderr, "WriteLine FAILED (fflush) errno=%d: \"%s\"\n", errno, text);
    }

    /* fflush only pushes the C stdio buffer into the File Manager; the File
     * Manager still holds the volume's blocks in its OWN cache and doesn't write
     * them back to the physical (emulated) disk until FlushVol / the volume is
     * unmounted. On a HARD emulator halt (bus error) that cache is lost, so
     * without this every diagnostic line we "wrote" vanishes — which is exactly
     * why a crash run produced an EMPTY results.txt despite the breadcrumbs.
     * FlushVol(NULL, 0) flushes the default volume (the app's floppy) to disk
     * after every line, so results.txt on the image is current up to the last
     * line written even if the very next call crashes. Slow, but this is a
     * diagnostic harness and durability of the last breadcrumb is the point. */
    FlushVol(NULL, 0);
}

void MacFilesLogLine(const char *line) {
    char msg[SCRIPT_MAX_LINE + 16];
    sprintf(msg, "> [%d] %s", (int)strlen(line), line);
    ResultsDebug(msg);
}

void MacFilesFormatTimestamp(char *buf, int bufSize) {
    DateTimeRec d;

    GetTime(&d);
    /* DateTimeRec.year is the actual calendar year (not offset), month is
     * 1-based, day 1-based, hour/minute/second are 0-based 24h — matches
     * the "YYYY-MM-DD HH:MM:SS" layout directly, no epoch math needed. */
    sprintf(buf, "%04d-%02d-%02d %02d:%02d:%02d",
            d.year, d.month, d.day, d.hour, d.minute, d.second);
    (void)bufSize;
}
