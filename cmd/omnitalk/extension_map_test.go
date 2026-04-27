//go:build afp

package main

import "testing"

func TestParseAFPExtensionMap_LookupAndDefault(t *testing.T) {
	parsed, err := parseAFPExtensionMap([]byte(`
.      "????"  "????"      Unix Binary
.txt   "TEXT"  "ttxt"      ASCII Text
.bin   "SIT!"  "SITx"      MacBinary
`))
	if err != nil {
		t.Fatalf("parseAFPExtensionMap error = %v", err)
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

func TestParseAFPExtensionMap_RequiresDefaultMapping(t *testing.T) {
	_, err := parseAFPExtensionMap([]byte(`.txt "TEXT" "ttxt"`))
	if err == nil {
		t.Fatal("parseAFPExtensionMap without '.' mapping = nil error, want error")
	}
}
