package fs

import (
	"reflect"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

const testSentinel = "********"

// registerSecretFS registers an fs_type with one secret param ("password") and one
// plain param ("username"), for the masking helpers to consult via ParamsFor.
func registerSecretFS(t *testing.T, name string) {
	t.Helper()
	RegisterFSWithParams(name, func(_ ShareSpec, _ bus.Bus, _ metastore.Store) (FileSystem, error) {
		return newMemFS(ShareSpec{}), nil
	},
		Param{Key: "username", Required: false},
		Param{Key: "password", Required: false, Secret: true},
	)
}

// TestMaskSecretOptions redacts only the secret-keyed option, leaves plain and empty
// ones alone, and is case-insensitive on the key.
func TestMaskSecretOptions(t *testing.T) {
	registerSecretFS(t, "test-mask-fs")

	got := MaskSecretOptions("test-mask-fs", []string{
		"username=alice",
		"PassWord=hunter2", // mixed case key, still a secret
		"password=",        // empty secret stays empty (unset vs hidden)
		"flag",             // bare key, no '=' — verbatim
	}, testSentinel)

	want := []string{
		"username=alice",
		"PassWord=" + testSentinel,
		"password=",
		"flag",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MaskSecretOptions = %q, want %q", got, want)
	}
}

// TestMaskSecretOptions_NoSecrets returns the list copied-but-unchanged when the
// fs_type declares no secret params.
func TestMaskSecretOptions_NoSecrets(t *testing.T) {
	RegisterFSWithParams("test-nosecret-fs", func(_ ShareSpec, _ bus.Bus, _ metastore.Store) (FileSystem, error) {
		return newMemFS(ShareSpec{}), nil
	}, Param{Key: "url", Required: true})

	in := []string{"url=ftp://h/p", "password=should-not-mask"}
	got := MaskSecretOptions("test-nosecret-fs", in, testSentinel)
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("MaskSecretOptions with no secret params = %q, want unchanged %q", got, in)
	}
}

// TestUnmaskSecretOptions restores a sentinel-valued secret from the prior list, keeps
// a genuinely edited secret, and drops a sentinel with no prior value.
func TestUnmaskSecretOptions(t *testing.T) {
	registerSecretFS(t, "test-unmask-fs")

	prev := []string{"username=alice", "password=hunter2"}

	// Blind round-trip: the UI returns the sentinel for the unchanged password.
	got := UnmaskSecretOptions("test-unmask-fs",
		[]string{"username=alice", "password=" + testSentinel}, prev, testSentinel)
	want := []string{"username=alice", "password=hunter2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("blind round-trip unmask = %q, want %q (stored secret restored)", got, want)
	}

	// Genuine edit: a non-sentinel value is kept verbatim.
	got = UnmaskSecretOptions("test-unmask-fs",
		[]string{"password=newpw"}, prev, testSentinel)
	if !reflect.DeepEqual(got, []string{"password=newpw"}) {
		t.Fatalf("edited secret unmask = %q, want password=newpw kept", got)
	}

	// Sentinel with no prior value → the entry is dropped, not persisted.
	got = UnmaskSecretOptions("test-unmask-fs",
		[]string{"username=bob", "password=" + testSentinel}, nil, testSentinel)
	if !reflect.DeepEqual(got, []string{"username=bob"}) {
		t.Fatalf("sentinel with no prior = %q, want the placeholder dropped", got)
	}
}

// TestSecretOptions_RoundTrip is the property that matters end-to-end: masking then
// unmasking against the original, with no edits, recovers the original list exactly.
func TestSecretOptions_RoundTrip(t *testing.T) {
	registerSecretFS(t, "test-roundtrip-fs")

	orig := []string{"username=alice", "password=s3cr3t"}
	masked := MaskSecretOptions("test-roundtrip-fs", orig, testSentinel)
	if masked[1] == orig[1] {
		t.Fatal("masking did not hide the password")
	}
	restored := UnmaskSecretOptions("test-roundtrip-fs", masked, orig, testSentinel)
	if !reflect.DeepEqual(restored, orig) {
		t.Fatalf("round-trip = %q, want original %q", restored, orig)
	}
}
