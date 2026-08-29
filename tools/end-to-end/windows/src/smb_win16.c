/* Win16 SMB back-end (MSVC 1.5, Windows 3.1 / Windows for Workgroups 3.11).
 *
 * The Windows 3.1 SDK shipped here exposes only the thin WNet API
 * (WNetAddConnection/WNetCancelConnection) — there is no WNetOpenEnum/
 * WNetEnumResource, so programmatic server/share enumeration is not available.
 * EnumerateServers/EnumerateShares therefore return SMB_UNSUPPORTED and the
 * handlers report that without counting a hard failure (mirroring the macOS
 * tool's MacIPX placeholders). In the real world a Win16 app mounted a known
 * UNC path with WNetAddConnection, which is exactly what the mount path does.
 *
 * Once a drive letter is mapped, every file op goes through DOS INT 21h via the
 * C runtime (_dos_findfirst / fopen / rename / remove / mkdir / rmdir), which
 * the WfW redirector turns into SMB requests to ClassicStack. DOS/FAT semantics
 * bound what we can report: 8.3 names only (shortName == name), a single write
 * date/time (created/accessed unavailable), size as a 32-bit long.
 */
#include <windows.h>
#include <dos.h>
#include <io.h>
#include <direct.h>
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include "smb.h"
#include "status.h"

/* The Windows 3.1 SDK winerror.h predates these Win32 system error codes, but
 * they are the values the modern SDK assigns and the Win32 back-end reports, so
 * define them here (guarded) to keep errCode meanings identical across builds. */
#ifndef ERROR_INVALID_PARAMETER
#define ERROR_INVALID_PARAMETER 87L
#endif
#ifndef ERROR_FILE_EXISTS
#define ERROR_FILE_EXISTS 80L
#endif

const char *SmbPlatformName(void) {
    return "win16 LFN=0";
}

/* ---- DOS packed date/time -> "YYYY-MM-DD HH:MM:SS" ---------------------- */

/* DOS date word: bits 15-9 = year-1980, 8-5 = month, 4-0 = day.
 * DOS time word: bits 15-11 = hour, 10-5 = minute, 4-0 = seconds/2. */
static void FormatDosDateTime(unsigned date, unsigned time, char *out) {
    int year  = 1980 + ((date >> 9) & 0x7F);
    int month = (date >> 5) & 0x0F;
    int day   = date & 0x1F;
    int hour  = (time >> 11) & 0x1F;
    int min   = (time >> 5) & 0x3F;
    int sec   = (time & 0x1F) * 2;

    if (date == 0) {
        out[0] = '\0';
        return;
    }
    sprintf(out, "%04d-%02d-%02d %02d:%02d:%02d",
            year, month, day, hour, min, sec);
}

static void FillEntry(const struct _find_t *f, SmbDirEntry *e) {
    char attr = f->attrib;

    memset(e, 0, sizeof(*e));
    strncpy(e->name, f->name, sizeof(e->name) - 1);
    /* No long names on DOS: the 8.3 name is the only name. */
    strncpy(e->shortName, f->name, sizeof(e->shortName) - 1);
    e->isDir    = (attr & _A_SUBDIR) ? 1 : 0;
    e->readOnly = (attr & _A_RDONLY) ? 1 : 0;
    e->hidden   = (attr & _A_HIDDEN) ? 1 : 0;
    e->system   = (attr & _A_SYSTEM) ? 1 : 0;
    e->archive  = (attr & _A_ARCH)   ? 1 : 0;
    /* DOS exposes only the last-write date/time; created/accessed stay blank. */
    FormatDosDateTime(f->wr_date, f->wr_time, e->modified);
    e->sizeLow  = (unsigned long)f->size;
    e->sizeHigh = 0;
}

/* ---- Discovery: not available on the Windows 3.1 WNet API --------------- */

int SmbEnumerateServers(SmbNameCb cb, void *ctx, unsigned long *errCode) {
    (void)cb; (void)ctx;
    if (errCode) *errCode = 0;
    return SMB_UNSUPPORTED;
}

int SmbEnumerateShares(const char *server, SmbNameCb cb, void *ctx,
                       unsigned long *errCode) {
    (void)server; (void)cb; (void)ctx;
    if (errCode) *errCode = 0;
    return SMB_UNSUPPORTED;
}

/* ---- Mount / unmount --------------------------------------------------- */

int SmbMount(const char *server, const char *share, const char *user,
             const char *pass, char *drive, int driveSize, unsigned long *errCode) {
    char remote[SMB_MAX_PATH];
    UINT rc;

    (void)user; (void)driveSize;
    if (drive == NULL || drive[0] == '\0') {
        if (errCode) *errCode = ERROR_INVALID_PARAMETER;
        return SMB_ERR;   /* Win16 needs an explicit local drive letter */
    }

    sprintf(remote, "\\\\%s\\%s", server, share);
    StatusSetf("Mount: WNetAddConnection %s -> %s", remote, drive);
    /* WNetAddConnection(remote, password, localdrive). A NULL/empty password
     * is fine for guest shares. */
    rc = WNetAddConnection((LPSTR)remote,
                           (LPSTR)((pass != NULL && pass[0] != '\0') ? pass : ""),
                           (LPSTR)drive);
    if (rc != WN_SUCCESS) {
        /* Already connected to this drive is a success for our purposes. */
        if (rc == WN_ALREADY_CONNECTED) {
            return SMB_OK;
        }
        if (errCode) *errCode = rc;
        return SMB_ERR;
    }
    return SMB_OK;
}

int SmbUnmount(const char *drive, unsigned long *errCode) {
    UINT rc;
    StatusSetf("Unmount: WNetCancelConnection %s", drive);
    rc = WNetCancelConnection((LPSTR)drive, TRUE);
    if (rc != WN_SUCCESS && rc != WN_NOT_CONNECTED) {
        if (errCode) *errCode = rc;
        return SMB_ERR;
    }
    return SMB_OK;
}

/* ---- File-system operations (DOS INT 21h via the C runtime) ------------- */

int SmbEnumerate(const char *path, SmbEntryCb cb, void *ctx, unsigned long *errCode) {
    char pattern[SMB_MAX_PATH];
    struct _find_t f;
    unsigned rc;

    sprintf(pattern, "%s\\*.*", path);
    StatusSetf("Enumerate: _dos_findfirst %s", pattern);
    /* Match everything including subdirs/hidden/system so the enumeration is
     * complete; individual entries carry their own attribute flags. */
    rc = _dos_findfirst(pattern,
                        _A_NORMAL | _A_SUBDIR | _A_HIDDEN | _A_SYSTEM | _A_RDONLY | _A_ARCH,
                        &f);
    if (rc != 0) {
        /* No entries at all == empty directory, not an error. */
        return SMB_OK;
    }
    do {
        if (strcmp(f.name, ".") == 0 || strcmp(f.name, "..") == 0) {
            continue;
        }
        {
            SmbDirEntry e;
            FillEntry(&f, &e);
            cb(&e, ctx);
        }
    } while (_dos_findnext(&f) == 0);
    (void)errCode;
    return SMB_OK;
}

int SmbStat(const char *path, SmbDirEntry *out, unsigned long *errCode) {
    struct _find_t f;
    unsigned rc;
    StatusSetf("Stat: _dos_findfirst %s", path);
    rc = _dos_findfirst(path,
                                 _A_NORMAL | _A_SUBDIR | _A_HIDDEN | _A_SYSTEM | _A_RDONLY,
                                 &f);
    (void)errCode;
    if (rc != 0) {
        return SMB_NOTFOUND;
    }
    FillEntry(&f, out);
    return SMB_OK;
}

int SmbCreateFile(const char *path, unsigned long *errCode) {
    FILE *fp;
    StatusSetf("CreateFile: fopen(wb) %s", path);
    /* CREATE_NEW semantics: fail if it exists. Check first, then create. */
    if (_access(path, 0) == 0) {
        if (errCode) *errCode = ERROR_FILE_EXISTS;
        return SMB_ERR;
    }
    fp = fopen(path, "wb");
    if (fp == NULL) {
        if (errCode) *errCode = (unsigned long)errno;
        return SMB_ERR;
    }
    fclose(fp);
    return SMB_OK;
}

int SmbWriteFile(const char *path, const char *data, unsigned long len,
                 unsigned long *errCode) {
    FILE *fp;
    StatusSetf("WriteFile: fopen(wb) %s", path);
    fp = fopen(path, "wb");
    if (fp == NULL) {
        if (errCode) *errCode = (unsigned long)errno;
        return SMB_ERR;
    }
    if (len > 0 && fwrite(data, 1, (size_t)len, fp) != (size_t)len) {
        if (errCode) *errCode = (unsigned long)errno;
        fclose(fp);
        return SMB_ERR;
    }
    fclose(fp);
    return SMB_OK;
}

long SmbReadFile(const char *path, char *buf, unsigned long bufSize,
                 unsigned long *errCode) {
    FILE *fp;
    size_t got;

    StatusSetf("ReadFile: fopen(rb) %s", path);
    fp = fopen(path, "rb");
    if (fp == NULL) {
        return SMB_NOTFOUND;
    }
    got = fread(buf, 1, (size_t)bufSize, fp);
    if (ferror(fp)) {
        if (errCode) *errCode = (unsigned long)errno;
        fclose(fp);
        return SMB_ERR;
    }
    fclose(fp);
    return (long)got;
}

int SmbRename(const char *oldPath, const char *newPath, unsigned long *errCode) {
    StatusSetf("Rename: %s -> %s", oldPath, newPath);
    if (rename(oldPath, newPath) != 0) {
        if (errCode) *errCode = (unsigned long)errno;
        return SMB_ERR;
    }
    return SMB_OK;
}

int SmbCopyFile(const char *src, const char *dst, unsigned long *errCode) {
    FILE *in;
    FILE *out;
    char buf[1024];
    size_t n;

    StatusSetf("CopyFile: %s -> %s", src, dst);
    in = fopen(src, "rb");
    if (in == NULL) {
        if (errCode) *errCode = (unsigned long)errno;
        return SMB_ERR;
    }
    out = fopen(dst, "wb");
    if (out == NULL) {
        if (errCode) *errCode = (unsigned long)errno;
        fclose(in);
        return SMB_ERR;
    }
    while ((n = fread(buf, 1, sizeof(buf), in)) > 0) {
        if (fwrite(buf, 1, n, out) != n) {
            if (errCode) *errCode = (unsigned long)errno;
            fclose(in);
            fclose(out);
            return SMB_ERR;
        }
    }
    fclose(in);
    fclose(out);
    return SMB_OK;
}

int SmbDeleteFile(const char *path, unsigned long *errCode) {
    StatusSetf("DeleteFile: remove %s", path);
    if (remove(path) != 0) {
        if (errCode) *errCode = (unsigned long)errno;
        return SMB_ERR;
    }
    return SMB_OK;
}

int SmbMkDir(const char *path, unsigned long *errCode) {
    StatusSetf("MkDir: _mkdir %s", path);
    if (_mkdir(path) != 0) {
        if (errCode) *errCode = (unsigned long)errno;
        return SMB_ERR;
    }
    return SMB_OK;
}

int SmbRmDir(const char *path, unsigned long *errCode) {
    StatusSetf("RmDir: _rmdir %s", path);
    if (_rmdir(path) != 0) {
        if (errCode) *errCode = (unsigned long)errno;
        return SMB_ERR;
    }
    return SMB_OK;
}

/* Recursive helpers use a private walk (can't nest _dos_findfirst handles, so
 * collect names into an array first, then recurse). */

#define WIN16_MAX_ENTRIES 128

int SmbCopyDir(const char *src, const char *dst, unsigned long *errCode) {
    char pattern[SMB_MAX_PATH];
    struct _find_t f;
    char names[WIN16_MAX_ENTRIES][14];
    char isdir[WIN16_MAX_ENTRIES];
    int count = 0;
    int i;
    int rc;

    rc = SmbMkDir(dst, errCode);
    if (rc != SMB_OK) {
        return rc;
    }

    sprintf(pattern, "%s\\*.*", src);
    if (_dos_findfirst(pattern,
                       _A_NORMAL | _A_SUBDIR | _A_HIDDEN | _A_SYSTEM | _A_RDONLY | _A_ARCH,
                       &f) == 0) {
        do {
            if (strcmp(f.name, ".") == 0 || strcmp(f.name, "..") == 0) {
                continue;
            }
            if (count < WIN16_MAX_ENTRIES) {
                strncpy(names[count], f.name, 13);
                names[count][13] = '\0';
                isdir[count] = (f.attrib & _A_SUBDIR) ? 1 : 0;
                count++;
            }
        } while (_dos_findnext(&f) == 0);
    }

    for (i = 0; i < count; i++) {
        char s[SMB_MAX_PATH];
        char d[SMB_MAX_PATH];
        sprintf(s, "%s\\%s", src, names[i]);
        sprintf(d, "%s\\%s", dst, names[i]);
        if (isdir[i]) {
            rc = SmbCopyDir(s, d, errCode);
        } else {
            rc = SmbCopyFile(s, d, errCode);
        }
        if (rc != SMB_OK) {
            return rc;
        }
    }
    return SMB_OK;
}

int SmbDeleteTree(const char *path, unsigned long *errCode) {
    char pattern[SMB_MAX_PATH];
    struct _find_t f;
    char names[WIN16_MAX_ENTRIES][14];
    char isdir[WIN16_MAX_ENTRIES];
    int count = 0;
    int i;
    int rc;

    sprintf(pattern, "%s\\*.*", path);
    if (_dos_findfirst(pattern,
                       _A_NORMAL | _A_SUBDIR | _A_HIDDEN | _A_SYSTEM | _A_RDONLY | _A_ARCH,
                       &f) == 0) {
        do {
            if (strcmp(f.name, ".") == 0 || strcmp(f.name, "..") == 0) {
                continue;
            }
            if (count < WIN16_MAX_ENTRIES) {
                strncpy(names[count], f.name, 13);
                names[count][13] = '\0';
                isdir[count] = (f.attrib & _A_SUBDIR) ? 1 : 0;
                count++;
            }
        } while (_dos_findnext(&f) == 0);
    }

    for (i = 0; i < count; i++) {
        char child[SMB_MAX_PATH];
        sprintf(child, "%s\\%s", path, names[i]);
        if (isdir[i]) {
            rc = SmbDeleteTree(child, errCode);
        } else {
            rc = SmbDeleteFile(child, errCode);
        }
        if (rc != SMB_OK) {
            return rc;
        }
    }
    return SmbRmDir(path, errCode);
}
