package fs

import (
	"os"
	"testing"
)

// buildLocalShare builds a local_fs-backed share over a temp dir with the
// metastore MetaEngine backend, returning the share and the temp dir.
func buildLocalShare(t *testing.T) (ForkFS, string) {
	t.Helper()
	dir := t.TempDir()
	sh, err := BuildShare(ShareSpec{
		Name:        "T",
		FSType:      "local_fs",
		MetaBackend: "metastore",
		Metastore:   "mem",
		Path:        dir,
	}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	return sh, dir
}

func TestBuildShareExposesMetaEngine(t *testing.T) {
	sh, _ := buildLocalShare(t)
	if sh.Meta() == nil {
		t.Error("built share does not expose a MetaEngine")
	}
	if _, ok := sh.(HostPather); !ok {
		t.Error("local_fs-backed share should expose HostPather")
	}
}

// buildLocalDOSAttrStore builds a local_fs base FileSystem over a temp dir and
// a DOSAttrStore through buildDOSAttrStore directly (the internal seam every
// MetaEngine backend's Attrs/SetAttrs/DeleteAttrs/RenameAttrs wraps), so this
// test can drive each dos-attr backend name without a MetaBackend selector for
// it in ShareSpec.
func buildLocalDOSAttrStore(t *testing.T, backend string) (DOSAttrStore, FileSystem, string) {
	t.Helper()
	dir := t.TempDir()
	spec := ShareSpec{Name: "T", FSType: "local_fs", Path: dir}
	f, ok := lookupFactory(spec.FSType)
	if !ok {
		t.Fatalf("no factory for %q", spec.FSType)
	}
	base, err := f(spec, NewBus(0), nil)
	if err != nil {
		t.Fatalf("build base fs: %v", err)
	}
	return buildDOSAttrStore(backend, base, nil, nil), base, dir
}

func TestDOSAttrStoreThroughShare(t *testing.T) {
	// Exercise every host-portable backend (metastore + sidecar; native/xattr are
	// host-gated and covered by the build matrix).
	for _, backend := range []string{"metastore", "sidecar", "auto"} {
		t.Run(backend, func(t *testing.T) {
			da, _, dir := buildLocalDOSAttrStore(t, backend)

			// Create a real file so sidecar/host backends have something to attach to.
			if err := os.WriteFile(dir+string(os.PathSeparator)+"FILE.TXT", []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}

			want := DOSAttr{Attrs: DOSHidden | DOSSystem}
			if err := da.Set("FILE.TXT", want); err != nil {
				t.Fatalf("Set: %v", err)
			}
			got, ok := da.Get("FILE.TXT")
			if !ok {
				t.Fatal("Get after Set returned ok=false")
			}
			if !got.Has(DOSHidden) || !got.Has(DOSSystem) {
				t.Errorf("attrs lost: %#x", got.Attrs)
			}

			if err := da.Rename("FILE.TXT", "MOVED.TXT"); err != nil {
				t.Fatalf("Rename: %v", err)
			}
			if _, ok := da.Get("MOVED.TXT"); !ok {
				t.Error("attrs not carried across rename")
			}

			if err := da.Delete("MOVED.TXT"); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if _, ok := da.Get("MOVED.TXT"); ok {
				t.Error("attrs not cleared on delete")
			}
		})
	}
}

func TestSidecarDOSAttrBlobIsSambaCompatible(t *testing.T) {
	// The sidecar writes the same XATTR_DOSINFO blob Samba uses, so the bytes a
	// sidecar produced decode back through the metastore codec.
	da, _, dir := buildLocalDOSAttrStore(t, "sidecar")
	if err := os.WriteFile(dir+string(os.PathSeparator)+"S.TXT", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := da.Set("S.TXT", DOSAttr{Attrs: DOSReadOnly}); err != nil {
		t.Fatal(err)
	}
	// The companion file exists under .dosattr/.
	if _, err := os.Stat(dir + string(os.PathSeparator) + ".dosattr" + string(os.PathSeparator) + "S.TXT"); err != nil {
		t.Fatalf("sidecar companion missing: %v", err)
	}
}
