"""Read-only HFS volume walker for diagnosing netboot / AFP captures.

Written for ClassicStack while diagnosing a netboot hang: given a raw .dsk
image it answers "which file and which resource owns sector N?", which is what
turns an offset in a chain-read capture into something you can reason about.

Read-only by design. It never writes to the image, so it is safe to point at a
live boot volume.

Layout references
-----------------
MDB (Inside Macintosh: Files, "Master Directory Block"), at byte 1024:
    drSigWord   0   drAtrb     10   drNmAlBlks 18   drAlBlkSiz 20
    drAlBlSt   28   drVN       36   drXTFlSize 130  drXTExtRec 134
    drCTFlSize 146  drCTExtRec 150
Catalog B*-tree nodes are 512 bytes; leaf nodes have ndType 0xFF.
Resource fork layout: Inside Macintosh: More Macintosh Toolbox, "Resource
Manager"; the map's type list is at mapOff+24.

Usage as a library:
    vol = HFSVolume(path)
    for f in vol.files(): ...
    vol.owner_of_sector(34132)
    ResourceFork(vol.read_fork(filerec, 'rsrc')).resource_at(offset)
"""

import io
import struct

SECTOR = 512
NODE = 512
LEAF = 0xFF
CDR_DIR = 1
CDR_FIL = 2


def _mac(b):
    return b.decode('mac-roman', 'replace')


class FileRec(object):
    __slots__ = ('name', 'parent', 'id', 'type', 'creator',
                 'dext', 'rext', 'dlen', 'rlen')

    def __init__(self, **kw):
        for k in self.__slots__:
            setattr(self, k, kw.get(k))

    def __repr__(self):
        return "<FileRec %r id=%d '%s'/'%s' dlen=%d rlen=%d>" % (
            self.name, self.id, self.type, self.creator, self.dlen, self.rlen)


class HFSVolume(object):
    def __init__(self, path):
        self.path = path
        self._f = io.open(path, 'rb')
        self._f.seek(1024)
        m = self._f.read(162)
        if struct.unpack('>H', m[0:2])[0] != 0x4244:
            raise ValueError('not an HFS volume (drSigWord != 0x4244): %s' % path)
        self.attrs = struct.unpack('>H', m[10:12])[0]
        self.num_al_blks = struct.unpack('>H', m[18:20])[0]
        self.al_blk_size = struct.unpack('>I', m[20:24])[0]
        self.al_blk_start = struct.unpack('>H', m[28:30])[0]
        self.free_blks = struct.unpack('>H', m[34:36])[0]
        self.name = _mac(m[37:37 + m[36]])
        self.cat_size = struct.unpack('>I', m[146:150])[0]
        self.cat_ext = struct.unpack('>HHHHHH', m[150:162])
        self.sectors_per_ablk = self.al_blk_size // SECTOR
        self._files = None
        self._dirs = None

    @property
    def clean_unmount(self):
        """drAtrb bit 8 -- set when the volume was unmounted cleanly."""
        return bool(self.attrs & 0x0100)

    def ablk_to_sector(self, ab):
        return self.al_blk_start + ab * self.sectors_per_ablk

    def read_extents(self, extrec, limit=None):
        out = b''
        for i in range(0, 6, 2):
            start, count = extrec[i], extrec[i + 1]
            if not count:
                continue
            self._f.seek(self.ablk_to_sector(start) * SECTOR)
            out += self._f.read(count * self.al_blk_size)
        return out[:limit] if limit else out

    def read_fork(self, rec, which='rsrc'):
        ext = rec.rext if which == 'rsrc' else rec.dext
        ln = rec.rlen if which == 'rsrc' else rec.dlen
        return self.read_extents(ext, ln)

    def read_sector(self, n):
        self._f.seek(n * SECTOR)
        return self._f.read(SECTOR)

    def _walk_catalog(self):
        cat = self.read_extents(self.cat_ext, self.cat_size)
        files, dirs = [], {}
        for n in range(len(cat) // NODE):
            nd = cat[n * NODE:(n + 1) * NODE]
            if len(nd) < 14 or nd[8] != LEAF:
                continue
            nrecs = struct.unpack('>H', nd[10:12])[0]
            try:
                offs = [struct.unpack('>H', nd[NODE - 2 * (r + 1):NODE - 2 * r])[0]
                        for r in range(nrecs + 1)]
            except struct.error:
                continue
            for r in range(nrecs):
                s, e = offs[r], offs[r + 1]
                if not (0 < s < e <= NODE):
                    continue
                rec = nd[s:e]
                klen = rec[0]
                if klen < 5 or len(rec) < 1 + klen:
                    continue
                key = rec[1:1 + klen]
                parent = struct.unpack('>I', key[1:5])[0]
                nm = _mac(key[6:6 + key[5]])
                d = 1 + klen
                d += d & 1
                data = rec[d:]
                if not data:
                    continue
                if data[0] == CDR_DIR and len(data) >= 70:
                    dirs[struct.unpack('>I', data[6:10])[0]] = (nm, parent)
                elif data[0] == CDR_FIL and len(data) >= 98:
                    usr = data[4:20]
                    files.append(FileRec(
                        name=nm, parent=parent,
                        id=struct.unpack('>I', data[20:24])[0],
                        type=_mac(usr[0:4]), creator=_mac(usr[4:8]),
                        dlen=struct.unpack('>I', data[26:30])[0],
                        rlen=struct.unpack('>I', data[36:40])[0],
                        dext=struct.unpack('>HHHHHH', data[74:86]),
                        rext=struct.unpack('>HHHHHH', data[86:98])))
        self._files, self._dirs = files, dirs

    def files(self):
        if self._files is None:
            self._walk_catalog()
        return self._files

    def dirs(self):
        if self._dirs is None:
            self._walk_catalog()
        return self._dirs

    def path_of(self, parent_id):
        dirs = self.dirs()
        parts, seen = [], set()
        while parent_id in dirs and parent_id not in seen:
            seen.add(parent_id)
            nm, up = dirs[parent_id]
            parts.append(nm)
            parent_id = up
        return ':'.join(reversed(parts))

    def extent_covers(self, extrec, sector):
        """-> (first_sector_of_fork, True) if sector lies in these extents."""
        for i in range(0, 6, 2):
            start, count = extrec[i], extrec[i + 1]
            if not count:
                continue
            s0 = self.ablk_to_sector(start)
            if s0 <= sector < s0 + count * self.sectors_per_ablk:
                return s0, True
        return None, False

    def owner_of_sector(self, sector):
        """-> list of (FileRec, 'data'|'rsrc', first_sector_of_fork).

        Only consults the three in-catalog extents; a heavily fragmented fork
        whose later extents live in the extents-overflow file will not match.
        """
        hits = []
        for fi in self.files():
            for which, ext in (('data', fi.dext), ('rsrc', fi.rext)):
                s0, ok = self.extent_covers(ext, sector)
                if ok:
                    hits.append((fi, which, s0))
        return hits

    def close(self):
        self._f.close()

    def __enter__(self):
        return self

    def __exit__(self, *a):
        self.close()


class ResourceFork(object):
    """Minimal resource-map parser: enough to say which resource owns a byte."""

    def __init__(self, blob):
        self.blob = blob
        self.data_off, self.map_off, self.data_len, self.map_len = \
            struct.unpack('>IIII', blob[:16])
        self._res = None

    def resources(self):
        """-> sorted list of (fork_offset, length, type, id)."""
        if self._res is not None:
            return self._res
        mp = self.blob[self.map_off:self.map_off + self.map_len]
        tlo = struct.unpack('>H', mp[24:26])[0]
        ntypes = struct.unpack('>h', mp[tlo:tlo + 2])[0] + 1
        out = []
        for i in range(ntypes):
            o = tlo + 2 + i * 8
            tname = _mac(mp[o:o + 4])
            count = struct.unpack('>H', mp[o + 4:o + 6])[0] + 1
            rlo = struct.unpack('>H', mp[o + 6:o + 8])[0]
            for j in range(count):
                ro = tlo + rlo + j * 12
                if ro + 12 > len(mp):
                    continue
                rid = struct.unpack('>h', mp[ro:ro + 2])[0]
                doff = struct.unpack('>I', mp[ro + 4:ro + 8])[0] & 0xFFFFFF
                abs_off = self.data_off + doff
                if abs_off + 4 > len(self.blob):
                    continue
                ln = struct.unpack('>I', self.blob[abs_off:abs_off + 4])[0]
                out.append((abs_off, ln, tname, rid))
        out.sort()
        self._res = out
        return out

    def resource_at(self, offset):
        """-> (fork_offset, length, type, id) containing this fork byte, or None."""
        for a, l, t, i in self.resources():
            if a <= offset < a + 4 + l:
                return (a, l, t, i)
        return None
