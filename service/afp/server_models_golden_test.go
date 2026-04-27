//go:build afp

package afp

import (
	"bytes"
	"encoding/hex"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

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

// TestFPGetSrvrParmsRes_MarshalGolden pins the wire-format output of
// FPGetSrvrParmsRes.Marshal. Also asserts Marshal/Unmarshal round-trips.
func TestFPGetSrvrParmsRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPGetSrvrParmsRes{
		ServerTime: 0xDEADBEEF,
		Volumes: []VolInfo{
			{Flags: VolInfoFlagHasPassword, Name: "Macintosh HD"},
			{Flags: 0, Name: "Public"},
		},
	}
	got := res.Marshal()
	want := goldenBytes(t, "fpgetsrvrparmsres_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
	var rt FPGetSrvrParmsRes
	if err := rt.Unmarshal(got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if rt.ServerTime != res.ServerTime || len(rt.Volumes) != len(res.Volumes) {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", rt, *res)
	}
	for i := range rt.Volumes {
		if rt.Volumes[i] != res.Volumes[i] {
			t.Fatalf("vol[%d]: got %+v, want %+v", i, rt.Volumes[i], res.Volumes[i])
		}
	}
}

// TestFPLoginRes_MarshalGolden pins the wire-format output of FPLoginRes.Marshal.
func TestFPLoginRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPLoginRes{SRefNum: 0x1234, IDNumber: 0x5678}
	got := res.Marshal()
	want := goldenBytes(t, "fploginres_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}

// TestFPGetSrvrInfoRes_MarshalGolden pins the current wire-format output of
// FPGetSrvrInfoRes.Marshal so a future migration to MarshalWire/UnmarshalWire
// (Step 14) can be validated by diff. Run with -update to regenerate.
func TestFPGetSrvrInfoRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPGetSrvrInfoRes{
		MachineType: "OmniTalk",
		AFPVersions: []string{"AFPVersion 1.1", "AFPVersion 2.0", "AFPVersion 2.1"},
		UAMs:        []string{"No User Authent", "Cleartxt Passwrd"},
		ServerName:  "Test Server",
		Flags:       0x8000,
	}
	got := res.Marshal()
	want := goldenBytes(t, "fpgetsrvrinfores_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}
