#include <string.h>
#include <Files.h>
#include <Errors.h>
#include "fileops.h"

/* Volume root directory id (fsRtDirID). Exposed here for callers that pass a
 * literal root; Files.h defines fsRtDirID = 2. */
#ifndef fsRtDirID
#define fsRtDirID 2
#endif

/* Convert a C string to a Str255 (Pascal) for the File Manager traps. */
static void CToP(const char *s, Str255 out) {
    size_t n = strlen(s);
    if (n > 255) {
        n = 255;
    }
    out[0] = (unsigned char)n;
    memcpy(&out[1], s, n);
}

/* Parse a 4-char OSType from a C string, space-padding if shorter. */
static OSType FourCharType(const char *s) {
    unsigned char b[4];
    int i;
    for (i = 0; i < 4; i++) {
        b[i] = (s != NULL && s[i] != '\0' && (i == 0 || s[i - 1] != '\0'))
                   ? (unsigned char)s[i]
                   : ' ';
    }
    return ((OSType)b[0] << 24) | ((OSType)b[1] << 16) |
           ((OSType)b[2] << 8) | (OSType)b[3];
}

short FileOpsCreate(short vRefNum, long dirID, const char *name,
                    const char *type, const char *creator) {
    Str255 pName;
    OSErr err;
    FInfo fndr;

    CToP(name, pName);
    err = HCreate(vRefNum, dirID, pName, FourCharType(creator), FourCharType(type));
    if (err != noErr) {
        return err;
    }

    /* HCreate sets type/creator, but be explicit (and confirm the Finder info
     * round-trips) via HSetFInfo — some servers ignore HCreate's type/creator. */
    err = HGetFInfo(vRefNum, dirID, pName, &fndr);
    if (err == noErr) {
        fndr.fdType = FourCharType(type);
        fndr.fdCreator = FourCharType(creator);
        HSetFInfo(vRefNum, dirID, pName, &fndr);
    }
    return noErr;
}

/* Open the requested fork of a file for writing/reading, returning the refNum. */
static short OpenFork(short vRefNum, long dirID, const char *name,
                      FileForkKind fork, short *refNum) {
    Str255 pName;
    CToP(name, pName);
    if (fork == kFileForkResource) {
        return HOpenRF(vRefNum, dirID, pName, fsRdWrPerm, refNum);
    }
    return HOpenDF(vRefNum, dirID, pName, fsRdWrPerm, refNum);
}

short FileOpsWriteFork(short vRefNum, long dirID, const char *name,
                       FileForkKind fork, const void *data, long len) {
    short refNum;
    OSErr err;
    long count = len;

    err = OpenFork(vRefNum, dirID, name, fork, &refNum);
    if (err != noErr) {
        return err;
    }

    /* Truncate to exactly len first so a shorter overwrite leaves no stale
     * trailing bytes, then write from offset 0. */
    err = SetEOF(refNum, len);
    if (err == noErr) {
        err = SetFPos(refNum, fsFromStart, 0);
    }
    if (err == noErr) {
        err = FSWrite(refNum, &count, data);
    }

    FSClose(refNum);
    return err;
}

short FileOpsReadFork(short vRefNum, long dirID, const char *name,
                      FileForkKind fork, void *buf, long bufSize) {
    short refNum;
    OSErr err;
    long count = bufSize;

    err = OpenFork(vRefNum, dirID, name, fork, &refNum);
    if (err != noErr) {
        return err;
    }

    err = FSRead(refNum, &count, buf);
    FSClose(refNum);

    /* eofErr just means the fork was shorter than the buffer — count holds the
     * bytes actually read, which is the result we want. */
    if (err != noErr && err != eofErr) {
        return err;
    }
    return (short)count;
}

short FileOpsRename(short vRefNum, long dirID, const char *oldName,
                    const char *newName) {
    Str255 pOld, pNew;
    CToP(oldName, pOld);
    CToP(newName, pNew);
    return HRename(vRefNum, dirID, pOld, pNew);
}

short FileOpsMove(short vRefNum, long dirID, const char *name, long newDirID) {
    Str255 pName;
    CToP(name, pName);
    /* CatMove does NOT rename: its last arg (ioNewName) is the DESTINATION
     * DIRECTORY's name, to be combined with newDirID to identify the target —
     * NOT a new name for the moved item. Passing the file's own name there made
     * the File Manager look for a destination directory called "<file>" under
     * newDirID, which doesn't exist → fnfErr (-43). Pass NULL so the move is
     * specified by newDirID alone; the item keeps its name. */
    return CatMove(vRefNum, dirID, pName, newDirID, NULL);
}

short FileOpsCopy(short vRefNum, long dirID, const char *name,
                  long destDirID, const char *destName) {
    Str255 pName;
    FInfo fndr;
    OSErr err;
    static unsigned char copyBuf[4096];
    short srcRef, dstRef;

    CToP(name, pName);

    /* Preserve type/creator on the copy. */
    err = HGetFInfo(vRefNum, dirID, pName, &fndr);
    if (err != noErr) {
        return err;
    }

    {
        Str255 pDest;
        CToP(destName, pDest);
        err = HCreate(vRefNum, destDirID, pDest, fndr.fdCreator, fndr.fdType);
        if (err != noErr && err != dupFNErr) {
            return err;
        }
    }

    /* Copy each fork. Missing/empty forks (fnfErr on open, or zero bytes) are
     * fine — just skip. */
    {
        int f;
        for (f = 0; f < 2; f++) {
            FileForkKind kind = (f == 0) ? kFileForkData : kFileForkResource;
            long total = 0;

            if (OpenFork(vRefNum, dirID, name, kind, &srcRef) != noErr) {
                continue;
            }
            if (OpenFork(vRefNum, destDirID, destName, kind, &dstRef) != noErr) {
                FSClose(srcRef);
                continue;
            }

            for (;;) {
                long count = (long)sizeof(copyBuf);
                OSErr rerr = FSRead(srcRef, &count, copyBuf);
                if (count > 0) {
                    long wcount = count;
                    FSWrite(dstRef, &wcount, copyBuf);
                    total += count;
                }
                if (rerr == eofErr || count == 0) {
                    break;
                }
                if (rerr != noErr) {
                    break;
                }
            }
            (void)total;
            FSClose(srcRef);
            FSClose(dstRef);
        }
    }

    return noErr;
}

short FileOpsDelete(short vRefNum, long dirID, const char *name) {
    Str255 pName;
    CToP(name, pName);
    return HDelete(vRefNum, dirID, pName);
}

short FileOpsMkDir(short vRefNum, long dirID, const char *name, long *outDirID) {
    Str255 pName;
    long created = 0;
    OSErr err;

    CToP(name, pName);
    err = DirCreate(vRefNum, dirID, pName, &created);
    if (outDirID != NULL) {
        *outDirID = created;
    }
    return err;
}

/* Look up a child directory's dirID by name (PBGetCatInfo by name). Returns
 * noErr and *outDirID on success, or a negative OSErr. */
short FileOpsResolveDir(short vRefNum, long dirID, const char *name, long *outDirID) {
    CInfoPBRec pb;
    Str255 pName;
    OSErr err;

    CToP(name, pName);
    memset(&pb, 0, sizeof(pb));
    pb.dirInfo.ioNamePtr = pName;
    pb.dirInfo.ioVRefNum = vRefNum;
    pb.dirInfo.ioDrDirID = dirID;
    pb.dirInfo.ioFDirIndex = 0; /* look up by name */
    err = PBGetCatInfoSync(&pb);
    if (err != noErr) {
        return err;
    }
    if (!(pb.dirInfo.ioFlAttrib & kioFlAttribDirMask)) {
        return dirNFErr; /* not a directory */
    }
    if (outDirID != NULL) {
        *outDirID = pb.dirInfo.ioDrDirID;
    }
    return noErr;
}

short FileOpsEnumerate(short vRefNum, long dirID, FileOpsEnumProc proc, void *ctx) {
    short index = 1;
    short count = 0;

    for (;;) {
        CInfoPBRec pb;
        Str255 pName;
        OSErr err;

        pName[0] = 0;
        memset(&pb, 0, sizeof(pb));
        pb.hFileInfo.ioNamePtr = pName;
        pb.hFileInfo.ioVRefNum = vRefNum;
        pb.hFileInfo.ioFDirIndex = index;
        pb.hFileInfo.ioDirID = dirID; /* reset each call: PBGetCatInfo overwrites it */

        err = PBGetCatInfoSync(&pb);
        if (err == fnfErr) {
            break; /* ran past the last entry */
        }
        if (err != noErr) {
            return (count > 0) ? count : err;
        }

        {
            char cName[256];
            int n = pName[0];
            int isDir = (pb.hFileInfo.ioFlAttrib & kioFlAttribDirMask) ? 1 : 0;
            if (n > 255) {
                n = 255;
            }
            memcpy(cName, &pName[1], (size_t)n);
            cName[n] = '\0';
            if (proc != NULL) {
                proc(cName, isDir, ctx);
            }
        }

        count++;
        index++;
    }

    return count;
}

/* Recursion helpers use a fixed-name buffer per level; classic Mac stacks are
 * small, and enumeration nests only as deep as the tree. Entry names collected
 * before mutating so we don't enumerate-and-delete the same index space. */

typedef struct {
    char names[64][64];
    int isDir[64];
    int count;
} DirListing;

static void CollectProc(const char *name, int isDir, void *ctx) {
    DirListing *dl = (DirListing *)ctx;
    if (dl->count < 64) {
        size_t n = strlen(name);
        if (n > 63) {
            n = 63;
        }
        memcpy(dl->names[dl->count], name, n);
        dl->names[dl->count][n] = '\0';
        dl->isDir[dl->count] = isDir;
        dl->count++;
    }
}

short FileOpsDeleteDir(short vRefNum, long dirID, const char *name) {
    long childID;
    OSErr err;
    DirListing dl;
    int i;

    err = FileOpsResolveDir(vRefNum, dirID, name, &childID);
    if (err != noErr) {
        return err;
    }

    dl.count = 0;
    FileOpsEnumerate(vRefNum, childID, CollectProc, &dl);

    for (i = 0; i < dl.count; i++) {
        if (dl.isDir[i]) {
            err = FileOpsDeleteDir(vRefNum, childID, dl.names[i]);
        } else {
            err = FileOpsDelete(vRefNum, childID, dl.names[i]);
        }
        /* fnfErr means the entry is already gone — tolerate it. An enumeration
         * that returns the same name twice (observed against the AFP server)
         * would otherwise make the second delete of that name fail and abort
         * the whole recursive delete. Treat "already deleted" as success. */
        if (err != noErr && err != fnfErr) {
            return err;
        }
    }

    /* Directory is now empty; delete it by name from its parent. */
    err = FileOpsDelete(vRefNum, dirID, name);
    if (err == fnfErr) {
        return noErr; /* already gone */
    }
    return err;
}

short FileOpsCopyDir(short vRefNum, long dirID, const char *name,
                     long destDirID, const char *destName) {
    long srcID, newID;
    OSErr err;
    DirListing dl;
    int i;

    err = FileOpsResolveDir(vRefNum, dirID, name, &srcID);
    if (err != noErr) {
        return err;
    }

    err = FileOpsMkDir(vRefNum, destDirID, destName, &newID);
    if (err != noErr && err != dupFNErr) {
        return err;
    }
    if (err == dupFNErr) {
        /* already exists — resolve its dirID and copy into it */
        err = FileOpsResolveDir(vRefNum, destDirID, destName, &newID);
        if (err != noErr) {
            return err;
        }
    }

    dl.count = 0;
    FileOpsEnumerate(vRefNum, srcID, CollectProc, &dl);

    for (i = 0; i < dl.count; i++) {
        if (dl.isDir[i]) {
            err = FileOpsCopyDir(vRefNum, srcID, dl.names[i], newID, dl.names[i]);
        } else {
            err = FileOpsCopy(vRefNum, srcID, dl.names[i], newID, dl.names[i]);
        }
        if (err != noErr) {
            return err;
        }
    }

    return noErr;
}
