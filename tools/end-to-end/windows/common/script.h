/* Portable command-script core.
 *
 * Platform- and protocol-agnostic: reads a script file a line at a time
 * through the PlatformReadLine seam, tokenizes "CommandName key=value
 * key2=\"quoted value\"" lines, and dispatches to a handler registered in a
 * ScriptCommand table. Neither this file nor script.c may reference AFP,
 * SMB, or any Toolbox/Win32 API — that is the point of the split (see
 * tools/end-to-end plan). This is the same core used by the macOS/AFP tool
 * under tools/end-to-end/macos/src/script; it is intended to stay
 * byte-for-byte identical between the two trees so a fix in one applies to
 * the other verbatim.
 */
#ifndef E2E_SCRIPT_H
#define E2E_SCRIPT_H

#define SCRIPT_MAX_LINE 256
#define SCRIPT_MAX_ARGS 8
#define SCRIPT_MAX_ARG_NAME 32
#define SCRIPT_MAX_ARG_VALUE 200
#define SCRIPT_MAX_COMMAND_NAME 32

typedef struct {
    char name[SCRIPT_MAX_ARG_NAME];
    char value[SCRIPT_MAX_ARG_VALUE];
} ScriptArg;

typedef struct {
    char command[SCRIPT_MAX_COMMAND_NAME];
    ScriptArg args[SCRIPT_MAX_ARGS];
    int argCount;
} ScriptArgs;

/* Looks up an arg by name; returns NULL if not present (handlers apply
 * their own default). Case-sensitive — scripts use exact command vocabulary. */
const char *ScriptArgsGet(const ScriptArgs *args, const char *name);

typedef void (*ScriptHandlerProc)(const ScriptArgs *args);

typedef struct {
    const char *name;
    ScriptHandlerProc handler;
} ScriptCommand;

/* Platform seam: fills buf (up to bufSize-1 bytes) with the next line from
 * the open script file, NUL-terminated, newline stripped. Returns 0 on
 * success, non-zero at EOF or on error. Implemented per-platform (e.g.
 * src/win_files.c over the C runtime). */
typedef int (*PlatformReadLineProc)(void *file, char *buf, int bufSize);

/* Optional platform seam: called with the raw script line just before it is
 * dispatched (blank/comment lines are not passed). Lets each platform show
 * live progress without script.c knowing anything about consoles/stdio.
 * Pass NULL to disable. */
typedef void (*PlatformLogLineProc)(const char *line);

/* Runs every line from `file` through PlatformReadLine + the tokenizer,
 * dispatching matched commands against `table` (NULL-terminated by a
 * {NULL, NULL} sentinel entry). Unknown commands and parse errors are
 * reported via ResultsFail (see results.h) and do not abort the run.
 * `logLine` may be NULL. Returns the number of commands executed. */
int ScriptRun(void *file, PlatformReadLineProc readLine, PlatformLogLineProc logLine,
              const ScriptCommand *table);

/* Tokenizes one already-read line into cmd/args. Returns 0 on success,
 * non-zero if the line is blank/comment-only (nothing to dispatch) or
 * malformed. Exposed for unit testing. */
int ScriptParseLine(const char *line, ScriptArgs *out);

#endif /* E2E_SCRIPT_H */
