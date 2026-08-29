/* AFP command builders/parsers, on top of the asp.h/atalk.h transport
 * wrappers. Each function here corresponds to one FP* AFP command from
 * spec/AFP_Connection_Flow.md; the mapping to script commands
 * (EnumerateServers, GetServerInfo, ...) happens in commands.c.
 */
#ifndef E2E_AFP_CLIENT_H
#define E2E_AFP_CLIENT_H

#include <AppleTalk.h>

#define AFP_MAX_VERSIONS 8
#define AFP_MAX_UAMS 8

typedef struct {
    char serverName[33];
    char machineType[33];
    char afpVersions[AFP_MAX_VERSIONS][33];
    short versionCount;
    char uams[AFP_MAX_UAMS][33];
    short uamCount;
    unsigned short flags;
} AfpServerInfo;

/* Parses a raw FPGetSrvrInfo block (as returned by ASPGetStatus) into
 * `out`. Layout: core/service/afp/handlers.go serverInfoBlock (mirrored in
 * a comment here) — 4 big-endian uint16 offsets (MachineType, AFPVersion
 * count, UAM count, icon — unused) + uint16 Flags, then a Pascal-string
 * ServerName immediately after the 10-byte header, then (at MachineType's
 * offset) a Pascal-string MachineType, a uint8 count + that many Pascal
 * strings for AFP versions, and the same shape for UAMs. Returns 0 on
 * success, non-zero if the block is too short/malformed. */
int AfpParseServerInfo(const unsigned char *buf, short len, AfpServerInfo *out);

#define AFP_MAX_VOLUMES 32

typedef struct {
    char names[AFP_MAX_VOLUMES][33];
    short count;
} AfpVolumeList;

/* Fetches the server's volume ("share") list, exactly as the Chooser's
 * AppleShare client does before it shows you the volume picker: open an ASP
 * session to `serverAddr`, FPLogin as guest ("No User Authent" UAM), FPGetSrvrParms,
 * parse the volume names out of the reply, then FPLogout and close the session.
 * Fills `out` and returns 0 on success, or a negative OSErr / positive AFP
 * result code on failure. Session-less from the caller's point of view: it
 * opens and tears down its own short-lived login, since a real mount later
 * establishes its own session via PBVolMount. Models AFP.c LogOnAsGuest +
 * GetServerParams + GetNumberVolumes/ExtractVolumeName. */
short AfpListVolumes(const AddrBlock *serverAddr, AfpVolumeList *out);

#endif /* E2E_AFP_CLIENT_H */
