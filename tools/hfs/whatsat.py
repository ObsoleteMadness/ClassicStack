"""Map sector numbers on an HFS image to the file (and resource) that owns them.

Turns an offset seen in a netboot / AFP capture into a name you can reason
about. Read-only.

    python whatsat.py <image.dsk> 34132 [34052 ...]
    python whatsat.py <image.dsk> --info
    python whatsat.py <image.dsk> --find 'System 7.5 Update'

Example -- diagnosing where a netboot stream stopped:

    $ python whatsat.py "Disk1.dsk" 34132
    sector 34132
      System 7.5.3:System Folder:System 7.5 Update
        id=182 type='gbly' creator='MACS' rsrc fork (starts at sector 29779)
        resource 'citt' id=43 len=41664 at fork offset 2187394
        resource spans sectors 34051..34132  <- sector is the LAST of it
"""

import sys
import os

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from hfsvol import HFSVolume, ResourceFork, SECTOR


def show_info(vol):
    print("volume      : %s" % vol.name)
    print("image       : %s" % vol.path)
    print("alloc blocks: %d x %d bytes (first at sector %d)" %
          (vol.num_al_blks, vol.al_blk_size, vol.al_blk_start))
    print("free blocks : %d" % vol.free_blks)
    print("drAtrb      : 0x%04X (%s)" % (
        vol.attrs,
        "unmounted cleanly" if vol.clean_unmount else "DIRTY -- not cleanly unmounted"))
    print("catalog     : %d bytes" % vol.cat_size)
    print("files/dirs  : %d / %d" % (len(vol.files()), len(vol.dirs())))


def show_sector(vol, sector):
    print("sector %d" % sector)
    hits = vol.owner_of_sector(sector)
    if not hits:
        print("  (no owner in the first three extents of any file --")
        print("   either free space or a fork continued in the extents-overflow file)")
        return
    for fi, which, fork_first_sector in hits:
        print("  %s:%s" % (vol.path_of(fi.parent), fi.name))
        print("    id=%d type='%s' creator='%s' %s fork (starts at sector %d)" %
              (fi.id, fi.type, fi.creator, which, fork_first_sector))
        if which != 'rsrc' or not fi.rlen:
            continue
        try:
            rf = ResourceFork(vol.read_fork(fi, 'rsrc'))
            off = (sector - fork_first_sector) * SECTOR
            hit = rf.resource_at(off)
        except Exception as exc:                       # malformed / truncated map
            print("    (resource map unreadable: %s)" % exc)
            continue
        if not hit:
            print("    (fork offset %d is not inside any resource)" % off)
            continue
        a, l, t, i = hit
        s0 = fork_first_sector + a // SECTOR
        s1 = fork_first_sector + (a + 4 + l) // SECTOR
        note = ""
        if sector == s1:
            note = "  <- sector is the LAST of it"
        elif sector == s0:
            note = "  <- sector is the FIRST of it"
        print("    resource '%s' id=%d len=%d at fork offset %d" % (t, i, l, a))
        print("    resource spans sectors %d..%d%s" % (s0, s1, note))


def main(argv):
    if len(argv) < 2:
        print(__doc__)
        return 2
    img = argv[1]
    args = argv[2:]
    with HFSVolume(img) as vol:
        if not args or args[0] == '--info':
            show_info(vol)
            return 0
        if args[0] == '--find':
            needle = args[1].lower()
            for fi in vol.files():
                if needle in fi.name.lower():
                    print("%s:%s" % (vol.path_of(fi.parent), fi.name))
                    print("   id=%d type='%s' creator='%s' dlen=%d rlen=%d" %
                          (fi.id, fi.type, fi.creator, fi.dlen, fi.rlen))
                    print("   data extents %s" % (fi.dext,))
                    print("   rsrc extents %s" % (fi.rext,))
            return 0
        for a in args:
            show_sector(vol, int(a))
            print()
    return 0


if __name__ == '__main__':
    sys.exit(main(sys.argv))
