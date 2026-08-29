/* AppleTalk zone discovery over the classic Toolbox drivers, modelled
 * directly on Apple DTS's GetZoneList sample (GetZoneList.c v1.04, the
 * BuildZoneListPhase2 path). The point of stripping the client back to this
 * is to reproduce, byte for byte, the driver-init + ZIP GetZoneList sequence
 * a real Mac uses — so if our server still doesn't answer, the fault is
 * unambiguously on the server, not in some divergence in how we drive the
 * Toolbox.
 *
 * Init sequence: OpenDriver("\p.MPP") (only if !IsMPPOpen) then ATPLoad (only
 * if !IsATPOpen); then, when zones are actually requested,
 * OpenDriver("\p.XPP", &refnum) to get the XPP driver reference number (the
 * safe way — never the -41 constant). We use the synchronous OpenDriver rather
 * than the MPPOpen() glue because MPPOpen's async _Open path bus-errors under
 * our Retro68 runtime; see atalk.c for the full disassembly rationale.
 */
#ifndef E2E_ATALK_H
#define E2E_ATALK_H

#include <AppleTalk.h>

#define ATALK_MAX_ZONES 16
#define ATALK_ZONE_NAME_SIZE 33  /* zone name is a 32-byte Pascal string; stored as C string */

/* Opens .MPP and loads ATP (MPPOpen + ATPLoad), exactly as Apple's
 * GetZoneList Initialize() does. Returns noErr (0) on success, else a
 * negative OSErr. Idempotent — safe to call more than once. Must run before
 * AtalkGetZones. */
short AtalkInit(void);

/* The AppleTalk driver version SysEnvirons reported during AtalkInit (>= 53
 * means Phase 2, where the .XPP ZIP GetZoneList xCall is available). 0 before
 * AtalkInit runs, or if SysEnvirons failed. Log this — it's the headline
 * diagnostic if zone discovery wedges or comes back empty. */
short AtalkDriverVersion(void);

/* Full zone list via the .XPP driver's ZIP GetZoneList xCall, driven by
 * PBControl in a loop until zipLastFlag is set — the BuildZoneListPhase2
 * algorithm from Apple's sample. Fills up to maxZones C-string names and
 * returns the count found (>=0), or a negative OSErr. noBridgeErr (-93) from
 * the first call means no router answered (no router on the segment). */
short AtalkGetZones(char zones[][ATALK_ZONE_NAME_SIZE], short maxZones);

#define ATALK_MAX_SERVERS 16
#define ATALK_SERVER_NAME_SIZE 33 /* NBP object name is a 32-byte Str; +NUL */

/* One AFP server discovered by NBP: its registered name and the AppleTalk
 * address (net/node/socket) its =:AFPServer@zone entity lives at — everything
 * ASPGetStatus / PBVolMount need to reach it. */
typedef struct {
    char name[ATALK_SERVER_NAME_SIZE];
    AddrBlock addr;
} AtalkServer;

/* NBP lookup of AppleShare file servers — PLookupName for the entity
 * "=:AFPServer@<zone>" (the type AppleShare servers register under), exactly
 * as the Chooser does. Pass zone="*" for the local zone. Fills up to maxServers
 * entries and returns the count found (>=0), or a negative OSErr. 0 means the
 * lookup ran cleanly but no server answered. Models Apple's NBP.c NBPGetList +
 * NBPGetAddress. */
short AtalkFindServers(const char *zone, AtalkServer *servers, short maxServers);

#endif /* E2E_ATALK_H */
