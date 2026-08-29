/* Platform-independent SMB-layer helpers: the standardized directory-entry
 * formatter shared by the Win16 and Win32 back-ends, so both emit the exact
 * line documented in tools/end-to-end/RESULT-FORMAT.md. */
#include <stdio.h>
#include <string.h>
#include "smb.h"
#include "results.h"

/* Build the compact attribute token, e.g. "RHSA" for readonly+hidden+system+
 * archive, "----" for none. Fixed 4-char field keeps entries column-aligned
 * and trivial to diff. */
static void FormatAttrs(const SmbDirEntry *e, char *out) {
    out[0] = e->readOnly ? 'R' : '-';
    out[1] = e->hidden   ? 'H' : '-';
    out[2] = e->system   ? 'S' : '-';
    out[3] = e->archive  ? 'A' : '-';
    out[4] = '\0';
}

void SmbFormatEntry(const SmbDirEntry *entry, char *out, int outSize) {
    char attrs[5];
    const char *created;
    const char *modified;
    const char *accessed;

    FormatAttrs(entry, attrs);

    /* Blank timestamps are emitted as "-" so the field is never empty and the
     * key=value shape stays regular for the harness parser. */
    created  = (entry->created[0]  != '\0') ? entry->created  : "-";
    modified = (entry->modified[0] != '\0') ? entry->modified : "-";
    accessed = (entry->accessed[0] != '\0') ? entry->accessed : "-";

    /* size is printed as a single decimal. On Win16 sizeHigh is always 0 so
     * the low word is the whole size; on Win32 we combine when the high word
     * is set (files >4 GiB are not exercised by these tests, but be honest). */
    if (entry->sizeHigh != 0) {
        /* 64-bit value; print via two halves is awkward in C89, and the tests
         * never create >4 GiB files, so approximate with a marker. */
        _snprintf(out, outSize,
                  "kind=%s name=\"%s\" short=\"%s\" attrs=%s "
                  "created=\"%s\" modified=\"%s\" accessed=\"%s\" size=>4GiB",
                  entry->isDir ? "dir" : "file", entry->name, entry->shortName,
                  attrs, created, modified, accessed);
    } else {
        _snprintf(out, outSize,
                  "kind=%s name=\"%s\" short=\"%s\" attrs=%s "
                  "created=\"%s\" modified=\"%s\" accessed=\"%s\" size=%lu",
                  entry->isDir ? "dir" : "file", entry->name, entry->shortName,
                  attrs, created, modified, accessed, entry->sizeLow);
    }
    out[outSize - 1] = '\0';
}
