//go:build windows

package winfsp

import "golang.org/x/sys/windows"

// staticSD is a single self-relative security descriptor granting Everyone full control.
// Legacy AFP/SMB/NCP/EtherDFS shares carry no NT ACLs, so we synthesise one SD and return
// it for every object. Read-only-ness is surfaced through FILE_ATTRIBUTE_READONLY and the
// ReadOnlyVolume mount flag, not by trimming the DACL, so a write fails with the read-only
// attribute (what apps expect) rather than access-denied.
//
// "O:BAG:BAD:(A;;FA;;;WD)" = owner+group Administrators, DACL: Everyone (WD) → FILE_ALL (FA).
var staticSD = func() *windows.SECURITY_DESCRIPTOR {
	sd, err := windows.SecurityDescriptorFromString("O:BAG:BAD:(A;;FA;;;WD)")
	if err != nil {
		// A malformed literal is a programming error; fall back to a nil SD (WinFsp then
		// applies its default), which is still safe.
		return nil
	}
	return sd
}()
