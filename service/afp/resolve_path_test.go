package afp

import (
	"path/filepath"
	"testing"
)

func TestAFPService_resolvePath(t *testing.T) {
	s := NewAFPService("TestServer", []VolumeConfig{
		{Name: "Vol1", Path: "/volumes/share"},
	}, nil, nil)

	tests := []struct {
		name       string
		parentPath string
		afpPath    string
		pathType   uint8
		wantPath   string
		wantCode   int32
	}{
		{
			name:       "short names unsupported",
			parentPath: "/volumes/share",
			afpPath:    "DOCUME~1",
			pathType:   1, // short name
			wantPath:   "",
			wantCode:   ErrObjectNotFound,
		},
		{
			name:       "simple valid path",
			parentPath: "/volumes/share",
			afpPath:    "docs\x00file.txt",
			pathType:   2,
			wantPath:   filepath.Clean("/volumes/share/docs/file.txt"),
			wantCode:   NoErr,
		},
		{
			name:       "ascend one level",
			parentPath: "/volumes/share/docs",
			afpPath:    "\x00\x00music",
			pathType:   2,
			wantPath:   filepath.Clean("/volumes/share/music"),
			wantCode:   NoErr,
		},
		{
			name:       "ascend two levels",
			parentPath: "/volumes/share/docs/2024",
			afpPath:    "\x00\x00\x00music\x00rock",
			pathType:   2,
			wantPath:   filepath.Clean("/volumes/share/music/rock"),
			wantCode:   NoErr,
		},
		{
			name:       "ascend past volume root should fail",
			parentPath: "/volumes/share/docs",
			afpPath:    "\x00\x00\x00\x00music",
			pathType:   2,
			wantPath:   "",
			wantCode:   ErrAccessDenied,
		},
		{
			name:       "ignore single leading null",
			parentPath: "/volumes/share/docs",
			afpPath:    "\x00file.txt",
			pathType:   2,
			wantPath:   filepath.Clean("/volumes/share/docs/file.txt"),
			wantCode:   NoErr,
		},
		{
			name:       "trailing null ignored",
			parentPath: "/volumes/share",
			afpPath:    "docs\x00",
			pathType:   2,
			wantPath:   filepath.Clean("/volumes/share/docs"),
			wantCode:   NoErr,
		},
		{
			name:       "invalid char slash",
			parentPath: "/volumes/share",
			afpPath:    "docs/file.txt",
			pathType:   2,
			wantPath:   filepath.Clean("/volumes/share/docs0x2Ffile.txt"),
			wantCode:   NoErr,
		},
		{
			name:       "macroman bytes decoded to utf8",
			parentPath: "/volumes/share",
			afpPath:    "tm\xaa.txt",
			pathType:   2,
			wantPath:   filepath.Clean("/volumes/share/tm™.txt"),
			wantCode:   NoErr,
		},
		{
			name:       "descend then ascend",
			parentPath: "/volumes/share",
			afpPath:    "docs\x00\x00music",
			pathType:   2,
			wantPath:   filepath.Clean("/volumes/share/music"),
			wantCode:   NoErr,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotPath, gotCode := s.resolvePath(tc.parentPath, tc.afpPath, tc.pathType)
			if gotCode != tc.wantCode {
				t.Errorf("resolvePath(%q, %q, %d) code = %d, want %d", tc.parentPath, tc.afpPath, tc.pathType, gotCode, tc.wantCode)
			}
			if gotPath != tc.wantPath {
				t.Errorf("resolvePath(%q, %q, %d) path = %q, want %q", tc.parentPath, tc.afpPath, tc.pathType, gotPath, tc.wantPath)
			}
		})
	}
}

func TestAFPService_resolvePath_ReservedCharsDisabled(t *testing.T) {
	s := NewAFPService("TestServer", []VolumeConfig{
		{Name: "Vol1", Path: "/volumes/share"},
	}, nil, nil, AFPOptions{DecomposedFilenames: false})

	gotPath, gotCode := s.resolvePath("/volumes/share", "docs/file.txt", 2)
	if gotCode != ErrAccessDenied {
		t.Fatalf("resolvePath code = %d, want %d", gotCode, ErrAccessDenied)
	}
	if gotPath != "" {
		t.Fatalf("resolvePath path = %q, want empty", gotPath)
	}

	gotPath, gotCode = s.resolvePath("/volumes/share", "tm\xaa.txt", 2)
	if gotCode != NoErr {
		t.Fatalf("resolvePath macroman decode code = %d, want %d", gotCode, NoErr)
	}
	if gotPath != filepath.Clean("/volumes/share/tm™.txt") {
		t.Fatalf("resolvePath macroman decode path = %q, want %q", gotPath, filepath.Clean("/volumes/share/tm™.txt"))
	}
}
