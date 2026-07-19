package afp

import (
	"bytes"
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
)

// TestSat32 proves the AFP volume byte fields SATURATE at the reporting cap
// (2 GiB − 1) instead of wrapping: a vintage AFP 2.x client must see "full"
// for any disk larger than we can safely express, never a wrapped (smaller,
// wrong) figure. The cap is MaxInt32, NOT the field's 4 GiB − 1 range — a
// BytesTotal ≥ 2 GiB overflows the classic AppleShare client's 16-bit
// allocation-block-size math and crashes the System 7.5 Finder with a divide
// by zero at mount (observed e2e; see afpMaxVolumeBytes).
func TestSat32(t *testing.T) {
	cases := []struct {
		name string
		in   uint64
		want uint32
	}{
		{"zero", 0, 0},
		{"small", 1 << 20, 1 << 20}, // 1 MiB passes through
		{"just-under-cap", uint64(afpMaxVolumeBytes) - 1, afpMaxVolumeBytes - 1}, // exact value kept
		{"at-cap", uint64(afpMaxVolumeBytes), afpMaxVolumeBytes},                 // 2 GiB − 1 kept
		{"just-over-cap", uint64(afpMaxVolumeBytes) + 1, afpMaxVolumeBytes},      // 2 GiB → capped
		{"4GiB-1", 0xFFFFFFFF, afpMaxVolumeBytes},                                // old cap → now capped (Finder div/0)
		{"over-cap-6GiB", 6 << 30, afpMaxVolumeBytes},                            // 6 GiB → capped, NOT wrapped to 2 GiB
		{"huge-1TiB", 1 << 40, afpMaxVolumeBytes},                                // 1 TiB → capped
	}
	for _, tc := range cases {
		if got := sat32(tc.in); got != tc.want {
			t.Errorf("sat32(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestReportVolBytes_DefaultAndClamp proves the presentation clamp behind the
// Finder's "size on disk" granularity: a backend that cannot report usage
// (memfs) presents an empty virtual disk of the reported size, and a host-
// backed volume (local_fs on a big modern disk) is clamped to it. The default
// reported size is 512 MiB (8 KiB allocation blocks on a classic client),
// never the 2 GiB wire cap (32 KiB blocks — the "sizes on disk too large"
// complaint).
func TestReportVolBytes_DefaultAndClamp(t *testing.T) {
	// memfs: DiskUsage reports 0/0 → an empty virtual disk of the default size.
	mem, err := NewVolume(VolumeSpec{ID: 1, Name: "Mem", Share: fs.ShareSpec{
		Name: "Mem", FSType: "memfs", ForkBackend: "appledouble", FilenameCodec: "macroman-utf8",
	}})
	if err != nil {
		t.Fatalf("NewVolume(memfs): %v", err)
	}
	if total, free := reportVolBytes(mem); total != defaultVolumeSizeLimit || free != defaultVolumeSizeLimit {
		t.Fatalf("memfs reportVolBytes = %d/%d, want %d/%d", total, free, defaultVolumeSizeLimit, defaultVolumeSizeLimit)
	}

	// An explicit size_limit wins over the default.
	limited, err := NewVolume(VolumeSpec{ID: 2, Name: "Small", SizeLimit: 100 << 20, Share: fs.ShareSpec{
		Name: "Small", FSType: "memfs", ForkBackend: "appledouble", FilenameCodec: "macroman-utf8",
	}})
	if err != nil {
		t.Fatalf("NewVolume(limited): %v", err)
	}
	if total, _ := reportVolBytes(limited); total != 100<<20 {
		t.Fatalf("limited total = %d, want %d", total, 100<<20)
	}

	// local_fs on the host disk: real figures exist and are clamped to the limit
	// (the host disk is far larger than 512 MiB; skip if usage is unavailable).
	host, err := NewVolume(VolumeSpec{ID: 3, Name: "Host", Share: fs.ShareSpec{
		Name: "Host", FSType: "local_fs", Path: t.TempDir(),
		ForkBackend: "appledouble", FilenameCodec: "macroman-utf8",
	}})
	if err != nil {
		t.Fatalf("NewVolume(local_fs): %v", err)
	}
	if ht, _, err := host.FS().DiskUsage(""); err != nil || ht == 0 {
		t.Skip("DiskUsage unavailable on this platform")
	}
	total, free := reportVolBytes(host)
	if total != defaultVolumeSizeLimit {
		t.Fatalf("host total = %d, want clamped %d", total, defaultVolumeSizeLimit)
	}
	if free > total {
		t.Fatalf("host free %d > total %d", free, total)
	}
}

// TestGetVolParms_EchoesRequestedBitmap pins the FPGetVolParms reply to what an
// observed AppleShare server answers for the classic client's periodic poll
// (bitmap 0x0048 = ModDate + BytesFree): the SAME bitmap echoed — no injected
// VolumeID field — followed by exactly those two 4-byte values. Over memfs the
// values are deterministic: epoch ModDate (0) and the default virtual free.
func TestGetVolParms_EchoesRequestedBitmap(t *testing.T) {
	svc, r := newRunningService(t)
	from := fakePort{}
	sessID := login(t, svc, r)

	// Mount the volume so GetVolParms has an open volume id.
	r.reset()
	openVol := []byte{cmdOpenVol, 0}
	openVol = bp.AppendBE16(openVol, volBitmapID)
	openVol = putPString(openVol, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), openVol)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("OpenVol result = %d, want 0", got)
	}

	r.reset()
	req := []byte{cmdGetVolParms, 0}
	req = bp.AppendBE16(req, 1)      // volume id from the reconcile order
	req = bp.AppendBE16(req, 0x0048) // ModDate + BytesFree — the observed poll
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 4), req)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("GetVolParms result = %d, want 0", got)
	}
	got := respPayload(r.lastReply())
	want := bp.AppendBE16(nil, 0x0048)                         // echoed bitmap, no forced VolumeID
	want = bp.AppendBE32(want, 0)                              // ModDate: memfs has no root mtime → epoch
	want = bp.AppendBE32(want, uint32(defaultVolumeSizeLimit)) // BytesFree: empty virtual disk
	if !bytes.Equal(got, want) {
		t.Fatalf("GetVolParms reply:\n got:  %x\n want: %x", got, want)
	}
}
