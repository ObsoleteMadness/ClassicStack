/* Result-line writer: appends PASS/FAIL/DONE lines to the results file,
 * flushed after every write so a mid-script crash leaves partial results
 * readable from the disk image. Portable format (see tools/end-to-end
 * plan "Shared script format"); file I/O itself goes through a platform
 * seam so this header stays platform-agnostic.
 */
#ifndef E2E_RESULTS_H
#define E2E_RESULTS_H

#define SCRIPT_RESULTS_LINE_MAX 800

/* Platform seam: appends `text` (NUL-terminated, no trailing newline needed)
 * as one line to the open results file and flushes it. */
typedef void (*PlatformWriteLineProc)(void *file, const char *text);

/* Call once before any commands run. `startedAt` is a platform-formatted
 * timestamp string (see plan: "RESULT v1 started=..."), already quoted. */
void ResultsInit(void *file, PlatformWriteLineProc writeLine, const char *startedAt);

/* `detail` may be NULL. Appends a PASS line: PASS <command> <detail> */
void ResultsPass(const char *command, const char *detail);

/* `detail` may be NULL. Appends a FAIL line: FAIL <command> <detail> */
void ResultsFail(const char *command, const char *detail);

/* Appends a DEBUG line: DEBUG <text>. Diagnostic trail written straight into
 * results.txt (rather than a console/stderr) so it survives even when a
 * platform's diagnostic console isn't available/visible (e.g. RetroConsole
 * failing to open a window under some emulator configs) — results.txt is
 * the one output channel this app has always reliably produced. */
void ResultsDebug(const char *text);

/* Call once after the script ends (including on abort). Appends:
 * DONE total=<n> pass=<n> fail=<n> */
void ResultsFinish(void);

/* True if any ResultsFail call has been made since the last ResultsInit.
 * Lets the caller decide whether to pause for a human to read the console
 * before doing something irreversible (e.g. shutting the machine down). */
int ResultsHadFailure(void);

#endif /* E2E_RESULTS_H */
