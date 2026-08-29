#include <stddef.h>
#include <stdio.h>
#include <string.h>
#include "commands.h"
#include "smb.h"
#include "results.h"

/* ---- Cross-command session state -----------------------------------------
 * A script is a linear session: EnumerateServers/EnumerateShares discover the
 * server, Mount maps \\server\share to a drive letter, then the file/dir
 * commands operate "inside" that mapped drive at a current relative directory.
 * The state lives here; each command reads/updates it. Paths handed to the SMB
 * layer are always absolute ("<drive><relative>") so the redirector resolves
 * them exactly as File Manager/Explorer would. */

#define DEFAULT_DRIVE "N:"

static char sServer[SMB_MAX_NAME] = "";
static char sShare[SMB_MAX_NAME]  = "";
static char sDrive[8]             = "";   /* e.g. "N:"; empty = not mounted */
static char sCurRel[SMB_MAX_PATH] = "";   /* relative dir under the drive, e.g. "\\sub"; "" = root */
static int  sMounted              = 0;

/* Shared scratch for the detail= portion of result lines. */
static char sDetail[SCRIPT_RESULTS_LINE_MAX];

/* Compose an absolute path for `name` under the current directory into `out`.
 * sCurRel is "" at the share root and "\sub\deeper" otherwise (always with a
 * leading backslash when non-empty), so the drive+rel+name concatenation is a
 * well-formed absolute path in every case. name==NULL/"" yields the current
 * directory itself; at the root that is "N:\" (an explicit trailing backslash,
 * so FindFirst/_dos_findfirst target the top level, not the drive's per-process
 * current directory). */
static void CurPath(const char *name, char *out, int outSize) {
    if (name != NULL && name[0] != '\0') {
        _snprintf(out, outSize, "%s%s\\%s", sDrive, sCurRel, name);
    } else if (sCurRel[0] != '\0') {
        _snprintf(out, outSize, "%s%s", sDrive, sCurRel);
    } else {
        _snprintf(out, outSize, "%s\\", sDrive);
    }
    out[outSize - 1] = '\0';
}

/* ---- Environment ---------------------------------------------------------- */

void CommandsReportEnvironment(void) {
    char msg[96];
    sprintf(msg, "env: platform=%s", SmbPlatformName());
    ResultsDebug(msg);
}

/* ---- Guard for volume-scoped commands ------------------------------------ */

static int RequireMount(const char *commandName) {
    if (!sMounted) {
        ResultsFail(commandName, "detail=\"no share mounted\"");
        return 1;
    }
    return 0;
}

/* ---- Discovery ------------------------------------------------------------ */

typedef struct {
    char list[512];
    int  p;
    int  count;
} NameList;

static void CollectName(const char *name, void *ctx) {
    NameList *nl = (NameList *)ctx;
    if (nl->p < (int)sizeof(nl->list) - 40) {
        nl->p += _snprintf(&nl->list[nl->p], sizeof(nl->list) - nl->p,
                           "%s%s", nl->count > 0 ? "," : "", name);
    }
    nl->count++;
}

static void CmdEnumerateServers(const ScriptArgs *args) {
    NameList nl;
    unsigned long err = 0;
    int rc;
    (void)args;

    nl.list[0] = '\0';
    nl.p = 0;
    nl.count = 0;

    rc = SmbEnumerateServers(CollectName, &nl, &err);
    if (rc == SMB_UNSUPPORTED) {
        ResultsPass("EnumerateServers",
                    "supported=0 detail=\"enumeration not available on this platform\"");
        return;
    }
    if (rc != SMB_OK) {
        sprintf(sDetail, "err=%lu detail=\"WNet enumeration failed\"", err);
        ResultsFail("EnumerateServers", sDetail);
        return;
    }
    sprintf(sDetail, "supported=1 count=%d list=\"%s\"", nl.count, nl.list);
    ResultsPass("EnumerateServers", sDetail);
}

static void CmdEnumerateShares(const ScriptArgs *args) {
    const char *server = ScriptArgsGet(args, "server");
    NameList nl;
    unsigned long err = 0;
    int rc;

    if (server == NULL || server[0] == '\0') {
        ResultsFail("EnumerateShares", "detail=\"missing server= argument\"");
        return;
    }

    nl.list[0] = '\0';
    nl.p = 0;
    nl.count = 0;

    rc = SmbEnumerateShares(server, CollectName, &nl, &err);
    if (rc == SMB_UNSUPPORTED) {
        ResultsPass("EnumerateShares",
                    "supported=0 detail=\"enumeration not available on this platform\"");
        return;
    }
    if (rc != SMB_OK) {
        sprintf(sDetail, "err=%lu server=\"%s\" detail=\"share enumeration failed\"",
                err, server);
        ResultsFail("EnumerateShares", sDetail);
        return;
    }
    sprintf(sDetail, "supported=1 server=\"%s\" shares=%d list=\"%s\"",
            server, nl.count, nl.list);
    ResultsPass("EnumerateShares", sDetail);
}

/* ---- Mount / unmount ------------------------------------------------------ */

static void CmdMount(const ScriptArgs *args) {
    const char *server = ScriptArgsGet(args, "server");
    const char *share  = ScriptArgsGet(args, "share");
    const char *drive  = ScriptArgsGet(args, "drive");
    const char *user   = ScriptArgsGet(args, "user");
    const char *pass   = ScriptArgsGet(args, "pass");
    char driveBuf[8];
    unsigned long err = 0;
    int rc;

    if (server == NULL || server[0] == '\0' || share == NULL || share[0] == '\0') {
        ResultsFail("Mount", "detail=\"need server= and share= arguments\"");
        return;
    }
    if (sMounted) {
        ResultsFail("Mount", "detail=\"a share is already mounted; unmount first\"");
        return;
    }

    /* Default to N: when the script doesn't name a drive letter. */
    strncpy(driveBuf, (drive != NULL && drive[0] != '\0') ? drive : DEFAULT_DRIVE,
            sizeof(driveBuf) - 1);
    driveBuf[sizeof(driveBuf) - 1] = '\0';

    rc = SmbMount(server, share, user, pass, driveBuf, sizeof(driveBuf), &err);
    if (rc != SMB_OK) {
        sprintf(sDetail, "err=%lu server=\"%s\" share=\"%s\" drive=\"%s\"",
                err, server, share, driveBuf);
        ResultsFail("Mount", sDetail);
        return;
    }

    strncpy(sServer, server, sizeof(sServer) - 1);
    strncpy(sShare, share, sizeof(sShare) - 1);
    strncpy(sDrive, driveBuf, sizeof(sDrive) - 1);
    sCurRel[0] = '\0';
    sMounted = 1;

    sprintf(sDetail, "server=\"%s\" share=\"%s\" drive=\"%s\"", server, share, driveBuf);
    ResultsPass("Mount", sDetail);
}

static void CmdUnmount(const ScriptArgs *args) {
    unsigned long err = 0;
    int rc;
    (void)args;

    if (RequireMount("Unmount")) {
        return;
    }
    rc = SmbUnmount(sDrive, &err);
    if (rc != SMB_OK) {
        sprintf(sDetail, "err=%lu drive=\"%s\"", err, sDrive);
        ResultsFail("Unmount", sDetail);
        return;
    }
    sMounted = 0;
    sDrive[0] = '\0';
    sCurRel[0] = '\0';
    ResultsPass("Unmount", NULL);
}

static void CmdDisconnect(const ScriptArgs *args) {
    (void)args;
    if (sMounted) {
        unsigned long err = 0;
        SmbUnmount(sDrive, &err);
        sMounted = 0;
        sDrive[0] = '\0';
    }
    sServer[0] = '\0';
    sShare[0] = '\0';
    ResultsPass("Disconnect", NULL);
}

/* ---- Enumeration ---------------------------------------------------------- */

typedef struct {
    int  files;
    int  dirs;
    char command[SCRIPT_MAX_COMMAND_NAME];
} EnumCtx;

/* Each entry is emitted as its own PASS line so the standardized per-entry
 * format (RESULT-FORMAT.md) is preserved verbatim and the harness can diff
 * entries individually. A trailing summary PASS line gives the counts. */
static void EmitEntry(const SmbDirEntry *entry, void *ctx) {
    EnumCtx *e = (EnumCtx *)ctx;
    char line[SCRIPT_RESULTS_LINE_MAX];

    SmbFormatEntry(entry, line, sizeof(line));
    ResultsPass(e->command, line);
    if (entry->isDir) {
        e->dirs++;
    } else {
        e->files++;
    }
}

static void EnumeratePath(const char *command, const char *absPath) {
    EnumCtx e;
    unsigned long err = 0;
    int rc;

    e.files = 0;
    e.dirs = 0;
    strncpy(e.command, command, sizeof(e.command) - 1);
    e.command[sizeof(e.command) - 1] = '\0';

    rc = SmbEnumerate(absPath, EmitEntry, &e, &err);
    if (rc != SMB_OK) {
        sprintf(sDetail, "err=%lu detail=\"enumerate failed\" path=\"%s\"", err, absPath);
        ResultsFail(command, sDetail);
        return;
    }
    sprintf(sDetail, "entries=%d files=%d dirs=%d path=\"%s\"",
            e.files + e.dirs, e.files, e.dirs, absPath);
    ResultsPass(command, sDetail);
}

static void CmdEnumerateVolume(const ScriptArgs *args) {
    char path[SMB_MAX_PATH];
    (void)args;
    if (RequireMount("EnumerateVolume")) {
        return;
    }
    _snprintf(path, sizeof(path), "%s\\", sDrive);
    path[sizeof(path) - 1] = '\0';
    EnumeratePath("EnumerateVolume", path);
}

static void CmdEnumerateDir(const ScriptArgs *args) {
    const char *name = ScriptArgsGet(args, "name");
    char path[SMB_MAX_PATH];

    if (RequireMount("EnumerateDir")) {
        return;
    }
    /* No name = current directory; a name enumerates that named subdir without
     * changing into it. */
    if (name == NULL || name[0] == '\0') {
        CurPath(NULL, path, sizeof(path));
    } else {
        CurPath(name, path, sizeof(path));
    }
    EnumeratePath("EnumerateDir", path);
}

/* ---- Current-directory navigation ---------------------------------------- */

static void CmdSetDir(const ScriptArgs *args) {
    const char *name = ScriptArgsGet(args, "name");
    char path[SMB_MAX_PATH];
    SmbDirEntry st;
    unsigned long err = 0;
    int rc;

    if (RequireMount("SetDir")) {
        return;
    }
    if (name == NULL || name[0] == '\0') {
        ResultsFail("SetDir", "detail=\"missing name= argument\"");
        return;
    }
    /* Resolve the named subdirectory of the current directory; SetDir only
     * enters an existing directory (CreateDir creates). */
    CurPath(name, path, sizeof(path));
    rc = SmbStat(path, &st, &err);
    if (rc != SMB_OK || !st.isDir) {
        sprintf(sDetail, "detail=\"cannot enter directory '%s'\"", name);
        ResultsFail("SetDir", sDetail);
        return;
    }
    /* Append to the relative path. */
    {
        char newRel[SMB_MAX_PATH];
        _snprintf(newRel, sizeof(newRel), "%s\\%s", sCurRel, name);
        newRel[sizeof(newRel) - 1] = '\0';
        strncpy(sCurRel, newRel, sizeof(sCurRel) - 1);
        sCurRel[sizeof(sCurRel) - 1] = '\0';
    }
    sprintf(sDetail, "name=\"%s\" path=\"%s%s\"", name, sDrive, sCurRel);
    ResultsPass("SetDir", sDetail);
}

static void CmdSetDirRoot(const ScriptArgs *args) {
    (void)args;
    if (RequireMount("SetDirRoot")) {
        return;
    }
    sCurRel[0] = '\0';
    sprintf(sDetail, "path=\"%s\\\"", sDrive);
    ResultsPass("SetDirRoot", sDetail);
}

/* ---- File operations ------------------------------------------------------ */

static void CmdCreateFile(const ScriptArgs *args) {
    const char *name = ScriptArgsGet(args, "name");
    char path[SMB_MAX_PATH];
    unsigned long err = 0;
    int rc;

    if (RequireMount("CreateFile")) {
        return;
    }
    if (name == NULL || name[0] == '\0') {
        ResultsFail("CreateFile", "detail=\"missing name= argument\"");
        return;
    }
    CurPath(name, path, sizeof(path));
    rc = SmbCreateFile(path, &err);
    if (rc != SMB_OK) {
        sprintf(sDetail, "err=%lu name=\"%s\"", err, name);
        ResultsFail("CreateFile", sDetail);
        return;
    }
    sprintf(sDetail, "name=\"%s\"", name);
    ResultsPass("CreateFile", sDetail);
}

/* Write data to a file and verify by reading it back — same round-trip check
 * the macOS tool does (minus the resource fork, which SMB has no concept of). */
static void CmdWriteFile(const ScriptArgs *args) {
    const char *name = ScriptArgsGet(args, "name");
    const char *data = ScriptArgsGet(args, "data");
    char path[SMB_MAX_PATH];
    char verify[256];
    unsigned long err = 0;
    unsigned long len;
    long got;
    int rc;

    if (RequireMount("WriteFile")) {
        return;
    }
    if (name == NULL || name[0] == '\0') {
        ResultsFail("WriteFile", "detail=\"missing name= argument\"");
        return;
    }
    if (data == NULL) {
        data = "";
    }
    len = (unsigned long)strlen(data);
    CurPath(name, path, sizeof(path));

    rc = SmbWriteFile(path, data, len, &err);
    if (rc != SMB_OK) {
        sprintf(sDetail, "err=%lu name=\"%s\"", err, name);
        ResultsFail("WriteFile", sDetail);
        return;
    }

    got = SmbReadFile(path, verify, sizeof(verify) - 1, &err);
    if (got < 0) {
        sprintf(sDetail, "wrote=%lu name=\"%s\" readback_err=%ld", len, name, got);
        ResultsFail("WriteFile", sDetail);
        return;
    }
    verify[got] = '\0';
    if ((unsigned long)got != len || memcmp(verify, data, (size_t)len) != 0) {
        sprintf(sDetail, "wrote=%lu readback=%ld name=\"%s\" detail=\"mismatch\"",
                len, got, name);
        ResultsFail("WriteFile", sDetail);
        return;
    }
    sprintf(sDetail, "name=\"%s\" bytes=%lu verified=1", name, len);
    ResultsPass("WriteFile", sDetail);
}

static void CmdRenameFile(const ScriptArgs *args) {
    const char *oldName = ScriptArgsGet(args, "old");
    const char *newName = ScriptArgsGet(args, "new");
    char oldPath[SMB_MAX_PATH];
    char newPath[SMB_MAX_PATH];
    unsigned long err = 0;
    int rc;

    if (RequireMount("RenameFile")) {
        return;
    }
    if (oldName == NULL || newName == NULL || oldName[0] == '\0' || newName[0] == '\0') {
        ResultsFail("RenameFile", "detail=\"need old= and new= arguments\"");
        return;
    }
    CurPath(oldName, oldPath, sizeof(oldPath));
    CurPath(newName, newPath, sizeof(newPath));
    rc = SmbRename(oldPath, newPath, &err);
    if (rc != SMB_OK) {
        sprintf(sDetail, "err=%lu old=\"%s\" new=\"%s\"", err, oldName, newName);
        ResultsFail("RenameFile", sDetail);
        return;
    }
    sprintf(sDetail, "old=\"%s\" new=\"%s\"", oldName, newName);
    ResultsPass("RenameFile", sDetail);
}

/* Move a file into a named subdirectory of the current directory. */
static void CmdMoveFile(const ScriptArgs *args) {
    const char *name  = ScriptArgsGet(args, "name");
    const char *toDir = ScriptArgsGet(args, "toDir");
    char src[SMB_MAX_PATH];
    char dst[SMB_MAX_PATH];
    SmbDirEntry st;
    unsigned long err = 0;
    int rc;

    if (RequireMount("MoveFile")) {
        return;
    }
    if (name == NULL || toDir == NULL || name[0] == '\0' || toDir[0] == '\0') {
        ResultsFail("MoveFile", "detail=\"need name= and toDir= arguments\"");
        return;
    }
    /* Verify the destination directory exists. */
    CurPath(toDir, dst, sizeof(dst));
    if (SmbStat(dst, &st, &err) != SMB_OK || !st.isDir) {
        sprintf(sDetail, "detail=\"destination dir '%s' not found\"", toDir);
        ResultsFail("MoveFile", sDetail);
        return;
    }
    CurPath(name, src, sizeof(src));
    _snprintf(dst, sizeof(dst), "%s%s\\%s\\%s", sDrive, sCurRel, toDir, name);
    dst[sizeof(dst) - 1] = '\0';
    rc = SmbRename(src, dst, &err);
    if (rc != SMB_OK) {
        sprintf(sDetail, "err=%lu name=\"%s\" toDir=\"%s\"", err, name, toDir);
        ResultsFail("MoveFile", sDetail);
        return;
    }
    sprintf(sDetail, "name=\"%s\" toDir=\"%s\"", name, toDir);
    ResultsPass("MoveFile", sDetail);
}

static void CmdCopyFile(const ScriptArgs *args) {
    const char *name = ScriptArgsGet(args, "name");
    const char *to   = ScriptArgsGet(args, "to");
    char src[SMB_MAX_PATH];
    char dst[SMB_MAX_PATH];
    unsigned long err = 0;
    int rc;

    if (RequireMount("CopyFile")) {
        return;
    }
    if (name == NULL || to == NULL || name[0] == '\0' || to[0] == '\0') {
        ResultsFail("CopyFile", "detail=\"need name= and to= arguments\"");
        return;
    }
    CurPath(name, src, sizeof(src));
    CurPath(to, dst, sizeof(dst));
    rc = SmbCopyFile(src, dst, &err);
    if (rc != SMB_OK) {
        sprintf(sDetail, "err=%lu name=\"%s\" to=\"%s\"", err, name, to);
        ResultsFail("CopyFile", sDetail);
        return;
    }
    sprintf(sDetail, "name=\"%s\" to=\"%s\"", name, to);
    ResultsPass("CopyFile", sDetail);
}

static void CmdDeleteFile(const ScriptArgs *args) {
    const char *name = ScriptArgsGet(args, "name");
    char path[SMB_MAX_PATH];
    unsigned long err = 0;
    int rc;

    if (RequireMount("DeleteFile")) {
        return;
    }
    if (name == NULL || name[0] == '\0') {
        ResultsFail("DeleteFile", "detail=\"missing name= argument\"");
        return;
    }
    CurPath(name, path, sizeof(path));
    rc = SmbDeleteFile(path, &err);
    if (rc != SMB_OK) {
        sprintf(sDetail, "err=%lu name=\"%s\"", err, name);
        ResultsFail("DeleteFile", sDetail);
        return;
    }
    sprintf(sDetail, "name=\"%s\"", name);
    ResultsPass("DeleteFile", sDetail);
}

/* Stat a single file/dir and emit its standardized entry — used by the LFN
 * tests to confirm a long name enumerates back correctly. */
static void CmdStatFile(const ScriptArgs *args) {
    const char *name = ScriptArgsGet(args, "name");
    char path[SMB_MAX_PATH];
    char line[SCRIPT_RESULTS_LINE_MAX];
    SmbDirEntry st;
    unsigned long err = 0;
    int rc;

    if (RequireMount("StatFile")) {
        return;
    }
    if (name == NULL || name[0] == '\0') {
        ResultsFail("StatFile", "detail=\"missing name= argument\"");
        return;
    }
    CurPath(name, path, sizeof(path));
    rc = SmbStat(path, &st, &err);
    if (rc == SMB_NOTFOUND) {
        sprintf(sDetail, "name=\"%s\" detail=\"not found\"", name);
        ResultsFail("StatFile", sDetail);
        return;
    }
    if (rc != SMB_OK) {
        sprintf(sDetail, "err=%lu name=\"%s\"", err, name);
        ResultsFail("StatFile", sDetail);
        return;
    }
    SmbFormatEntry(&st, line, sizeof(line));
    ResultsPass("StatFile", line);
}

/* ---- Directory operations ------------------------------------------------- */

static void CmdCreateDir(const ScriptArgs *args) {
    const char *name = ScriptArgsGet(args, "name");
    char path[SMB_MAX_PATH];
    unsigned long err = 0;
    int rc;

    if (RequireMount("CreateDir")) {
        return;
    }
    if (name == NULL || name[0] == '\0') {
        ResultsFail("CreateDir", "detail=\"missing name= argument\"");
        return;
    }
    CurPath(name, path, sizeof(path));
    rc = SmbMkDir(path, &err);
    if (rc != SMB_OK) {
        sprintf(sDetail, "err=%lu name=\"%s\"", err, name);
        ResultsFail("CreateDir", sDetail);
        return;
    }
    sprintf(sDetail, "name=\"%s\"", name);
    ResultsPass("CreateDir", sDetail);
}

static void CmdRenameDir(const ScriptArgs *args) {
    /* Files and directories rename identically. */
    CmdRenameFile(args);
}

static void CmdCopyDir(const ScriptArgs *args) {
    const char *name = ScriptArgsGet(args, "name");
    const char *to   = ScriptArgsGet(args, "to");
    char src[SMB_MAX_PATH];
    char dst[SMB_MAX_PATH];
    unsigned long err = 0;
    int rc;

    if (RequireMount("CopyDir")) {
        return;
    }
    if (name == NULL || to == NULL || name[0] == '\0' || to[0] == '\0') {
        ResultsFail("CopyDir", "detail=\"need name= and to= arguments\"");
        return;
    }
    CurPath(name, src, sizeof(src));
    CurPath(to, dst, sizeof(dst));
    rc = SmbCopyDir(src, dst, &err);
    if (rc != SMB_OK) {
        sprintf(sDetail, "err=%lu name=\"%s\" to=\"%s\"", err, name, to);
        ResultsFail("CopyDir", sDetail);
        return;
    }
    sprintf(sDetail, "name=\"%s\" to=\"%s\"", name, to);
    ResultsPass("CopyDir", sDetail);
}

static void CmdDeleteDir(const ScriptArgs *args) {
    const char *name = ScriptArgsGet(args, "name");
    char path[SMB_MAX_PATH];
    unsigned long err = 0;
    int rc;

    if (RequireMount("DeleteDir")) {
        return;
    }
    if (name == NULL || name[0] == '\0') {
        ResultsFail("DeleteDir", "detail=\"missing name= argument\"");
        return;
    }
    CurPath(name, path, sizeof(path));
    rc = SmbDeleteTree(path, &err);
    if (rc != SMB_OK) {
        sprintf(sDetail, "err=%lu name=\"%s\"", err, name);
        ResultsFail("DeleteDir", sDetail);
        return;
    }
    sprintf(sDetail, "name=\"%s\"", name);
    ResultsPass("DeleteDir", sDetail);
}

const ScriptCommand gCommandTable[] = {
    { "EnumerateServers", CmdEnumerateServers },
    { "EnumerateShares",  CmdEnumerateShares },
    { "Mount",            CmdMount },
    { "EnumerateVolume",  CmdEnumerateVolume },
    { "EnumerateDir",     CmdEnumerateDir },
    { "SetDir",           CmdSetDir },
    { "SetDirRoot",       CmdSetDirRoot },
    { "CreateFile",       CmdCreateFile },
    { "WriteFile",        CmdWriteFile },
    { "StatFile",         CmdStatFile },
    { "RenameFile",       CmdRenameFile },
    { "MoveFile",         CmdMoveFile },
    { "CopyFile",         CmdCopyFile },
    { "DeleteFile",       CmdDeleteFile },
    { "CreateDir",        CmdCreateDir },
    { "RenameDir",        CmdRenameDir },
    { "CopyDir",          CmdCopyDir },
    { "DeleteDir",        CmdDeleteDir },
    { "Unmount",          CmdUnmount },
    { "Disconnect",       CmdDisconnect },
    { NULL, NULL }
};
