//go:build afp || all

package afp

import "testing"

func TestParseVolumeFlag(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantPath string
		wantErr  bool
	}{
		{`Mac Share:c:\mac`, "Mac Share", `c:\mac`, false},
		{"Mac Stuff:/media/mac/classic", "Mac Stuff", "/media/mac/classic", false},
		{"Simple:/tmp/vol", "Simple", "/tmp/vol", false},
		// Windows-style absolute path with drive letter
		{`Docs:D:\Users\mac\docs`, "Docs", `D:\Users\mac\docs`, false},
		// Error cases
		{":noname", "", "", true},
		{"nopath:", "", "", true},
		{"nocolon", "", "", true},
	}

	for _, tc := range tests {
		cfg, err := ParseVolumeFlag(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseVolumeFlag(%q): expected error, got nil", tc.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseVolumeFlag(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if cfg.Name != tc.wantName {
			t.Errorf("ParseVolumeFlag(%q): Name = %q, want %q", tc.input, cfg.Name, tc.wantName)
		}
		if cfg.Path != tc.wantPath {
			t.Errorf("ParseVolumeFlag(%q): Path = %q, want %q", tc.input, cfg.Path, tc.wantPath)
		}
	}
}
