/* AFP volume mounting over the AppleShare client, via PBVolumeMount with an
 * AFPVolMountInfo record — the same File Manager path the Chooser drives when
 * you pick a server and volume. Once mounted, the share is an ordinary Mac
 * volume (a vRefNum) that the File Manager traps in fileops.c operate on, so
 * every subsequent file/fork/directory operation goes over the real AFP
 * client stack exactly as the Finder's would.
 *
 * This depends on the AppleShare client software being present in the booted
 * System (it supplies the 'afpm' external file system PBVolumeMount hands the
 * record to). System 7.1 ships it. The record carries the server name and zone
 * as Pascal strings; the AppleShare client does its own NBP lookup to find the
 * server's address, so we do NOT need to pre-resolve the AppleTalk address for
 * the mount (though ListServers still does, for reporting).
 */
#ifndef E2E_VOLMOUNT_H
#define E2E_VOLMOUNT_H

/* Mounts AFP volume `volName` on server `serverName` in `zone` as a guest
 * (No User Authentication), returning the mounted volume's vRefNum via
 * *outVRefNum. `zone` may be "*" for the local zone. Returns noErr (0) on
 * success, or a negative OSErr (e.g. nsvErr / afpAccessDenied) on failure.
 * Names are C strings; they are packed into the mount record as Pascal
 * strings internally. */
short VolMountAFP(const char *zone, const char *serverName, const char *volName,
                  short *outVRefNum);

/* Unmounts (ejects+offlines) a previously mounted AFP volume by vRefNum via
 * UnmountVol. Returns noErr on success or a negative OSErr. */
short VolUnmount(short vRefNum);

#endif /* E2E_VOLMOUNT_H */
