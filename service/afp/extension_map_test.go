package afp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtensionMap_LookupAndDefault(t *testing.T) {
	parsed, err := NewExtensionMap(map[string]ExtensionMapping{
		".":    mustExtensionMapping(t, "????", "????"),
		".txt": mustExtensionMapping(t, "TEXT", "ttxt"),
		".bin": mustExtensionMapping(t, "SIT!", "SITx"),
	})
	if err != nil {
		t.Fatalf("NewExtensionMap error = %v", err)
	}

	txtMapping, ok := parsed.Lookup("ReadMe.TXT")
	if !ok {
		t.Fatal("Lookup(.txt) = not found, want mapping")
	}
	if string(txtMapping.FileType[:]) != "TEXT" || string(txtMapping.Creator[:]) != "ttxt" {
		t.Fatalf("Lookup(.txt) = (%q,%q), want (%q,%q)", string(txtMapping.FileType[:]), string(txtMapping.Creator[:]), "TEXT", "ttxt")
	}

	defaultMapping, ok := parsed.Lookup("Makefile")
	if !ok {
		t.Fatal("Lookup(default) = not found, want mapping")
	}
	if string(defaultMapping.FileType[:]) != "????" || string(defaultMapping.Creator[:]) != "????" {
		t.Fatalf("Lookup(default) = (%q,%q), want (%q,%q)", string(defaultMapping.FileType[:]), string(defaultMapping.Creator[:]), "????", "????")
	}
}

func TestExtensionMap_RequiresDefaultMapping(t *testing.T) {
	_, err := NewExtensionMap(map[string]ExtensionMapping{
		".txt": mustExtensionMapping(t, "TEXT", "ttxt"),
	})
	if err == nil {
		t.Fatal("NewExtensionMap without '.' mapping = nil error, want error")
	}
}

func TestHandleGetFileParms_UsesExtensionMapWithoutPersisting(t *testing.T) {
	tests := []struct {
		name               string
		fileName           string
		extMap             map[string]ExtensionMapping
		seedFinderInfo     *[32]byte
		wantType           string
		wantCreator        string
		checkNoPersistence bool
	}{
		{
			name:     "uses extension map without persisting metadata",
			fileName: "ReadMe.txt",
			extMap: map[string]ExtensionMapping{
				".":    mustExtensionMapping(t, "????", "????"),
				".txt": mustExtensionMapping(t, "TEXT", "ttxt"),
			},
			wantType:           "TEXT",
			wantCreator:        "ttxt",
			checkNoPersistence: true,
		},
		{
			name:     "uses default extension mapping",
			fileName: "Program",
			extMap: map[string]ExtensionMapping{
				".":    mustExtensionMapping(t, "BINA", "UNIX"),
				".txt": mustExtensionMapping(t, "TEXT", "ttxt"),
			},
			wantType:    "BINA",
			wantCreator: "UNIX",
		},
		{
			name:     "prefers existing finder info",
			fileName: "ReadMe.txt",
			extMap: map[string]ExtensionMapping{
				".":    mustExtensionMapping(t, "????", "????"),
				".txt": mustExtensionMapping(t, "TEXT", "ttxt"),
			},
			seedFinderInfo: func() *[32]byte {
				var fi [32]byte
				copy(fi[0:4], "APPL")
				copy(fi[4:8], "MSWD")
				return &fi
			}(),
			wantType:    "APPL",
			wantCreator: "MSWD",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			filePath := filepath.Join(root, tc.fileName)
			if err := os.WriteFile(filePath, []byte("hello"), 0644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}

			extMap, err := NewExtensionMap(tc.extMap)
			if err != nil {
				t.Fatalf("NewExtensionMap: %v", err)
			}

			options := DefaultOptions()
			options.ExtensionMap = extMap
			s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil, options)

			if tc.seedFinderInfo != nil {
				if err := s.metaFor(1).WriteFinderInfo(filePath, *tc.seedFinderInfo); err != nil {
					t.Fatalf("WriteFinderInfo: %v", err)
				}
			}

			res, errCode := s.handleGetFileParms(&FPGetFileParmsReq{
				VolumeID: 1,
				DirID:    CNIDRoot,
				Bitmap:   FileBitmapFinderInfo,
				PathType: 2,
				Path:     tc.fileName,
			})
			if errCode != NoErr {
				t.Fatalf("handleGetFileParms err = %d, want %d", errCode, NoErr)
			}
			if got := string(res.Data[0:4]); got != tc.wantType {
				t.Fatalf("FinderInfo type = %q, want %q", got, tc.wantType)
			}
			if got := string(res.Data[4:8]); got != tc.wantCreator {
				t.Fatalf("FinderInfo creator = %q, want %q", got, tc.wantCreator)
			}

			if tc.checkNoPersistence {
				if len(s.desktopDBs) != 0 {
					t.Fatalf("desktopDBs len = %d, want 0", len(s.desktopDBs))
				}
				if _, err := os.Stat(filepath.Join(root, "._ReadMe.txt")); !os.IsNotExist(err) {
					t.Fatalf("AppleDouble sidecar unexpectedly created: err=%v", err)
				}
				if _, err := os.Stat(filepath.Join(root, ".AppleDouble", "ReadMe.txt")); !os.IsNotExist(err) {
					t.Fatalf("legacy AppleDouble sidecar unexpectedly created: err=%v", err)
				}
			}
		})
	}
}

func mustExtensionMapping(t *testing.T, fileType, creator string) ExtensionMapping {
	t.Helper()
	m, err := NewExtensionMapping(fileType, creator)
	if err != nil {
		t.Fatalf("NewExtensionMapping(%q,%q): %v", fileType, creator, err)
	}
	return m
}
