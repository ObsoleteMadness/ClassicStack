/* Windows/SMB command set — registers protocol-specific handlers into the
 * portable ScriptCommand dispatch table (common/script.h). The vocabulary
 * mirrors the macOS/AFP tool's where the operations are the same (mount,
 * enumerate, create/write/rename/copy/delete files and dirs) and drops the
 * resource-fork commands (SMB has no forks). See
 * tools/end-to-end/RESULT-FORMAT.md for the shared output contract.
 */
#ifndef E2E_COMMANDS_H
#define E2E_COMMANDS_H

#include "script.h"

/* The command table for this build, terminated by a {NULL, NULL} sentinel. */
extern const ScriptCommand gCommandTable[];

/* Emits environment diagnostics (platform/LFN banner) as a DEBUG line into
 * results.txt before the script runs, so every results file records which
 * client (win16/win32) produced it. Safe to call once, after ResultsInit. */
void CommandsReportEnvironment(void);

#endif /* E2E_COMMANDS_H */
