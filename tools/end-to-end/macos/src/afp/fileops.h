/* File Manager operations over a mounted AFP volume.
 *
 * Once volmount.c has mounted an AFP share as a real Mac volume (a vRefNum),
 * every file/fork/directory operation the script exercises is just an ordinary
 * HFS File Manager trap against that vRefNum — HCreate, HOpenDF/HOpenRF +
 * FSWrite, HRename, CatMove, HDelete, DirCreate, PBGetCatInfo. Driving the
 * mounted volume this way means the operations traverse the real AppleShare
 * client → AFP → ClassicStack path exactly as the Finder's would, which is the
 * whole point of the end-to-end test.
 *
 * All calls are relative to a (vRefNum, dirID) pair — dirID 2 (fsRtDirID) is
 * the volume root. Names are C strings, converted to Pascal internally.
 */
#ifndef E2E_FILEOPS_H
#define E2E_FILEOPS_H

/* Which fork FileOpsWriteFork targets. */
typedef enum {
    kFileForkData = 0,
    kFileForkResource = 1
} FileForkKind;

/* Creates an empty file `name` in (vRefNum,dirID) with the given 4-char type
 * and creator (e.g. "TEXT"/"ttxt"). Returns noErr or a negative OSErr
 * (dupFNErr if it already exists). */
short FileOpsCreate(short vRefNum, long dirID, const char *name,
                    const char *type, const char *creator);

/* Overwrites `fork` of `name` with `len` bytes from `data` (opens the fork,
 * truncates to len via SetEOF, writes from the start, closes). The file must
 * already exist. Returns noErr or a negative OSErr. */
short FileOpsWriteFork(short vRefNum, long dirID, const char *name,
                       FileForkKind fork, const void *data, long len);

/* Reads up to bufSize bytes of `fork` of `name` into buf; returns the byte
 * count read (>=0) or a negative OSErr. Used to verify a write round-tripped. */
short FileOpsReadFork(short vRefNum, long dirID, const char *name,
                      FileForkKind fork, void *buf, long bufSize);

/* Renames `oldName` to `newName` within the same directory (HRename). */
short FileOpsRename(short vRefNum, long dirID, const char *oldName,
                    const char *newName);

/* Moves `name` from (vRefNum,dirID) into directory newDirID, keeping the same
 * name (CatMove). */
short FileOpsMove(short vRefNum, long dirID, const char *name, long newDirID);

/* Copies file `name` in (vRefNum,dirID) to `destName` in (vRefNum,destDirID),
 * duplicating both forks and the Finder type/creator. Returns noErr or a
 * negative OSErr. There is no single File Manager "copy file" trap, so this is
 * a create + fork-by-fork copy (what the Finder does under the hood). */
short FileOpsCopy(short vRefNum, long dirID, const char *name,
                  long destDirID, const char *destName);

/* Deletes file `name` (HDelete). */
short FileOpsDelete(short vRefNum, long dirID, const char *name);

/* Creates subdirectory `name` in (vRefNum,dirID); returns the new directory's
 * dirID via *outDirID (DirCreate). */
short FileOpsMkDir(short vRefNum, long dirID, const char *name, long *outDirID);

/* Resolves an existing subdirectory `name` of (vRefNum,dirID) to its dirID via
 * PBGetCatInfo (by name). Returns noErr and *outDirID, or a negative OSErr
 * (fnfErr if it doesn't exist, dirNFErr if `name` is a file not a directory). */
short FileOpsResolveDir(short vRefNum, long dirID, const char *name, long *outDirID);

/* Recursively deletes directory `name` in (vRefNum,dirID) and everything under
 * it (depth-first: empties each subdirectory before HDelete'ing it, since
 * HDelete refuses a non-empty directory). */
short FileOpsDeleteDir(short vRefNum, long dirID, const char *name);

/* Recursively copies directory `name` in (vRefNum,dirID) to a new directory
 * `destName` in (vRefNum,destDirID), duplicating every file (both forks) and
 * nested subdirectory. */
short FileOpsCopyDir(short vRefNum, long dirID, const char *name,
                     long destDirID, const char *destName);

/* Callback for FileOpsEnumerate: receives each entry's name (C string) and
 * whether it is a directory. */
typedef void (*FileOpsEnumProc)(const char *name, int isDir, void *ctx);

/* Enumerates the immediate contents of directory dirID on vRefNum, invoking
 * `proc` for each entry (PBGetCatInfo with ioFDirIndex 1..n). Returns the
 * number of entries (>=0) or a negative OSErr. */
short FileOpsEnumerate(short vRefNum, long dirID, FileOpsEnumProc proc, void *ctx);

#endif /* E2E_FILEOPS_H */
