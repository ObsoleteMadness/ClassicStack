#include <string.h>
#include <Files.h>    /* AFPVolMountInfo, PBVolumeMount, UnmountVol */
#include <OSUtils.h>
#include "volmount.h"
#include "../platform/statuswin.h"

/* AppleShare's external-file-system signature and the guest UAM type
 * (Files.h). AppleShareMediaType tells PBVolumeMount which installed foreign
 * file system to hand the record to; kNoUserAuthentication is the "guest"
 * User Authentication Method. */
#ifndef AppleShareMediaType
#define AppleShareMediaType 'afpm'
#endif

/* NBP retry pacing the AppleShare client uses when it looks up the server by
 * name (IM:Networking; AFPVolMountInfo nbpInterval/nbpCount). 8 ticks / 5. */
#define VOLMOUNT_NBP_INTERVAL 8
#define VOLMOUNT_NBP_COUNT    5

/* Append a Pascal string built from a C string at buf[*off]; record the offset
 * (from the record base) into *offField for the mount record, then advance. */
static void PackPStr(unsigned char *base, short *off, short *offField,
                     const char *s) {
    size_t n = strlen(s);
    if (n > 32) {
        n = 32; /* AFP zone/server/volume names are Str32 */
    }
    *offField = *off;
    base[(*off)++] = (unsigned char)n;
    memcpy(&base[*off], s, n);
    *off += (short)n;
}

/* Build an AFPVolMountInfo and hand it to PBVolumeMount. The record's variable
 * data area (AFPData) holds the zone/server/volume/user/password/volpassword
 * Pascal strings; the fixed fields carry their offsets. For a guest mount the
 * user, user-password and volume-password strings are all empty. */
short VolMountAFP(const char *zone, const char *serverName, const char *volName,
                  short *outVRefNum) {
    AFPVolMountInfo mi;
    IOParam pb;
    unsigned char *base = (unsigned char *)&mi;
    short off;
    OSErr err;

    if (outVRefNum != NULL) {
        *outVRefNum = 0;
    }

    memset(&mi, 0, sizeof(mi));
    mi.media = AppleShareMediaType;
    mi.flags = 0;
    mi.nbpInterval = VOLMOUNT_NBP_INTERVAL;
    mi.nbpCount = VOLMOUNT_NBP_COUNT;
    mi.uamType = kNoUserAuthentication; /* guest */

    /* The strings live in the AFPData tail; offsets are measured from the
     * start of the whole record. AFPData begins right after userPasswordOffset/
     * volPasswordOffset, so start packing there. */
    off = (short)offsetof(AFPVolMountInfo, AFPData);

    PackPStr(base, &off, &mi.zoneNameOffset,
             (zone != NULL && zone[0] != '\0') ? zone : "*");
    PackPStr(base, &off, &mi.serverNameOffset, serverName);
    PackPStr(base, &off, &mi.volNameOffset, volName);
    /* Guest: empty user / user-password / volume-password strings. */
    PackPStr(base, &off, &mi.userNameOffset, "");
    PackPStr(base, &off, &mi.userPasswordOffset, "");
    PackPStr(base, &off, &mi.volPasswordOffset, "");

    /* length = everything up to and including the packed strings. */
    mi.length = off;

    memset(&pb, 0, sizeof(pb));
    pb.ioBuffer = (Ptr)&mi;

    StatusWinSet("MountVolume: PBVolumeMount...");
    err = PBVolumeMount((ParmBlkPtr)&pb);
    if (err != noErr) {
        return err;
    }

    if (outVRefNum != NULL) {
        *outVRefNum = pb.ioVRefNum; /* the mounted volume's vRefNum */
    }
    return noErr;
}

short VolUnmount(short vRefNum) {
    /* UnmountVol flushes and offlines the volume; the AppleShare client tears
     * down its AFP session as part of that. */
    return UnmountVol(NULL, vRefNum);
}
