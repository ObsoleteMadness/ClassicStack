package fs

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

func TestEncodeParseAfpInfo_RoundTrip(t *testing.T) {
	var finder [32]byte
	copy(finder[:], []byte("TEXTttxt-arbitrary-finder-info!!"))
	in := afpInfo{backupTime: 0xDEADBEEF, finderInfo: finder, prodosInfo: [6]byte{1, 2, 3, 4, 5, 6}}

	b := encodeAfpInfo(in)
	if len(b) != afpInfoSize {
		t.Fatalf("encoded length = %d, want %d", len(b), afpInfoSize)
	}
	if got := binary.BigEndian.Uint32(b[0:4]); got != afpInfoSignature {
		t.Fatalf("signature = %#x, want %#x", got, afpInfoSignature)
	}
	if got := binary.BigEndian.Uint32(b[4:8]); got != afpInfoVersion {
		t.Fatalf("version = %#x, want %#x", got, afpInfoVersion)
	}

	out, err := parseAfpInfo(b)
	if err != nil {
		t.Fatalf("parseAfpInfo: %v", err)
	}
	if out.backupTime != in.backupTime {
		t.Errorf("backupTime = %#x, want %#x", out.backupTime, in.backupTime)
	}
	if out.finderInfo != in.finderInfo {
		t.Errorf("finderInfo round-trip mismatch")
	}
	if out.prodosInfo != in.prodosInfo {
		t.Errorf("prodosInfo round-trip mismatch")
	}
}

func TestParseAfpInfo_RejectsBadSignature(t *testing.T) {
	b := make([]byte, afpInfoSize) // all-zero: wrong signature
	if _, err := parseAfpInfo(b); err == nil {
		t.Fatal("expected error for zero signature")
	}
	if _, err := parseAfpInfo(b[:afpInfoSize-1]); err == nil {
		t.Fatal("expected error for short record")
	}
}

// adsTestFS adapts a memFS into a stream-naming FileSystem: it is just the memFS,
// so "path:AFP_Resource" is an ordinary path key. This exercises the ads engine's
// record + stream-path logic without needing a real NTFS volume.
func newADSTestEngine() *adsForkEngine {
	return newADSForkEngine(newMemFS(ShareSpec{}))
}

func TestADSForkEngine_FinderInfoRoundTrip(t *testing.T) {
	e := newADSTestEngine()

	// Create the data file first so the file "exists".
	if _, err := e.fs.CreateFile("doc"); err != nil {
		t.Fatalf("create data: %v", err)
	}

	if _, ok, err := e.ReadFinderInfo("doc"); err != nil || ok {
		t.Fatalf("ReadFinderInfo before write: ok=%v err=%v, want ok=false", ok, err)
	}

	var finder [32]byte
	copy(finder[:], []byte("APPLmdrp________________________"))
	if err := e.WriteFinderInfo("doc", finder); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}

	got, ok, err := e.ReadFinderInfo("doc")
	if err != nil || !ok {
		t.Fatalf("ReadFinderInfo: ok=%v err=%v", ok, err)
	}
	if got != finder {
		t.Errorf("FinderInfo mismatch after round-trip")
	}

	// The AfpInfo stream must hold a valid 60-byte record at the stream path.
	raw, err := e.readAll(afpInfoStreamPath("doc"))
	if err != nil {
		t.Fatalf("read AfpInfo stream: %v", err)
	}
	if len(raw) != afpInfoSize {
		t.Errorf("AfpInfo stream length = %d, want %d", len(raw), afpInfoSize)
	}
}

func TestADSForkEngine_PreservesBackupTime(t *testing.T) {
	e := newADSTestEngine()
	if _, err := e.fs.CreateFile("doc"); err != nil {
		t.Fatalf("create data: %v", err)
	}

	// Seed an AfpInfo stream as Windows SFM would, with a non-zero backupTime.
	seed := afpInfo{backupTime: 0x11223344}
	if err := e.writeAll(afpInfoStreamPath("doc"), encodeAfpInfo(seed)); err != nil {
		t.Fatalf("seed AfpInfo: %v", err)
	}

	var finder [32]byte
	copy(finder[:], []byte("disk____________________________"))
	if err := e.WriteFinderInfo("doc", finder); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}

	raw, _ := e.readAll(afpInfoStreamPath("doc"))
	a, err := parseAfpInfo(raw)
	if err != nil {
		t.Fatalf("parse after write: %v", err)
	}
	if a.backupTime != seed.backupTime {
		t.Errorf("backupTime clobbered: got %#x, want %#x", a.backupTime, seed.backupTime)
	}
	if a.finderInfo != finder {
		t.Errorf("finderInfo not written")
	}
}

func TestADSForkEngine_ResourceForkStream(t *testing.T) {
	e := newADSTestEngine()
	if _, err := e.fs.CreateFile("doc"); err != nil {
		t.Fatalf("create data: %v", err)
	}

	rf, err := e.OpenFork("doc", ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		t.Fatalf("OpenFork resource: %v", err)
	}
	payload := []byte("resource-fork-bytes")
	if _, err := rf.WriteAt(payload, 0); err != nil {
		t.Fatalf("write resource: %v", err)
	}
	if err := rf.Sync(); err != nil {
		t.Fatalf("sync resource: %v", err)
	}
	_ = rf.Close()

	n, err := e.ForkLen("doc", ResourceFork)
	if err != nil {
		t.Fatalf("ForkLen: %v", err)
	}
	if n != int64(len(payload)) {
		t.Errorf("ForkLen = %d, want %d", n, len(payload))
	}

	// Resource bytes must land in the AFP_Resource stream path.
	got, err := e.readAll(resourceStreamPath("doc"))
	if err != nil {
		t.Fatalf("read resource stream: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("resource stream = %q, want %q", got, payload)
	}
}

func TestADSForkEngine_DeleteAndMoveMetadata(t *testing.T) {
	e := newADSTestEngine()
	if _, err := e.fs.CreateFile("doc"); err != nil {
		t.Fatalf("create data: %v", err)
	}
	var finder [32]byte
	copy(finder[:], []byte("foo_____________________________"))
	if err := e.WriteFinderInfo("doc", finder); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}

	if err := e.MoveMetadata("doc", "moved"); err != nil {
		t.Fatalf("MoveMetadata: %v", err)
	}
	if _, ok, _ := e.ReadFinderInfo("doc"); ok {
		t.Error("old path still has FinderInfo after move")
	}
	if _, ok, _ := e.ReadFinderInfo("moved"); !ok {
		t.Error("moved path lost FinderInfo")
	}

	if err := e.DeleteMetadata("moved"); err != nil {
		t.Fatalf("DeleteMetadata: %v", err)
	}
	if _, ok, _ := e.ReadFinderInfo("moved"); ok {
		t.Error("FinderInfo survived DeleteMetadata")
	}
}

func TestForkEngineByName_ADSIsRealEngine(t *testing.T) {
	eng, err := forkAdapterByName("ads", newMemFS(ShareSpec{}))
	if err != nil {
		t.Fatalf("forkAdapterByName(ads): %v", err)
	}
	if _, ok := eng.(*adsForkEngine); !ok {
		t.Fatalf("ads backend = %T, want *adsForkEngine", eng)
	}
}
