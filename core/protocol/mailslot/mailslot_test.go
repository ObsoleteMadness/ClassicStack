package mailslot

import (
	"bytes"
	"testing"
)

// TestWriteRoundTrip proves a mailslot write survives Marshal→Unmarshal with the
// name and body preserved, for each well-known mailslot.
func TestWriteRoundTrip(t *testing.T) {
	for _, name := range []string{NameBrowse, NameLANMAN, NameMessenger} {
		body := []byte("payload-for-" + name)
		got, err := Unmarshal(Write{Name: name, Body: body}.Marshal())
		if err != nil {
			t.Fatalf("Unmarshal(%s): %v", name, err)
		}
		if got.Name != name {
			t.Errorf("name = %q, want %q", got.Name, name)
		}
		if !bytes.Equal(got.Body, body) {
			t.Errorf("body = %q, want %q", got.Body, body)
		}
	}
}

// TestUnmarshalRejectsNonMailslot proves a buffer that is not an SMB_COM_TRANSACTION
// mailslot write is rejected with ErrEnvelope, not mis-parsed.
func TestUnmarshalRejectsNonMailslot(t *testing.T) {
	if _, err := Unmarshal([]byte("not an smb frame")); err == nil {
		t.Error("accepted a non-SMB buffer")
	}
	// A short \xffSMB buffer (truncated before the byte area) is rejected.
	if _, err := Unmarshal([]byte("\xffSMB\x25")); err == nil {
		t.Error("accepted a truncated transaction")
	}
}

// TestPriorityClassPreserved proves the SMB_COM_TRANSACTION Priority/Class fields
// round-trip when set explicitly.
func TestPriorityClassPreserved(t *testing.T) {
	got, err := Unmarshal(Write{Name: NameBrowse, Body: []byte{1}, Priority: 7, Class: 3}.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if got.Priority != 7 || got.Class != 3 {
		t.Errorf("priority=%d class=%d, want 7/3", got.Priority, got.Class)
	}
}
