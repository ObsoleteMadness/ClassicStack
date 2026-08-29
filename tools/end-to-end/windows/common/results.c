#include <string.h>
#include <stdio.h>
#include "results.h"

static void *sFile;
static PlatformWriteLineProc sWriteLine;
static int sTotal;
static int sPass;
static int sFail;

static void EmitLine(const char *status, const char *command, const char *detail) {
    char line[SCRIPT_RESULTS_LINE_MAX];

    if (detail != NULL && detail[0] != '\0') {
        sprintf(line, "%s %s %s", status, command, detail);
    } else {
        sprintf(line, "%s %s", status, command);
    }
    sWriteLine(sFile, line);
}

void ResultsInit(void *file, PlatformWriteLineProc writeLine, const char *startedAt) {
    char header[SCRIPT_RESULTS_LINE_MAX];

    sFile = file;
    sWriteLine = writeLine;
    sTotal = 0;
    sPass = 0;
    sFail = 0;

    sprintf(header, "RESULT v1 started=\"%s\"", startedAt);
    sWriteLine(sFile, header);
}

void ResultsPass(const char *command, const char *detail) {
    sTotal++;
    sPass++;
    EmitLine("PASS", command, detail);
}

void ResultsFail(const char *command, const char *detail) {
    sTotal++;
    sFail++;
    EmitLine("FAIL", command, detail);
}

void ResultsFinish(void) {
    char line[SCRIPT_RESULTS_LINE_MAX];

    sprintf(line, "DONE total=%d pass=%d fail=%d", sTotal, sPass, sFail);
    sWriteLine(sFile, line);
}

int ResultsHadFailure(void) {
    return sFail > 0;
}

void ResultsDebug(const char *text) {
    char line[SCRIPT_RESULTS_LINE_MAX];

    sprintf(line, "DEBUG %s", text);
    sWriteLine(sFile, line);
}
