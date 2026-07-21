#include <string.h>
#include <Devices.h>  /* OpenDriver */
#include "asp.h"
#include "atalk.h"    /* AtalkInit */
#include "../platform/statuswin.h"

/* SCB (Session Control Block) memory the .XPP driver needs per open session;
 * scbMemSize (192) is the Toolbox-defined constant (AppleTalk.h). One SCB is
 * enough since this test app only ever holds one AFP session open at a time. */
static unsigned char sScb[scbMemSize];
static Boolean sInited = false;
/* The .XPP driver refNum OpenXPP hands back. It is USUALLY the documented
 * xppRefNum (-41), but OpenXPP is the authority — the driver may be installed
 * at a different unit — so we must use the returned value, not the -41 constant.
 * (Earlier code opened the driver but discarded the refNum into a local
 * `xppRefnum` and then wrongly used the global `xppRefNum` constant in every
 * param block.) 0 until AspInit succeeds. */
static short sXppRefNum = 0;

short AspInit(void) {
    OSErr err;

    if (sInited) {
        return noErr;
    }

    /* The .XPP driver (ASP/AFP) is layered on ATP, which is layered on MPP, so
     * both must be up before OpenDriver("\p.XPP"). Bring them up via AtalkInit()
     * — NOT a bare ATPLoad() here.
     *
     * Why not ATPLoad(): by the time AspInit runs, AtalkInit has ALREADY loaded
     * ATP (that's how ListZones/ListServers worked). Calling the ATPLoad() glue
     * a SECOND time, when ATP is already loaded, bus-errors (crash pinned to the
     * "AspInit: ATPLoad..." breadcrumb) — the same libInterface-glue hazard that
     * bit IsMPPOpen(). AtalkInit is idempotent and uses the safe inline
     * MppIsOpen/AtpIsOpen guards, so it only calls ATPLoad if ATP isn't already
     * open (i.e. never, on this path) — bringing MPP+ATP up without the
     * redundant, crashing glue call. */
    StatusWinSet("AspInit: AtalkInit (MPP+ATP)...");
    err = AtalkInit();
    if (err != noErr) {
        return err;
    }

    /* Open the .XPP driver via OpenDriver rather than the OpenXPP() glue.
     * OpenXPP is documented to be exactly OpenDriver("\p.XPP", &refnum), and
     * AtalkGetZones already opens .XPP that way successfully — whereas the
     * libInterface glue wrappers (cf. IsMPPOpen) have bitten us. Use the path
     * we've proven works on the wire. */
    StatusWinSet("AspInit: OpenDriver .XPP...");
    err = OpenDriver("\p.XPP", &sXppRefNum);
    if (err == noErr) {
        sInited = true;
    }
    return err;
}

short AspGetStatus(const AddrBlock *serverAddr, unsigned char *buf, short bufSize) {
    XPPParamBlock pb;
    OSErr err;

    err = AspInit();
    if (err != noErr) {
        return err;
    }

    memset(&pb, 0, sizeof(pb));
    pb.XPP.ioRefNum = sXppRefNum;
    pb.XPP.csCode = getStatus;
    pb.XPP.aspTimeout = 2;  /* seconds between ATP retries */
    pb.XPP.aspRetry = 5;
    /* GetStatus takes the target address BY VALUE in the ASPOpenPrm
     * variant's serverAddr slot (offset 32 per the XPPParamBlock record in
     * AppleTalk.a / IM:Networking "ASPGetStatus") — the same bytes the XPP
     * variant calls cbSize + cbPtr. Passing a cbPtr POINTER here instead
     * makes the driver read a garbage AddrBlock (net = sizeof(AddrBlock),
     * node/socket = pointer bytes) and the request goes nowhere. The reply
     * buffer, by contrast, really is rbSize/rbPtr (offsets 38/40, which
     * don't overlap serverAddr). */
    pb.OPEN.serverAddr = *serverAddr;
    pb.XPP.rbSize = bufSize;
    pb.XPP.rbPtr = (Ptr)buf;

    err = ASPGetStatusSync((XPPParmBlkPtr)&pb);
    if (err != noErr) {
        return err;
    }

    return (short)pb.XPP.cmdResult; /* bytes received on success */
}

short AspOpenSession(const AddrBlock *serverAddr, short *sessRefnum) {
    /* Use the full XPPParamBlock union, not a bare ASPOpenPrm: the .XPP driver
     * reserves CCB scratch past the fixed fields for some calls, and the union
     * is sized (via XPPPrmBlk.ccbStart / AFPLoginPrm.ccbFill) to hold it. A
     * bare ASPOpenPrm ends right after attnRoutine, so the driver could write
     * past it. */
    XPPParamBlock pb;
    OSErr err;

    err = AspInit();
    if (err != noErr) {
        return err;
    }

    memset(&pb, 0, sizeof(pb));
    pb.OPEN.ioRefNum = sXppRefNum;
    pb.OPEN.csCode = openSess;
    pb.OPEN.aspTimeout = 2;
    pb.OPEN.aspRetry = 5;
    pb.OPEN.serverAddr = *serverAddr;
    pb.OPEN.scbPointer = (Ptr)sScb;
    pb.OPEN.attnRoutine = NULL;

    StatusWinSet("AspOpenSession: ASPOpenSession...");
    err = ASPOpenSession((XPPParmBlkPtr)&pb, false);
    if (err != noErr) {
        return err;
    }

    *sessRefnum = pb.OPEN.sessRefnum;
    return noErr;
}

short AspCloseSession(short sessRefnum) {
    XPPParamBlock pb;

    memset(&pb, 0, sizeof(pb));
    pb.XPP.ioRefNum = sXppRefNum;
    pb.XPP.csCode = closeSess;
    pb.XPP.sessRefnum = sessRefnum;

    return ASPCloseSession((XPPParmBlkPtr)&pb, false);
}

/* FPLogin command codes/strings (Inside AppleTalk 13-104). */
#define ASP_AFP_LOGIN     18 /* afpLogin */
#define ASP_AFP_LOGOUT    20 /* afpLogout */

/* Guest login via the AFPLoginPrm variant — this is the call that opens the
 * ASP session for AFP (the SCB pointer rides here), exactly as AFP.c's
 * LogOnAsGuest does. Do NOT precede this with ASPOpenSession: FPLogin as a
 * plain afpCall uses the XPPPrmBlk layout which has NO afpSCBPtr, so the driver
 * reads garbage for the SCB and bus-errors — which is precisely the crash we
 * hit at "AspCommand: AFPCommand..." on the guest FPLogin. */
short AspGuestLogin(const AddrBlock *serverAddr, short *sessRefnum) {
    XPPParamBlock pb;
    unsigned char cmd[64];
    static unsigned char reply[578]; /* login reply; static to spare the stack */
    short off;
    OSErr err;

    err = AspInit();
    if (err != noErr) {
        return err;
    }

    /* Command block: [afpLogin][pstr "AFPVersion 2.0"][pstr "No User Authent"]. */
    off = 0;
    cmd[off++] = ASP_AFP_LOGIN;
    {
        const char *ver = "AFPVersion 2.0";
        const char *uam = "No User Authent";
        size_t n;
        n = strlen(ver); cmd[off++] = (unsigned char)n; memcpy(&cmd[off], ver, n); off += (short)n;
        n = strlen(uam); cmd[off++] = (unsigned char)n; memcpy(&cmd[off], uam, n); off += (short)n;
    }

    memset(&pb, 0, sizeof(pb));
    pb.LOGIN.ioRefNum = sXppRefNum;
    pb.LOGIN.ioCompletion = NULL;
    pb.LOGIN.aspTimeout = 2;
    pb.LOGIN.aspRetry = 3;
    pb.LOGIN.afpAddrBlock = *serverAddr;   /* server address (login opens the session) */
    pb.LOGIN.afpAttnRoutine = NULL;
    pb.LOGIN.cbPtr = (Ptr)cmd;
    pb.LOGIN.cbSize = off;
    pb.LOGIN.rbPtr = (Ptr)reply;
    pb.LOGIN.rbSize = (short)sizeof(reply);
    pb.LOGIN.afpSCBPtr = (Ptr)sScb;        /* THE crucial field the afpCall path lacks */

    StatusWinSet("AspGuestLogin: AFPCommand FPLogin...");
    err = AFPCommand((XPPParmBlkPtr)&pb, false);
    if (err != noErr) {
        return err; /* transport/driver error */
    }
    if (pb.LOGIN.cmdResult != noErr) {
        return (short)pb.LOGIN.cmdResult; /* AFP-level error (e.g. bad UAM) */
    }

    *sessRefnum = pb.LOGIN.sessRefnum;
    return noErr;
}

short AspLogout(short sessRefnum) {
    XPPParamBlock pb;
    unsigned char cmd[1];

    cmd[0] = ASP_AFP_LOGOUT;

    memset(&pb, 0, sizeof(pb));
    pb.XPP.ioRefNum = sXppRefNum;
    pb.XPP.csCode = afpCall;
    pb.XPP.sessRefnum = sessRefnum;
    pb.XPP.aspTimeout = 2;
    pb.XPP.aspRetry = 3;
    pb.XPP.cbPtr = (Ptr)cmd;
    pb.XPP.cbSize = 1;

    StatusWinSet("AspLogout: AFPCommand FPLogout...");
    return AFPCommand((XPPParmBlkPtr)&pb, false);
}

short AspCommand(short sessRefnum, const unsigned char *cbPtr, short cbSize,
                  unsigned char *rbPtr, short rbSize, long *afpResult) {
    XPPParamBlock pb;
    OSErr err;

    memset(&pb, 0, sizeof(pb));
    pb.XPP.ioRefNum = sXppRefNum;
    pb.XPP.csCode = afpCall;
    pb.XPP.sessRefnum = sessRefnum;
    pb.XPP.aspTimeout = 2;
    pb.XPP.aspRetry = 5;
    pb.XPP.cbSize = cbSize;
    pb.XPP.cbPtr = (Ptr)cbPtr;
    pb.XPP.rbSize = rbSize;
    pb.XPP.rbPtr = (Ptr)rbPtr;

    StatusWinSet("AspCommand: AFPCommand...");
    err = AFPCommand((XPPParmBlkPtr)&pb, false);
    if (err != noErr) {
        return err;
    }

    *afpResult = pb.XPP.cmdResult;
    /* cmdResult doubles as the AFP result code on reply; the actual reply
     * byte count isn't separately reported by this call shape, so callers
     * that need it size rbSize exactly or parse a length-prefixed reply. */
    return rbSize;
}
