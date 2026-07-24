package afp

import (
	"strings"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// commands.go holds the client-direction AFP command DTOs: each request type marshals
// the command block a client sends (command byte + arguments), and each reply type
// parses the body the server returns. Wire layouts mirror the server handlers in
// core/service/afp exactly (cited per command); round-trip tests assert the server's
// own parser accepts what these marshal.
//
// A "command block" is the bytes carried in the ASP Command/Write payload: block[0] is
// the AFP command byte and the arguments follow. The 4-byte AFP result code lives in
// the ASP/ATP reply UserData, not in these bodies — the ASP layer surfaces it.

// even pads a builder to an even length by appending a zero byte when the current
// length is odd (AFP word-aligns a parameter block after a Pascal pathname).
func even(b []byte) []byte {
	if len(b)%2 != 0 {
		return append(b, 0)
	}
	return b
}

// --- FPLogin (cmd 18) — core/service/afp/handlers.go:afpLogin ---
// Request: cmd(1) pstring AFPVersion, pstring UAM, [UAM data].
// Cleartext adds: pstring username, 8-byte space-padded password.

// LoginRequest builds an FPLogin block for a single-step UAM. For UAMNoUserAuthent the
// User/Pass fields are ignored; for UAMCleartext they are the credentials.
type LoginRequest struct {
	AFPVersion string
	UAM        string
	User       string
	Pass       string
}

// Marshal encodes the FPLogin command block. The cleartext credential trailer (a
// username Pascal string + an 8-byte password field) is appended for every UAM EXCEPT
// the guest "No User Authent" — keyed on the UAM being non-guest rather than an exact
// match against UAMCleartext, because a server advertises the cleartext UAM under its
// own spelling/case ("Cleartxt passwrd" vs "Cleartxt Passwrd") and the client sends
// that exact string back (a classic Mac ignores an FPLogin naming a UAM it did not
// advertise). Matching the capital-P constant here dropped the credentials whenever the
// server used the lower-case spelling, so the login carried no username/password and the
// server silently discarded it (observed against System 7.5 — see spec/errata.md).
func (r LoginRequest) Marshal() []byte {
	out := []byte{CmdLogin}
	out = PutPString(out, []byte(r.AFPVersion))
	out = PutPString(out, []byte(r.UAM))
	if !strings.EqualFold(r.UAM, UAMNoUserAuthent) {
		out = PutPString(out, []byte(r.User))
		// 8-byte cleartext password field, space-padded then NUL-filled to 8.
		var pw [8]byte
		copy(pw[:], r.Pass)
		out = append(out, pw[:]...)
	}
	return out
}

// --- FPLogout (cmd 20) ---
// Request: cmd(1) pad(1). Reply: empty.

// LogoutRequest builds an FPLogout block.
type LogoutRequest struct{}

// Marshal encodes the FPLogout command block.
func (LogoutRequest) Marshal() []byte { return []byte{CmdLogout, 0} }

// --- FPGetSrvrParms (cmd 16) — handlers.go:afpGetSrvrParms ---
// Request: cmd(1) pad(1).
// Reply: uint32 ServerTime, uint8 volCount, {uint8 flags, pstring name} × count.

// GetSrvrParmsRequest builds an FPGetSrvrParms block.
type GetSrvrParmsRequest struct{}

// Marshal encodes the FPGetSrvrParms command block.
func (GetSrvrParmsRequest) Marshal() []byte { return []byte{CmdGetSrvrParms, 0} }

// VolumeListEntry is one volume the server advertises.
type VolumeListEntry struct {
	Flags uint8
	Name  string // MacRoman bytes, decoded as string
}

// GetSrvrParmsReply is the parsed FPGetSrvrParms reply.
type GetSrvrParmsReply struct {
	ServerTime uint32
	Volumes    []VolumeListEntry
}

// ParseGetSrvrParmsReply decodes the server-parameters reply body.
func ParseGetSrvrParmsReply(b []byte) (GetSrvrParmsReply, bool) {
	if len(b) < 5 {
		return GetSrvrParmsReply{}, false
	}
	r := GetSrvrParmsReply{ServerTime: bp.BE32(b[0:4])}
	count := int(b[4])
	off := 5
	for i := 0; i < count; i++ {
		if off >= len(b) {
			return r, false
		}
		flags := b[off]
		off++
		name, next, ok := PString(b, off)
		if !ok {
			return r, false
		}
		off = next
		r.Volumes = append(r.Volumes, VolumeListEntry{Flags: flags, Name: string(name)})
	}
	return r, true
}

// --- FPOpenVol (cmd 24) — handlers.go:afpOpenVol ---
// Request: cmd(1) pad(1) bitmap(2) pstring VolName.
// Reply: bitmap(2) <packed volume params>.

// OpenVolRequest builds an FPOpenVol block. VolName is the MacRoman volume name.
type OpenVolRequest struct {
	Bitmap  uint16
	VolName string
}

// Marshal encodes the FPOpenVol command block.
func (r OpenVolRequest) Marshal() []byte {
	out := []byte{CmdOpenVol, 0}
	out = bp.AppendBE16(out, r.Bitmap)
	out = PutPString(out, []byte(r.VolName))
	return out
}

// --- FPCloseVol (cmd 2) ---
// Request: cmd(1) pad(1) volID(2). Reply: empty.

// CloseVolRequest builds an FPCloseVol block.
type CloseVolRequest struct{ VolID uint16 }

// Marshal encodes the FPCloseVol command block.
func (r CloseVolRequest) Marshal() []byte {
	return append([]byte{CmdCloseVol, 0}, be16(r.VolID)...)
}

// --- FPGetVolParms (cmd 17) — handlers.go:afpGetVolParms ---
// Request: cmd(1) pad(1) volID(2) bitmap(2).
// Reply: bitmap(2) <packed volume params>.

// GetVolParmsRequest builds an FPGetVolParms block.
type GetVolParmsRequest struct {
	VolID  uint16
	Bitmap uint16
}

// Marshal encodes the FPGetVolParms command block.
func (r GetVolParmsRequest) Marshal() []byte {
	out := []byte{CmdGetVolParms, 0}
	out = bp.AppendBE16(out, r.VolID)
	out = bp.AppendBE16(out, r.Bitmap)
	return out
}

// VolParams is the parsed volume-parameter block returned by FPOpenVol/FPGetVolParms.
// Only the fields the reply bitmap set are populated.
type VolParams struct {
	Bitmap     uint16
	Attributes uint16
	Signature  uint16
	VolID      uint16
	BytesFree  uint32
	BytesTotal uint32
	Name       string
}

// ParseVolParams decodes a volume-parameter reply body: bitmap(2) followed by the
// fixed fields in ascending bit order, with Name a trailing Pascal string addressed by
// a 2-byte offset (relative to the start of the params block, i.e. after the bitmap).
func ParseVolParams(b []byte) (VolParams, bool) {
	if len(b) < 2 {
		return VolParams{}, false
	}
	v := VolParams{Bitmap: bp.BE16(b[0:2])}
	params := b[2:]
	off := 0
	read16 := func() uint16 {
		if off+2 > len(params) {
			off = len(params) + 1
			return 0
		}
		x := bp.BE16(params[off : off+2])
		off += 2
		return x
	}
	read32 := func() uint32 {
		if off+4 > len(params) {
			off = len(params) + 1
			return 0
		}
		x := bp.BE32(params[off : off+4])
		off += 4
		return x
	}
	bm := v.Bitmap
	if bm&VolBitmapAttributes != 0 {
		v.Attributes = read16()
	}
	if bm&VolBitmapSignature != 0 {
		v.Signature = read16()
	}
	if bm&VolBitmapCreateDate != 0 {
		read32()
	}
	if bm&VolBitmapModDate != 0 {
		read32()
	}
	if bm&VolBitmapBackupDate != 0 {
		read32()
	}
	if bm&VolBitmapID != 0 {
		v.VolID = read16()
	}
	if bm&VolBitmapBytesFree != 0 {
		v.BytesFree = read32()
	}
	if bm&VolBitmapBytesTotal != 0 {
		v.BytesTotal = read32()
	}
	if bm&VolBitmapName != 0 {
		ptr := read16()
		if int(ptr) < len(params) {
			if s, _, ok := PString(params, int(ptr)); ok {
				v.Name = string(s)
			}
		}
	}
	return v, true
}

// --- FPGetFileDirParms (cmd 34) — handlers.go:afpGetFileDirParms ---
// Request: cmd(1) pad(1) volID(2) dirID(4) fileBitmap(2) dirBitmap(2) pathType(1)
//          pathname(pascal).
// Reply: fileBitmap(2) dirBitmap(2) isDir(1) pad(1) <packed params>.

// GetFileDirParmsRequest builds an FPGetFileDirParms block. Path is the wire-encoded
// pathname (already in the request's PathType charset); an empty Path names the dirID
// root.
type GetFileDirParmsRequest struct {
	VolID      uint16
	DirID      uint32
	FileBitmap uint16
	DirBitmap  uint16
	PathType   uint8
	Path       []byte
}

// Marshal encodes the FPGetFileDirParms command block.
func (r GetFileDirParmsRequest) Marshal() []byte {
	out := []byte{CmdGetFileDirParms, 0}
	out = bp.AppendBE16(out, r.VolID)
	out = bp.AppendBE32(out, r.DirID)
	out = bp.AppendBE16(out, r.FileBitmap)
	out = bp.AppendBE16(out, r.DirBitmap)
	out = append(out, r.PathType)
	out = PutPString(out, r.Path)
	return out
}

// GetFileDirParmsReply is the parsed reply: the echoed bitmaps, the isDir flag, and the
// parsed parameter block governed by the applicable bitmap.
type GetFileDirParmsReply struct {
	FileBitmap uint16
	DirBitmap  uint16
	IsDir      bool
	Params     FileDirParams
}

// ParseGetFileDirParmsReply decodes an FPGetFileDirParms reply body.
func ParseGetFileDirParmsReply(b []byte) (GetFileDirParmsReply, bool) {
	if len(b) < 6 {
		return GetFileDirParmsReply{}, false
	}
	r := GetFileDirParmsReply{
		FileBitmap: bp.BE16(b[0:2]),
		DirBitmap:  bp.BE16(b[2:4]),
		IsDir:      b[4]&0x80 != 0,
	}
	bitmap := r.FileBitmap
	if r.IsDir {
		bitmap = r.DirBitmap
	}
	r.Params = ParseFileDirParams(b[6:], bitmap, r.IsDir)
	return r, true
}

// --- FPEnumerate (cmd 9) — handlers.go:afpEnumerate ---
// Request: cmd(1) pad(1) volID(2) dirID(4) fileBitmap(2) dirBitmap(2) reqCount(2)
//          startIndex(2) maxReplySize(2) pathType(1) pathname(pascal).
// Reply: fileBitmap(2) dirBitmap(2) actCount(2) {entryLen(1) isDir(1) <params>}×count,
//        each entry padded to even length.

// EnumerateRequest builds an FPEnumerate block.
type EnumerateRequest struct {
	VolID        uint16
	DirID        uint32
	FileBitmap   uint16
	DirBitmap    uint16
	ReqCount     uint16
	StartIndex   uint16 // 1-based
	MaxReplySize uint16
	PathType     uint8
	Path         []byte
}

// Marshal encodes the FPEnumerate command block.
func (r EnumerateRequest) Marshal() []byte {
	out := []byte{CmdEnumerate, 0}
	out = bp.AppendBE16(out, r.VolID)
	out = bp.AppendBE32(out, r.DirID)
	out = bp.AppendBE16(out, r.FileBitmap)
	out = bp.AppendBE16(out, r.DirBitmap)
	out = bp.AppendBE16(out, r.ReqCount)
	out = bp.AppendBE16(out, r.StartIndex)
	out = bp.AppendBE16(out, r.MaxReplySize)
	out = append(out, r.PathType)
	out = PutPString(out, r.Path)
	return out
}

// EnumerateReply is the parsed FPEnumerate reply: the echoed bitmaps and one
// FileDirParams per child.
type EnumerateReply struct {
	FileBitmap uint16
	DirBitmap  uint16
	Entries    []FileDirParams
}

// ParseEnumerateReply decodes an FPEnumerate reply body. Each entry is framed
// [entryLen(1)][isDir/type(1)][params...] padded to an even total length; entryLen
// counts the whole framed entry including the two leading bytes.
func ParseEnumerateReply(b []byte) (EnumerateReply, bool) {
	if len(b) < 6 {
		return EnumerateReply{}, false
	}
	r := EnumerateReply{
		FileBitmap: bp.BE16(b[0:2]),
		DirBitmap:  bp.BE16(b[2:4]),
	}
	count := int(bp.BE16(b[4:6]))
	off := 6
	for i := 0; i < count; i++ {
		if off+2 > len(b) {
			return r, false
		}
		entryLen := int(b[off])
		typeByte := b[off+1]
		isDir := typeByte&0x80 != 0
		if entryLen < 2 || off+entryLen > len(b) {
			return r, false
		}
		params := b[off+2 : off+entryLen]
		bitmap := r.FileBitmap
		if isDir {
			bitmap = r.DirBitmap
		}
		r.Entries = append(r.Entries, ParseFileDirParams(params, bitmap, isDir))
		off += entryLen
	}
	return r, true
}

// be16 is a small helper for a two-byte big-endian value.
func be16(v uint16) []byte { return []byte{byte(v >> 8), byte(v)} }
