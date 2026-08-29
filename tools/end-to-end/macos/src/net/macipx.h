/* MacIPX client placeholders.
 *
 * MacIPX is Novell's IPX/SPX stack for the classic Mac OS: a '.IPX' driver
 * (plus an 'INIT') that speaks IPX either natively over Ethernet/Token Ring or
 * tunnelled over AppleTalk (the encapsulation ClassicStack's MacIPX gateway
 * bridges — see the project's MacIPX gateway notes). Applications reach it
 * through the IPX SPI: an 'IPXGetVersion'-style call for the driver version and
 * 'IPXGetInternetworkAddress' for the local IPX net.node.socket.
 *
 * These are deliberately PLACEHOLDERS for now — enough of an interface and
 * script vocabulary (GetMacIPXVersion / GetIPXAddress) to slot into the test
 * harness, so the IPX path can be fleshed out against ClassicStack's MacIPX
 * gateway later without reshaping the command table. They report
 * "not implemented" cleanly rather than making any driver call, and never
 * touch the .IPX driver, so they are safe to run on a System without MacIPX.
 */
#ifndef E2E_MACIPX_H
#define E2E_MACIPX_H

/* Sentinel returned by the placeholders: the feature isn't implemented yet.
 * Distinct from any real OSErr so the caller/logs can tell "stub" from a
 * genuine driver error once these are implemented for real. */
#define MACIPX_NOT_IMPLEMENTED 1

/* Placeholder: intended to return the MacIPX driver version (via the IPX SPI
 * IPXGetVersion equivalent once implemented). Fills `outVersion` with a
 * human-readable string and returns MACIPX_NOT_IMPLEMENTED for now. */
short GetMacIPXVersion(char *outVersion, int outSize);

/* Placeholder: intended to return the local IPX internetwork address as
 * "netnum.node.socket" (via IPXGetInternetworkAddress once implemented).
 * Fills `outAddr` and returns MACIPX_NOT_IMPLEMENTED for now. */
short GetIPXAddress(char *outAddr, int outSize);

#endif /* E2E_MACIPX_H */
