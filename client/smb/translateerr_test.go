package smb

import (
	"errors"
	stdfs "io/fs"
	"testing"

	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// TestTranslateErrDOSAndNTStatus checks translateErr maps BOTH a Win9x DOS-format status
// (ErrorClass in the low byte, ErrorCode in the high 16 bits) AND a real NTSTATUS to the
// fs sentinels. A Win98 server negotiates NT LM 0.12 without CAP_STATUS32 and returns e.g.
// 0x00020001 (ERRDOS class 1, ERRbadfile code 2) for a missing file; the mount's
// GetSecurityByName needs that to become os.ErrNotExist so WinFsp proceeds to Create.
func TestTranslateErrDOSAndNTStatus(t *testing.T) {
	cases := []struct {
		name   string
		status uint32
		want   error
	}{
		{"DOS not-found (ERRbadfile)", 0x00020001, stdfs.ErrNotExist},
		{"DOS bad-path (ERRbadpath)", 0x00030001, stdfs.ErrNotExist},
		{"DOS no-more-files", 0x00120001, stdfs.ErrNotExist},
		{"DOS access-denied (ERRnoaccess)", 0x00050001, stdfs.ErrPermission},
		{"DOS file-exists (ERRfilexists)", 0x00500001, stdfs.ErrExist},
		{"DOS invalid-param (ERRDOS 87) — passes through", 0x00570001, nil},
		{"NT object-name-not-found", 0xC0000034, stdfs.ErrNotExist},
		{"NT name-collision", 0xC0000035, stdfs.ErrExist},
		{"NT access-denied", 0xC0000022, stdfs.ErrPermission},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := translateErr(&proto.ErrStatus{Command: 0x2e, Status: c.status})
			if c.want == nil {
				// Should pass through as the raw ErrStatus (not a sentinel).
				var st *proto.ErrStatus
				if !errors.As(err, &st) {
					t.Fatalf("status %#08x: want raw ErrStatus passthrough, got %v", c.status, err)
				}
				return
			}
			if !errors.Is(err, c.want) {
				t.Errorf("status %#08x: got %v, want errors.Is %v", c.status, err, c.want)
			}
		})
	}
}
