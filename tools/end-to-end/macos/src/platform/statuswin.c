#include <Quickdraw.h>
#include <Windows.h>
#include <Fonts.h>
#include <TextEdit.h>
#include <TextUtils.h>
#include <Events.h>
#include <string.h>
#include "statuswin.h"

/* A plain drawn window (no WIND resource needed — NewWindow with an explicit
 * rect) so the app has zero resource dependencies. We draw the label
 * ourselves in the update path rather than using a TE record; it's one line
 * of text, so DrawString is enough. */

static WindowPtr sWin = NULL;
static char sLabel[256] = "";
static StatusWinEchoProc sEcho = NULL;

void StatusWinSetEcho(StatusWinEchoProc echo) {
    sEcho = echo;
}

static void DrawLabel(void) {
    Rect r;

    if (sWin == NULL) {
        return;
    }
    SetPort(sWin);
    r = sWin->portRect;
    EraseRect(&r);
    MoveTo(12, 24);
    /* sLabel is a C string; convert to a Pascal string for DrawString. */
    {
        unsigned char pstr[256];
        int n = (int)strlen(sLabel);
        if (n > 255) {
            n = 255;
        }
        pstr[0] = (unsigned char)n;
        memcpy(&pstr[1], sLabel, (size_t)n);
        DrawString(pstr);
    }
}

void StatusWinInit(void) {
    Rect bounds;

    if (sWin != NULL) {
        return;
    }

    /* Top-centred so it's visible on any screen size. dBoxProc = a plain
     * modal-style frame with no title bar / close box (we manage its whole
     * lifetime). */
    SetRect(&bounds, 40, 40, 40 + 420, 40 + 44);
    sWin = NewWindow(NULL, &bounds, "\pAFPE2E", true, dBoxProc,
                     (WindowPtr)-1L, false, 0L);
    if (sWin != NULL) {
        SetPort(sWin);
        TextFont(systemFont); /* font ID 0 — always present, no resource needed */
        TextSize(12);
    }
}

void StatusWinSet(const char *text) {
    EventRecord event;
    int i;

    /* Durable breadcrumb FIRST, before any drawing or the (potentially
     * crash-prone) call this label precedes — so it's on disk even if we never
     * return from here. Runs regardless of whether the window opened. */
    if (sEcho != NULL) {
        sEcho(text);
    }

    if (sWin == NULL) {
        return;
    }

    strncpy(sLabel, text, sizeof(sLabel) - 1);
    sLabel[sizeof(sLabel) - 1] = '\0';

    DrawLabel();

    /* Give the Window/Event managers a few turns so the redraw actually
     * reaches the screen before the caller heads into a blocking call — and
     * so a watching human sees each step. We only PROCESS update events for
     * our own window; everything else is drained and ignored.
     *
     * Use SystemTask() + GetNextEvent(), NOT WaitNextEvent(): WaitNextEvent is
     * only available with MultiFinder / the System 7 Process Manager. On a
     * System 6 (or System 7 without it) boot — which our netboot/Mini vMac
     * images can be — WaitNextEvent is an UNIMPLEMENTED trap, and calling it
     * jumps to garbage → bus error at a semi-random high PC. That exactly
     * matches the crash we saw right after a StatusWinSet label (the PC even
     * moved when the failing label moved). SystemTask + GetNextEvent exist on
     * every classic system and are the correct pre-WNE event pump. */
    for (i = 0; i < 3; i++) {
        SystemTask();
        if (GetNextEvent(everyEvent, &event)) {
            if (event.what == updateEvt && (WindowPtr)event.message == sWin) {
                BeginUpdate(sWin);
                DrawLabel();
                EndUpdate(sWin);
            }
        }
    }
}

void StatusWinClose(void) {
    if (sWin != NULL) {
        DisposeWindow(sWin);
        sWin = NULL;
    }
}
