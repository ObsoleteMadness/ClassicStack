//go:build windows

package winfsp

import (
	"bytes"
	"testing"

	winfsp "github.com/winfsp/go-winfsp"
	"golang.org/x/sys/windows"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// newStreamAdapter builds a stream-aware Adapter (native forks on) over an in-memory
// ForkFS, so the SFM stream delegates can be exercised without the WinFsp driver.
func newStreamAdapter(t *testing.T) *Adapter {
	t.Helper()
	forkFS, err := fs.BuildShare(fs.ShareSpec{
		Name:        "Test",
		FSType:      "memfs",
		ForkBackend: "appledouble",
	}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	return New(forkFS, Options{VolumeLabel: "Test", NativeForks: true})
}

// createFile creates an empty data file through the delegate and closes the handle.
func createFile(t *testing.T, a *Adapter, name string) {
	t.Helper()
	var info winfsp.FSP_FSCTL_FILE_INFO
	ctx, err := a.Create(nil, name, 0, windows.GENERIC_WRITE, 0, nil, 0, &info)
	if err != nil {
		t.Fatalf("Create %s: %v", name, err)
	}
	a.Close(nil, ctx)
}

// TestSplitStream covers the base/stream separation, including the ":$DATA" type suffix.
func TestSplitStream(t *testing.T) {
	cases := []struct {
		in         string
		base, strm string
	}{
		{`\dir\file.txt`, `\dir\file.txt`, ""},
		{`\file.txt:AFP_Resource`, `\file.txt`, "AFP_Resource"},
		{`\file.txt:AFP_Resource:$DATA`, `\file.txt`, "AFP_Resource"},
		{`\dir\file:Comments`, `\dir\file`, "Comments"},
		{`\file::$DATA`, `\file`, ""}, // fully-qualified unnamed data stream
	}
	for _, c := range cases {
		base, strm := splitStream(c.in)
		if base != c.base || strm != c.strm {
			t.Errorf("splitStream(%q) = (%q,%q), want (%q,%q)", c.in, base, strm, c.base, c.strm)
		}
	}
}

// TestResourceStreamRoundTrip writes the :AFP_Resource stream and reads it back, then
// confirms the fork landed in the ForkEngine.
func TestResourceStreamRoundTrip(t *testing.T) {
	a := newStreamAdapter(t)
	createFile(t, a, "\\doc")

	rsrc := []byte("\x00\x01\x02resource-fork-bytes")
	var winfo winfsp.FSP_FSCTL_FILE_INFO
	ctx, err := a.Create(nil, "\\doc:AFP_Resource", 0, windows.GENERIC_WRITE, 0, nil, 0, &winfo)
	if err != nil {
		t.Fatalf("Create resource stream: %v", err)
	}
	n, err := a.Write(nil, ctx, rsrc, 0, false, false, &winfo)
	if err != nil || n != len(rsrc) {
		t.Fatalf("Write resource: n=%d err=%v", n, err)
	}
	if winfo.FileSize != uint64(len(rsrc)) {
		t.Errorf("post-write stream FileSize=%d, want %d", winfo.FileSize, len(rsrc))
	}
	a.Close(nil, ctx)

	// The fork must now be visible through the ForkEngine seam.
	if got, err := a.fsys.ForkLen("doc", fs.ResourceFork); err != nil || got != int64(len(rsrc)) {
		t.Fatalf("ForkLen after write: got=%d err=%v, want %d", got, err, len(rsrc))
	}

	// Re-open the stream and read it back.
	var oinfo winfsp.FSP_FSCTL_FILE_INFO
	rctx, err := a.Open(nil, "\\doc:AFP_Resource", 0, windows.GENERIC_READ, &oinfo)
	if err != nil {
		t.Fatalf("Open resource stream: %v", err)
	}
	if oinfo.FileSize != uint64(len(rsrc)) {
		t.Errorf("Open stream FileSize=%d, want %d", oinfo.FileSize, len(rsrc))
	}
	buf := make([]byte, len(rsrc))
	rn, err := a.Read(nil, rctx, buf, 0)
	if err != nil {
		t.Fatalf("Read resource stream: %v", err)
	}
	if !bytes.Equal(buf[:rn], rsrc) {
		t.Errorf("resource stream round-trip: got %q want %q", buf[:rn], rsrc)
	}
	a.Close(nil, rctx)
}

// TestAfpInfoStream writes a 60-byte AfpInfo record through the stream and confirms the
// FinderInfo reached the ForkEngine, and that the stream reads back a valid record.
func TestAfpInfoStream(t *testing.T) {
	a := newStreamAdapter(t)
	createFile(t, a, "\\doc")

	var finder [32]byte
	copy(finder[:], []byte("TEXTttxt------finder-info-bytes!"))
	rec := fs.AfpInfo{FinderInfo: finder}.Marshal()

	var winfo winfsp.FSP_FSCTL_FILE_INFO
	ctx, err := a.Create(nil, "\\doc:AFP_AfpInfo", 0, windows.GENERIC_WRITE, 0, nil, 0, &winfo)
	if err != nil {
		t.Fatalf("Create AfpInfo stream: %v", err)
	}
	if n, err := a.Write(nil, ctx, rec, 0, false, false, &winfo); err != nil || n != len(rec) {
		t.Fatalf("Write AfpInfo: n=%d err=%v", n, err)
	}
	a.Close(nil, ctx) // flush persists FinderInfo

	got, ok, err := a.fsys.ReadFinderInfo("doc")
	if err != nil || !ok {
		t.Fatalf("ReadFinderInfo after write: ok=%v err=%v", ok, err)
	}
	if got != finder {
		t.Errorf("FinderInfo mismatch after AfpInfo stream write")
	}

	// Reading the stream yields a valid 60-byte record carrying the same FinderInfo.
	var oinfo winfsp.FSP_FSCTL_FILE_INFO
	rctx, err := a.Open(nil, "\\doc:AFP_AfpInfo", 0, windows.GENERIC_READ, &oinfo)
	if err != nil {
		t.Fatalf("Open AfpInfo stream: %v", err)
	}
	if oinfo.FileSize != fs.AfpInfoSize {
		t.Errorf("AfpInfo stream size=%d, want %d", oinfo.FileSize, fs.AfpInfoSize)
	}
	buf := make([]byte, fs.AfpInfoSize)
	if _, err := a.Read(nil, rctx, buf, 0); err != nil {
		t.Fatalf("Read AfpInfo stream: %v", err)
	}
	parsed, err := fs.UnmarshalAfpInfo(buf)
	if err != nil {
		t.Fatalf("UnmarshalAfpInfo(stream): %v", err)
	}
	if parsed.FinderInfo != finder {
		t.Errorf("AfpInfo stream FinderInfo round-trip mismatch")
	}
	a.Close(nil, rctx)
}

// TestCommentsStream round-trips the :Comments stream through ReadComment/WriteComment.
func TestCommentsStream(t *testing.T) {
	a := newStreamAdapter(t)
	createFile(t, a, "\\doc")

	comment := []byte("Get Info comment from Windows")
	var winfo winfsp.FSP_FSCTL_FILE_INFO
	ctx, err := a.Create(nil, "\\doc:Comments", 0, windows.GENERIC_WRITE, 0, nil, 0, &winfo)
	if err != nil {
		t.Fatalf("Create Comments stream: %v", err)
	}
	if n, err := a.Write(nil, ctx, comment, 0, false, false, &winfo); err != nil || n != len(comment) {
		t.Fatalf("Write Comments: n=%d err=%v", n, err)
	}
	a.Close(nil, ctx)

	got, ok := a.fsys.ReadComment("doc")
	if !ok || !bytes.Equal(got, comment) {
		t.Fatalf("ReadComment after write: ok=%v got=%q want=%q", ok, got, comment)
	}
}

// TestGetStreamInfoListsForks confirms a file with a resource fork + Finder info + comment
// enumerates the data stream plus the three SFM streams, and that a bare file lists only
// the data stream.
func TestGetStreamInfoListsForks(t *testing.T) {
	a := newStreamAdapter(t)
	s := streamAdapter{a}
	createFile(t, a, "\\doc")

	// Bare file: only the unnamed data stream.
	dctx, err := a.Open(nil, "\\doc", 0, windows.GENERIC_READ, &winfsp.FSP_FSCTL_FILE_INFO{})
	if err != nil {
		t.Fatalf("Open doc: %v", err)
	}
	names := collectStreams(t, s, dctx)
	if len(names) != 1 || !names[""] {
		t.Errorf("bare file streams = %v, want just the data stream", names)
	}
	a.Close(nil, dctx)

	// Add a resource fork, Finder info, and a comment.
	writeStreamContent(t, a, "\\doc:AFP_Resource", []byte("rsrc"))
	var finder [32]byte
	copy(finder[:], "TEXTttxt")
	writeStreamContent(t, a, "\\doc:AFP_AfpInfo", fs.AfpInfo{FinderInfo: finder}.Marshal())
	writeStreamContent(t, a, "\\doc:Comments", []byte("hi"))

	dctx2, err := a.Open(nil, "\\doc", 0, windows.GENERIC_READ, &winfsp.FSP_FSCTL_FILE_INFO{})
	if err != nil {
		t.Fatalf("Open doc (2): %v", err)
	}
	names = collectStreams(t, s, dctx2)
	for _, want := range []string{"", streamNameResource, streamNameAfpInfo, streamNameComments} {
		if !names[want] {
			t.Errorf("GetStreamInfo missing %q; got %v", want, names)
		}
	}
	a.Close(nil, dctx2)
}

// (The streams-disabled rejection path is covered by TestStreamSuffixRejected in
// adapter_test.go, which drives an adapter built without NativeForks.)

// writeStreamContent creates a stream and writes buf to it, then closes it.
func writeStreamContent(t *testing.T, a *Adapter, name string, buf []byte) {
	t.Helper()
	var info winfsp.FSP_FSCTL_FILE_INFO
	ctx, err := a.Create(nil, name, 0, windows.GENERIC_WRITE, 0, nil, 0, &info)
	if err != nil {
		t.Fatalf("Create %s: %v", name, err)
	}
	if _, err := a.Write(nil, ctx, buf, 0, false, false, &info); err != nil {
		t.Fatalf("Write %s: %v", name, err)
	}
	a.Close(nil, ctx)
}

// collectStreams drives GetStreamInfo and returns the set of stream names reported.
func collectStreams(t *testing.T, s streamAdapter, ctx uintptr) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	err := s.GetStreamInfo(nil, ctx, func(name string, _, _ uint64) (bool, error) {
		names[name] = true
		return true, nil
	})
	if err != nil {
		t.Fatalf("GetStreamInfo: %v", err)
	}
	return names
}
