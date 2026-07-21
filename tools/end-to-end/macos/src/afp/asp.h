/* ASP session layer over the classic Toolbox .XPP driver (OpenXPP/
 * ASPGetStatus/ASPOpenSession/AFPCommand — InterfaceLib 7.1). The Toolbox
 * driver does the ATP transaction/session bookkeeping itself; this module
 * just wraps the parameter-block setup into small, script-command-shaped
 * calls.
 */
#ifndef E2E_ASP_H
#define E2E_ASP_H

#include <AppleTalk.h>

/* Opens the .XPP driver. Returns noErr on success. Call once before any
 * other Asp* call. */
short AspInit(void);

/* Session-less ASPGetStatus / FPGetSrvrInfo against `serverAddr`. Fills
 * `buf` (up to bufSize bytes) with the raw FPGetSrvrInfo block and returns
 * the number of bytes received (>=0), or a negative OSErr on failure. */
short AspGetStatus(const AddrBlock *serverAddr, unsigned char *buf, short bufSize);

/* Opens an ASP session to `serverAddr`. On success returns noErr and fills
 * *sessRefnum; on failure returns a negative OSErr.
 *
 * NOTE: for AFP over the .XPP driver you almost always want AspGuestLogin
 * instead — the AFP login (FPLogin) is what actually establishes the ASP
 * session (it carries the SCB pointer in the AFPLoginPrm variant), so a
 * separate ASPOpenSession is neither needed nor how the AppleShare client
 * does it. Kept for completeness / non-AFP ASP services. */
short AspOpenSession(const AddrBlock *serverAddr, short *sessRefnum);

/* Guest AFP login: sends FPLogin ("AFPVersion 2.0" / "No User Authent") to
 * `serverAddr` via the AFPLoginPrm param-block variant, which — per Apple's
 * AFP.c LogOnAsGuest — is what OPENS the ASP session AND logs in in one call
 * (the SCB pointer rides in the login param block; there is NO separate
 * ASPOpenSession in the AFP flow). On success returns noErr and fills
 * *sessRefnum with the session to use for subsequent AspCommand calls; on an
 * AFP-level failure returns the positive AFP result code, or a negative OSErr
 * on a transport/driver error. Pair with AspLogout. */
short AspGuestLogin(const AddrBlock *serverAddr, short *sessRefnum);

/* FPLogout on `sessRefnum` (a plain in-session command). */
short AspLogout(short sessRefnum);

/* Closes a previously opened session. */
short AspCloseSession(short sessRefnum);

/* Sends one AFP command (cbPtr/cbSize) over an open ASP session and fills
 * rbPtr (up to rbSize bytes) with the reply. Returns the AFP result code
 * (kFPNoErr on success) via *afpResult, and the number of reply bytes
 * received (>=0) as the return value, or a negative OSErr on a
 * transport-level failure (session/driver error, not an AFP error). */
short AspCommand(short sessRefnum, const unsigned char *cbPtr, short cbSize,
                  unsigned char *rbPtr, short rbSize, long *afpResult);

#endif /* E2E_ASP_H */
