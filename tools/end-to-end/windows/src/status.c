/* Console-backed status line — see status.h. Mirrors the macOS statuswin.c
 * echo-first ordering: the durable breadcrumb is emitted BEFORE the on-screen
 * print, so it lands even if the call this label precedes never returns. */
#include <stdio.h>
#include <stdarg.h>
#include <string.h>
#ifndef WIN32
#include <io.h>      /* Win16 QuickWin: _wsetexit / _WINEXITNOPERSIST */
#endif
#include "status.h"

static StatusEchoProc sEcho = NULL;

void StatusInit(void) {
#ifndef WIN32
    /* Win16 QuickWin: close the text window automatically when main() returns
     * instead of leaving it up waiting for the user to close it by hand. The
     * results are already on results.txt (the floppy), so there's nothing to
     * read on screen after the run. _wsetexit is a QuickWin-only CRT API (no
     * Win32 console equivalent — a console window closes on exit anyway).
     * Set before any output so it applies even on the early-return paths. */
    _wsetexit(_WINEXITNOPERSIST);
#endif

    /* Emit to stdout FIRST, then unbuffer. Under the MSVC QuickWin runtime the
     * text window and its stream are wired lazily on the first stdio *output*
     * (_InitEasyWin); calling setvbuf(stdout,...) before that first write can
     * deref an as-yet-uninitialised stream and GPF (the frame window appears
     * but nothing prints, then it faults). Writing the header line first forces
     * the window/stream into existence, after which unbuffering is safe. */
    fputs("SMBE2E: live status ---------------------------------------\n", stdout);
    fflush(stdout);
    setvbuf(stdout, NULL, _IONBF, 0);
}

void StatusSetEcho(StatusEchoProc echo) {
    sEcho = echo;
}

void StatusSet(const char *text) {
    /* Durable breadcrumb FIRST, before the on-screen print and before the
     * (potentially crash-prone) call this label precedes — so it is on disk
     * even if we never return from here. */
    if (sEcho != NULL) {
        sEcho(text);
    }
    fputs("  ", stdout);
    fputs(text, stdout);
    fputc('\n', stdout);
    fflush(stdout);
}

void StatusSetf(const char *fmt, ...) {
    char buf[256];
    va_list ap;

    va_start(ap, fmt);
    /* MSVC 1.x has no vsnprintf; the callers all pass short, bounded strings
     * (an op name plus one path, well under 256), so vsprintf is safe here. */
    vsprintf(buf, fmt, ap);
    va_end(ap);
    StatusSet(buf);
}
