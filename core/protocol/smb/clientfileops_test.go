package smb

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// TestBuildFindFirst2Trans2Caps proves TRANS2 FIND advertises a modest MaxParameterCount
// (never 0 / 0xFFFF — Win98 misframes those) and honours MaxTransactBytes as MaxDataCount.
func TestBuildFindFirst2Trans2Caps(t *testing.T) {
	b := &Builder{PID: 0xFEFF, TID: 1, UID: 1, MaxTransactBytes: 2792}
	req := b.BuildFindFirst2("WINDOWS", 256)
	wct := int(req[HeaderLen])
	if wct != 15 {
		t.Fatalf("WCT = %d, want 15", wct)
	}
	w := req[HeaderLen+1 : HeaderLen+1+2*wct]
	if got := bp.LE16(w[4:6]); got != 32 {
		t.Errorf("MaxParameterCount = %d, want 32", got)
	}
	if got := bp.LE16(w[6:8]); got != 2792 {
		t.Errorf("MaxDataCount = %d, want 2792 (MaxTransactBytes)", got)
	}
	if got := bp.LE16(w[28:30]); got != trans2FindFirst2Sub {
		t.Errorf("Setup[0] = %#x, want FIND_FIRST2 %#x", got, trans2FindFirst2Sub)
	}
}

// TestParseQueryInformationDisk round-trips a WCT=5 disk-info reply into total/free bytes.
func TestParseQueryInformationDisk(t *testing.T) {
	// 5120 total units × 64 blocks/unit × 512 bytes = 160 MiB;
	// 5184 free units would be slightly more free than total — use 4000 free → 125 MiB.
	const (
		totalUnits    = 5120
		blocksPerUnit = 64
		blockSize     = 512
		freeUnits     = 4000
	)
	h := Header{Command: CommandQueryInformationDisk, Status: StatusSuccess, Flags: FlagReply}
	out := h.Encode(nil)
	out = append(out, 5) // WCT
	words := make([]byte, 10)
	bp.PutLE16(words[0:2], totalUnits)
	bp.PutLE16(words[2:4], blocksPerUnit)
	bp.PutLE16(words[4:6], blockSize)
	bp.PutLE16(words[6:8], freeUnits)
	out = append(out, words...)
	out = append(out, 0, 0) // ByteCount = 0

	info, err := ParseQueryInformationDisk(out)
	if err != nil {
		t.Fatalf("ParseQueryInformationDisk: %v", err)
	}
	wantTotal := uint64(totalUnits) * blocksPerUnit * blockSize
	wantFree := uint64(freeUnits) * blocksPerUnit * blockSize
	if info.Total != wantTotal {
		t.Errorf("Total = %d, want %d", info.Total, wantTotal)
	}
	if info.Free != wantFree {
		t.Errorf("Free = %d, want %d", info.Free, wantFree)
	}
}
