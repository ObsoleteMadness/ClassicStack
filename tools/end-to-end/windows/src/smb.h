/* Windows SMB client seam for the end-to-end test tool.
 *
 * Like the macOS tool drives the real AppleShare client (so every op traverses
 * AppleShare -> AFP -> ClassicStack), this drives the real Windows redirector:
 * enumeration/mount go through the WNet API and file operations go through the
 * ordinary file APIs once a drive letter is mapped, so every op traverses
 * MS-redirector -> SMB -> ClassicStack exactly as Explorer/File Manager would.
 * The tool speaks no SMB itself and is transport-agnostic — it asks the
 * redirector for \\SERVER and the guest's bound transport (NetBEUI and/or IPX,
 * matching ClassicStack's [SMB].transports) carries it.
 *
 * Two implementations behind one seam, selected at compile time:
 *   Win32 (smb_win32.c):  WNetOpenEnum/WNetEnumResource + WNetAddConnection2,
 *                         FindFirstFile/WIN32_FIND_DATA (LFN, short name, all
 *                         three FILETIMEs, 64-bit size), CreateFile.
 *   Win16 (smb_win16.c):  WNetAddConnection (the Windows 3.1 SDK ships no
 *                         WNet enumeration API — EnumerateServers/Shares report
 *                         "unsupported on Win16" and continue), _dos_findfirst
 *                         (8.3 name only, DOS write time only), fopen.
 *
 * All strings are ANSI/OEM char (the tool is built ANSI on both platforms).
 */
#ifndef E2E_SMB_H
#define E2E_SMB_H

/* Result codes. 0 == success. Negative == a real failure. SMB_UNSUPPORTED is
 * a soft "this platform/build can't do this op" — handlers report it without
 * counting a hard failure, mirroring the macOS tool's MacIPX placeholders. */
#define SMB_OK            0
#define SMB_ERR          (-1)   /* generic failure; see errCode for the OS code */
#define SMB_UNSUPPORTED  (-2)   /* op not available on this platform/build */
#define SMB_NOTFOUND     (-3)   /* named file/dir/share/server not found */

/* Longest fields we carry. Win32 LFN paths can be long; keep generous. */
#define SMB_MAX_NAME    260
#define SMB_MAX_PATH    300

/* One standardized directory entry, filled by SmbEnumerate's callback. Fields
 * a platform can't supply are left blank/zero (Win16: shortName==name,
 * created/accessed empty, size capped at 2 GiB by the DOS long). See
 * tools/end-to-end/RESULT-FORMAT.md for the exact emitted line. */
typedef struct {
    char name[SMB_MAX_NAME];        /* long file name (Win32) / 8.3 name (Win16) */
    char shortName[16];             /* 8.3 alternate name; == name when none */
    int  isDir;                     /* 1 = directory, 0 = file */
    int  readOnly;                  /* attribute bits, decoded to flags... */
    int  hidden;
    int  system;
    int  archive;
    char created[24];               /* "YYYY-MM-DD HH:MM:SS" or "" if unavailable */
    char modified[24];
    char accessed[24];
    unsigned long sizeLow;          /* file size in bytes, low 32 bits */
    unsigned long sizeHigh;         /* high 32 bits (Win32 only; 0 on Win16) */
} SmbDirEntry;

/* Enumeration callbacks. Each returns nothing; the tool aggregates. `ctx` is
 * the opaque pointer passed to the Smb* call. */
typedef void (*SmbNameCb)(const char *name, void *ctx);
typedef void (*SmbEntryCb)(const SmbDirEntry *entry, void *ctx);

/* --- Discovery (Win32 only; Win16 returns SMB_UNSUPPORTED) --------------- */

/* Enumerate visible servers on the network. Calls `cb` once per server name
 * (bare name, no leading "\\"). Returns SMB_OK/SMB_UNSUPPORTED/SMB_ERR. On
 * SMB_ERR, *errCode holds the OS error (WNet error / GetLastError). */
int SmbEnumerateServers(SmbNameCb cb, void *ctx, unsigned long *errCode);

/* Enumerate shares exported by `server` (bare name, no "\\"). Calls `cb` once
 * per share name. Returns SMB_OK/SMB_UNSUPPORTED/SMB_NOTFOUND/SMB_ERR. */
int SmbEnumerateShares(const char *server, SmbNameCb cb, void *ctx,
                       unsigned long *errCode);

/* --- Mount / unmount ---------------------------------------------------- */

/* Map \\server\share to a local drive. If `drive` is non-empty (e.g. "N:")
 * that letter is requested; if empty, the redirector picks one and it is
 * written back into drive (at least 4 bytes). `user`/`pass` may be NULL/empty
 * for guest/anonymous. Returns SMB_OK/SMB_ERR (*errCode set on error). */
int SmbMount(const char *server, const char *share, const char *user,
             const char *pass, char *drive, int driveSize, unsigned long *errCode);

/* Tear down a previously mapped drive (e.g. "N:"). */
int SmbUnmount(const char *drive, unsigned long *errCode);

/* --- File-system operations (operate on an absolute path, e.g. "N:\dir") - */

/* Enumerate a directory. `path` is a mapped path with no trailing slash
 * (e.g. "N:" or "N:\sub"). Calls `cb` once per entry (excluding "." and "..").
 * Returns SMB_OK/SMB_ERR. */
int SmbEnumerate(const char *path, SmbEntryCb cb, void *ctx, unsigned long *errCode);

/* Stat a single path into `out`. Returns SMB_OK/SMB_NOTFOUND/SMB_ERR. */
int SmbStat(const char *path, SmbDirEntry *out, unsigned long *errCode);

/* Create an empty file (fails if it already exists). */
int SmbCreateFile(const char *path, unsigned long *errCode);

/* Overwrite `path` with `len` bytes of `data` (truncating). */
int SmbWriteFile(const char *path, const char *data, unsigned long len,
                 unsigned long *errCode);

/* Read up to bufSize bytes from `path` into buf; returns byte count read
 * (>=0) or a negative SMB_* code. *errCode set on SMB_ERR. */
long SmbReadFile(const char *path, char *buf, unsigned long bufSize,
                 unsigned long *errCode);

/* Rename/move oldPath -> newPath (both absolute). Works for files and dirs. */
int SmbRename(const char *oldPath, const char *newPath, unsigned long *errCode);

/* Copy a single file src -> dst. */
int SmbCopyFile(const char *src, const char *dst, unsigned long *errCode);

/* Delete a single file. */
int SmbDeleteFile(const char *path, unsigned long *errCode);

/* Create / remove a directory. */
int SmbMkDir(const char *path, unsigned long *errCode);
int SmbRmDir(const char *path, unsigned long *errCode);

/* Copy a directory tree src -> dst recursively (files only, no forks). */
int SmbCopyDir(const char *src, const char *dst, unsigned long *errCode);

/* Recursively delete a directory tree. */
int SmbDeleteTree(const char *path, unsigned long *errCode);

/* Format one SmbDirEntry as the standardized RESULT-FORMAT.md entry string
 * into `out` (must hold SCRIPT_RESULTS_LINE_MAX). Shared by both platforms so
 * the emitted format is identical regardless of which fields were populated. */
void SmbFormatEntry(const SmbDirEntry *entry, char *out, int outSize);

/* Platform/build banner (e.g. "win32 LFN=1" / "win16 LFN=0") for the env
 * DEBUG line, so every results file records which client produced it. */
const char *SmbPlatformName(void);

#endif /* E2E_SMB_H */
