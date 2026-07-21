#include <string.h>
#include "script.h"
#include "../results.h"

const char *ScriptArgsGet(const ScriptArgs *args, const char *name) {
    int i;

    for (i = 0; i < args->argCount; i++) {
        if (strcmp(args->args[i].name, name) == 0) {
            return args->args[i].value;
        }
    }
    return NULL;
}

static const char *SkipSpaces(const char *p) {
    while (*p == ' ' || *p == '\t') {
        p++;
    }
    return p;
}

/* Parses one whitespace-delimited token starting at *pp into out (up to
 * outSize-1 bytes), where a value may be a bare word or a "quoted string"
 * (spaces allowed inside quotes, no escape sequences). Advances *pp past
 * the token. Returns 0 on success. */
static int ReadToken(const char **pp, char *out, int outSize) {
    const char *p = SkipSpaces(*pp);
    int n = 0;

    if (*p == '\0') {
        *pp = p;
        return -1;
    }

    if (*p == '"') {
        p++;
        while (*p != '\0' && *p != '"') {
            if (n < outSize - 1) {
                out[n++] = *p;
            }
            p++;
        }
        if (*p == '"') {
            p++;
        }
    } else {
        while (*p != '\0' && *p != ' ' && *p != '\t' && *p != '=') {
            if (n < outSize - 1) {
                out[n++] = *p;
            }
            p++;
        }
    }

    out[n] = '\0';
    *pp = p;
    return 0;
}

int ScriptParseLine(const char *line, ScriptArgs *out) {
    const char *p;
    char cmd[SCRIPT_MAX_COMMAND_NAME];

    memset(out, 0, sizeof(*out));

    p = SkipSpaces(line);
    if (*p == '\0' || *p == '#') {
        return -1; /* blank or comment: nothing to dispatch */
    }

    if (ReadToken(&p, cmd, sizeof(cmd)) != 0) {
        return -1;
    }
    strcpy(out->command, cmd);

    for (;;) {
        char name[SCRIPT_MAX_ARG_NAME];
        char value[SCRIPT_MAX_ARG_VALUE];

        p = SkipSpaces(p);
        if (*p == '\0' || *p == '#') {
            break;
        }

        if (ReadToken(&p, name, sizeof(name)) != 0) {
            break;
        }

        value[0] = '\0';
        if (*p == '=') {
            p++;
            ReadToken(&p, value, sizeof(value));
        }

        if (out->argCount < SCRIPT_MAX_ARGS) {
            strcpy(out->args[out->argCount].name, name);
            strcpy(out->args[out->argCount].value, value);
            out->argCount++;
        }
    }

    return 0;
}

static ScriptHandlerProc FindHandler(const ScriptCommand *table, const char *name) {
    int i;

    for (i = 0; table[i].name != NULL; i++) {
        if (strcmp(table[i].name, name) == 0) {
            return table[i].handler;
        }
    }
    return NULL;
}

int ScriptRun(void *file, PlatformReadLineProc readLine, PlatformLogLineProc logLine,
              const ScriptCommand *table) {
    char line[SCRIPT_MAX_LINE];
    int executed = 0;

    while (readLine(file, line, sizeof(line)) == 0) {
        ScriptArgs args;
        ScriptHandlerProc handler;

        if (ScriptParseLine(line, &args) != 0) {
            continue;
        }

        if (logLine != NULL) {
            logLine(line);
        }

        handler = FindHandler(table, args.command);
        if (handler == NULL) {
            char detail[SCRIPT_MAX_LINE + 32];
            strcpy(detail, "unknown command, raw line=\"");
            strcat(detail, line);
            strcat(detail, "\"");
            ResultsFail(args.command, detail);
            continue;
        }

        handler(&args);
        executed++;
    }

    return executed;
}
