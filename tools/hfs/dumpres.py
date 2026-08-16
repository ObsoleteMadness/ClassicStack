"""Extract a resource from a file on an HFS image, and scan it for trap patches.

    python dumpres.py <image.dsk> <file name> --list
    python dumpres.py <image.dsk> <file name> citt 43 [-o out.bin]
    python dumpres.py <image.dsk> <file name> citt 43 --traps

--traps scans the resource for 68k trap words ($Axxx) and reports them by name,
which is how you find out whether a system extension patches the Device Manager
traps a third-party driver depends on.
"""

import sys
import os
import struct

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from hfsvol import HFSVolume, ResourceFork

# Traps that matter to a block driver, plus the common patch vectors.
TRAPS = {
    0xA000: '_Open', 0xA001: '_Close', 0xA002: '_Read', 0xA003: '_Write',
    0xA004: '_Control', 0xA005: '_Status', 0xA006: '_KillIO',
    0xA007: '_GetVolInfo', 0xA008: '_Create', 0xA009: '_Delete',
    0xA00A: '_OpenRF', 0xA00B: '_Rename', 0xA00C: '_GetFileInfo',
    0xA00D: '_SetFileInfo', 0xA00E: '_UnmountVol', 0xA00F: '_MountVol',
    0xA010: '_Allocate', 0xA011: '_GetEOF', 0xA012: '_SetEOF',
    0xA013: '_FlushVol', 0xA014: '_GetVol', 0xA015: '_SetVol',
    0xA016: '_FInitQueue', 0xA017: '_Eject', 0xA018: '_GetFPos',
    0xA019: '_InitZone', 0xA01B: '_SetZone', 0xA01C: '_FreeMem',
    0xA01F: '_DisposePtr', 0xA020: '_SetPtrSize', 0xA021: '_GetPtrSize',
    0xA022: '_NewHandle', 0xA023: '_DisposeHandle', 0xA024: '_SetHandleSize',
    0xA025: '_GetHandleSize', 0xA029: '_HLock', 0xA02A: '_HUnlock',
    0xA02E: '_BlockMove', 0xA02F: '_PostEvent', 0xA030: '_OSEventAvail',
    0xA033: '_CmpString', 0xA035: '_Offline', 0xA036: '_MoreMasters',
    0xA03C: '_CmpString', 0xA03F: '_InitFS', 0xA040: '_ResrvMem',
    0xA044: '_SetFPos', 0xA045: '_FlushFile', 0xA047: '_SetTrapAddress',
    0xA049: '_HPurge', 0xA04A: '_HNoPurge', 0xA04C: '_CompactMem',
    0xA04D: '_PurgeMem', 0xA051: '_ReadXPRam', 0xA054: '_UprString',
    0xA055: '_StripAddress', 0xA058: '_InsTime', 0xA059: '_RmvTime',
    0xA05A: '_PrimeTime', 0xA05B: '_PowerOff', 0xA05C: '_MemoryDispatch',
    0xA060: '_FSDispatch', 0xA063: '_MaxBlock', 0xA064: '_PurgeSpace',
    0xA065: '_MaxApplZone', 0xA069: '_HGetState', 0xA06A: '_HSetState',
    0xA06E: '_SlotManager', 0xA07D: '_GetDefaultStartup',
    0xA080: '_InitDogCow', 0xA085: '_InitProcMenu',
    0xA089: '_SwapMMUMode', 0xA08D: '_DebugUtil',
    0xA146: '_GetTrapAddress', 0xA746: '_GetToolTrapAddress',
    0xA647: '_SetToolTrapAddress', 0xA43F: '_HFSDispatch',
    # Async / immediate variants of the Device Manager traps: bit 10 ($400)
    # selects async, so _Read ($A002) issued async is $A402. A driver-hostile
    # patch usually shows up on these.
    0xA400: '_OpenAsync', 0xA402: '_ReadAsync', 0xA403: '_WriteAsync',
    0xA404: '_ControlAsync', 0xA405: '_StatusAsync', 0xA40F: '_MountVolAsync',
    0xA22E: '_BlockMoveData', 0xA460: '_FSDispatchAsync',
}


def scan_traps(blob):
    seen = {}
    for i in range(0, len(blob) - 1, 2):
        w = struct.unpack('>H', blob[i:i + 2])[0]
        if 0xA000 <= w <= 0xAFFF:
            seen.setdefault(w, []).append(i)
    return seen


def find_file(vol, name):
    hits = [f for f in vol.files() if f.name == name]
    if not hits:
        hits = [f for f in vol.files() if name.lower() in f.name.lower()]
    return hits


def main(argv):
    if len(argv) < 3:
        print(__doc__)
        return 2
    img, fname = argv[1], argv[2]
    rest = argv[3:]
    with HFSVolume(img) as vol:
        hits = find_file(vol, fname)
        if not hits:
            print("no file matching %r" % fname)
            return 1
        fi = hits[0]
        print("%s:%s  id=%d type='%s' rlen=%d" %
              (vol.path_of(fi.parent), fi.name, fi.id, fi.type, fi.rlen))
        rf = ResourceFork(vol.read_fork(fi, 'rsrc'))
        res = rf.resources()

        if not rest or rest[0] == '--list':
            bytype = {}
            for a, l, t, i in res:
                bytype.setdefault(t, []).append((i, l, a))
            print("%d resources, %d types" % (len(res), len(bytype)))
            for t in sorted(bytype):
                items = sorted(bytype[t])
                tot = sum(x[1] for x in items)
                print("  '%s'  n=%-4d total=%-9d  ids=%s" %
                      (t, len(items), tot,
                       [x[0] for x in items[:12]] + (['...'] if len(items) > 12 else [])))
            return 0

        rtype = rest[0]
        rid = int(rest[1]) if len(rest) > 1 and rest[1].lstrip('-').isdigit() else None
        match = [r for r in res if r[2] == rtype and (rid is None or r[3] == rid)]
        if not match:
            print("no resource '%s' id=%s" % (rtype, rid))
            return 1
        a, l, t, i = match[0]
        blob = rf.blob[a + 4:a + 4 + l]
        print("resource '%s' id=%d  len=%d  fork offset %d" % (t, i, l, a))

        if '--traps' in rest:
            seen = scan_traps(blob)
            print()
            print("trap words found (%d distinct):" % len(seen))
            interesting = []
            for w in sorted(seen):
                nm = TRAPS.get(w)
                n = len(seen[w])
                line = "  $%04X %-22s x%-4d first@%d" % (w, nm or '', n, seen[w][0])
                if nm:
                    interesting.append(line)
            for line in interesting:
                print(line)
            print()
            print("(unnamed $Axxx words omitted; %d named of %d distinct)" %
                  (len(interesting), len(seen)))
            print()
            print("first 64 bytes:")
            print("  " + blob[:64].hex())
            return 0

        out = None
        if '-o' in rest:
            out = rest[rest.index('-o') + 1]
        if out:
            open(out, 'wb').write(blob)
            print("wrote %d bytes to %s" % (len(blob), out))
        else:
            print("first 128 bytes:")
            print(blob[:128].hex())
    return 0


if __name__ == '__main__':
    sys.exit(main(sys.argv))
