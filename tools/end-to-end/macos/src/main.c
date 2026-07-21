#include <stdio.h>
#include <OSUtils.h>
#include <Quickdraw.h>
#include <Fonts.h>
#include <Windows.h>
#include <Menus.h>
#include <TextEdit.h>
#include <Dialogs.h>
#include <Memory.h>
#include "script/script.h"
#include "script/commands.h"
#include "results.h"
#include "platform/mac_files.h"
#include "platform/statuswin.h"

#if AFPE2E_SHUTDOWN_ON_EXIT
#include <ShutDown.h>
#endif

/* Every Apple AppleTalk sample (GetZoneList.c Initialize(), the AppleTalk
 * Libraries) runs inside a normal Mac application that has brought up the
 * Toolbox managers and expanded the heap BEFORE it touches .MPP/.ATP/.XPP or
 * even stdio. A Retro68 app only auto-inits the Toolbox when RetroConsole is
 * linked (InitConsole → InitGraf/InitFonts/InitWindows/InitMenus), and we
 * build with CONSOLE OFF — so nothing initialised the managers, and the app
 * was calling fopen()/GetTime()/MPPOpen() in an un-initialised environment
 * (the observed "freezes / runs slowly, nothing on the wire" with no server
 * traffic at all: the hang is here, before any AppleTalk packet is sent).
 * Do the standard init once, up front, exactly as the samples do. */
static void InitToolbox(void) {
    MaxApplZone();      /* expand the heap first (samples call this in main) */
    InitGraf(&qd.thePort);
    InitFonts();
    InitWindows();
    InitMenus();
    TEInit();
    InitDialogs(nil);
    InitCursor();
}

#define SCRIPT_FILE_NAME "script.txt"
#define RESULTS_FILE_NAME "results.txt"

/* How long to hold the console open (so a human watching the emulator can
 * read it) before ending the run, when something went wrong. 60 ticks/sec;
 * 10 sec is enough to read a screen of diagnostics without making an
 * unattended CI run wait needlessly on the success path (where we don't
 * pause at all). */
#define FAILURE_PAUSE_TICKS (10 * 60)

static void PauseBeforeExit(void) {
    unsigned long finalTicks;

    fprintf(stderr, "\n--- run had failures; pausing before exit ---\n");
    Delay(FAILURE_PAUSE_TICKS, &finalTicks);
}

/* By default the app just quits back to the Finder (return from main), so
 * the same booted emulator/machine can be reused for repeated test runs
 * without a full power cycle — much faster debugging loop, and it avoids
 * accidentally testing against a stale disk/emulator snapshot from a
 * previous run. Build with -DAFPE2E_SHUTDOWN_ON_EXIT=ON (see CMakeLists.txt)
 * to power the machine off at the end instead, e.g. for an unattended
 * harness run where nothing else needs the machine afterwards. */
static void EndRun(void) {
#if AFPE2E_SHUTDOWN_ON_EXIT
    ShutDwnPower();
#endif
}

/* StatusWinSet echo sink: mirror every on-screen status label into results.txt
 * as a durable, flushed-to-disk "STEP" breadcrumb. Because MacFilesWriteLine
 * FlushVol's after every line, the last STEP line in results.txt survives a
 * hard emulator halt and names exactly which step was in progress when it died
 * — which the (unreadable, post-mortem) on-screen label cannot. */
static void StatusEcho(const char *text) {
    char msg[SCRIPT_MAX_LINE + 8];
    sprintf(msg, "STEP %s", text);
    ResultsDebug(msg);
}

int main(int argc, char **argv) {
    FILE *scriptFile;
    FILE *resultsFile;
    char timestamp[24];
    int closeErr;

    (void)argc;
    (void)argv;

    InitToolbox();
    StatusWinInit();
    StatusWinSet("AFPE2E starting...");

    resultsFile = MacFilesOpenWrite(RESULTS_FILE_NAME);
    if (resultsFile == NULL) {
        /* Nowhere to report failure if we can't even open the results
         * file — MacFilesOpenWrite already reported this to stderr as a
         * last resort. Nothing more we can do but pause (in case a console
         * happens to be attached) before ending the run. */
        PauseBeforeExit();
        EndRun();
        return 1;
    }

    MacFilesFormatTimestamp(timestamp, sizeof(timestamp));
    ResultsInit(resultsFile, MacFilesWriteLine, timestamp);

    /* From here on, mirror every status-window label into results.txt as a
     * durable breadcrumb (see StatusEcho) so a hard crash leaves a trail on
     * disk. Registered right after ResultsInit so it can safely ResultsDebug. */
    StatusWinSetEcho(StatusEcho);

    ResultsDebug("AFPE2E starting");

    /* Record the AppleTalk driver version (and leave breadcrumbs around driver
     * init) before the script runs, so every results file says which stack the
     * client came up on. No-op when the AppleTalk command set isn't compiled in. */
    CommandsReportEnvironment();

    scriptFile = MacFilesOpenRead(SCRIPT_FILE_NAME);
    if (scriptFile == NULL) {
        ResultsFail("OpenScript", "could not open " SCRIPT_FILE_NAME);
    } else {
        char msg[48];
        int executed;
        StatusWinSet("Running script...");
        executed = ScriptRun(scriptFile, MacFilesReadLine, MacFilesLogLine, gCommandTable);
        sprintf(msg, "ScriptRun: %d command(s) executed", executed);
        ResultsDebug(msg);
        MacFilesClose(scriptFile);
    }

    StatusWinSet("Script finished.");
    ResultsFinish();
    StatusWinClose();

    closeErr = MacFilesClose(resultsFile);
    if (closeErr != 0) {
        fprintf(stderr, "WARNING: results.txt close failed — results may be incomplete\n");
    }

    if (ResultsHadFailure() || closeErr != 0) {
        PauseBeforeExit();
    }

    EndRun();
    return 0; /* unreached only if EndRun() shut the machine down */
}
