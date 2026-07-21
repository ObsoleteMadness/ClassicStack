#include <string.h>
#include "macipx.h"

/* Copy a C string into out (clamped), returning the sentinel. Shared by both
 * placeholders so their shape matches the real implementations that will
 * replace them (fill an out buffer, return a status). */
static short StubFill(char *out, int outSize, const char *s) {
    if (outSize > 0) {
        int n = (int)strlen(s);
        if (n > outSize - 1) {
            n = outSize - 1;
        }
        memcpy(out, s, (size_t)n);
        out[n] = '\0';
    }
    return MACIPX_NOT_IMPLEMENTED;
}

/* PLACEHOLDER — see macipx.h. When implemented, this opens the '.IPX' driver
 * and issues the IPXGetVersion SPI call, formatting the returned major.minor
 * into outVersion. */
short GetMacIPXVersion(char *outVersion, int outSize) {
    return StubFill(outVersion, outSize, "MacIPX not implemented");
}

/* PLACEHOLDER — see macipx.h. When implemented, this issues
 * IPXGetInternetworkAddress and formats the 4-byte network, 6-byte node and
 * 2-byte socket as "netnum.node.socket". */
short GetIPXAddress(char *outAddr, int outSize) {
    return StubFill(outAddr, outSize, "0.0.0");
}
