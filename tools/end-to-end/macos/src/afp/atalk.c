#include <string.h>
#include <Devices.h>  /* OpenDriver */
#include <OSUtils.h>  /* SysEnvirons, SysEnvRec */
#include <Errors.h>   /* nbpBuffOvr */
#include "atalk.h"
#include "../platform/statuswin.h"

/* atDrvrVersNum >= 53 means AppleTalk Phase 2 (the .XPP driver's ZIP
 * GetZoneList xCall exists). Below that is Phase 1, where GetZoneList must be
 * done as a raw ATP request to the bridge's ZIP socket instead — issuing the
 * XPP xCall on a Phase-1 stack is exactly the kind of thing that hangs. This
 * is the same gate Apple's GetZoneList.c (DoZoneList) and ZIP.c (GetZones)
 * use. Our target is System 7.1 (always Phase 2), but checking it the Apple
 * way means we FAIL CLEANLY rather than wedge if that assumption is wrong. */
#define AT_PHASE2_DRVR_VERS 53

/* ZIP GetZoneList reply buffer: Apple's sample fixes this at 578 bytes
 * (kZonesSize) — one ATP response's worth of back-to-back Pascal zone-name
 * strings. The .XPP driver requires zipBuffPtr to point at a buffer of
 * exactly this size (AppleTalk.h: "must be 578 bytes"). Static, not stack:
 * 578 bytes is a lot of classic-Mac stack and only one call runs at a time. */
#define ZIP_BUF_SIZE 578

/* Retry pacing for the ZIP xCall, matching Apple's kATPTimeOutVal /
 * kATPRetryCount (3 s between retries, 5 retries). */
#define ZIP_TIMEOUT 3
#define ZIP_RETRY   5

static Boolean sInited = false;
static short sAtDrvrVers = 0; /* atDrvrVersNum from SysEnvirons; 0 until AtalkInit */
static unsigned char sZipBuf[ZIP_BUF_SIZE];

/* AppleTalk "is the .MPP driver open?" check, done OURSELVES rather than via the
 * Toolbox IsMPPOpen()/IsATPOpen() glue.
 *
 * Why not the glue: disassembling Retro68's linked libInterface.a IsMPPOpen
 * shows it reads low-memory byte 0x0291 with a `move.b (0x0291).W` (absolute
 * SHORT addressing), and Retro68's multi-segment relocator spuriously RELOCATES
 * that 0x0291 operand at load time — turning the safe low-memory read into a
 * wild pointer. Result: IsMPPOpen() bus-errors at a DIFFERENT address each run
 * (the non-deterministic-PC signature we saw: 079A45F0 then 40802208), with the
 * durable STEP breadcrumb pinned exactly at "IsMPPOpen?...".
 *
 * The low-memory global at 0x0291 is `PortBUse` (Inside AppleTalk; the LAP/MPP
 * "port B in use" byte): bit 7 set (negative) means the port is unused/closed;
 * otherwise the low nibble is the driver id using it, and MPP being open is
 * indicated by a non-zero-after-the-documented-decode value. We read it through
 * a pointer built from an integer CONSTANT — no symbol, so nothing for the
 * relocator to touch — matching what the glue was supposed to do.
 *
 * PortBUse decode (from the IsMPPOpen glue semantics): MPP open iff
 * (b & 0x0F) != 0 and bit 7 clear; ATP additionally requires bit 4 set. */
#define kLowMemPortBUse 0x0291

static Boolean MppIsOpen(void) {
    unsigned char b = *((volatile unsigned char *)kLowMemPortBUse);
    if (b & 0x80) {
        return false; /* bit 7 set: port unused */
    }
    return (b & 0x0F) != 0;
}

static Boolean AtpIsOpen(void) {
    unsigned char b = *((volatile unsigned char *)kLowMemPortBUse);
    if (!MppIsOpen()) {
        return false;
    }
    return (b & 0x10) != 0; /* bit 4: ATP loaded */
}

/* Open .MPP and load ATP, in that order, once. The .XPP (ZIP/ASP) calls are
 * all layered on ATP, so ATP MUST be loaded before any of them — that's the
 * whole reason a bare MPPOpen (what this used to do) left GetZoneList's
 * SendRequests going out on the wire but never completing.
 *
 * We deliberately do NOT call the MPPOpen() glue. Disassembling libInterface.a
 * shows MPPOpen(), when .MPP isn't already open, issues an ASYNCHRONOUS _Open
 * with a completion-routine pointer stored in the param block — that async open
 * path bus-errors under our Retro68 runtime / emulator (crash at a low PC while
 * "AtalkInit: MPPOpen" was on screen). OpenDriver("\p.MPP") is a plain
 * SYNCHRONOUS _Open with no completion routine and no A5-world dependency,
 * which is exactly what a client should use to bring AppleTalk up. If AppleTalk
 * is already active, IsMPPOpen() is true and OpenDriver just hands back the
 * existing refnum — cheap and safe (IsMPPOpen is a pure low-memory read, no
 * trap, so it can never fault). This mirrors what the NCSA atalk.c sample does
 * (OpenDriver "\p.MPP") rather than the MPPOpen() glue. */
short AtalkInit(void) {
    SysEnvRec env;
    OSErr err;
    short mppRefNum;

    if (sInited) {
        return noErr;
    }

    /* SysEnvirons is a real inline trap ($A090) with no AppleTalk dependency —
     * do it FIRST as a health probe. If the crash happens after this label
     * shows, the Toolbox is fine and the fault is specifically the AppleTalk
     * driver path below. Also gives us atDrvrVersNum for the Phase-1/2 gate. */
    StatusWinSet("AtalkInit: SysEnvirons...");
    if (SysEnvirons(1, &env) == noErr) {
        sAtDrvrVers = env.atDrvrVersNum;
    }

    /* MppIsOpen() reads low-memory 0x0291 ourselves (see above) — the Toolbox
     * IsMPPOpen() glue is mis-relocated by Retro68 and bus-errors here. */
    StatusWinSet("AtalkInit: MppIsOpen?...");
    if (!MppIsOpen()) {
        StatusWinSet("AtalkInit: OpenDriver .MPP...");
        err = OpenDriver("\p.MPP", &mppRefNum);
        if (err != noErr) {
            return err;
        }
    }

    StatusWinSet("AtalkInit: AtpIsOpen?...");
    if (!AtpIsOpen()) {
        StatusWinSet("AtalkInit: ATPLoad...");
        err = ATPLoad();
        if (err != noErr) {
            return err;
        }
    }

    sInited = true;
    return noErr;
}

/* The AppleTalk driver version SysEnvirons reported (>= AT_PHASE2_DRVR_VERS
 * means Phase 2). 0 until AtalkInit has run. Exposed so the caller can log
 * it — a wrong/zero value here is a headline diagnostic for a wedged run. */
short AtalkDriverVersion(void) {
    return sAtDrvrVers;
}

/* The NBP type AppleShare file servers register under (Inside AppleTalk /
 * Chooser). We look up the wildcard object "=" of type "AFPServer" in the
 * requested zone. */
#define NBP_AFP_TYPE "\pAFPServer"
#define NBP_WILDCARD "\p="

/* NBP retry pacing (NBP.c uses interval 8 ticks, count 3). */
#define NBP_INTERVAL 8
#define NBP_COUNT    3

/* Copy a C string into a Str32 (Pascal) buffer, clamped to 32 data bytes. */
static void CToStr32(const char *s, unsigned char *pstr) {
    size_t n = strlen(s);
    if (n > 32) {
        n = 32;
    }
    pstr[0] = (unsigned char)n;
    memcpy(&pstr[1], s, n);
}

/* NBPGetList + NBPGetAddress (Apple NBP.c), reduced to gathering AFP servers.
 * Builds the "=:AFPServer@zone" entity, PLookupName's it, then NBPExtract's
 * each returned tuple into name+address. */
short AtalkFindServers(const char *zone, AtalkServer *servers, short maxServers) {
    MPPParamBlock pb;
    EntityName entity;
    unsigned char zoneStr[34];
    static unsigned char nbpBuf[ATALK_MAX_SERVERS *
                                (sizeof(EntityName) + sizeof(AddrBlock))];
    OSErr err;
    short got, i, count;

    err = AtalkInit();
    if (err != noErr) {
        return err;
    }

    /* "*" is the documented "this zone" wildcard for NBP; pass it through as-is
     * (NBPSetEntity takes a Pascal string). An empty/NULL zone means local. */
    if (zone == NULL || zone[0] == '\0') {
        CToStr32("*", zoneStr);
    } else {
        CToStr32(zone, zoneStr);
    }

    NBPSetEntity((Ptr)&entity, NBP_WILDCARD, NBP_AFP_TYPE, zoneStr);

    if (maxServers > ATALK_MAX_SERVERS) {
        maxServers = ATALK_MAX_SERVERS;
    }

    memset(&pb, 0, sizeof(pb));
    pb.NBP.interval = NBP_INTERVAL;
    pb.NBP.count = NBP_COUNT;
    pb.NBP.nbpPtrs.entityPtr = (Ptr)&entity;
    pb.NBP.parm.Lookup.retBuffPtr = (Ptr)nbpBuf;
    pb.NBP.parm.Lookup.retBuffSize = (short)sizeof(nbpBuf);
    pb.NBP.parm.Lookup.maxToGet = maxServers;

    StatusWinSet("FindServers: NBP LookupName...");
    err = PLookupName((MPPPBPtr)&pb, false);
    /* nbpBuffOvr just means the reply buffer filled — whatever tuples landed in
     * it are still valid, so treat it like success and parse numGotten. */
    if (err != noErr && err != nbpBuffOvr) {
        return err;
    }

    got = pb.NBP.parm.Lookup.numGotten;
    if (got < 0) {
        got = 0;
    }

    count = 0;
    for (i = 1; i <= got && count < maxServers; i++) {
        EntityName tuple;
        AddrBlock addr;

        if (NBPExtract((Ptr)nbpBuf, got, i, &tuple, &addr) != noErr) {
            continue;
        }

        /* objStr is the server's registered name as a Str32 (length-prefixed);
         * store it as a C string. */
        {
            unsigned char *obj = (unsigned char *)tuple.objStr;
            int n = obj[0];
            if (n > ATALK_SERVER_NAME_SIZE - 1) {
                n = ATALK_SERVER_NAME_SIZE - 1;
            }
            memcpy(servers[count].name, &obj[1], (size_t)n);
            servers[count].name[n] = '\0';
        }
        servers[count].addr = addr;
        count++;
    }

    return count;
}

/* BuildZoneListPhase2 (Apple GetZoneList.c v1.04), reduced to just gathering
 * the names. Opens the .XPP driver for its reference number, then issues
 * repeated zipGetZoneList xCalls via PBControl until zipLastFlag is set,
 * accumulating the Pascal zone-name strings out of the reply buffer. */
short AtalkGetZones(char zones[][ATALK_ZONE_NAME_SIZE], short maxZones) {
    XCallParam pb;
    short xppRefNum;
    OSErr err;
    short count = 0;

    err = AtalkInit();
    if (err != noErr) {
        return err;
    }

    /* Phase-2 (.XPP ZIP xCall) only. If the stack is Phase 1, bail with a
     * clean error rather than issuing an XPP xCall it can't service (which is
     * how you wedge). Our System 7.1 target is always Phase 2, so this should
     * never trip — but if it does, the caller logs sAtDrvrVers and we know
     * immediately. -1 is our "wrong phase" sentinel (distinct from any OSErr). */
    if (sAtDrvrVers < AT_PHASE2_DRVR_VERS) {
        return -1;
    }

    /* Get the .XPP driver reference number the safe way — OpenDriver returns
     * it — rather than assuming the documented -41 constant. */
    StatusWinSet("GetZones: OpenDriver .XPP...");
    err = OpenDriver("\p.XPP", &xppRefNum);
    if (err != noErr) {
        return err;
    }

    /* zipInfoField carries the driver's continuation state BETWEEN calls: its
     * first word MUST be zero on the initial call, and the driver fills the
     * rest in and hands it back for the next call. So clear the whole param
     * block once up front, before the loop — never inside it. */
    memset(&pb, 0, sizeof(pb));

    do {
        short i;
        const unsigned char *p = sZipBuf;

        pb.ioRefNum = xppRefNum;
        pb.csCode = xCall;
        pb.xppSubCode = zipGetZoneList;
        pb.xppTimeout = ZIP_TIMEOUT;
        pb.xppRetry = ZIP_RETRY;
        pb.zipBuffPtr = (Ptr)sZipBuf;

        StatusWinSet("GetZones: ZIP GetZoneList xCall...");
        /* GetZoneList is the .XPP glue that sets csCode=xCall /
         * xppSubCode=zipGetZoneList and issues the PBControl — the exact call
         * Apple's ZIP.c XPPGetZoneList uses. (We set those fields anyway so
         * the param block is self-describing.) */
        err = GetZoneList((XPPParmBlkPtr)&pb, false);
        if (err != noErr) {
            /* A partial list already gathered is still a result; only a
             * failing FIRST call reports the error. */
            return count > 0 ? count : err;
        }

        if (pb.zipNumZones == 0) {
            /* Defensive: a zero-zone page means the list is exhausted even if
             * the router forgot to set zipLastFlag (a router that does that
             * otherwise traps this loop — and the real Chooser — in an
             * infinite re-ask, since every call "succeeds"). */
            break;
        }

        /* Reply buffer holds zipNumZones back-to-back Pascal strings. */
        for (i = 0; i < pb.zipNumZones && count < maxZones; i++) {
            int rawLen = p[0]; /* advance by the WIRE length, copy the clamped one */
            int len = rawLen;

            if (p + 1 + rawLen > sZipBuf + ZIP_BUF_SIZE) {
                break; /* malformed reply; keep what parsed cleanly */
            }
            if (len > ATALK_ZONE_NAME_SIZE - 1) {
                len = ATALK_ZONE_NAME_SIZE - 1;
            }
            memcpy(zones[count], &p[1], (size_t)len);
            zones[count][len] = '\0';
            count++;
            p += 1 + rawLen;
        }
    } while (pb.zipLastFlag == 0 && count < maxZones);

    return count;
}
