/* macOS/AFP command set — registers protocol-specific handlers into the
 * portable ScriptCommand dispatch table. See tools/end-to-end plan for the
 * full command list and the equivalent Win16/Win32 SMB vocabulary.
 */
#ifndef E2E_COMMANDS_H
#define E2E_COMMANDS_H

#include "script.h"

/* The command table for this build, terminated by a {NULL, NULL} sentinel.
 * Implemented in commands.c; handlers live in ../afp/afp_client.c and are
 * added to this table incrementally as each stage of the AFP client lands
 * (see plan verification stages 2-7). Stage-1 skeleton ships with zero
 * registered commands — a script with only comments still produces a
 * valid RESULT v1 / DONE pair. */
extern const ScriptCommand gCommandTable[];

/* Emits environment diagnostics (AppleTalk driver version) as DEBUG lines into
 * results.txt before the script runs, so every results file records the stack
 * the client came up on. Also drops "before/after AtalkInit" breadcrumbs so a
 * hard fault in driver init is pinpointed by the last line in results.txt.
 * No-op when the AppleTalk command set isn't compiled in (AFPE2E_AFP_COMMANDS=0).
 * Safe to call once, after ResultsInit. */
void CommandsReportEnvironment(void);

#endif /* E2E_COMMANDS_H */
