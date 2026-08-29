---
title: "Netboot & ChainBoot"
weight: 6
---

# AppleTalk Netboot & ChainBoot

ClassicStack can netboot Old-World classic Macs whose ROM carries the `.netBOOT` /
`.ATBOOT` drivers (Macintosh Classic, IIci, and other SuperMario-era ROMs). A client
with netboot enabled in XPRAM discovers a "BootServer" over NBP, downloads a boot
payload over AppleTalk, verifies it with a checksum, and executes it as 68k code.

This is a from-scratch, spec-compliant reimplementation — there is no published Apple
spec for any of this; it is reverse-engineered from Apple's own SuperMario source tree
(`os/netboot/`) and from Elliot Nunn's prior reverse-engineering work. The full
byte-level wire protocol, every observed ROM quirk, and the debugging war stories behind
each fix live in [`spec/19-netboot.md`](../spec/19-netboot.md) — this document is the
operational picture: what the two protocols do, how they fit together, and how to
configure and build a working setup.

## Two protocols, layered

**Part A — ABP (Apple Boot Protocol).** This is Apple's own protocol, built into ROM.
The client opens DDP socket 10, finds a boot server by NBP (`<serverNum>:BootServer@*`),
and requests a boot image in fixed-size blocks (a request bitmap names which blocks are
still missing; the client retransmits on timeout). ABP has hard limits baked into the
ROM: the request bitmap caps an image at 4088 blocks (~2 MB at 512 bytes/block), and
`GetServer.c` additionally refuses an image bigger than a quarter of the machine's RAM,
because the whole thing is downloaded into RAM before it runs.

The payload ABP delivers is not a disk image — it's **executable 68k code**. `.ATBOOT`
calls it directly: `((j_code)(buffer))(getBootBlocks, g, &var1, &var2)`, driving three
call-backs in order (`getBootBlocks` → `getSysVol` → `mountSysVol`) that hand back boot
blocks, install a driver + Device Queue Entry, and mount a volume. Any payload that
implements this three-call contract works, regardless of how it gets its data — which
is what makes Part B possible.

Every payload also carries a trailer the ROM checks before running it: a Snefru-128 hash
of the payload body, in the last 16 bytes. ClassicStack computes and appends this
automatically at load time.

**Part B — ChainBoot EBP (an extension, not Apple's).** Designed by Elliot Nunn to get
around ABP's RAM-residency ceiling. The chain-loaded driver salvages the ABP server
address, increments the socket by one, and switches to a streaming block protocol
(commands 128–131: chunked reads/writes of up to 32 × 512-byte blocks at a time,
sequence-numbered, client-driven retransmission) against a **read/write** disk image
living on the server, with no size limit and no RAM-residency requirement. One client at
a time — the server image is mutated in place, so concurrent clients would corrupt it.

## Two payload styles

| | `ChainLoader.a` (Elliot's) | `ChainDisk.a` (ours) |
|---|---|---|
| Takes control by | Scanning the stack for the ROM's `_Read` return address and rewriting it | Implementing the three-call ABP contract and returning normally — no ROM assumptions |
| Portability | Depends on the exact ROM's `_Read` call shape — verified **false** on the LC 475 | ROM-independent by construction; proven on Macintosh Classic |
| EBP driver | Original, with the fix batch below | Same EBP driver as `ChainLoader`, wrapped in `BootWrapper`-style contract-conformant scaffolding |

`ChainDisk` also carries a `CSDSKSZ\0`-cookied volume-size field, stamped by
ClassicStack at load time (`stampDiskSize` in `compose/registry/reg_netboot.go`) — EBP
itself has no "how big is the disk?" query, and the server is the only party that knows
the image size. The stamp runs before the Snefru trailer is computed, so the hash covers
the stamped bytes.

Both payloads speak the identical EBP wire protocol, so ClassicStack serves either
unchanged; `ChainDisk` is the one to reach for on a ROM `ChainLoader`'s stack-scanning
trick doesn't work on.

## What we found and fixed

Getting a real (or accurately emulated) Mac through a full ChainBoot boot — not just a
protocol exchange, but System 6/7 actually coming up from a streamed image — surfaced a
long list of latent bugs in the original ChainBoot payload and driver, none of them
protocol-level: a flag-clobbering poll loop that made "all blocks arrived" unreachable
dead code, an odd-address longword read that page-faults on real 68000 CPUs (masked by
Mini vMac's lenient core), a socket-close call coded with the wrong opcode, a register
clobber that zeroed every write's target offset, and several race conditions between the
async send-completion and the packet filter that could hard-hang the driver. Each is
recorded with the wire evidence and the fix in
[`spec/19-netboot.md` "ChainDisk debugging notes"](../spec/19-netboot.md#chaindisk-debugging-notes-2026-08)
and in [`netboot/readme.md`](../netboot/readme.md#changes) — worth reading if you're
touching the driver or chasing a boot that stalls or Sad-Macs partway through.

The pacing behaviour is also non-obvious and configurable: real LocalTalk cannot deliver
frames faster than about 18 ms apart, and the client's own send-completion race means a
burst has to be **held** for one pace interval before the first reply goes out, or the
first block of every chunk is dropped by a disabled packet filter. See `chain_pace_ms`
below.

## Configuring a server

~~~toml
[Netboot]
enabled = true
payload = "/srv/netboot/BootWrapper.bin"  # boot payload or driver stub (ABP)
image = "/srv/netboot/system607.dsk"      # RAM-disk image appended to the stub
block_size = 512          # ABP block size: 512 for RAM-disk payloads,
                           # 256 for ChainLoader/ChainDisk (0 = 512)
disk = "/srv/netboot/system71.dsk"        # ChainBoot streamed image (excludes image=)
pace_ms = 2                # ABP block-send inter-packet delay (0 = 2 ms)
chain_pace_ms = 10         # ChainBoot read-reply BASE inter-packet delay (0 = 10 ms;
                            # real LocalTalk is ~18 ms/frame). The server backs off
                            # automatically on chunk-read retries, so this rarely
                            # needs tuning.
name = "0000"               # NBP object shown in the registry (matching is any-object)
zone = "*"                  # NBP zone to register in
~~~

Two serving shapes, picked by which keys you set:

- **RAM disk**: `payload` = a `BootWrapper`/romdrv-style driver stub, `image` = the
  (read-only) HFS disk image. ClassicStack concatenates and hashes them at load. Or
  point `payload` at an already-assembled file and omit `image`. Size limit ~2 MB and
  at most a quarter of the client's RAM.
- **ChainBoot**: `payload` = `ChainDisk.bin` or `ChainLoader.bin` (`block_size = 256`),
  `disk` = a full-size HFS image streamed read/write. No size limit; one booted client
  at a time.

Requires build tag `netboot` (and `router` — `all` already includes both). Section keys
are exact-case: `[Netboot]`, not `[netboot]`.

## Building payloads

`netboot/` holds the 68k assembly sources (`ChainDisk.a`, `ChainLoader.a`,
`BootWrapper.a`, `Bootstrap.a`) and prebuilt `.bin`s, forked from Elliot Nunn's
[NetBoot project](https://github.com/elliotnunn/NetBoot) (used
[with permission](https://github.com/elliotnunn/NetBoot/issues/2), MIT-licensed) with
our fixes applied. Building needs `vasmm68k_mot` and Python (`machfs`, etc.):

~~~bash
bin/vasmm68k_mot.exe -Fbin -m68000 -o ChainDisk.bin ChainDisk.a
~~~

ClassicStack appends the Snefru trailer itself at load time, so the raw `.bin` from
`vasm` is exactly what `payload =` should point at. See
[`netboot/readme.md`](../netboot/readme.md) for the payload table and license details.

## Diagnostics

EBP has no dedicated diagnostic channel, but the `imageNum` field is unused when serving
a single image, so the patched client packs forensic byte counters into it; the server
logs the value as `diag=` (`handleChainRead` in `compose/registry/reg_netboot.go`). If a
boot stalls, capturing the wire traffic (`[Capture]` in `server.toml`, or `tshark`) and
reading `diag=` alongside `tools/hfs/whatsat.py` (maps a failing sector back to the HFS
catalog entry it belongs to) and `tools/hfs/verifychain.py` (byte-compares served blocks
against the source image) is the same toolkit used to find every bug listed above.
