package smb

import (
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// SMB threads its filename wire charset per request from the SMB_FLAGS2_UNICODE
// bit (MS-CIFS 2.2.3.1) on each header, exactly as the AFP service threads the
// path-type byte (§2a). The charset is keyed off this per-request flag, NOT off
// the negotiated dialect: SMB 1.0 (NT LM 0.12) clients also set
// SMB_FLAGS2_UNICODE to send UTF-16LE names, so the same dialect carries both
// ANSI and UTF-16 requests over a single session. A request with the flag set
// sends UTF-16LE names; one without it sends OEM-code-page (ANSI) names. The
// service passes the resulting WireEncoding into the share's FilenameCodec on
// every Decode/Encode, so one share serves DOS/WfW (ANSI) and Unicode-capable
// (UTF-16) clients at once without a fixed server-side charset.

// wireFor maps the per-request FLAGS2 Unicode bit (protocol.Flags2Unicode) to the
// FilenameCodec wire charset. flags2 is the 16-bit FLAGS2 field from the SMB
// header; when its UNICODE bit is set the client speaks UTF-16LE for that
// request, otherwise the negotiated OEM page (ANSI) — independent of dialect. The
// codec advertises which it implements via Wire(); an unsupported request fails
// with fs.ErrWireUnsupported rather than mangling the name.
func wireFor(flags2 uint16) fs.WireEncoding {
	if flags2&protocol.Flags2Unicode != 0 {
		return fs.WireUTF16
	}
	return fs.WireANSI
}
