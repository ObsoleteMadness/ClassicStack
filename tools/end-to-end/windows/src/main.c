/* SMBE2E — ClassicStack SMB end-to-end test tool (Win16 / Win32).
 *
 * Reads script.txt (a plain-text command list, shared grammar with the macOS
 * AFP tool) from the current directory, runs each command against ClassicStack
 * over SMB via the real Windows redirector, and writes PASS/FAIL/DEBUG/DONE
 * lines to results.txt. Both files live next to the .EXE (on the floppy image
 * built by flopgen and mounted in the emulator). See
 * tools/end-to-end/RESULT-FORMAT.md for the output contract and
 * tools/end-to-end/windows/readme.md for the build/run workflow.
 *
 * A plain console main() serves both targets: the Win16 build links the
 * QuickWin runtime (a text window in Windows 3.x), the Win32 build is a console
 * subsystem app. Neither needs any GUI/Toolbox init — unlike the Mac tool.
 */
#include <stdio.h>
#include "script.h"
#include "results.h"
#include "win_files.h"
#include "commands.h"
#include "status.h"

#define SCRIPT_FILE_NAME  "script.txt"
#define RESULTS_FILE_NAME "results.txt"

/* StatusSet echo sink: mirror every on-screen status label into results.txt as
 * a durable, flushed-to-disk "STEP" breadcrumb (WinFilesWriteLine fflush()es
 * every line). If the run ends cleanly the last STEP line names the step in
 * progress; if a GPF loses the floppy write, the on-screen console line does.
 * Identical role to the macOS tool's StatusEcho. */
static void StatusEcho(const char *text) {
    char msg[SCRIPT_MAX_LINE + 8];
    sprintf(msg, "STEP %s", text);
    ResultsDebug(msg);
}

int main(int argc, char **argv) {
    FILE *scriptFile;
    FILE *resultsFile;
    char timestamp[24];
    char scriptPath[260];
    char resultsPath[260];
    const char *argv0;
    int closeErr;

    (void)argc;

    StatusInit();
    StatusSet("SMBE2E starting...");

    /* Anchor script.txt/results.txt to the .EXE's directory (the floppy), not
     * the launch CWD — Program Manager / File Manager launch with a working
     * directory that isn't the floppy, which would send results.txt somewhere
     * we never read back. See WinFilesResolveBesideExe. */
    argv0 = (argv != NULL) ? argv[0] : NULL;
    WinFilesResolveBesideExe(argv0, SCRIPT_FILE_NAME,  scriptPath,  sizeof(scriptPath));
    WinFilesResolveBesideExe(argv0, RESULTS_FILE_NAME, resultsPath, sizeof(resultsPath));
    StatusSetf("results -> %s", resultsPath);
    StatusSetf("script  <- %s", scriptPath);

    resultsFile = WinFilesOpenWrite(resultsPath);
    if (resultsFile == NULL) {
        /* Nowhere to report failure if we can't open results.txt — the open
         * helper already logged to stderr. Say so on the console too. */
        StatusSetf("FATAL: cannot open results file %s", resultsPath);
        return 1;
    }

    WinFilesFormatTimestamp(timestamp, sizeof(timestamp));
    ResultsInit(resultsFile, WinFilesWriteLine, timestamp);

    /* From here on, mirror every status label into results.txt as a durable
     * breadcrumb (see StatusEcho). Registered right after ResultsInit so the
     * echo can safely ResultsDebug. */
    StatusSetEcho(StatusEcho);

    ResultsDebug("SMBE2E starting");
    ResultsDebug(resultsPath);
    CommandsReportEnvironment();

    scriptFile = WinFilesOpenRead(scriptPath);
    if (scriptFile == NULL) {
        char detail[300];
        sprintf(detail, "could not open \"%s\"", scriptPath);
        ResultsFail("OpenScript", detail);
    } else {
        char msg[48];
        int executed;
        StatusSet("Running script...");
        executed = ScriptRun(scriptFile, WinFilesReadLine, WinFilesLogLine, gCommandTable);
        sprintf(msg, "ScriptRun: %d command(s) executed", executed);
        ResultsDebug(msg);
        WinFilesClose(scriptFile);
    }

    StatusSet("Script finished.");
    ResultsFinish();

    closeErr = WinFilesClose(resultsFile);
    if (closeErr != 0) {
        fprintf(stderr, "WARNING: results.txt close failed — results may be incomplete\n");
    }

    return 0;
}
