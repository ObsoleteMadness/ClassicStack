#include <string.h>
#include "afp_client.h"
#include "asp.h"
#include "afp_const.h"
#include "../platform/statuswin.h"

static unsigned short GetBE16(const unsigned char *p) {
    return (unsigned short)((p[0] << 8) | p[1]);
}

/* Reads one Pascal string at buf[off] into out (C string, up to outSize-1
 * bytes), returning the offset just past it, or -1 if it runs past len. */
static short ReadPString(const unsigned char *buf, short len, short off, char *out, int outSize) {
    unsigned char plen;
    short n;

    if (off < 0 || off >= len) {
        return -1;
    }
    plen = buf[off];
    if (off + 1 + plen > len) {
        return -1;
    }

    n = plen;
    if (n > outSize - 1) {
        n = outSize - 1;
    }
    memcpy(out, &buf[off + 1], n);
    out[n] = '\0';

    return off + 1 + plen;
}

int AfpParseServerInfo(const unsigned char *buf, short len, AfpServerInfo *out) {
    short machineOff, versionsOff, uamsOff;
    short off;
    short i;

    memset(out, 0, sizeof(*out));

    if (len < 10) {
        return -1;
    }

    machineOff = (short)GetBE16(&buf[0]);
    versionsOff = (short)GetBE16(&buf[2]);
    uamsOff = (short)GetBE16(&buf[4]);
    out->flags = GetBE16(&buf[8]);

    if (ReadPString(buf, len, 10, out->serverName, sizeof(out->serverName)) < 0) {
        return -1;
    }

    if (ReadPString(buf, len, machineOff, out->machineType, sizeof(out->machineType)) < 0) {
        return -1;
    }

    if (versionsOff < 0 || versionsOff >= len) {
        return -1;
    }
    out->versionCount = buf[versionsOff];
    if (out->versionCount > AFP_MAX_VERSIONS) {
        out->versionCount = AFP_MAX_VERSIONS;
    }
    off = versionsOff + 1;
    for (i = 0; i < out->versionCount; i++) {
        off = ReadPString(buf, len, off, out->afpVersions[i], sizeof(out->afpVersions[i]));
        if (off < 0) {
            out->versionCount = i;
            break;
        }
    }

    if (uamsOff < 0 || uamsOff >= len) {
        return 0; /* versions parsed fine; UAM list missing is tolerated */
    }
    out->uamCount = buf[uamsOff];
    if (out->uamCount > AFP_MAX_UAMS) {
        out->uamCount = AFP_MAX_UAMS;
    }
    off = uamsOff + 1;
    for (i = 0; i < out->uamCount; i++) {
        off = ReadPString(buf, len, off, out->uams[i], sizeof(out->uams[i]));
        if (off < 0) {
            out->uamCount = i;
            break;
        }
    }

    return 0;
}

/* The guest login itself (FPLogin via the AFPLoginPrm variant) lives in asp.c
 * as AspGuestLogin — it's what opens the ASP session for AFP. This file just
 * drives login -> FPGetSrvrParms -> logout and parses the volume list. */

short AfpListVolumes(const AddrBlock *serverAddr, AfpVolumeList *out) {
    short sessRefnum;
    short err;
    long afpResult;
    unsigned char cmd[128];
    unsigned char reply[578];
    short off;
    short numVols;
    short i;
    const unsigned char *p;

    memset(out, 0, sizeof(*out));

    /* Guest login. This one call OPENS the ASP session and logs in (the SCB
     * pointer rides in the login param block) — there is NO separate
     * ASPOpenSession in the AFP-over-.XPP flow; doing that + a plain-afpCall
     * FPLogin is what bus-errored (the afpCall layout has no afpSCBPtr). */
    StatusWinSet("ListVolumes: FPLogin (guest)...");
    err = AspGuestLogin(serverAddr, &sessRefnum);
    if (err != noErr) {
        return err; /* negative OSErr or positive AFP result code */
    }

    /* FPGetSrvrParms: just the one-byte command. Reply = 4-byte server time,
     * 1-byte volume count, then per-volume [flags byte][Pascal name]. */
    off = 0;
    cmd[off++] = afpGetSParms;

    StatusWinSet("ListVolumes: FPGetSrvrParms...");
    afpResult = 0;
    err = AspCommand(sessRefnum, cmd, off, reply, sizeof(reply), &afpResult);
    if (err < 0) {
        AspLogout(sessRefnum);
        return err;
    }
    if (afpResult != kFPNoErr) {
        AspLogout(sessRefnum);
        return (short)afpResult;
    }

    /* Reply is self-describing (a volume count then that many [flags][Pascal
     * name] entries), so we bound the walk by the reply buffer itself rather
     * than a reported length — AFPCommand's param-block shape doesn't surface a
     * reply byte count, and AFP.c never reads one either. */
    {
        const unsigned char *end = reply + sizeof(reply);
        numVols = reply[4]; /* count follows the 4-byte server time */
        if (numVols > AFP_MAX_VOLUMES) {
            numVols = AFP_MAX_VOLUMES;
        }

        p = &reply[5];
        for (i = 0; i < numVols; i++) {
            unsigned char plen;
            short n;

            /* each entry: 1 flags byte then a Pascal-string volume name */
            if (p >= end) {
                break;
            }
            p++; /* skip flags */
            if (p >= end) {
                break;
            }
            plen = *p++;
            if (p + plen > end) {
                break;
            }
            n = plen;
            if (n > (short)sizeof(out->names[0]) - 1) {
                n = (short)sizeof(out->names[0]) - 1;
            }
            memcpy(out->names[out->count], p, (size_t)n);
            out->names[out->count][n] = '\0';
            out->count++;
            p += plen;
        }
    }

    /* FPLogout ends the session (which is what login opened — no separate ASP
     * session to close). */
    StatusWinSet("ListVolumes: FPLogout...");
    AspLogout(sessRefnum);

    return noErr;
}
