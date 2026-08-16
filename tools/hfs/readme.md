# HFS image tools

Read-only Python tools for inspecting classic Mac HFS disk images and
correlating them with protocol captures. Written while diagnosing a netboot
hang; kept because "which file owns sector N?" comes up constantly when reading
an AFP or netboot capture.

Nothing here writes to an image, so they are safe to point at a live boot
volume. Python 3, no dependencies.

| file | role |
|---|---|
| `hfsvol.py` | library: MDB + catalog B*-tree walker, resource-fork map parser |
| `whatsat.py` | which file (and resource) owns a given sector |
| `verifychain.py` | verify a netboot chain-read capture against the image it served |
| `dumpres.py` | list/extract resources; scan one for 68k trap patches |

## whatsat.py — turn a sector number into a name

```
python whatsat.py "Disk1.dsk" --info
python whatsat.py "Disk1.dsk" 34132
python whatsat.py "Disk1.dsk" --find "System 7.5 Update"
```

```
sector 34132
  System 7.5.3:System Folder:System 7.5 Update
    id=182 type='gbly' creator='MACS' rsrc fork (starts at sector 29779)
    resource 'citt' id=43 len=41664 at fork offset 2187394
    resource spans sectors 34051..34132  <- sector is the LAST of it
```

That "LAST of it" marker is the useful part: a stream that stops on a resource
boundary stopped because of what the client did with the resource, not because
delivery failed.

`--info` also reports `drAtrb` bit 8, i.e. whether the volume was unmounted
cleanly.

## verifychain.py — did we serve the right bytes?

Separates a data-corruption bug from a client-side fault. Reconstructs every
block the server put on the wire and compares it with the image.

```
tshark -r tashtalk.pcap -T fields -e frame.number -e frame.time_relative \
       -e data.data > raw.txt
python verifychain.py raw.txt "Disk1.dsk"
```

Reports the EBP command histogram, any short reads, a byte-for-byte comparison,
the tail of the stream with the owning file, and the decoded `gDrvrDiag`
counters ChainDisk smuggles out in the unused `imageNum` field (the server logs
it as `diag=`). See `spec/19-netboot.md` for the packed-byte layout.

On Windows, `tshark` is at `c:\Program Files\Wireshark\tshark.exe`.

## dumpres.py — what is this resource, and what does it patch?

```
python dumpres.py "Disk1.dsk" "System 7.5 Update" --list
python dumpres.py "Disk1.dsk" "System 7.5 Update" citt 43 --traps
python dumpres.py "Disk1.dsk" "System 7.5 Update" citt 43 -o citt43.bin
```

`--traps` scans for `$Axxx` trap words and names the ones that matter to a block
driver. This is how the netboot stop was identified: `citt` id=43 patches no
Device Manager traps at all, while its sibling `gcko` id=43 patches `_Read`,
`_Write`, `_GetVolInfo`, `_Create`, `_GetFileInfo`, `_FlushVol` and
`_FSDispatch`.

## Limitations

- `owner_of_sector` consults only the three extents held in the catalog record.
  A heavily fragmented fork continued in the extents-overflow file will report
  no owner; that file is not parsed.
- HFS only (`drSigWord` 0x4244). HFS+ (`H+`) is not supported.
- The resource-map parser reads the type and reference lists only — no names,
  no attributes.
