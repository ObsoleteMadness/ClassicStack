//go:build netboot || all

package registry

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/hash/snefru"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/abp"
)

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestLoadPayloadAssemblesStubPlusImage: with an image path, the stub and the
// disk image are concatenated verbatim and trailered — the served bytes must
// start with stub||image and end in a valid Snefru trailer.
func TestLoadPayloadAssemblesStubPlusImage(t *testing.T) {
	stub := bytes.Repeat([]byte{0x4E}, 1024)            // BootWrapper.bin-sized stub
	img := bytes.Repeat([]byte{0xD5}, 8*abp.DiskSector) // small "disk image"
	got := loadPayload(
		writeTemp(t, "stub.bin", stub),
		writeTemp(t, "disk.dsk", img),
		abp.DiskSector, log.New("test"))
	if got == nil {
		t.Fatal("loadPayload returned nil")
	}
	if !bytes.Equal(got[:len(stub)], stub) || !bytes.Equal(got[len(stub):len(stub)+len(img)], img) {
		t.Fatal("stub/image not concatenated verbatim")
	}
	if len(got)%abp.DiskSector != 0 {
		t.Fatalf("assembled payload %d bytes not block-aligned", len(got))
	}
	if !snefru.HasValidTrailer(got) {
		t.Fatal("assembled payload has no valid trailer")
	}
}

// TestLoadPayloadPreTrailered: a payload already carrying a valid block-aligned
// trailer is served byte-for-byte untouched.
func TestLoadPayloadPreTrailered(t *testing.T) {
	pre, err := snefru.AppendTrailer(bytes.Repeat([]byte{0xAB}, 3*abp.DiskSector), abp.DiskSector)
	if err != nil {
		t.Fatal(err)
	}
	got := loadPayload(writeTemp(t, "payload", pre), "", abp.DiskSector, log.New("test"))
	if !bytes.Equal(got, pre) {
		t.Fatal("pre-trailered payload was modified")
	}
}

// TestLoadPayloadTrailersRaw: a raw (untrailered) payload gets padded and
// trailered at load.
func TestLoadPayloadTrailersRaw(t *testing.T) {
	raw := []byte("raw 68k payload bytes")
	got := loadPayload(writeTemp(t, "payload", raw), "", abp.DiskSector, log.New("test"))
	if got == nil {
		t.Fatal("loadPayload returned nil")
	}
	if !bytes.Equal(got[:len(raw)], raw) || !snefru.HasValidTrailer(got) || len(got)%abp.DiskSector != 0 {
		t.Fatal("raw payload not correctly trailered")
	}
}

// TestLoadPayloadRejectsOversize: an assembled payload beyond the client's
// 4088-block bitmap limit is refused (inert service beats an unbootable one).
func TestLoadPayloadRejectsOversize(t *testing.T) {
	img := make([]byte, (abp.MaxImageBlocks+8)*abp.DiskSector)
	got := loadPayload(
		writeTemp(t, "stub.bin", make([]byte, 1024)),
		writeTemp(t, "disk.dsk", img),
		abp.DiskSector, log.New("test"))
	if got != nil {
		t.Fatalf("oversize payload accepted (%d bytes)", len(got))
	}
}

func TestLoadPayloadMissingFiles(t *testing.T) {
	if loadPayload(filepath.Join(t.TempDir(), "nope.bin"), "", abp.DiskSector, log.New("test")) != nil {
		t.Fatal("missing payload accepted")
	}
	if loadPayload(writeTemp(t, "stub.bin", make([]byte, 64)), filepath.Join(t.TempDir(), "nope.dsk"), abp.DiskSector, log.New("test")) != nil {
		t.Fatal("missing image accepted")
	}
}
