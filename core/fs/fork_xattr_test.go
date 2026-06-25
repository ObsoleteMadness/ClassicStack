package fs

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/appledouble"
)

// newXattrTestEngine adapts a memFS into an EA-naming FileSystem: it is just the
// memFS, so "path\x00ea\x00<name>" is an ordinary path key. This exercises the
// xattr engine's Metadata-EA record + EA-path logic without a real xattr host.
func newXattrTestEngine() *xattrForkEngine {
	return newXattrForkEngine(newMemFS(ShareSpec{}))
}

func TestEncodeParseMetadataEA_RoundTrip(t *testing.T) {
	var finder [32]byte
	copy(finder[:], []byte("TEXTttxt-netatalk-finder-info!!!"))
	in := appledouble.Parsed{
		FinderInfo: finder,
		HasFinder:  true,
		Comment:    []byte("hello world"),
		HasComment: true,
	}

	b := encodeMetadataEA(in, 1234)
	if len(b) != xattrMetadataSize {
		t.Fatalf("encoded length = %d, want %d (AD_DATASZ_EA)", len(b), xattrMetadataSize)
	}
	if got := binary.BigEndian.Uint32(b[0:4]); got != appledouble.Magic {
		t.Fatalf("magic = %#x, want %#x", got, appledouble.Magic)
	}
	if got := string(b[8:24]); got != xattrFiller {
		t.Fatalf("filler = %q, want %q", got, xattrFiller)
	}

	out, rsrcLen, err := parseMetadataEA(b)
	if err != nil {
		t.Fatalf("parseMetadataEA: %v", err)
	}
	if out.FinderInfo != in.FinderInfo {
		t.Errorf("FinderInfo round-trip mismatch")
	}
	if !out.HasComment || !bytes.Equal(out.Comment, in.Comment) {
		t.Errorf("comment round-trip = %q, want %q", out.Comment, in.Comment)
	}
	if rsrcLen != 1234 {
		t.Errorf("recorded resource length = %d, want 1234", rsrcLen)
	}
}

func TestParseMetadataEA_RejectsBadMagic(t *testing.T) {
	b := make([]byte, xattrMetadataSize) // all-zero: wrong magic
	if _, _, err := parseMetadataEA(b); err == nil {
		t.Fatal("expected error for zero magic")
	}
	if _, _, err := parseMetadataEA(b[:appledouble.HeaderSize-1]); err == nil {
		t.Fatal("expected error for short record")
	}
}

func TestXattrForkEngine_FinderInfoRoundTrip(t *testing.T) {
	e := newXattrTestEngine()
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

	// The Metadata EA must hold a fixed-size record at the EA path.
	raw, err := e.readAll(metadataEAPath("doc"))
	if err != nil {
		t.Fatalf("read Metadata EA: %v", err)
	}
	if len(raw) != xattrMetadataSize {
		t.Errorf("Metadata EA length = %d, want %d", len(raw), xattrMetadataSize)
	}
}

func TestXattrForkEngine_CommentRoundTrip(t *testing.T) {
	e := newXattrTestEngine()
	if _, err := e.fs.CreateFile("doc"); err != nil {
		t.Fatalf("create data: %v", err)
	}

	if err := e.WriteComment("doc", []byte("a finder comment")); err != nil {
		t.Fatalf("WriteComment: %v", err)
	}
	got, ok := e.ReadComment("doc")
	if !ok {
		t.Fatal("ReadComment: not present after write")
	}
	if string(got) != "a finder comment" {
		t.Errorf("comment = %q, want %q", got, "a finder comment")
	}
}

func TestXattrForkEngine_ResourceForkEA(t *testing.T) {
	e := newXattrTestEngine()
	if _, err := e.fs.CreateFile("doc"); err != nil {
		t.Fatalf("create data: %v", err)
	}

	rf, err := e.OpenFork("doc", ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		t.Fatalf("OpenFork resource: %v", err)
	}
	payload := []byte("resource-fork-bytes-in-an-EA")
	if _, err := rf.WriteAt(payload, 0); err != nil {
		t.Fatalf("write resource: %v", err)
	}
	if err := rf.Close(); err != nil {
		t.Fatalf("close resource: %v", err)
	}

	n, err := e.ForkLen("doc", ResourceFork)
	if err != nil {
		t.Fatalf("ForkLen: %v", err)
	}
	if n != int64(len(payload)) {
		t.Errorf("ForkLen = %d, want %d", n, len(payload))
	}

	// Resource bytes must land in the ResourceFork EA path.
	got, err := e.readAll(resourceEAPath("doc"))
	if err != nil {
		t.Fatalf("read resource EA: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("resource EA = %q, want %q", got, payload)
	}

	// The Metadata EA must now record the resource length, matching Netatalk's
	// invariant that the two EAs stay in step.
	raw, err := e.readAll(metadataEAPath("doc"))
	if err != nil {
		t.Fatalf("read Metadata EA: %v", err)
	}
	_, recorded, err := parseMetadataEA(raw)
	if err != nil {
		t.Fatalf("parse Metadata EA: %v", err)
	}
	if recorded != uint32(len(payload)) {
		t.Errorf("Metadata EA recorded resource length = %d, want %d", recorded, len(payload))
	}
}

func TestXattrForkEngine_DeleteAndMoveMetadata(t *testing.T) {
	e := newXattrTestEngine()
	if _, err := e.fs.CreateFile("doc"); err != nil {
		t.Fatalf("create data: %v", err)
	}
	var finder [32]byte
	copy(finder[:], []byte("foo_____________________________"))
	if err := e.WriteFinderInfo("doc", finder); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}
	rf, err := e.OpenFork("doc", ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		t.Fatalf("OpenFork: %v", err)
	}
	if _, err := rf.WriteAt([]byte("rsrc"), 0); err != nil {
		t.Fatalf("write resource: %v", err)
	}
	_ = rf.Close()

	if err := e.MoveMetadata("doc", "moved"); err != nil {
		t.Fatalf("MoveMetadata: %v", err)
	}
	if _, ok, _ := e.ReadFinderInfo("doc"); ok {
		t.Error("old path still has FinderInfo after move")
	}
	if _, ok, _ := e.ReadFinderInfo("moved"); !ok {
		t.Error("moved path lost FinderInfo")
	}
	if n, _ := e.ForkLen("moved", ResourceFork); n != 4 {
		t.Errorf("moved resource fork len = %d, want 4", n)
	}

	if err := e.DeleteMetadata("moved"); err != nil {
		t.Fatalf("DeleteMetadata: %v", err)
	}
	if _, ok, _ := e.ReadFinderInfo("moved"); ok {
		t.Error("FinderInfo survived DeleteMetadata")
	}
	if n, _ := e.ForkLen("moved", ResourceFork); n != 0 {
		t.Errorf("resource fork survived DeleteMetadata: len = %d", n)
	}
}

func TestForkEngineByName_XattrIsRealEngine(t *testing.T) {
	eng, err := forkAdapterByName("xattr", newMemFS(ShareSpec{}))
	if err != nil {
		t.Fatalf("forkAdapterByName(xattr): %v", err)
	}
	if _, ok := eng.(*xattrForkEngine); !ok {
		t.Fatalf("xattr backend = %T, want *xattrForkEngine", eng)
	}
}
