/* Integer-only printf family — keeps the single CODE segment under 64 KB.
 *
 * This is a 68K --mac-single (single-segment) app: ALL code lands in one CODE
 * segment reached by intra-segment references. Once that segment crosses 64 KB
 * the single-segment startup/relocation breaks and the first real Toolbox call
 * jumps to garbage — the exact "illegal instruction / bus error at IsMPPOpen"
 * symptom, since IsMPPOpen (a pure low-memory read) cannot fault on its own.
 *
 * Newlib's default printf/sprintf/fprintf pull in the floating-point formatter
 * (_vfprintf_r + _dtoa_r ≈ 20 KB). This app formats only integers and strings,
 * never floats, so we redirect the whole family to newlib's integer-only
 * variants (iprintf/siprintf/fiprintf). That drops _dtoa_r entirely and keeps
 * the segment comfortably below 64 KB.
 *
 * Force-included into every translation unit via the build's -include flag (see
 * CMakeLists.txt), so no source file has to remember to include it. If you ever
 * genuinely need %f/%e/%g, print it a different way — do NOT re-enable the float
 * formatter without checking the linked .code.bin stays under 64 KB.
 */
#ifndef E2E_PRINTF_COMPAT_H
#define E2E_PRINTF_COMPAT_H

#include <stdio.h>

/* Map the (variadic) printf family to the integer-only newlib entry points.
 * Only the forms this app actually uses are mapped. */
#define sprintf  siprintf
#define fprintf  fiprintf
#define printf   iprintf

#endif /* E2E_PRINTF_COMPAT_H */
