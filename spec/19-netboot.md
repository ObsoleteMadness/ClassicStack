# 19 — Netboot: AppleTalk Boot Protocol (ABP) + ChainBoot EBP

Serves network boot to Old-World Macs whose ROM carries the `.netBOOT` / `.ATBOOT`
drivers (Macintosh Classic, IIci, and SuperMario-era ROMs; the Classic is the only
Mini-vMac-emulatable one). A client with netboot enabled in XPRAM discovers a
"BootServer" via NBP, downloads a boot payload over a simple DDP block protocol,
verifies it, and **executes it as 68k code**.

## Sources

There is no published Apple spec. This document is compiled from:

- **Apple's original source** (authoritative): the SuperMario source tree,
  `os/netboot/` — `ATBootEqu.h`, `BootDefines.h`, `NetBoot.h`, `GetServer.c`,
  `ATBoot.c`, `NewProto.a`, `Hash/Hash.c`. Struct and constant names below are
  Apple's.
- **Elliot Nunn's NetBoot project** (https://github.com/elliotnunn — reverse
  engineering, working reference servers `NetBoot.py` / `ChainBoot.py`, the
  ChainLoader client, and the payload build system). The ChainBoot EBP extension
  (Part B) is **Elliot's design, not Apple's** — it appears nowhere in the Apple
  source.
- **Rob Braun (bbraun)**: XPRAM enabler layout (`bbraun-pram/NBPRAM.c`) and the
  romdrv ROM-disk driver reused in netboot payloads.

## Client trigger (XPRAM)

The ROM Start Manager netboots only when the XPRAM `bootVars` record says so:
`{osType, protocol, errors, flags}` + AppleTalk union `ATPRAMrec {nbpVars,
timeout, signature[16], userName[31], password[8], serverNum}`. `flags` bit 0x80
enables netboot; 0x40 allows guest. `serverNum` (u16) names the server (see NBP
below). `signature` holds the expected image hash — the 8 bytes `'PWD PWD '`
(two longs of `STORED_WILDCARD 'PWD '`) mean "wildcard": accept a
self-authenticating image (see Payload).

## Part A — ABP (Apple Boot Protocol)

All integers big-endian. DDP type **10** (`BOOTDDPTYPE`). The client opens DDP
socket **10** (`BOOTSOCKET`, hardcoded) and sends to the server address learned
from NBP; the server's socket is whatever its NBP tuple advertises. Command
byte + version byte lead every packet; `thispversion = 1` (clients trash
version > 1).

Commands (`BootDefines.h`):

| # | Name | Direction |
|---|------|-----------|
| 1 | `User_record_request` (`rbMapUser`) | workstation → server |
| 2 | `User_record_reply` (`rbUserReply`) | server → workstation |
| 3 | `Boot_image_request` (`rbImageRequest`) | workstation → server |
| 4 | `Boot_image_reply` (`rbImageData`) | server → workstation |
| 5 | `Image_done` | server → workstation (unused by the boot path) |
| 6 | `User_record_update` | workstation → server (unused) |
| 7 | `User_update_reply` | server → workstation (unused) |

### Discovery (NBP)

The client looks up `<serverID>:BootServer@*` where `<serverID>` is `serverNum`
rendered as **4 hex digits, low nibble first** (`myNumToStr`, GetServer.c:
0xEBAB → "BABE", 0 → "0000"). It sends `rbMapUser` to every server found (up to
`MAX_SERVERS 2`) and settles on the first valid `rbUserReply`; the reply's source
address becomes the boot server for the whole session.

> Server behaviour: because the object name is client-PRAM-dependent, this
> implementation answers the LkUp for **any object** of type `BootServer`,
> echoing the requested object back in the reply tuple (exactly what
> `NetBoot.py` does).

### UserRecordRequest (cmd 1) — 42 bytes

```
0   u8   type = 1
1   u8   version = 1
2   u16  machineID        (client fills from PRAM osType)
4   u32  timestamp        (client TickCount at send)
8   34   userName         (Pascal string in a 34-byte field)
```

### BootPktRply (cmd 2) — exactly 586 bytes (= ddpMaxData)

```
0   u8   Command = 2
1   u8   pversion = 1
2   u16  osID             MUST be 1 (MACHINE_MAC) — see errata
4   u32  userData         MUST echo the request timestamp (client RTT source)
8   u16  blockSize        bytes per rbImageData block (512 disksector; 256 chain)
10  u16  imageID          echoed in the client's image requests (we use 0)
12  i16  result           0 = success
14  u32  imageSize        payload length in blocks
18  568  userRecord       zeros work (proven e2e); layout below
```

`userRecord` (568 bytes): `serverName[33] serverZone[33] serverVol[32]
serverAuthMeth(u16) sharedSysDirID(u32) userDirID(u32) finderInfo[8](u32)
bootBlocks[138] bootFlag(u16) pad[288]`. Real servers were meant to fill it from
per-user records; the ROM path boots with it zeroed.

### bir / Boot Image Req (cmd 3) — 8 bytes + variable bitmap

```
0   u8   Command = 3
1   u8   pversion = 1
2   u16  imageID
4   u8   section          always 0 (multi-section unimplemented client-side)
5   u8   flags
6   u16  replyDelay
8   ..   bitmap[≤512]     1 bit per wanted block, LSB-first within each byte
```

### BootBlock (cmd 4) — 6 bytes + blockSize data

```
0   u8   packetType = 4
1   u8   packetVersion = 1
2   u16  packetImage      must equal the reply's imageID
4   u16  packetBlockNo    0-BASED (see errata)
6   ..   packetData[blockSize]
```

### Transfer discipline

- The server **honours a non-empty request bitmap** (sends only the wanted
  blocks) and **floods every block when the bitmap is empty** (the initial
  request of a <9-block image is buggy-empty — errata). The client dedups
  received blocks in its own bitmap and re-requests on timeout; retransmission
  is entirely client-driven (`DEFAULT_RETRANS 15` ticks, backoff-doubled). The
  server keeps no per-request state and never retransmits on its own.
- Full-flood-always (what NetBoot.py does) fails to converge on real transfers:
  the client's receive path overruns with a POSITIONALLY-REPEATING loss pattern
  — the same blocks are lost at the same flood offsets every round, so repeated
  identical floods plateau (observed live: wanted-bits 1640 → 675 → … → 437 →
  437 → 436 → 436, ltoudp capture 2026-07-16). Per-bitmap retransmits shift the
  packet positions each round and converge.
- **Rotate the send order every round.** For a <9-block payload (ChainLoader is
  7 × 256) the bitmap is ALWAYS empty (errata), so bitmap honouring cannot
  help: an identical 7-block flood is a fixed point under positional loss and
  the client re-requests forever with doubling backoff (observed live:
  ltoudp-netboot capture 2026-07-16 — ~20 empty-bitmap cmd-3s, zero progress,
  no cmd 128 ever sent). Starting each round at the next block offset lands
  the loss on different blocks and converges in a few rounds.
- The client accepts `rbImageData` only from the **same (net, node, socket)**
  that sent the `rbUserReply` — the whole ABP conversation must come from one
  server socket.
- Payload ceilings: the 512-byte bitmap caps the image at **4088 blocks**
  (~2 MB @ 512); `GetServer.c` also rejects images larger than **¼ of machine
  RAM** (they are downloaded whole into RAM). This is what motivates Part B.

### Payload = executable code, not a disk image

`ATBoot.c` calls the downloaded buffer: `((j_code)(buffer))(getBootBlocks, g,
&var1, &var2)` — csCodes `getBootBlocks 1 / getSysVol 2 / mountSysVol 3 /
goodBye 4 / getDriverGlobals 5`. A bootable payload is a driver stub plus data:
Elliot's `BootWrapper.bin + HFS image` (RAM disk) or `ChainLoader.bin`
(installs the Part-B streaming disk driver), or bbraun's romdrv builds.

**Snefru-128 self-authentication**: the ROM hashes `payload[0 : len-64]` with
Apple's Snefru variant (`Hash/Hash.c`) and, when PRAM holds the `'PWD '`
wildcard, compares the first 8 hash bytes against `payload[len-16 : len]`
(`compare_signature`, ATBoot.c). Every served payload therefore needs the
trailer: zero-pad so `len % blockSize == blockSize-64`, then 48 zero bytes +
16-byte hash. This server appends the trailer automatically unless the file
already carries a valid one.

**Server-side payload assembly**: a RAM-disk payload is just `stub || disk
image || trailer` (the NetBoot repo builds it as `cat BootWrapper.bin
disk.dsk` + `snefru_hash.py`), so the server can assemble it at load from a
configured `payload` (driver stub) + `image` (HFS disk image) pair — the stub
and image are concatenated verbatim (no padding between; the stub knows its
own length). Any j_code-conforming stub works: Elliot's BootWrapper, a
romdrv-derived stub (Mac ROM-inator ROM-disk builds), etc. Verified e2e:
snowemu boots the BootWrapper + System 6.0.7 assembly (2026-07-16).

## Part B — ChainBoot EBP (Elliot Nunn's extension; NOT Apple protocol)

The chain-loaded driver replaces `.netBOOT`/`.ATBOOT` and streams a full-size
**read/write HFS image** from the server — no RAM residency, no ABP size
ceilings. Same DDP type 10; the client salvages the ABP server address and
**increments the socket by 1** (`Client.a`), so the server listens on
`advertised socket + 1` for these commands. Blocks are always 512 bytes,
transferred in chunks of ≤ 32 blocks (16 KB); the `seq` word ties responses to
the outstanding request and retransmission is client-driven.

### 128 — chain read request (client → server), 16 bytes

```
0   u8   command = 128    ($80 "polite request" flag byte)
1   u8   flag             (unused)
2   u16  seq
4   u32  imageNum         0 observed = "configuration mode" / default image
8   u32  blockOffset      in 512-byte blocks
12  u32  blockCount       server clamps to 32
```

Observed live (ltoudp-netboot capture 2026-07-16): exactly 16 bytes, and the
first read (seq 1, imageNum 0, blocks 0–1 — the disk's boot blocks) arrives on
the **ABP boot socket**, not socket+1. Dispatch EBP by command byte on both
sockets; the socket+1 convention applies to the later replacement-driver flow
(Client.a's salvage-and-increment), not ChainLoader's first contact.

### 129 — chain read data (server → client), 4 bytes + 512 data

```
0   u8   command = 129
1   u8   blkIndex         plain index within the chunk (NO bit7 flag on reads —
                          the client tracks completion in its progress bitmap)
2   u16  seq              echoed
4   512  data
```

What the client actually validates (ChainLoader.a `DrvrSockListener`): it reads
the first 4 bytes and XORs them against `gExpectHdr` = `81 00 <seq16>`, then
`swap`+`clr.b` — so the **command byte must be exactly $81 and seq must echo
exactly; the blkIndex byte is NOT filtered** (it is later masked `& 31` to place
the data, and bit 1 of the command byte routes read data vs write ack —
$81 reads / $83 acks). The data portion **must be exactly 512 bytes**: the
listener calls ReadRest with a 512-byte buffer and trashes the packet on any
other length. Replies need no particular source address — the filter is the
only gate.

**Upstream ChainLoader alignment bug (found 2026-07-16, fixed locally):** the
original listener read the 4-byte header in place in the MPP RHA and loaded it
with `move.l -4(A3),D2`. The RHA leaves the DDP payload at an ODD address
(odd RHA base + 3-byte LLAP header + 5/13-byte DDP header), so this longword
read is a 68000 address error on the FIRST packet the listener ever sees —
**Sad Mac 0F/0002 (dsAddressErr) the moment the first chain-read reply
arrives**. Mini vMac's lenient CPU core masks it (Elliot's test platform);
Snow and real hardware fault. Server behavior was verified byte-exact against
the client's own parsing before this was found — no server-side workaround
exists (payload parity is fixed by the header sizes). Fixed in the local
NetBoot clone by reading the header into an even-aligned `gHdr` global and
rebuilding with the repo's vasm (pristine rebuild verified byte-identical to
the shipped ChainLoader.bin first).

**Upstream write-ack filter bug (CONFIRMED 2026-07-16, fixed locally):** for
writes, `gExpectHdr` was only ever `00 00 <seq>` (nothing set a `$83xx`
command word the way `DrvrDidSendRead` sets `$8100`), so a cmd-131 ack could
never pass the packet filter; additionally the first-block-of-chunk test
(`tst.l D1`) missed single-block chunks (blkIndex `$80`), leaving a stale seq.
Confirmed by evidence: a Mini vMac chain boot committed exactly ONE write
chunk ever, then hung resending it — the upstream write path was never
functional (ChainBoot.py's own write handler is also broken: it takes the
data at `whole_data[8:]`, inside the 12-byte header, shifting every write by
4 bytes). Fixed in the local clone: first-block test masks `& 31` and sets
`gExpectHdr = $8300 <seq+1>`.

**Misdirected client writes (ROOT-CAUSED 2026-07-17, fixed locally):** every
chain write carried `hunkStart = 0` on the wire (observed: the MDB — data
read from block 2 — committed over the boot blocks; earlier a catalog leaf
node belonging at block 720; finally a whole sequential cache flush all at
hunk 0). Two position-sourcing theories (dCtlPosition unreliable at
write-Prime; 6.0.8 flushes are fsAtMark against a mark the driver never
maintained) were both disproved by instrumentation: the patched ChainLoader
repurposes the always-zero `imageNum` field of cmds 128/130 to carry the raw
`ioPosOffset` with `ioPosMode & $F` in its low 4 bits (positions are
512-aligned, so those bits are free; the server logs it as `diag`), and the
diag showed every write arriving at Prime as **fsFromStart with a perfectly
valid ioPosOffset** — while `hunkStart` in the very same packet was 0.

The actual bug is a register clobber in upstream `DrvrSendWrite`: it computes
the chunk-base block into D0, then calls `DrvrCopyAddrStruct` — which does
`moveq #16,D0` for `_BlockMoveData` (and BlockMove returns noErr in D0),
preserving only A0/A1 — and only afterwards stores D0 into the packet. So
**every write ChainBoot ever sent had hunkStart = 0 unconditionally,
regardless of posMode or mark**. `DrvrSendRead` computes its offset *after*
the same `bsr`, which is why reads always positioned correctly. Fixed in the
local clone by recomputing the chunk base after the call. (This is the third
independent reason upstream ChainBoot writes never worked, after the
unmatchable write-ack filter and ChainBoot.py's `[8:]` data offset.)

While chasing the ghost theories, `DrvrPrime` also gained the full romdrv-
style ioPosMode decode (fsAtMark → `dCtlPosition`, fsFromStart →
`ioPosOffset`, fsFromMark → sum) and `DrvrIODone` now maintains the mark
(`dCtlPosition` = final byte position, also written back to `ioPosOffset`) —
that is the Inside Macintosh driver contract, matches bbraun's proven romdrv,
and is kept as correctness hardening even though 6.0.8 was observed sending
fsFromStart throughout.

**Stale-timer derangement (fixed locally):** after IODone empties the driver
queue, a still-armed resend timer (or a late duplicate reply/ack) fired
handlers that dereference `dCtlQHdr.qHead` — building requests from freed
memory (observed: a chain read of block offset $0A0A0A00 — DDP header bytes)
and ReadRest-ing 512 bytes through a dead parameter block, corrupting RAM
until the CPU executed garbage. Fixed with qHead-nil guards in
`DrvrReSendRead`/`DrvrReSendWrite`/`DrvrDidReceiveRead`/`DrvrDidReceiveWrite`/
`DrvrDidSendWrite`. Server-side defense in depth: chain reads whose offset is
entirely past EOF are WARN-logged and dropped (zero-filling them feeds the
deranged client; ChainBoot.py's slice semantics drop them too).

**Chunk-read bursts: pace and burst-initial hold (observed 2026-07-17):** the
chain client needs EVERY block of a chunk in one burst — `DrvrSendRead` resets
the progress bitmap and the seq on each 1-second timer retry, so partial
progress is discarded. Two distinct loss mechanisms were seen:

1. *Receive overrun*: at the 2 ms ABP flood rate 32-block chunks retried up to
   9×, scaling with burst length. Real LocalTalk cannot deliver a 530-byte
   frame faster than ~18 ms (230.4 kbit/s), so clients were never built for
   faster arrival. EBP read replies pace at `chain_pace_ms` (default 10 ms,
   separate from the ABP `pace_ms`).
2. *Listener-enable race*: pacing alone did NOT stop retries — one 32-block
   chunk was re-requested 73× at metronomic 1.37 s intervals with all 32
   replies served each round, and even 1-block requests retried 4–5×, which
   rules out overrun. The client's packet filter is DISABLED between
   `DrvrSendRead` (clears `gExpectHdr`) and the async send-completion
   (`DrvrDidSendRead` sets `$8100`), so a reply arriving in that window is
   trashed; with a deterministic send order the same block dies every round —
   the identical fixed-point pathology as the ABP flood. The server therefore
   HOLDS the burst for one pace interval before the first reply so the
   completion wins the race, then sends blocks in order (matching the
   reference servers). Block-order rotation plus a bookend duplicate of the
   first block was tried first and is now REMOVED: the bookend directly
   caused a Sad Mac (double-ReadRest, below) and a happy-Mac freeze (a frame
   landing in the System's SCC re-init window), regressing runs that had
   previously reached deep into System 6.
3. *Emulator ingest overrun at real-time speed (observed 2026-07-17, snow)*:
   with the hold in place, snow at fast-forward boots System 6 fully
   (read/write confirmed; Mini vMac too) — but at REAL-TIME emulated speed a
   10-block read looped forever: seq incremented every ~1 s with the same
   offset/count, i.e. the client's retry timer, not resends. Memory forensics
   nailed it: `gProgress` = 0 (zero blocks of the current retry accepted) while
   `gHdr` held block 9 — the LAST block — of the PREVIOUS seq: the burst's
   tail survives, its head is dropped before the emulated Mac drains it, and
   since retries reset progress the loss is again a fixed point. A 1×-speed
   Mac (real or emulated) simply cannot ingest 512-byte frames at 10 ms
   spacing — real LocalTalk would deliver them at ~20 ms. The server therefore
   detects retries (same client re-requesting the same offset+count chunk) and
   DOUBLES the inter-packet pace per consecutive retry, capped so the whole
   burst still lands well inside the client's 1 s retry timer
   (≤ 800 ms / (count+1)); the backoff state resets as soon as the client asks
   for a different chunk. `chain_pace_ms` remains the base pace only.

**Double-ReadRest on duplicates (Sad Mac 0F/0003, observed 2026-07-17):** the
bookend exposed a latent upstream listener bug. `DrvrDidReceiveRead` calls
`ReadRest` (which consumes the packet) and only THEN detects a length error or
a progress-bitmap duplicate — and branched to `DrvrTrashPacket`, which calls
`ReadRest` a second time. Inside AppleTalk allows exactly one ReadRest per
packet; the second call runs .MPP with dead read state and the next jump goes
wild (memory forensics: PC = $1748, inside the DCE master-pointer block,
executing $0E00). It never fired upstream because nothing ever sent a
mid-chunk duplicate: a bookend after a COMPLETED request is rejected before
ReadRest by the disabled filter (safe path), but a bookend arriving while the
chunk is still missing blocks passes the armed filter and hits the dedup.
Fixed in the local clone: after ReadRest has run, error and duplicate paths
`rts` instead of jumping to `DrvrTrashPacket`. Snow-emu memory-dump forensics
that found it: running driver located via unit table (unit 49 → DCE) →
`gExpectHdr` mid-seq, `gProgress` missing two blocks, `gHdr` = the bookend's
header as the last packet read.

Restore a bricked image: `A608.dsk.pristine` sits beside it (also in NetBoot
git).

### 130 — chain write block (client → server), 12 bytes + ≤512 data

```
0   u8   command = 130    ($82)
1   u8   blkIndex         index within the chunk; bit7 set on the LAST block
2   u16  seq
4   u32  imageNum
8   u32  hunkStart        first block of this chunk
12  ..   data (≤512)
```

The server accumulates blocks of one `seq` in a 32-block window; when the
bit7-flagged block arrives it truncates the window after that block, commits it
to the image at `hunkStart*512`, and acks.

**Multi-chunk writes were a protocol hole (observed 2026-07-17):** upstream
ChainLoader sets bit7 only on the final block of the whole REQUEST, and both
this server and ChainBoot.py commit/ack only on bit7 — so every intermediate
chunk of a >32-block write was silently discarded when the next seq reset the
window (observed: a 232-block flush, seqs 375–384, vanished whole; only 4 acks
against 323 write blocks on the wire). Worse, `DrvrDidSendWrite`'s
chunk-boundary "pause for ack" test masked `ioReqCount` (constant) instead of
the advanced `ioActCount`, so the client barreled through chunk boundaries
without awaiting any ack. Fixed in the local ChainLoader clone: bit7 is set on
the last block of EACH chunk (each chunk is its own commit at its own
`hunkStart`) and the boundary test uses `ioActCount`. Server defense in depth:
a window displaced by a new seq with data but no flag is committed as its
contiguous block prefix (WARN "committed on eviction") instead of dropped —
this also makes the unpatched upstream client's multi-chunk writes land.

**Write-ack race hard-hangs the client (observed 2026-07-17):** unlike reads
(filter enabled in the send-completion `DrvrDidSendRead`), ChainLoader armed
the write-ack filter synchronously in `DrvrSendWrite` — so a server ack
arriving before the async `_Control` completion (`DrvrDidSendWrite`) was
accepted while `ioActCount` was still 0; `DrvrDidReceiveWrite` then concluded
more blocks remained and re-entered `DrvrSendWrite`, issuing a second
`_Control` on the still-queued `gMyPB`. Double-enqueueing one parameter block
loops the .MPP driver queue: hard freeze at interrupt level, total network
silence, not even the 10 s resend timer fires. On the wire: write at
t+0 ms, our ack at t+0.3 ms, then nothing forever; an identical write 60 ms
earlier survived (the completion won that race). Fixed twice over: the local
ChainLoader now bumps the seq but keeps the filter DISABLED in `DrvrSendWrite`
and enables `$8300` in `DrvrInstallReSendWrite` (i.e. once the chunk's final
block is out, mirroring the read path); and the server holds every write ack
for one `chain_pace_ms` interval so even the unpatched client's completion
wins.

### 131 — chain write ack (server → client), 4 bytes

```
0   u8   command = 131
1   u8   0
2   u16  seq              echoed
```

### Caveats

- `imageNum` is carried but a single configured disk image is served
  (matches `ChainBoot.py`).
- The disk image is opened read-write and mutated in place; **one booted client
  at a time** — concurrent clients writing one image would corrupt it.
- The ChainLoader payload itself is served over Part A with `blockSize 256`
  (`ATBOOT_BLOCK_SIZE`; must be a multiple of 64 so the Snefru trailer fills the
  last block) and must be at least 2 blocks long (1-block payloads crash the
  client).

## Part C — the boot-image entry contract (Apple), and ChainDisk

Everything above concerns the wire. This section is the *client-side* contract
that a served payload must satisfy, which is what decides how portable a
payload is across ROMs.

### The contract

`ATBoot.c` (`get_the_image`, `DOATCONTROL`) enters the downloaded image as a C
function — `ATBootEqu.h` declares the type:

```c
typedef short (*j_code)(short command, DGlobals *g, int **var1, int **var2);
```

`NetBoot.c`'s `DOREAD` drives exactly three calls, in order:

| csCode | when | the payload's job |
|---|---|---|
| `getBootBlocks` 1 | during the ROM's `_Read` of blocks 0–1 | supply 1 KB of boot blocks |
| `getSysVol` 2 | immediately after, same `_Read` | install a driver + DQE; return the DQE in `var2` |
| `mountSysVol` 3 | after `_InitFS`, via the `ToExtFS` hook | `_MountVol`; return VCB in `var1`, DQE in `var2` |

The third call is the important one: **`.netBOOT` installs the `ToExtFS` hook
itself** (`DOREAD`, right after `getSysVol` succeeds) and calls the payload
back through `.ATBOOT`. A payload therefore does not need to hook anything to
gain control after the file system comes up — it is called.

Return 0 for success; `DOREAD` maps a positive result to `noDriveErr` (fatal)
and a negative one to `offLinErr` (the ROM retries).

`DGlobals` (`ATBootEqu.h`) is the second argument, and the payload's only
channel to what `.ATBOOT` learned:

```
+0   netBootRefNum(2)  +2 error(2)  +4 netimageBuffer(4)
+8   netImageSignature[4](16)
+24  netServerAddr     AddrBlock — the ABP server we downloaded from
+28  ur                BootPktRply (18 bytes, then userRecord)
+46  ur.userRec        serverName[33] serverZone[33] serverVol[32] ...
+184 ur.userRec.bootBlocks[138]
```

`getBootBlocks` writes its boot blocks into `+184`, because `ATBoot.c` copies
`ur.userRec.bootBlocks` into the caller's buffer **after** the payload returns.

### How the ROM finds a boot protocol driver (and what that means for links)

`NetBoot.c`'s `FINDNOPENDRIVER` picks the boot protocol driver two different
ways, on the PRAM `protocol` byte:

- **`DrSwATalk` (1) — built in, no Slot Manager.** `.ATBOOT` is opened by name
  through hand-written glue (`DoATBootOpen`, `ATBootUtils.a`). The changelog is
  explicit that this was made to bypass slots: *"Inline open for pc-relative
  atboot driver, open atboot if pram = 0 (default protocol)"*. This is the path
  every payload here uses.
- **anything else — Slot Manager.** `find_BPTentry` does `SNextTypeSRsrc` for
  `spCategory = CatBoot (40)`, `spCType = TypRemote (1)`,
  `spDrvrSW = <PRAM protocol>`, then `SReadDrvrName` + `OpenSlot` — i.e. the
  driver comes off a NuBus card's declaration ROM, not the Mac ROM. Guarded on
  `_SlotManager` being implemented at all, else `dProtocolNotFound`.

**Ethernet.** ABP is DDP, so it rides whatever link `.MPP` is bound to —
LocalTalk or EtherTalk (via a card's `.ENET` + ELAP). Netbooting over Ethernet
is therefore just AppleTalk netbooting on a machine whose AppleTalk happens to
be Ethernet; nothing in ABP, ChainBoot, or any payload here is LocalTalk-
specific. The bootstrapping requirement is that AppleTalk is already up on that
interface by boot-image time — a Start Manager / declaration ROM concern, which
is exactly why the built-in path just opens `.MPP` by name.

`BOOT_IP 0x02` is declared in `NetBoot.h` beside `BOOT_ATALK 0x01`, but no IP
boot driver exists in the Apple source tree; the slot path is the extension
mechanism such a driver would have arrived through.

**The slot path is a defined-but-unpopulated extension point (verified).**
Searching the whole SuperMario drop, `CatBoot` (40) appears in exactly two
files — `OS/NetBoot/NetBoot.c` and `NetBoot.h` — both on the *consumer* side
(`find_BPTentry` looking one up). Nothing in the tree ever **declares** a
`CatBoot`/`TypRemote` sRsrc, so Apple shipped no slot-based boot protocol
driver; one would have had to come from a third-party card's declaration ROM.

This holds even though built-in Apple Ethernet ROM code is present and
substantial: `DeclData/DeclNet/` has full MACE (`Mace.a`, `MaceEnet.a`,
`MaceEqu.a`, `PDMMaceEnet`, plus per-machine `'ecfg'` hardware config in
`MACEecfg.r`) and SONIC (`Sonic.a`, `SonicEnet.a`, `SonicEqu.a`) drivers, with
shared `802Equ.a` / `ENETEqu.a` / `SNMPLAP.a`. They are registered as pseudo-slot
resources in `DeclData/DeclData.r`:

```
resource 'styp' (1625, "_NetSonic")   {CatNetwork, TypEthernet, DrSwApple, DrHwSonic};
resource 'styp' (1630, "_NetMace")    {CatNetwork, TypEthernet, DrSwApple, DrHwMace};
resource 'styp' (1633, "_NetPDMMace") {CatNetwork, TypEthernet, DrSwApple, DrHwMace};
```

— every one `CatNetwork (4)` / `TypEthernet`, i.e. ordinary "here is an Ethernet
interface" declarations for `.ENET` to bind, never a boot declaration. So
netbooting over built-in Ethernet works, but strictly as AppleTalk-over-
EtherTalk down the `DrSwATalk` built-in path: the Ethernet ROM brings up
`.ENET` so `.MPP`/ELAP can sit on it, and ABP rides that DDP like any other
link. There is no Ethernet-native boot protocol in the ROM.

### Two payload styles

| | `ChainLoader.a` | `ChainDisk.a` |
|---|---|---|
| takes control by | scanning the stack for the ROM's `_Read` return address, rewriting it, `_DrvrRemove`ing `.netBOOT` + `.ATBOOT`, re-executing the `_Read` trap | implementing the three csCodes and returning normally |
| assumes | a `_Read` return address is on the stack; ROM within `ROMBase..ROMBase+$4000`; `$A002` immediately precedes it | nothing about the ROM |
| unit number | steals `.netBOOT`'s | its own (52) |
| known good on | Macintosh Classic (e2e, snow + Mini vMac) | — |
| portability | the stack scan is **verified false on the LC 475**: none of the 15 `_Read` call sites leaves a return address on the stack | ROM-independent by construction |

`ChainDisk.a` is structured after Elliot Nunn's `BootWrapper.a` (the RAM-disk
payload, which is contract-conformant) with `ChainLoader.a`'s EBP driver as its
`DrvrPrime` body, carrying over every fix listed in Part B. Both payloads speak
the identical EBP wire protocol, so the server serves either unchanged.

The one ROM-adjacent thing `ChainDisk` retains is `BootWrapper`'s
`fixDriveNumBug`: `.netBOOT`'s `ToExtFS` hook tests for drive number **4**
specifically, so on a machine with more than two existing drives the hook never
calls `mountSysVol`. The workaround is a one-shot `_MountVol` patch that
installs a `ToExtFS` head patch testing the *actual* drive number. That is a
data-driven scan for a documented low-memory global, not a return-address
guess, and it is proven on Classic and Mini vMac.

### Volume size: the `CSDSKSZ` stamp

EBP has no "how big is the disk?" query, but the client must report a drive
size to the Device Manager (`dQDrvSz`, and `Status` `fmtLstCode`) before it has
read anything. `ChainLoader` leaves this zero. `ChainDisk` instead exposes a
patch point — the 8-byte cookie `CSDSKSZ\0` followed by a big-endian u32 of the
volume size in 512-byte blocks — and the **server stamps it at load**
(`stampDiskSize`, `compose/registry/reg_netboot.go`), because the server is the
only party that knows the image size. The stamp happens **before** the Snefru
trailer is computed, so the hash covers the stamped bytes. Payloads without the
cookie (BootWrapper, ChainLoader) pass through untouched.

## ChainDisk debugging notes (2026-08)

These are **our own bugs**, not spec errata — recorded because each one wasted
real time and each has a reusable lesson for 68k payload work.

### The one that broke netboot: a flag clobber in the poll loop

`SyncChainRead` polled for its blocks like this:

```
.spin   move.l  D0,-(SP)            ; stash the deadline
        bsr     AllBlocksIn         ; D0 = 0 once every block has landed
        tst.l   D0                  ; set Z from the result...
        move.l  (SP)+,D0            ; ...then CLOBBER it restoring D0
        beq.s   .done
```

`move.l` is **not flag-transparent** on the 68000 — it sets N and Z from the
value moved. So `beq` tested "is the deadline zero?", and the deadline is
`Ticks+180`, never zero. `.done` was unreachable: every chain read spun its
full 3 s and retried five times regardless of what had already arrived, then
returned a negative result, which `NetBoot.c` maps to `offLinErr` and the ROM
abandons netboot for the next device (flashing question mark).

The tell was in every capture from the first: chain-read requests exactly
~3.03 s apart, five of them, while the server's replies arrived ~15 ms after
each request and were LLAP-acked. Only a move to an **address** register leaves
the flags alone.

### Diagnosing it: the `imageNum` forensic channel

EBP has no diagnostic channel, but `imageNum` is unused when serving a single
image, so the client packs four byte counters into it and the server logs the
long as `diag=` (`netboot.go`, `handleChainRead`):

```
[entries][ReadPacket-fail][filter-reject][ReadRest-fail]
```

That single number falsified four successive wire-level theories in one boot
each. It showed `entries` climbing +2 per burst with all failure bytes zero —
i.e. both replies were being received, filtered, and read correctly, and the
progress bitmap was being filled the whole time — which isolated the fault to
the reader. **Measure before theorising**: the wire looked identical whether
the client was deaf or merely unable to notice it had heard.

### PC-relative addressing is read-only (this broke the instrument itself)

The first version of that counter was `addq.l #1,gListenerHits`. There is no
PC-relative *destination* mode on the 68000, so vasm silently emitted an
**absolute-long** write to the link-time offset (`$510`). The payload runs from
a heap block at an arbitrary address, so this scribbled on low memory and left
the counter permanently zero — producing two rounds of `diag=0` readings that
looked like hard evidence and were noise.

Every global in a relocatable 68k payload must be reached PC-relative; writes
must `lea` the address into a register first. Check the listing (`-L`) for
`...B9`/`...F9` opcodes, which indicate absolute addressing.

### Two real defects found on paths that had never executed

- **`closeSkt` was coded as 249, which is `loadNBP`.** Apple's equates
  (`Interfaces/AIncludes/AppleTalk.a`) are `writeDDP 246`, `closeSkt 247`,
  `openSkt 248`, `loadNBP 249`. `getSysVol`'s socket handover therefore never
  closed socket 10, so its `openSkt` for the driver listener would have failed
  with `ddpSktErr`, leaving the installed driver permanently deaf. Not the
  cause of the boot failure — it is downstream of `getBootBlocks` — but it
  would have been the *next* failure.
- **`OpenNetwork` reused a dirty parameter block**, calling `ClearBlock` once
  before `_Open` and then issuing `openSkt` on the block `_Open` had written
  into, without re-clearing or setting `ioRefNum`. Every other `_Control` site
  in the payload re-clears and sets `ioRefNum` explicitly.

### Also confirmed while chasing this

`.ATBOOT` **closes socket 10 before calling the boot image**: `get_image`
(`GetServer.c`) opens it with `DDPOpenSocket`, and its `err_exit` path runs
`DDPCloseSocket(thesocket)` before returning to `get_the_image`, which only
then calls the image at `getBootBlocks`. So the payload owns socket 10 outright
and there is no contention with Apple's own listener — a theory that cost two
rebuild cycles to disprove.

### Where the boot stops now: after the SCSI Manager gibbly loads (2026-08)

With the read path fixed, netboot gets all the way into system startup and then
stops dead: the Mac takes delivery of a block, LLAP-acks it, and never issues
another request. No retry, no timeout, and the driver's own 1-second resend
timer never fires either — the machine has stopped executing, it has not given
up on the network.

The stop is exactly reproducible and lands on a **resource boundary**, which is
what localised it. Reconstructing the wire stream and mapping the final sectors
back through the HFS catalog (`tools/hfs/whatsat.py`) gives:

| request | sectors | resource |
|---|---|---|
| seq=371 | 34051 | last sector of `gcko` id=43 |
| seq=372-375 | 34052-34132 | `citt` id=43, all 41664 bytes, 100% complete |

Both resources live in the resource fork of
`System Folder:System 7.5 Update` (type `gbly`, creator `MACS`) — a **Gibbly**,
loaded and executed by the ROM startup very early.

`citt` id=43 is **SCSI Manager 4.3**. Its strings identify it beyond doubt:
`APPLE   PDM (PDM,CF,CS) 04.3{wolfware} & {gecko}`, `NCR 53c96`,
`HAL SCSIHALunusedVector`, and the machine HALs `Quadra` / `Cyclone` / `TNT`.
`gcko` ("gecko", the sibling codename) is the matching File Manager patch table
— it patches `_Read`, `_Write`, `_GetVolInfo`, `_Create`, `_GetFileInfo`,
`_FlushVol` and `_FSDispatch`. Notably `citt` patches **no** Device Manager
traps, so it does not hijack our driver's entry points.

So the last thing we successfully deliver is the SCSI Manager, complete and
byte-perfect, and the machine stops somewhere after starting to use it. What
runs next is not yet pinned down.

**A dead end, recorded so it is not chased again.** `INITSCSIBOOT`
(`OS/SCSIMgr4pt3/BootItt.c`, called from `OS/StartMgr/StartInit.a:1862`)
contains a `DebugStr("\pInitSCSIBoot:BusInquiry failed getting numBuses")`
on a failed `SCSIBusInquiry`, which looks like an obvious diskless-client trap.
It is almost certainly *not* our stop:

- The `DebugStr` is not followed by a return or bail-out — execution falls
  straight through to `numBuses = scPB.scsiHiBusID + 1` and carries on.
- `INITSCSIBOOT` only allocates a `BootInfo` and loads third-party SIMs. It
  does not look for a boot device.
- It runs at `StartInit.a:1862`, immediately before `BRA BootMe` (line 1875).
  Our volume is mounted and being read long before this point.

**Our driver should not care about any of this.** The Start Manager selects a
startup device by walking `DrvQHdr` and reading `dqRefNum` / `dqDrive` off each
drive queue entry (`OS/StartMgr/StartSearch.a`, `NextDQEntry` / `SelectDevice`)
— there is nothing SCSI-specific in that path. A netboot disk is a block device
like any other: essentially a very large floppy, which is the same shape Basilisk
II and Mini vMac present. Our DQE conforms: `qType = 1` with the block count
split `dQDrvSz` (`$C`, low word) / `dQDrvSz2` (`$E`, high word) per Apple's
`SysEqu.a:663-664`, and `dQFSID = 0` for the native file system. The fix, when
found, belongs in how we behave as a block device — not in emulating a SCSI bus.

**What this is not.** Three hypotheses were killed by measurement, and are
worth recording so they are not re-run:

- *Not a data-corruption bug.* All 1986 blocks served were compared
  byte-for-byte against the source image (`tools/hfs/verifychain.py`): **zero
  mismatches**, including every block of the terminal region. Every request
  also received exactly the blocks it asked for — no short reads anywhere.
- *Not a write failure.* Instrumenting `DrvrPrime` by trap type showed
  `_Write` reaching the driver **zero** times in 338 Prime calls. The System
  never asks us to write, so the absence of EBP cmd 130 on the wire is correct
  behaviour, not a fault in `DrvrSendWrite`.
- *Not a dirty volume.* Reproduced identically on a freshly-copied image with
  `drAtrb` bit 8 (`unmounted cleanly`) set.

The `gDrvrDiag` counter added for the second of these is retained; see
"Diagnosing it" above for how the packed bytes are read.

Next avenue: `DrvrStatus` answers only `fmtLstCode` (6) and `drvStsCode` (8)
and returns `statusErr` (-18) for everything else, and `DrvrControl` is
similarly narrow. A System that asks a newly-patched File Manager to query the
boot drive may well issue a csCode we reject. Instrumenting *which* csCode
arrives — the same `gDrvrDiag` trick, tallying Control/Status csCodes — is the
cheapest next measurement.

## Errata / observations

- **`packetBlockNo` is 0-based.** The `ATBootEqu.h` struct comment says
  "starts with 1", but `NewProto.a getImageBuffer` computes
  `offset = blockNo * blockSize` and range-checks `blockNo <= imageSize-1`.
  Elliot's servers send 0-based and boot successfully.
- **The client's request bitmap is buggy only for tiny images.**
  `makeImageRequest` and `makeBitmap` (GetServer.c) compute the trailing-byte
  test as `lastBlockNo >> 3` where `& 7` was intended, so images under 9 blocks
  request an **empty** bitmap forever — an empty bitmap must be treated as
  "send everything". For normal-size images the bitmap is valid (initial
  request all-set, retransmits carry exactly the missing blocks) and honouring
  it is REQUIRED in practice — see Transfer discipline for why flood-always
  (the reference servers' shortcut) stalls under positional receive overrun.
- **`osID` must be the constant 1**, not an echo of the request's `machineID`.
  `get_image` hardcodes `g.machineID = MACHINE_MAC (1)` and `CLISTENER` compares
  the reply's `osID` against it, while the *request's* machineID field carries
  PRAM `osType`. Echoing works only when PRAM has 1 there (the common enabler
  setup); the constant works always.
- **`userData` must echo the request timestamp** — `CLISTENER` computes
  `roundTrip = (TickCount() - userReply.userData) << 2` and bases every
  retransmission timer on it. A wrong echo skews the client's timeout schedule.
- **The reply must be padded to 586 bytes**; the client's socket listener reads
  `ddpMaxData` for a user reply.
- **NBP object name is nibble-reversed hex** of PRAM `serverNum`
  (`myNumToStr` emits low nibble first). Answering any object of type
  `BootServer` (echoing the requested object) sidesteps the encoding entirely.
- Apple's Snefru use is nonstandard: `generate_hash` feeds `p2 = bitlen` and
  post-increments it per 512-bit block and per fold — port `snefru_hash.py`
  verbatim, do not substitute textbook Snefru.
- The EBP ATP "AskQuestion" boot menu found in Elliot's `Client.a` /
  `ServerDRVR.a` is an unfinished experiment (the server handler is a
  `_Debugger` trap); it is not part of either protocol here and is not
  implemented.
