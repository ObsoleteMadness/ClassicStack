/* Live "current action" status line, the Windows counterpart of the macOS
 * tool's platform/statuswin. Two purposes, identical to the Mac one:
 *   1. UX — a human watching the emulator sees "Mount: WNetAddConnection..."
 *      scroll past instead of a blank window.
 *   2. Diagnosis — because each SMB step updates the status BEFORE it makes the
 *      (synchronous, potentially crash-prone) redirector call, a general
 *      protection fault leaves the last-attempted step on screen. That tells us
 *      exactly which call faulted (WNetAddConnection? FindFirstFile? CreateFile?)
 *      — something results.txt on the floppy can't, since a GPF loses the
 *      buffered/uncommitted disk writes.
 *
 * The Windows tool has no Toolbox window to manage: on Win16 it is a QuickWin
 * app whose text window IS stdout, and on Win32 it is a console app — so the
 * status line is simply printed (and flushed) to stdout. All calls are safe
 * before StatusInit(); StatusSet just prints.
 */
#ifndef E2E_STATUS_H
#define E2E_STATUS_H

/* Prepares the status channel (prints a header line). Safe to call once. */
void StatusInit(void);

/* Optional echo sink: if set, every StatusSet(text) is ALSO reported here.
 * Wired (in main) to a durable, per-line-flushed logger (ResultsDebug via
 * WinFilesWriteLine, which fflush()es every line) so each on-screen step also
 * becomes a breadcrumb in results.txt. On a clean crash the last STEP line in
 * results.txt names the step in progress; on a GPF that loses the floppy write,
 * the on-screen console line does. Pass NULL to disable. */
typedef void (*StatusEchoProc)(const char *text);
void StatusSetEcho(StatusEchoProc echo);

/* Sets the current-action status: prints `text` to the console, flushed
 * immediately so it reaches the screen before the caller proceeds into a
 * blocking/crash-prone call, and forwards it to the echo sink (if set) FIRST
 * so the durable breadcrumb is written even if this call never returns. */
void StatusSet(const char *text);

/* printf-style convenience over StatusSet, for the "op path=..." trace lines
 * the SMB back-ends emit. */
void StatusSetf(const char *fmt, ...);

#endif /* E2E_STATUS_H */
