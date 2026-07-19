# Netboot ROM Experiments

This is a modified version of Elliot Nunn's effort to enable the netBOOT and ATBoot drivers. 
His original repository is can be found at https://github.com/elliotnunn/NetBoot/.

The discussion mentioned can be found on the Wayback Machine.
https://web.archive.org/web/20210923014929/https://mac68k.info/forums/thread.jspa?threadID=76&tstart=0


Elliot had done an enormous amount of work to get this far and deserves all the credit.


## Building
You'll need vasmm68 and Python with machfs, etc.



## Changes

A batch of driver-correctness fixes for the network block-driver, mostly targeting hangs/crashes/data corruption accurate emulation (Snow) that didn't show up under lenient emulation (Mini vMac).

1. `ioPosMode`/`ioPosOffset` decoding (Prime)
Previously always read `ioPosOffset` directly regardless of positioning mode. Now properly branches on `fsFromStart` / `fsAtMark` / `fsFromMark` per Inside Macintosh semantics, since the driver never maintained the "mark" — meaning `fsAtMark` writes always resolved to block 0, corrupting the MDB. Added gDiag as a scratch field to smuggle the raw `ioPosMode`/`ioPosOffset` out over the wire (repurposing the `imageNum` packet field) for forensic capture.

2. Mark maintenance (`DrvrIODone`)
The driver now updates dCtlPosition (the mark) and echoes the final position back into `ioPosOffset` after each completed I/O, as real block drivers (and Inside Macintosh) require.

3. Stale/dead-queue guards (`DrvrReSendRead`, `DrvrReSendWrite`, `DrvrDidSendWrite`, `DrvrDidReceiveRead`, `DrvrDidReceiveWrite`)
Added `qHead == 0` checks before touching the queued parameter block, so a timer or duplicate network reply that fires after `IODone` already emptied the queue doesn't corrupt RAM or double-process a dead PB.

4. Chunk-boundary/flagging fix (`DrvrSendWrite`)
Last-block flagging now also triggers per chunk (every 32 blocks), not just at the end of the whole request — previously multi-chunk writes streamed without ever pausing for an ack, and the server silently dropped intermediate chunks that were never flagged/committed.

5. First-block-of-chunk detection fix
Switched from `tst.l D1` to masking out bit 7 before testing, since a single-block chunk sets D1=$80 and the naive test would skip re-arming the sequence filter.

6. Chunk base recomputation after `DrvrCopyAddrStruct`
That routine clobbers D0 via `_BlockMoveData`, so the previously computed chunk-start block number was lost and every write went out with `hunkStart = 0`, corrupting the boot blocks. Recomputed after the call. (Reads were unaffected because they compute their offset after the call already.)

7. Ack-enable timing (`DrvrInstallReSendWrite`)
Reception (cmd $8300) is now only enabled once the final block of a chunk is actually sent, instead of permanently left disabled — the old code could never match an ack, hanging the first synchronous write; enabling too early caused a race where a fast server ack could double-enqueue the PB and hard-hang the machine.

8. Odd-address read fix (`DrvrSockListener`)
DDP payload lands at an odd address; reading it with move.l -4(A3) is an address error on real 68000/accurate emulation. Fixed by reading into an even-aligned scratch buffer (gHdr) instead.

9. ReadRest-called-twice fix (`DrvrDidReceiveRead`)
`ReadRest` consumes the packet exactly once, even on error. The old code jumped to `DrvrTrashPacket` on a length mismatch, calling `ReadRest` again and corrupting `.MPP`'s read state / sending execution wild. Now just `rtss` after a consumed packet instead of re-trashing.