/* Win32 SMB back-end: WNet enumeration + WNetAddConnection2 mount, then the
 * Win32 file APIs (FindFirstFile/CreateFile/...) for all file-system ops. Every
 * call flows through the MS network redirector to ClassicStack's SMB server.
 *
 * Built ANSI (no UNICODE) so the char* seam in smb.h is a direct fit; the *A
 * WNet/file entry points are called explicitly. Links WSOCK32/MPR via
 * NETAPI32? no — only mpr.lib (WNet*) and kernel32 (file APIs) are needed.
 */
#include <windows.h>
#include <winnetwk.h>
#include <stdio.h>
#include <string.h>
#include "smb.h"
#include "status.h"

const char *SmbPlatformName(void) {
    return "win32 LFN=1";
}

/* ---- FILETIME -> "YYYY-MM-DD HH:MM:SS" (local time) --------------------- */

static void FormatFileTime(const FILETIME *ft, char *out) {
    FILETIME local;
    SYSTEMTIME st;

    out[0] = '\0';
    /* A zero FILETIME means "not recorded" — leave the field blank so the
     * formatter emits "-". Some redirectors return 0 for create/access. */
    if (ft->dwLowDateTime == 0 && ft->dwHighDateTime == 0) {
        return;
    }
    if (!FileTimeToLocalFileTime(ft, &local) ||
        !FileTimeToSystemTime(&local, &st)) {
        return;
    }
    sprintf(out, "%04d-%02d-%02d %02d:%02d:%02d",
            st.wYear, st.wMonth, st.wDay, st.wHour, st.wMinute, st.wSecond);
}

/* Decode WIN32_FIND_DATA into our standardized entry. */
static void FillEntry(const WIN32_FIND_DATA *fd, SmbDirEntry *e) {
    DWORD a = fd->dwFileAttributes;

    memset(e, 0, sizeof(*e));
    strncpy(e->name, fd->cFileName, sizeof(e->name) - 1);
    /* cAlternateFileName is the 8.3 short name; empty when the long name is
     * already 8.3-legal — in that case the short name *is* the name. */
    if (fd->cAlternateFileName[0] != '\0') {
        strncpy(e->shortName, fd->cAlternateFileName, sizeof(e->shortName) - 1);
    } else {
        strncpy(e->shortName, fd->cFileName, sizeof(e->shortName) - 1);
    }
    e->isDir    = (a & FILE_ATTRIBUTE_DIRECTORY) ? 1 : 0;
    e->readOnly = (a & FILE_ATTRIBUTE_READONLY)  ? 1 : 0;
    e->hidden   = (a & FILE_ATTRIBUTE_HIDDEN)    ? 1 : 0;
    e->system   = (a & FILE_ATTRIBUTE_SYSTEM)    ? 1 : 0;
    e->archive  = (a & FILE_ATTRIBUTE_ARCHIVE)   ? 1 : 0;
    FormatFileTime(&fd->ftCreationTime,   e->created);
    FormatFileTime(&fd->ftLastWriteTime,  e->modified);
    FormatFileTime(&fd->ftLastAccessTime, e->accessed);
    e->sizeLow  = fd->nFileSizeLow;
    e->sizeHigh = fd->nFileSizeHigh;
}

/* ---- Discovery: WNet enumeration --------------------------------------- */

/* Strip "\\SERVER" or "\\SERVER\Share" down to the bare trailing component. */
static const char *BareName(const char *remote) {
    const char *p = remote;
    const char *bare = remote;
    while (*p) {
        if (*p == '\\') bare = p + 1;
        p++;
    }
    return bare;
}

/* Walk one container, invoking `cb` for every child whose dwDisplayType matches
 * `wantType`. When `descend` is non-zero, child containers that are NOT already
 * the wanted type (e.g. domains/workgroups while hunting for servers) are
 * recursed into — this is required because WNetOpenEnum(GLOBALNET, parent=NULL)
 * yields the top-level DOMAIN containers, and the servers we want live one level
 * inside each domain. `depth` bounds the recursion defensively. */
static int EnumWalk(LPNETRESOURCE parent, DWORD wantType, int descend, int depth,
                    SmbNameCb cb, void *ctx, unsigned long *errCode) {
    HANDLE hEnum;
    DWORD rc;

    if (depth > 4) {
        return SMB_OK; /* guard against a pathological container cycle */
    }

    rc = WNetOpenEnumA(RESOURCE_GLOBALNET, RESOURCETYPE_DISK,
                       RESOURCEUSAGE_CONTAINER, parent, &hEnum);
    if (rc != NO_ERROR) {
        if (errCode) *errCode = rc;
        return SMB_ERR;
    }

    for (;;) {
        /* WNet packs NETRESOURCE structs plus their strings into this buffer.
         * 16 KiB comfortably holds a small workgroup's server/share list. */
        char buffer[16384];
        DWORD count = (DWORD)-1;      /* as many as fit */
        DWORD bufSize = sizeof(buffer);
        DWORD i;

        rc = WNetEnumResourceA(hEnum, &count, buffer, &bufSize);
        if (rc == ERROR_NO_MORE_ITEMS) {
            break;
        }
        if (rc != NO_ERROR) {
            WNetCloseEnum(hEnum);
            if (errCode) *errCode = rc;
            return SMB_ERR;
        }

        {
            LPNETRESOURCE nr = (LPNETRESOURCE)buffer;
            for (i = 0; i < count; i++) {
                if (nr[i].lpRemoteName == NULL) {
                    continue;
                }
                if (nr[i].dwDisplayType == wantType) {
                    cb(BareName(nr[i].lpRemoteName), ctx);
                } else if (descend &&
                           (nr[i].dwUsage & RESOURCEUSAGE_CONTAINER)) {
                    /* A container that isn't the target type (a domain while
                     * hunting servers) — descend one level. Ignore per-branch
                     * failures so one unreachable domain doesn't fail the whole
                     * enumeration. */
                    EnumWalk(&nr[i], wantType, descend, depth + 1, cb, ctx, NULL);
                }
            }
        }
    }

    WNetCloseEnum(hEnum);
    return SMB_OK;
}

int SmbEnumerateServers(SmbNameCb cb, void *ctx, unsigned long *errCode) {
    StatusSet("EnumerateServers: WNetOpenEnum(GLOBALNET)");
    /* Top-level GLOBALNET enumeration yields DOMAIN containers; descend into
     * each to collect the SERVER leaves inside. */
    return EnumWalk(NULL, RESOURCEDISPLAYTYPE_SERVER, 1, 0, cb, ctx, errCode);
}

int SmbEnumerateShares(const char *server, SmbNameCb cb, void *ctx,
                       unsigned long *errCode) {
    NETRESOURCE nr;
    char remote[SMB_MAX_PATH];

    memset(&nr, 0, sizeof(nr));
    sprintf(remote, "\\\\%s", server);
    StatusSetf("EnumerateShares: WNetOpenEnum %s", remote);
    nr.dwScope       = RESOURCE_GLOBALNET;
    nr.dwType        = RESOURCETYPE_DISK;
    nr.dwDisplayType = RESOURCEDISPLAYTYPE_SERVER;
    nr.dwUsage       = RESOURCEUSAGE_CONTAINER;
    nr.lpRemoteName  = remote;

    /* Shares are direct children of the server container — no descent. */
    return EnumWalk(&nr, RESOURCEDISPLAYTYPE_SHARE, 0, 0, cb, ctx, errCode);
}

/* ---- Mount / unmount --------------------------------------------------- */

int SmbMount(const char *server, const char *share, const char *user,
             const char *pass, char *drive, int driveSize, unsigned long *errCode) {
    NETRESOURCE nr;
    char remote[SMB_MAX_PATH];
    DWORD rc;

    memset(&nr, 0, sizeof(nr));
    sprintf(remote, "\\\\%s\\%s", server, share);
    nr.dwType       = RESOURCETYPE_DISK;
    nr.lpRemoteName = remote;
    nr.lpLocalName  = (drive != NULL && drive[0] != '\0') ? drive : NULL;

    /* An explicit local drive letter is required (the caller always supplies
     * one — the command layer defaults to N:). WNetAddConnection2 with a NULL
     * lpLocalName would make a deviceless connection we couldn't reach by path. */
    if (nr.lpLocalName == NULL) {
        if (errCode) *errCode = ERROR_INVALID_PARAMETER;
        return SMB_ERR;
    }
    (void)driveSize;

    StatusSetf("Mount: WNetAddConnection2 %s -> %s", remote, drive);
    rc = WNetAddConnection2A(&nr,
                             (pass != NULL && pass[0] != '\0') ? (LPSTR)pass : NULL,
                             (user != NULL && user[0] != '\0') ? (LPSTR)user : NULL,
                             0);
    if (rc != NO_ERROR) {
        if (errCode) *errCode = rc;
        return SMB_ERR;
    }
    return SMB_OK;
}

int SmbUnmount(const char *drive, unsigned long *errCode) {
    DWORD rc;
    StatusSetf("Unmount: WNetCancelConnection2 %s", drive);
    rc = WNetCancelConnection2A((LPSTR)drive, 0, TRUE);
    if (rc != NO_ERROR) {
        if (errCode) *errCode = rc;
        return SMB_ERR;
    }
    return SMB_OK;
}

/* ---- File-system operations -------------------------------------------- */

int SmbEnumerate(const char *path, SmbEntryCb cb, void *ctx, unsigned long *errCode) {
    char pattern[SMB_MAX_PATH];
    WIN32_FIND_DATA fd;
    HANDLE h;

    sprintf(pattern, "%s\\*", path);
    StatusSetf("Enumerate: FindFirstFile %s", pattern);
    h = FindFirstFileA(pattern, &fd);
    if (h == INVALID_HANDLE_VALUE) {
        DWORD e = GetLastError();
        if (e == ERROR_FILE_NOT_FOUND || e == ERROR_NO_MORE_FILES) {
            return SMB_OK; /* empty directory */
        }
        if (errCode) *errCode = e;
        return SMB_ERR;
    }
    do {
        if (strcmp(fd.cFileName, ".") == 0 || strcmp(fd.cFileName, "..") == 0) {
            continue;
        }
        {
            SmbDirEntry e;
            FillEntry(&fd, &e);
            cb(&e, ctx);
        }
    } while (FindNextFileA(h, &fd));
    FindClose(h);
    return SMB_OK;
}

int SmbStat(const char *path, SmbDirEntry *out, unsigned long *errCode) {
    WIN32_FIND_DATA fd;
    HANDLE h;
    StatusSetf("Stat: FindFirstFile %s", path);
    h = FindFirstFileA(path, &fd);
    if (h == INVALID_HANDLE_VALUE) {
        DWORD e = GetLastError();
        if (e == ERROR_FILE_NOT_FOUND || e == ERROR_PATH_NOT_FOUND) {
            return SMB_NOTFOUND;
        }
        if (errCode) *errCode = e;
        return SMB_ERR;
    }
    FillEntry(&fd, out);
    FindClose(h);
    return SMB_OK;
}

int SmbCreateFile(const char *path, unsigned long *errCode) {
    HANDLE h;
    StatusSetf("CreateFile: CreateFile(CREATE_NEW) %s", path);
    h = CreateFileA(path, GENERIC_WRITE, 0, NULL, CREATE_NEW,
                    FILE_ATTRIBUTE_NORMAL, NULL);
    if (h == INVALID_HANDLE_VALUE) {
        if (errCode) *errCode = GetLastError();
        return SMB_ERR;
    }
    CloseHandle(h);
    return SMB_OK;
}

int SmbWriteFile(const char *path, const char *data, unsigned long len,
                 unsigned long *errCode) {
    HANDLE h;
    DWORD written = 0;

    StatusSetf("WriteFile: CreateFile(CREATE_ALWAYS) %s", path);
    h = CreateFileA(path, GENERIC_WRITE, 0, NULL, CREATE_ALWAYS,
                    FILE_ATTRIBUTE_NORMAL, NULL);
    if (h == INVALID_HANDLE_VALUE) {
        if (errCode) *errCode = GetLastError();
        return SMB_ERR;
    }
    if (len > 0 && !WriteFile(h, data, len, &written, NULL)) {
        if (errCode) *errCode = GetLastError();
        CloseHandle(h);
        return SMB_ERR;
    }
    CloseHandle(h);
    if (written != len) {
        if (errCode) *errCode = ERROR_WRITE_FAULT;
        return SMB_ERR;
    }
    return SMB_OK;
}

long SmbReadFile(const char *path, char *buf, unsigned long bufSize,
                 unsigned long *errCode) {
    HANDLE h;
    DWORD got = 0;

    StatusSetf("ReadFile: CreateFile(OPEN_EXISTING) %s", path);
    h = CreateFileA(path, GENERIC_READ, FILE_SHARE_READ, NULL, OPEN_EXISTING,
                    FILE_ATTRIBUTE_NORMAL, NULL);
    if (h == INVALID_HANDLE_VALUE) {
        DWORD e = GetLastError();
        if (e == ERROR_FILE_NOT_FOUND || e == ERROR_PATH_NOT_FOUND) {
            return SMB_NOTFOUND;
        }
        if (errCode) *errCode = e;
        return SMB_ERR;
    }
    if (!ReadFile(h, buf, bufSize, &got, NULL)) {
        if (errCode) *errCode = GetLastError();
        CloseHandle(h);
        return SMB_ERR;
    }
    CloseHandle(h);
    return (long)got;
}

int SmbRename(const char *oldPath, const char *newPath, unsigned long *errCode) {
    StatusSetf("Rename: MoveFile %s -> %s", oldPath, newPath);
    if (!MoveFileA(oldPath, newPath)) {
        if (errCode) *errCode = GetLastError();
        return SMB_ERR;
    }
    return SMB_OK;
}

int SmbCopyFile(const char *src, const char *dst, unsigned long *errCode) {
    StatusSetf("CopyFile: CopyFile %s -> %s", src, dst);
    if (!CopyFileA(src, dst, FALSE)) {
        if (errCode) *errCode = GetLastError();
        return SMB_ERR;
    }
    return SMB_OK;
}

int SmbDeleteFile(const char *path, unsigned long *errCode) {
    StatusSetf("DeleteFile: DeleteFile %s", path);
    if (!DeleteFileA(path)) {
        if (errCode) *errCode = GetLastError();
        return SMB_ERR;
    }
    return SMB_OK;
}

int SmbMkDir(const char *path, unsigned long *errCode) {
    StatusSetf("MkDir: CreateDirectory %s", path);
    if (!CreateDirectoryA(path, NULL)) {
        if (errCode) *errCode = GetLastError();
        return SMB_ERR;
    }
    return SMB_OK;
}

int SmbRmDir(const char *path, unsigned long *errCode) {
    StatusSetf("RmDir: RemoveDirectory %s", path);
    if (!RemoveDirectoryA(path)) {
        if (errCode) *errCode = GetLastError();
        return SMB_ERR;
    }
    return SMB_OK;
}

/* Recursive copy: create dst, then walk src copying files and recursing into
 * subdirs. Resource forks are not applicable on SMB, so this is a plain tree. */
int SmbCopyDir(const char *src, const char *dst, unsigned long *errCode) {
    char pattern[SMB_MAX_PATH];
    WIN32_FIND_DATA fd;
    HANDLE h;
    int rc;

    rc = SmbMkDir(dst, errCode);
    if (rc != SMB_OK) {
        return rc;
    }

    sprintf(pattern, "%s\\*", src);
    h = FindFirstFileA(pattern, &fd);
    if (h == INVALID_HANDLE_VALUE) {
        return SMB_OK; /* empty source */
    }
    do {
        char s[SMB_MAX_PATH];
        char d[SMB_MAX_PATH];
        if (strcmp(fd.cFileName, ".") == 0 || strcmp(fd.cFileName, "..") == 0) {
            continue;
        }
        sprintf(s, "%s\\%s", src, fd.cFileName);
        sprintf(d, "%s\\%s", dst, fd.cFileName);
        if (fd.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY) {
            rc = SmbCopyDir(s, d, errCode);
        } else {
            rc = SmbCopyFile(s, d, errCode);
        }
        if (rc != SMB_OK) {
            FindClose(h);
            return rc;
        }
    } while (FindNextFileA(h, &fd));
    FindClose(h);
    return SMB_OK;
}

int SmbDeleteTree(const char *path, unsigned long *errCode) {
    char pattern[SMB_MAX_PATH];
    WIN32_FIND_DATA fd;
    HANDLE h;
    int rc;

    sprintf(pattern, "%s\\*", path);
    h = FindFirstFileA(pattern, &fd);
    if (h != INVALID_HANDLE_VALUE) {
        do {
            char child[SMB_MAX_PATH];
            if (strcmp(fd.cFileName, ".") == 0 || strcmp(fd.cFileName, "..") == 0) {
                continue;
            }
            sprintf(child, "%s\\%s", path, fd.cFileName);
            if (fd.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY) {
                rc = SmbDeleteTree(child, errCode);
            } else {
                rc = SmbDeleteFile(child, errCode);
            }
            if (rc != SMB_OK) {
                FindClose(h);
                return rc;
            }
        } while (FindNextFileA(h, &fd));
        FindClose(h);
    }
    return SmbRmDir(path, errCode);
}
