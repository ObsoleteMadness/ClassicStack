"""Verify a netboot chain-read capture against the disk image it served.

Reconstructs every block the server actually put on the wire (EBP cmd 129
replies, correlated to their cmd 128 request) and compares it byte-for-byte
with the same sector of the source image. Answers "did we deliver the right
bytes?" -- which separates a data-corruption bug from a client-side fault.

Also decodes the diagnostic counter ChainDisk smuggles out in the unused
imageNum field (the server logs it as diag=).

    tshark -r tashtalk.pcap -T fields -e frame.number -e frame.time_relative \
           -e data.data > raw.txt
    python verifychain.py raw.txt "Disk1.dsk"

EBP wire forms (spec/19-netboot.md Part B):
    cmd 128 request : cmd(1) pad(1) seq(2) imageNum(4) blockOffset(4) blockCount(4)
    cmd 129 data    : cmd(1) blkIndex(1) seq(2) data(512)
    cmd 130 write   : cmd(1) blkIndex(1) seq(2) imageNum(4) hunkStart(4)
"""

import io
import struct
import sys
import os
from collections import Counter

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from hfsvol import HFSVolume, SECTOR

CMD_READ_REQ = 128
CMD_READ_DATA = 129
CMD_WRITE = 130
CMD_WRITE_ACK = 131
BLOCKS_PER_CHUNK = 32


def load_payloads(path):
    rows = []
    for line in io.open(path):
        p = line.rstrip('\n').split('\t')
        if len(p) < 3 or not p[2]:
            continue
        rows.append((int(p[0]), float(p[1]), bytes.fromhex(p[2].replace(':', ''))))
    return rows


def decode(rows):
    reqs, served, groups = [], {}, []
    cur = None
    for fn, t, b in rows:
        if not b:
            continue
        if b[0] == CMD_READ_REQ and len(b) >= 16:
            seq = struct.unpack('>H', b[2:4])[0]
            img, off, cnt = struct.unpack('>III', b[4:16])
            if cur:
                groups.append(cur)
            cur = dict(frame=fn, time=t, seq=seq, diag=img, off=off, cnt=cnt, blocks=[])
            reqs.append(cur)
        elif b[0] == CMD_READ_DATA and len(b) >= 4 and cur is not None:
            blk = b[1] & 0x7F
            rseq = struct.unpack('>H', b[2:4])[0]
            if rseq != cur['seq']:
                continue
            data = b[4:4 + SECTOR]
            if len(data) == SECTOR:
                served[cur['off'] + blk] = data
                cur['blocks'].append(blk)
    if cur:
        groups.append(cur)
    return reqs, served, groups


def main(argv):
    if len(argv) < 3:
        print(__doc__)
        return 2
    rows = load_payloads(argv[1])
    reqs, served, groups = decode(rows)

    cmds = Counter(b[0] for _, _, b in rows if b)
    print("=== wire summary ===")
    print("read requests (128) : %d" % cmds.get(CMD_READ_REQ, 0))
    print("data replies  (129) : %d" % cmds.get(CMD_READ_DATA, 0))
    print("writes        (130) : %d" % cmds.get(CMD_WRITE, 0))
    print("write acks    (131) : %d" % cmds.get(CMD_WRITE_ACK, 0))
    if served:
        print("distinct blocks     : %d (range %d..%d)" %
              (len(served), min(served), max(served)))

    print()
    print("=== short reads (got fewer blocks than min(cnt,32)) ===")
    short = [g for g in groups if len(g['blocks']) != min(g['cnt'], BLOCKS_PER_CHUNK)]
    if not short:
        print("none -- every request received exactly the blocks it asked for")
    for g in short[:20]:
        print("  seq=%-5d off=%-7d cnt=%-4d expected=%-3d got=%d" %
              (g['seq'], g['off'], g['cnt'],
               min(g['cnt'], BLOCKS_PER_CHUNK), len(g['blocks'])))

    print()
    print("=== data integrity vs image ===")
    bad = []
    with HFSVolume(argv[2]) as vol:
        for blk in sorted(served):
            if vol.read_sector(blk) != served[blk]:
                bad.append(blk)
        print("blocks compared: %d   MISMATCHES: %d" % (len(served), len(bad)))
        if bad:
            print("first mismatching blocks: %s" % bad[:20])
        else:
            print("every block served matches the image byte-for-byte")

        print()
        print("=== tail of the stream ===")
        for g in groups[-6:]:
            print("  f%-6d %8.3f seq=%-5d diag=0x%08x off=%-7d cnt=%-4d got=%d" %
                  (g['frame'], g['time'], g['seq'], g['diag'],
                   g['off'], g['cnt'], len(g['blocks'])))
        if groups:
            last = groups[-1]['off']
            print()
            print("  last sector requested: %d" % last)
            for fi, which, s0 in vol.owner_of_sector(last):
                print("    owned by %s:%s (%s fork, type '%s')" %
                      (vol.path_of(fi.parent), fi.name, which, fi.type))

    print()
    print("=== ChainDisk diag counters (imageNum) ===")
    print("bytes are [Prime _Read][Prime _Write][Prime other][SendWrite entries]")
    if reqs:
        f, l = reqs[0]['diag'], reqs[-1]['diag']
        print("  first=0x%08x  last=0x%08x" % (f, l))
        for i, nm in enumerate(('Prime _Read', 'Prime _Write',
                                'Prime other', 'SendWrite')):
            shift = 24 - 8 * i
            nz = sum(1 for r in reqs if (r['diag'] >> shift) & 0xFF)
            wraps = sum(1 for i2 in range(1, len(reqs))
                        if ((reqs[i2]['diag'] >> shift) & 0xFF) <
                           ((reqs[i2 - 1]['diag'] >> shift) & 0xFF))
            total = wraps * 256 + ((l >> shift) & 0xFF)
            print("  %-14s final=%-4d wraps=%-3d total=%-5d (non-zero in %d requests)" %
                  (nm, (l >> shift) & 0xFF, wraps, total, nz))
    return 0


if __name__ == '__main__':
    sys.exit(main(sys.argv))
