package uri

import (
	"errors"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Target
	}{
		{
			name: "afp name:zone with transport",
			in:   "afp://classicstack:MyZone,ddp/Volume",
			want: Target{Scheme: "afp", Server: "classicstack:MyZone", Transport: "ddp", Volume: "Volume"},
		},
		{
			name: "afp no transport",
			in:   "afp://classicstack/Volume",
			want: Target{Scheme: "afp", Server: "classicstack", Volume: "Volume"},
		},
		{
			name: "smb full creds and path",
			in:   "smb://pete:secret@classicstack,ipx/foo/bar/baz.txt",
			want: Target{Scheme: "smb", User: "pete", Pass: "secret", HasCreds: true, Server: "classicstack", Transport: "ipx", Volume: "foo", Path: "bar/baz.txt"},
		},
		{
			name: "empty user with password",
			in:   "smb://:secret@server/share",
			want: Target{Scheme: "smb", User: "", Pass: "secret", HasCreds: true, Server: "server", Volume: "share"},
		},
		{
			name: "user only no password",
			in:   "smb://guest@server/share",
			want: Target{Scheme: "smb", User: "guest", HasCreds: true, Server: "server", Volume: "share"},
		},
		{
			name: "empty creds at sign only",
			in:   "smb://@server/share",
			want: Target{Scheme: "smb", HasCreds: true, Server: "server", Volume: "share"},
		},
		{
			name: "etherdfs dash-separated MAC",
			in:   "etherdfs://02-1a-4d-11-22-33/C",
			want: Target{Scheme: "etherdfs", Server: "02-1a-4d-11-22-33", Volume: "C"},
		},
		{
			name: "etherdfs bare-hex MAC",
			in:   "etherdfs://021a4d112233/C",
			want: Target{Scheme: "etherdfs", Server: "021a4d112233", Volume: "C"},
		},
		{
			name: "etherdfs MAC with explicit transport",
			in:   "etherdfs://02-1a-4d-11-22-33,ether/C",
			want: Target{Scheme: "etherdfs", Server: "02-1a-4d-11-22-33", Transport: "ether", Volume: "C"},
		},
		{
			name: "ncp SAP name",
			in:   "ncp://SERVER,ipx/SYS",
			want: Target{Scheme: "ncp", Server: "SERVER", Transport: "ipx", Volume: "SYS"},
		},
		{
			name: "afp literal net.node server",
			in:   "afp://65280.128/Volume",
			want: Target{Scheme: "afp", Server: "65280.128", Volume: "Volume"},
		},
		{
			name: "server only no volume",
			in:   "smb://server",
			want: Target{Scheme: "smb", Server: "server"},
		},
		{
			name: "server with trailing slash no volume",
			in:   "smb://server/",
			want: Target{Scheme: "smb", Server: "server"},
		},
		{
			name: "creds with dash MAC server",
			in:   "etherdfs://user:pw@aa-bb-cc-dd-ee-ff/A",
			want: Target{Scheme: "etherdfs", User: "user", Pass: "pw", HasCreds: true, Server: "aa-bb-cc-dd-ee-ff", Volume: "A"},
		},
		{
			name: "scheme is lowercased",
			in:   "SMB://server/share",
			want: Target{Scheme: "smb", Server: "server", Volume: "share"},
		},
		{
			name: "transport is lowercased",
			in:   "smb://server,TCP/share",
			want: Target{Scheme: "smb", Server: "server", Transport: "tcp", Volume: "share"},
		},
		{
			name: "deep path preserved",
			in:   "afp://server/Vol/a/b/c",
			want: Target{Scheme: "afp", Server: "server", Volume: "Vol", Path: "a/b/c"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.in)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("Parse(%q)\n got  %+v\n want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"empty", "", ErrEmpty},
		{"blank", "   ", ErrEmpty},
		{"no scheme", "server/share", ErrNoScheme},
		{"no scheme sep", "afp:server/share", ErrNoScheme},
		{"empty scheme", "://server/share", ErrNoScheme},
		{"empty server", "afp:///Volume", ErrNoServer},
		{"empty server with creds", "afp://user@/Volume", ErrNoServer},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.in)
			if !errors.Is(err, tc.want) {
				t.Errorf("Parse(%q) error = %v, want %v", tc.in, err, tc.want)
			}
		})
	}
}

// TestRoundTrip asserts String() reverses Parse() for well-formed inputs, so the
// canonical form is stable.
func TestRoundTrip(t *testing.T) {
	inputs := []string{
		"afp://classicstack:MyZone,ddp/Volume",
		"smb://pete:secret@classicstack,ipx/foo/bar/baz.txt",
		"etherdfs://02-1a-4d-11-22-33/C",
		"ncp://SERVER,ipx/SYS",
		"smb://guest@server/share",
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			parsed, err := Parse(in)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			round := parsed.String()
			reparsed, err := Parse(round)
			if err != nil {
				t.Fatalf("re-Parse(%q): %v", round, err)
			}
			if reparsed != parsed {
				t.Errorf("round trip diverged:\n first  %+v\n second %+v", parsed, reparsed)
			}
		})
	}
}

func TestRedacted(t *testing.T) {
	tgt, _ := Parse("smb://pete:secret@server/share")
	got := tgt.Redacted()
	want := "smb://pete:***@server/share"
	if got != want {
		t.Errorf("Redacted() = %q, want %q", got, want)
	}
}
