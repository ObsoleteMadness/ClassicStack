//go:build afp || all

package afp

import (
	"bytes"
	"encoding/hex"
	"flag"
	"os"
	"path/filepath"
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
)

// These golden tests pin the byte-exact wire framing of AFP reply headers so a
// refactor can't silently drift a bitmap word or the file/dir flag byte again.
// They are the port of the DTO Marshal goldens from the pre-refactor tree
// (service/afp/*_models_golden_test.go); the .hex fixtures in testdata/ are the
// same files, so the new inline-assembled replies are held to the old bytes.
//
// The refactored core builds replies inline in the handlers rather than through
// FP*Res.Marshal DTOs, so the header framing is exercised two ways: the pure
// framing helpers below (fileDirParmsHeader), and a full end-to-end handler
// capture driven through the memfs harness.

var updateGolden = flag.Bool("update", false, "regenerate golden files in testdata/")

// goldenBytes loads the named hex golden, or rewrites it from got when -update
// is set. Hex format: whitespace-tolerant lowercase pairs (the file is meant to
// be human-readable, e.g. via `xxd -r -p`).
func goldenBytes(t *testing.T, name string, got []byte) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(hex.EncodeToString(got)+"\n"), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return got
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update to create): %v", path, err)
	}
	stripped := make([]byte, 0, len(raw))
	for _, b := range raw {
		if b == ' ' || b == '\n' || b == '\r' || b == '\t' {
			continue
		}
		stripped = append(stripped, b)
	}
	want, err := hex.DecodeString(string(stripped))
	if err != nil {
		t.Fatalf("decode golden %s: %v", path, err)
	}
	return want
}

// TestFileDirParmsHeader_FileGolden pins the FPGetFileDirParms reply header for a
// FILE: fileBitmap(2) dirBitmap(2) 00 00, then the opaque packed params.
func TestFileDirParmsHeader_FileGolden(t *testing.T) {
	t.Parallel()
	got := fileDirParmsHeader(nil, 0x07FB, 0x0DFF, false)
	got = append(got, 0xAA, 0xBB, 0xCC) // stand-in for the packed params
	want := goldenBytes(t, "fpgetfiledirparmsres_file.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("header drift:\n got:  %x\n want: %x", got, want)
	}
}

// TestFileDirParmsHeader_DirGolden pins the FPGetFileDirParms reply header for a
// DIRECTORY: fileBitmap(2) dirBitmap(2) 80 00, then the opaque packed params.
func TestFileDirParmsHeader_DirGolden(t *testing.T) {
	t.Parallel()
	got := fileDirParmsHeader(nil, 0x07FB, 0x0DFF, true)
	got = append(got, 0x11, 0x22, 0x33, 0x44) // stand-in for the packed params
	want := goldenBytes(t, "fpgetfiledirparmsres_dir.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("header drift:\n got:  %x\n want: %x", got, want)
	}
}

// TestFPOpenVol_ReplyGolden drives a full FPOpenVol end-to-end (login → open the
// memfs "Share" volume, requesting only the deterministic ID+Name params) and
// pins the packed reply: bitmap(2) volID(2) nameOffset(2) pstring("Share"). The
// date/disk-usage bits are deliberately not requested so the capture is stable.
func TestFPOpenVol_ReplyGolden(t *testing.T) {
	svc, r := newRunningService(t)
	from := fakePort{}
	sessID := login(t, svc, r)

	r.reset()
	openVol := []byte{cmdOpenVol, 0}
	openVol = bp.AppendBE16(openVol, volBitmapID|volBitmapName)
	openVol = putPString(openVol, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), openVol)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("OpenVol result = %d, want 0", got)
	}

	got := respPayload(r.lastReply())
	want := goldenBytes(t, "fpopenvol_share_id_name.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("OpenVol reply drift:\n got:  %x\n want: %x", got, want)
	}
}

// TestFPGetSrvrInfo_BlockGolden pins the offset-driven FPGetSrvrInfo reply block
// for a default-config server: the four 2-byte offsets (machine/versions/UAMs/
// icon), the Flags word, then the packed ServerName / MachineType / AFP-version
// list / UAM list. This is the most offset-intricate assembly in the service, so
// a golden guards the offset arithmetic against silent drift.
func TestFPGetSrvrInfo_BlockGolden(t *testing.T) {
	svc, _ := newRunningService(t)
	got := svc.serverInfoBlock()
	want := goldenBytes(t, "fpgetsrvrinfo_default.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("GetSrvrInfo block drift:\n got:  %x\n want: %x", got, want)
	}
}
