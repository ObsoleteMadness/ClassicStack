/* A tiny always-on-top status window that shows the current action as the
 * script runs. Two purposes:
 *   1. UX — a human watching the emulator sees "Running script..." /
 *      "ListZones: MPPOpen..." instead of a blank screen.
 *   2. Diagnosis — because each AppleTalk step updates the label BEFORE it
 *      makes the (synchronous, potentially blocking) Toolbox call, a freeze
 *      leaves the last-attempted step frozen on screen. That tells us exactly
 *      which call wedged (MPPOpen? OpenXPP? GetZoneList?) — something
 *      results.txt can't, since a hung sync call never returns to write it.
 *
 * All calls are no-ops until StatusWinInit() has run, so it's safe to call
 * StatusWinSet() from deep in the AppleTalk code without a guard.
 */
#ifndef E2E_STATUSWIN_H
#define E2E_STATUSWIN_H

/* Opens the status window. Requires the Toolbox (InitGraf/InitWindows/etc.)
 * to be up already. Safe to call more than once. */
void StatusWinInit(void);

/* Optional echo sink: if set, every StatusWinSet(text) is ALSO reported here.
 * Wiring this to a durable, per-line-flushed logger (ResultsDebug via
 * MacFilesWriteLine + FlushVol) turns each on-screen step into a breadcrumb
 * that SURVIVES a hard emulator halt — so after a bus error the last line in
 * results.txt names the step that was in progress (the on-screen label can't be
 * read post-mortem, but the disk file can). Pass NULL to disable. */
typedef void (*StatusWinEchoProc)(const char *text);
void StatusWinSetEcho(StatusWinEchoProc echo);

/* Replaces the window's label with `text` and forces an immediate redraw
 * (and a spin through the event loop) so the update is visible before the
 * caller goes on to a blocking call. No-op if StatusWinInit hasn't run. */
void StatusWinSet(const char *text);

/* Closes the status window. */
void StatusWinClose(void);

#endif /* E2E_STATUSWIN_H */
