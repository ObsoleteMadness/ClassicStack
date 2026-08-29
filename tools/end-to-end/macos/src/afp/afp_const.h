/* AFP wire constants mirrored from core/service/afp/dispatch.go (command
 * numbers) so the two implementations stay in lockstep as the server's
 * dispatch table changes. AFPCall command codes (afpXxx) are the classic
 * Toolbox names already defined by AppleTalk.h — this header only adds the
 * result codes and constants Retro68's header doesn't carry. */
#ifndef E2E_AFP_CONST_H
#define E2E_AFP_CONST_H

/* AFP result codes (kFP*; Inside Macintosh: Networking, "AFP result codes").
 * Mirrors core/service/afp/dispatch.go's afpErr* block. */
#define kFPNoErr             0
#define kFPAuthContinue      5
#define kFPAccessDenied      -5000
#define kFPCantMove          -5005
#define kFPBadUAM            -5002
#define kFPBadVersNum        -5003
#define kFPBitmapErr         -5004
#define kFPDiskFull          -5008
#define kFPEOFErr            -5009
#define kFPLockErr           -5013
#define kFPMiscErr           -5014
#define kFPNoMoreLocks       -5015
#define kFPRangeNotLocked    -5020
#define kFPRangeOverlap      -5021
#define kFPObjectExists      -5017
#define kFPObjectNotFound    -5018
#define kFPParamErr          -5019
#define kFPCallNotSupported  -5024
#define kFPObjectTypeErr     -5025
#define kFPDirNotFound       -5029
#define kFPUserNotAuth       -5023

/* srvrInfoSupportsSrvrMsg — FPGetSrvrInfo Flags bit 3 (core/service/afp/
 * handlers.go). Not otherwise used by this stage but documented here since
 * GetServerInfo logs the raw Flags word. */
#define kFPSupportsSrvrMsg  0x0008

#endif /* E2E_AFP_CONST_H */
