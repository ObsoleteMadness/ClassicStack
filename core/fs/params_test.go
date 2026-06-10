package fs

import (
	"os"
	"strings"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// TestParamSchema_RequiredValidation asserts BuildShare rejects a share whose
// fs_type declares a Required param that the spec doesn't supply, and accepts it
// once the param is present — in Path (for PathKey) or in Extra.
func TestParamSchema_RequiredValidation(t *testing.T) {
	build := func(_ ShareSpec, _ bus.Bus, _ metastore.Store) (FileSystem, error) {
		return newMemFS(ShareSpec{}), nil
	}
	// A path-backed type and a url-backed type, to cover both PathKey and Extra.
	RegisterFSWithParams("test-pathfs", build, Param{Key: PathKey, Required: true, Doc: "host dir"})
	RegisterFSWithParams("test-ftp", build,
		Param{Key: "url", Required: true, Doc: "ftp url"},
		Param{Key: "username", Required: false},
		Param{Key: "password", Required: false, Secret: true},
	)

	if _, err := BuildShare(ShareSpec{FSType: "test-pathfs"}, nil); err == nil {
		t.Fatal("expected missing-path share to be rejected")
	} else if !strings.Contains(err.Error(), "path") {
		t.Fatalf("error %q should mention the missing path", err)
	}
	if _, err := BuildShare(ShareSpec{FSType: "test-pathfs", Path: "/srv/share"}, nil); err != nil {
		t.Fatalf("path-supplied share rejected: %v", err)
	}

	if _, err := BuildShare(ShareSpec{FSType: "test-ftp"}, nil); err == nil {
		t.Fatal("expected ftp share missing url to be rejected")
	}
	if _, err := BuildShare(ShareSpec{FSType: "test-ftp", Extra: map[string]any{"url": "  "}}, nil); err == nil {
		t.Fatal("expected blank url to be rejected")
	}
	if _, err := BuildShare(ShareSpec{FSType: "test-ftp", Extra: map[string]any{"url": "ftp://host/pub"}}, nil); err != nil {
		t.Fatalf("ftp share with url rejected: %v", err)
	}
}

// TestParamsFor_ReturnsSchema asserts the declared schema (incl. Secret flags) is
// readable back for the UI/config layer, and that RegisterFS declares none.
func TestParamsFor_ReturnsSchema(t *testing.T) {
	RegisterFSWithParams("test-schemafs", func(_ ShareSpec, _ bus.Bus, _ metastore.Store) (FileSystem, error) {
		return newMemFS(ShareSpec{}), nil
	}, Param{Key: "password", Required: true, Secret: true, Doc: "pw"})

	got := ParamsFor("test-schemafs")
	if len(got) != 1 || got[0].Key != "password" || !got[0].Secret || !got[0].Required {
		t.Fatalf("ParamsFor schema = %+v, want one required secret 'password'", got)
	}
	// memfs declares no params (registered via plain RegisterFS).
	if len(ParamsFor("memfs")) != 0 {
		t.Fatalf("memfs should declare no params, got %+v", ParamsFor("memfs"))
	}
}

// TestForkFS_RenameRemoveCarryMetadata asserts the assembled ForkFS moves and
// deletes a file's metadata container together with its data fork, with no caller
// pairing of MoveMetadata/DeleteMetadata.
func TestForkFS_RenameRemoveCarryMetadata(t *testing.T) {
	share, err := BuildShare(ShareSpec{FSType: "memfs", ForkBackend: "appledouble"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}

	if _, err := share.CreateFile("doc"); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if err := share.WriteFinderInfo("doc", [32]byte{'F', 'I'}); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}

	if err := share.Rename("doc", "moved"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if info, ok, _ := share.ReadFinderInfo("moved"); !ok || info[0] != 'F' {
		t.Fatalf("FinderInfo did not follow the rename: ok=%v info=%v", ok, info)
	}
	if _, ok, _ := share.ReadFinderInfo("doc"); ok {
		t.Fatal("FinderInfo lingered at the old path after rename")
	}

	if err := share.Remove("moved"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok, _ := share.ReadFinderInfo("moved"); ok {
		t.Fatal("FinderInfo survived Remove")
	}
	if _, err := share.Stat("moved"); !os.IsNotExist(err) {
		t.Fatalf("data fork survived Remove: err=%v", err)
	}
}
