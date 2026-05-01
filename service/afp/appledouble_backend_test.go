//go:build afp || all

package afp

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestAppleDoubleBackend_WritesExpectedSidecarByMode(t *testing.T) {
	tests := []struct {
		name          string
		mode          AppleDoubleMode
		sidecarPath   string
		artifactName  string
		artifactIsDir bool
	}{
		{
			name:          "modern writes underscore sidecar",
			mode:          AppleDoubleModeModern,
			sidecarPath:   filepath.Join("._Configuration"),
			artifactName:  "._Configuration",
			artifactIsDir: false,
		},
		{
			name:          "legacy writes .AppleDouble directory sidecar",
			mode:          AppleDoubleModeLegacy,
			sidecarPath:   filepath.Join(".AppleDouble", "Configuration"),
			artifactName:  ".AppleDouble",
			artifactIsDir: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			backend := NewAppleDoubleBackend(&LocalFileSystem{}, tc.mode, true)

			target := filepath.Join(root, "Configuration")
			if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
				t.Fatalf("seed file: %v", err)
			}

			var fi [32]byte
			fi[0] = 0xCA
			if err := backend.WriteFinderInfo(target, fi); err != nil {
				t.Fatalf("WriteFinderInfo: %v", err)
			}

			if _, err := os.Stat(filepath.Join(root, tc.sidecarPath)); err != nil {
				t.Fatalf("expected sidecar, stat err=%v", err)
			}
			if backend.IsMetadataArtifact(tc.artifactName, tc.artifactIsDir) != true {
				t.Fatalf("expected %q artifact to be hidden", tc.artifactName)
			}
		})
	}
}

func TestAppleDoubleBackend_LegacyFallbackStatsMetadataFile(t *testing.T) {
	root := t.TempDir()
	backend := NewAppleDoubleBackend(&LocalFileSystem{}, AppleDoubleModeLegacy, true)

	requested := filepath.Join(root, "Netscape Navigator 2.02")
	legacySidecar := filepath.Join(root, ".AppleDouble", filepath.Base(requested))
	if err := os.MkdirAll(filepath.Dir(legacySidecar), 0755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.WriteFile(legacySidecar, []byte("adouble"), 0644); err != nil {
		t.Fatalf("write legacy sidecar: %v", err)
	}

	gotPath, info, err := backend.StatWithMetadataFallback(requested)
	if err != nil {
		t.Fatalf("StatWithMetadataFallback: %v", err)
	}
	if gotPath != legacySidecar {
		t.Fatalf("fallback path = %q, want %q", gotPath, legacySidecar)
	}
	if info.IsDir() {
		t.Fatalf("fallback info should be file")
	}
}

func TestAppleDoubleBackend_MetadataPath_IsIdempotentForSidecars(t *testing.T) {
	tests := []struct {
		name string
		mode AppleDoubleMode
		path string
	}{
		{
			name: "modern sidecar",
			mode: AppleDoubleModeModern,
			path: filepath.Join("vault", "._CD-ROM Toolkit™ Installer"),
		},
		{
			name: "legacy sidecar",
			mode: AppleDoubleModeLegacy,
			path: filepath.Join("vault", ".AppleDouble", "CD-ROM Toolkit™ Installer"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			backend := NewAppleDoubleBackend(&LocalFileSystem{}, tc.mode, true)
			if got := backend.MetadataPath(tc.path); got != tc.path {
				t.Fatalf("MetadataPath(%q) = %q, want %q", tc.path, got, tc.path)
			}
		})
	}
}

// TestPerVolumeAppleDoubleMode verifies that two volumes in the same AFPService can
// each use a different AppleDouble mode. Writing FinderInfo to each volume should
// produce sidecars in the layout appropriate to that volume's mode.
func TestPerVolumeAppleDoubleMode(t *testing.T) {
	modernRoot := t.TempDir()
	legacyRoot := t.TempDir()

	s := NewService("TestServer",
		[]VolumeConfig{
			{Name: "Modern", Path: modernRoot, AppleDoubleMode: AppleDoubleModeModern},
			{Name: "Legacy", Path: legacyRoot, AppleDoubleMode: AppleDoubleModeLegacy},
		},
		&LocalFileSystem{}, nil,
		Options{DecomposedFilenames: true},
	)

	// Volume 1 == Modern, Volume 2 == Legacy (IDs assigned by NewAFPService).
	const modernVolID = uint16(1)
	const legacyVolID = uint16(2)

	// Confirm the per-volume backends have the expected mode.
	modernMeta := s.metaFor(modernVolID)
	if modernMeta == nil {
		t.Fatal("expected a backend for modern volume, got nil")
	}
	legacyMeta := s.metaFor(legacyVolID)
	if legacyMeta == nil {
		t.Fatal("expected a backend for legacy volume, got nil")
	}

	// Write a file into each volume root and write FinderInfo through the service.
	modernFile := filepath.Join(modernRoot, "ReadMe")
	legacyFile := filepath.Join(legacyRoot, "ReadMe")
	for _, p := range []string{modernFile, legacyFile} {
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatalf("seed file %q: %v", p, err)
		}
	}

	var fi [32]byte
	fi[0] = 0xAB
	if err := modernMeta.WriteFinderInfo(modernFile, fi); err != nil {
		t.Fatalf("WriteFinderInfo modern: %v", err)
	}
	if err := legacyMeta.WriteFinderInfo(legacyFile, fi); err != nil {
		t.Fatalf("WriteFinderInfo legacy: %v", err)
	}

	// Modern: sidecar should be ._ReadMe in the same directory.
	modernSidecar := filepath.Join(modernRoot, "._ReadMe")
	if _, err := os.Stat(modernSidecar); err != nil {
		t.Fatalf("expected modern sidecar %q, stat err=%v", modernSidecar, err)
	}
	// Legacy: sidecar should be under .AppleDouble/.
	legacySidecar := filepath.Join(legacyRoot, ".AppleDouble", "ReadMe")
	if _, err := os.Stat(legacySidecar); err != nil {
		t.Fatalf("expected legacy sidecar %q, stat err=%v", legacySidecar, err)
	}

	// Confirm that the modern-volume sidecar does NOT appear in the legacy directory, and vice-versa.
	if _, err := os.Stat(filepath.Join(legacyRoot, "._ReadMe")); err == nil {
		t.Fatal("legacy volume unexpectedly created a modern-style sidecar")
	}
	if _, err := os.Stat(filepath.Join(modernRoot, ".AppleDouble", "ReadMe")); err == nil {
		t.Fatal("modern volume unexpectedly created a legacy-style sidecar")
	}

	// Confirm isMetadataArtifact respects per-volume mode.
	if s.isMetadataArtifact("._ReadMe", false, modernVolID) != true {
		t.Error("modern volume: ._ReadMe should be a metadata artifact")
	}
	if s.isMetadataArtifact(".AppleDouble", true, legacyVolID) != true {
		t.Error("legacy volume: .AppleDouble should be a metadata artifact")
	}
	// .AppleDouble is always hidden regardless of volume mode.
	if s.isMetadataArtifact(".AppleDouble", true, modernVolID) != true {
		t.Error("modern volume: .AppleDouble should be a metadata artifact (always hidden)")
	}
}

func TestHandleCopyFile_ConvertsAppleDoubleModeBetweenVolumes(t *testing.T) {
	tests := []struct {
		name                string
		srcMode             AppleDoubleMode
		dstMode             AppleDoubleMode
		expectSourceSidecar string
		expectTargetSidecar string
		forbidTargetSidecar string
	}{
		{
			name:                "modern to legacy",
			srcMode:             AppleDoubleModeModern,
			dstMode:             AppleDoubleModeLegacy,
			expectSourceSidecar: "._ReadMe",
			expectTargetSidecar: filepath.Join(".AppleDouble", "Copied ReadMe"),
			forbidTargetSidecar: "._Copied ReadMe",
		},
		{
			name:                "legacy to modern",
			srcMode:             AppleDoubleModeLegacy,
			dstMode:             AppleDoubleModeModern,
			expectSourceSidecar: filepath.Join(".AppleDouble", "ReadMe"),
			expectTargetSidecar: "._Copied ReadMe",
			forbidTargetSidecar: filepath.Join(".AppleDouble", "Copied ReadMe"),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srcRoot := t.TempDir()
			dstRoot := t.TempDir()

			s := NewService("TestServer",
				[]VolumeConfig{
					{Name: "Source", Path: srcRoot, AppleDoubleMode: tc.srcMode},
					{Name: "Target", Path: dstRoot, AppleDoubleMode: tc.dstMode},
				},
				&LocalFileSystem{}, nil,
				Options{DecomposedFilenames: true},
			)

			const srcVolID = uint16(1)
			const dstVolID = uint16(2)

			srcMeta := s.metaFor(srcVolID)
			dstMeta := s.metaFor(dstVolID)
			if srcMeta == nil || dstMeta == nil {
				t.Fatal("expected source and destination metadata backends")
			}

			srcPath := filepath.Join(srcRoot, "ReadMe")
			dstPath := filepath.Join(dstRoot, "Copied ReadMe")
			if err := os.WriteFile(srcPath, []byte("data fork"), 0644); err != nil {
				t.Fatalf("seed source file: %v", err)
			}

			var finderInfo [32]byte
			finderInfo[0] = 0x41
			finderInfo[8] = 0x99
			if err := srcMeta.WriteFinderInfo(srcPath, finderInfo); err != nil {
				t.Fatalf("WriteFinderInfo: %v", err)
			}

			commentBackend, ok := srcMeta.(CommentBackend)
			if !ok {
				t.Fatal("source metadata backend does not support comments")
			}
			if err := commentBackend.WriteComment(srcPath, []byte("copied comment")); err != nil {
				t.Fatalf("WriteComment: %v", err)
			}

			forkData := []byte("resource fork payload")
			fork, forkInfo, err := srcMeta.OpenResourceFork(srcPath, true)
			if err != nil {
				t.Fatalf("OpenResourceFork source writable: %v", err)
			}
			if fork == nil {
				t.Fatal("expected source resource fork handle")
			}
			if _, err := fork.WriteAt(forkData, forkInfo.Offset); err != nil {
				fork.Close()
				t.Fatalf("write source resource fork: %v", err)
			}
			if err := srcMeta.TruncateResourceFork(fork, forkInfo, int64(len(forkData))); err != nil {
				fork.Close()
				t.Fatalf("truncate source resource fork: %v", err)
			}
			if err := fork.Close(); err != nil {
				t.Fatalf("close source resource fork: %v", err)
			}

			_, errCode := s.handleCopyFile(&FPCopyFileReq{
				SrcVolumeID: srcVolID,
				SrcDirID:    CNIDRoot,
				SrcPathType: 2,
				SrcName:     "ReadMe",
				DstVolumeID: dstVolID,
				DstDirID:    CNIDRoot,
				DstPathType: 2,
				NewName:     "Copied ReadMe",
			})
			if errCode != NoErr {
				t.Fatalf("handleCopyFile err = %d, want %d", errCode, NoErr)
			}

			if _, err := os.Stat(filepath.Join(srcRoot, tc.expectSourceSidecar)); err != nil {
				t.Fatalf("source sidecar missing: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dstRoot, tc.expectTargetSidecar)); err != nil {
				t.Fatalf("target sidecar missing: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dstRoot, tc.forbidTargetSidecar)); !os.IsNotExist(err) {
				t.Fatalf("unexpected target sidecar layout present, stat err=%v", err)
			}

			gotMeta, err := dstMeta.ReadForkMetadata(dstPath)
			if err != nil {
				t.Fatalf("ReadForkMetadata: %v", err)
			}
			if gotMeta.FinderInfo != finderInfo {
				t.Fatalf("finder info = %v, want %v", gotMeta.FinderInfo, finderInfo)
			}
			if gotMeta.ResourceForkLen != int64(len(forkData)) {
				t.Fatalf("resource fork len = %d, want %d", gotMeta.ResourceForkLen, len(forkData))
			}

			dstCommentBackend, ok := dstMeta.(CommentBackend)
			if !ok {
				t.Fatal("destination metadata backend does not support comments")
			}
			comment, ok := dstCommentBackend.ReadComment(dstPath)
			if !ok {
				t.Fatal("destination comment missing")
			}
			if string(comment) != "copied comment" {
				t.Fatalf("comment = %q, want %q", string(comment), "copied comment")
			}

			dstFork, dstForkInfo, err := dstMeta.OpenResourceFork(dstPath, false)
			if err != nil {
				t.Fatalf("OpenResourceFork destination: %v", err)
			}
			if dstFork == nil {
				t.Fatal("expected destination resource fork handle")
			}
			defer dstFork.Close()

			gotFork := make([]byte, len(forkData))
			if _, err := dstFork.ReadAt(gotFork, dstForkInfo.Offset); err != nil {
				t.Fatalf("read destination resource fork: %v", err)
			}
			if !bytes.Equal(gotFork, forkData) {
				t.Fatalf("resource fork = %q, want %q", string(gotFork), string(forkData))
			}
		})
	}
}

func TestHandleCopyFile_DstPathTypeZeroIgnoresDstDirMarkerPayload(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()

	s := NewService("TestServer",
		[]VolumeConfig{
			{Name: "Source", Path: srcRoot, AppleDoubleMode: AppleDoubleModeModern},
			{Name: "Target", Path: dstRoot, AppleDoubleMode: AppleDoubleModeLegacy},
		},
		&LocalFileSystem{}, nil,
		Options{DecomposedFilenames: true},
	)

	const srcVolID = uint16(1)
	const dstVolID = uint16(2)

	srcDir := filepath.Join(srcRoot, "Mouse Basics")
	dstDir := filepath.Join(dstRoot, "Mouse Basics")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}

	srcDirID := s.getPathDID(srcVolID, srcDir)
	dstDirID := s.getPathDID(dstVolID, dstDir)

	if err := os.WriteFile(filepath.Join(srcDir, "MouseSkills.color"), []byte("data"), 0644); err != nil {
		t.Fatalf("seed source file: %v", err)
	}

	_, errCode := s.handleCopyFile(&FPCopyFileReq{
		SrcVolumeID: srcVolID,
		SrcDirID:    srcDirID,
		DstVolumeID: dstVolID,
		DstDirID:    dstDirID,
		SrcPathType: 2,
		SrcName:     "MouseSkills.color",
		DstPathType: 0,
		DstDirName:  "\x11M",
	})
	if errCode != NoErr {
		t.Fatalf("handleCopyFile err = %d, want %d", errCode, NoErr)
	}

	if _, err := os.Stat(filepath.Join(dstDir, "MouseSkills.color")); err != nil {
		t.Fatalf("copied file missing in destination dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstDir, "0x11M", "MouseSkills.color")); !os.IsNotExist(err) {
		t.Fatalf("copy unexpectedly used marker payload as destination subpath, stat err=%v", err)
	}
}

func TestHandleCopyFile_PreservesInfinityWhenNewNameEmpty(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()

	s := NewService("TestServer",
		[]VolumeConfig{{Name: "Source", Path: srcRoot}, {Name: "Target", Path: dstRoot}},
		&LocalFileSystem{}, nil,
	)

	const srcVolID = uint16(1)
	const dstVolID = uint16(2)

	name := "Marathon ∞ 1.5"
	srcPath := filepath.Join(srcRoot, name)
	if err := os.WriteFile(srcPath, []byte("data"), 0644); err != nil {
		t.Fatalf("seed source file: %v", err)
	}

	_, errCode := s.handleCopyFile(&FPCopyFileReq{
		SrcVolumeID: srcVolID,
		SrcDirID:    CNIDRoot,
		DstVolumeID: dstVolID,
		DstDirID:    CNIDRoot,
		SrcPathType: 2,
		SrcName:     "Marathon \xB0 1.5",
		DstPathType: 2,
	})
	if errCode != NoErr {
		t.Fatalf("handleCopyFile err = %d, want %d", errCode, NoErr)
	}

	if _, err := os.Stat(filepath.Join(dstRoot, name)); err != nil {
		t.Fatalf("copied file missing with infinity name: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstRoot, "Marathon � 1.5")); !os.IsNotExist(err) {
		t.Fatalf("unexpected replacement-character filename present, stat err=%v", err)
	}
}

func TestHandleCopyFile_DecodesMacRomanNewName(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()

	s := NewService("TestServer",
		[]VolumeConfig{{Name: "Source", Path: srcRoot}, {Name: "Target", Path: dstRoot}},
		&LocalFileSystem{}, nil,
	)

	const srcVolID = uint16(1)
	const dstVolID = uint16(2)

	if err := os.WriteFile(filepath.Join(srcRoot, "Seed"), []byte("data"), 0644); err != nil {
		t.Fatalf("seed source file: %v", err)
	}

	_, errCode := s.handleCopyFile(&FPCopyFileReq{
		SrcVolumeID: srcVolID,
		SrcDirID:    CNIDRoot,
		DstVolumeID: dstVolID,
		DstDirID:    CNIDRoot,
		SrcPathType: 2,
		SrcName:     "Seed",
		DstPathType: 2,
		NewPathType: 2,
		NewName:     string([]byte{'M', 'a', 'r', 'a', 't', 'h', 'o', 'n', ' ', 0xB0, ' ', '1', '.', '5'}),
	})
	if errCode != NoErr {
		t.Fatalf("handleCopyFile err = %d, want %d", errCode, NoErr)
	}

	if _, err := os.Stat(filepath.Join(dstRoot, "Marathon ∞ 1.5")); err != nil {
		t.Fatalf("copied file missing with decoded MacRoman name: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstRoot, "Marathon � 1.5")); !os.IsNotExist(err) {
		t.Fatalf("unexpected replacement-character filename present, stat err=%v", err)
	}
}
