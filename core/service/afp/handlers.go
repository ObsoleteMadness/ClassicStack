package afp

import (
	"errors"
	stdfs "io/fs"
	"strings"
	"time"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	"github.com/ObsoleteMadness/ClassicStack/core/encoding"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// --- Pascal-string helpers; big-endian integer codecs come from
// core/binaryprimitives (core ring: no encoding/binary, §1 / archtest). ---

// putPString appends a Pascal string (1-byte length prefix + bytes, truncated to
// 255). Names longer than 255 bytes cannot be represented on the AFP wire.
func putPString(dst []byte, s []byte) []byte {
	if len(s) > 255 {
		s = s[:255]
	}
	dst = append(dst, byte(len(s)))
	return append(dst, s...)
}

// pString reads a Pascal string from b at off; returns the bytes and the offset
// past it. ok=false if b is too short for the declared length.
func pString(b []byte, off int) (s []byte, next int, ok bool) {
	if off >= len(b) {
		return nil, off, false
	}
	n := int(b[off])
	off++
	if off+n > len(b) {
		return nil, off, false
	}
	return b[off : off+n], off + n, true
}

// --- the Mac epoch: AFP timestamps count seconds since 1 Jan 2000, 00:00 GMT
// (Inside Macintosh: Networking, "AFP date/time"). ---

var afpEpoch = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)

// macTime converts a wall-clock time to the signed 32-bit AFP timestamp.
func macTime(t time.Time) uint32 { return uint32(int32(t.Sub(afpEpoch) / time.Second)) }

// noBackupDate is the AFP "never backed up" sentinel date (0x80000000), used in
// catalog and volume parameter replies when no backup time is tracked.
const noBackupDate uint32 = 0x80000000

// --- FPGetSrvrInfo (server-information block; spec/AFP_Connection_Flow §2). ---

// ServerInfo is the identity this AFP server advertises in FPGetSrvrInfo /
// ASPGetStatus. Defaults are filled by the service when unset.
type ServerInfo struct {
	ServerName  string
	MachineType string
	AFPVersions []string
	UAMs        []string
	Flags       uint16
}

// srvrInfoSupportsSrvrMsg is the FPGetSrvrInfo Flags bit advertising server-
// message support (SupportsSrvrMsg, bit 3 — Inside Macintosh: Networking,
// "GetSrvrInfo reply"; confirmed against an observed AppleShare capture). A
// client only polls FPGetSrvrMsg / honours message attentions when it is set.
const srvrInfoSupportsSrvrMsg uint16 = 0x0008

// serverInfoBlock packs the FPGetSrvrInfo reply block. Layout (Inside Macintosh:
// Networking, "GetSrvrInfo reply"):
//
//	uint16 offset to MachineType
//	uint16 offset to AFP-version count
//	uint16 offset to UAM count
//	uint16 offset to icon/mask (0 — none)
//	uint16 Flags
//	pstring ServerName            (immediately after the header)
//	(pad to even boundary)
//	pstring MachineType
//	uint8 versionCount; pstring × versionCount
//	uint8 uamCount;     pstring × uamCount
//
// All offsets are from the start of the block.
func (s *Service) serverInfoBlock() []byte {
	info := s.serverInfo()

	const headerLen = 10 // 4 offsets + Flags
	base := headerLen + 1 + len(info.ServerName)
	if base%2 != 0 {
		base++ // pad after ServerName to an even boundary
	}
	machineOff := base
	versionsOff := machineOff + 1 + len(info.MachineType)
	versionsLen := 1
	for _, v := range info.AFPVersions {
		versionsLen += 1 + len(v)
	}
	uamsOff := versionsOff + versionsLen
	uamsLen := 1
	for _, u := range info.UAMs {
		uamsLen += 1 + len(u)
	}
	total := uamsOff + uamsLen

	b := make([]byte, total)
	out := b[:0]
	out = bp.AppendBE16(out, uint16(machineOff))
	out = bp.AppendBE16(out, uint16(versionsOff))
	out = bp.AppendBE16(out, uint16(uamsOff))
	out = bp.AppendBE16(out, 0) // icon/mask offset — none
	out = bp.AppendBE16(out, info.Flags)
	out = putPString(out, []byte(info.ServerName))
	// out now ends before the even-boundary pad; jump to machineOff (the pad
	// bytes between are already zero from the make).
	out = b[:machineOff]
	out = putPString(out, []byte(info.MachineType))
	out = append(out, byte(len(info.AFPVersions)))
	for _, v := range info.AFPVersions {
		out = putPString(out, []byte(v))
	}
	out = append(out, byte(len(info.UAMs)))
	for _, u := range info.UAMs {
		out = putPString(out, []byte(u))
	}
	return b
}

// --- FPLogin (spec/AFP_Connection_Flow §4). ---

// afpLogin handles FPLogin for the single-step UAMs the spine supports:
// "No User Authent" (guest) and "Cleartxt Passwrd" (accepted without credential
// checking — this is a compatibility server, not an auth server; the honest
// security posture is documented in the package doc). The argument block is the
// command block with the command byte already removed (see dispatchAFP).
//
// Request: pstring AFPVersion, pstring UAM, [UAM-specific data].
// Reply (single-step success): empty block, result 0.
func (s *Service) afpLogin(a *afpSession, args []byte) ([]byte, int32) {
	ver, off, ok := pString(args, 0)
	if !ok {
		return nil, afpErrParamErr
	}
	uam, uoff, ok := pString(args, off)
	if !ok {
		return nil, afpErrParamErr
	}
	if !s.supportsVersion(string(ver)) {
		return nil, afpErrBadVersNum
	}
	switch string(uam) {
	case "No User Authent":
		// Guest login: no credential, no user store consulted. Admitted as guest;
		// the session then only sees guest-open volumes.
		a.setLogin("", true)
		return nil, afpNoErr
	case "Cleartxt Passwrd":
		// Cleartext UAM: username (pstring) then an 8-byte password field (Inside
		// AppleTalk: Networking, "Cleartext Password UAM"), space-padded/NUL-padded.
		// With no user store wired we admit as guest (the historical behaviour);
		// with one wired we validate, and a non-empty username that fails is denied.
		user, poff, ok := pString(args, uoff)
		if !ok {
			return nil, afpErrParamErr
		}
		username := strings.TrimRight(string(user), " \x00")
		password := ""
		if poff+8 <= len(args) {
			password = strings.TrimRight(string(args[poff:poff+8]), " \x00")
		}

		s.mu.Lock()
		authn := s.auth
		s.mu.Unlock()

		if authn == nil || username == "" {
			// No store, or an anonymous cleartext attempt → guest.
			a.setLogin("", true)
			return nil, afpNoErr
		}
		okCred, err := authn.Authenticate(username, password)
		if err != nil {
			if s.logger != nil && s.logger.Enabled(log.Warn) {
				s.logger.Log2(log.Warn, "FPLogin authenticate error",
					log.Str("user", username), log.Str("err", err.Error()))
			}
			return nil, afpErrUserNotAuth
		}
		if !okCred {
			return nil, afpErrUserNotAuth
		}
		a.setLogin(username, true)
		return nil, afpNoErr
	default:
		return nil, afpErrBadUAM
	}
}

// --- FPGetSrvrParms (spec/AFP_Connection_Flow §5). ---

// afpGetSrvrParms packs the server-parameters reply: the server clock plus the
// volume list (one flags byte + a Pascal name per volume).
//
// Reply: uint32 ServerTime, uint8 volCount, {uint8 flags, pstring name} × count.
func (s *Service) afpGetSrvrParms(a *afpSession) []byte {
	// Snapshot under the lock: the share.Manager can mutate s.volumes at runtime.
	// Only volumes the logged-in identity may access are listed — a guest session
	// never sees a restricted volume (defence-in-depth with the FPOpenVol gate).
	all := s.Volumes()
	vols := make([]*Volume, 0, len(all))
	for _, v := range all {
		if v.allows(a.user) {
			vols = append(vols, v)
		}
	}
	out := make([]byte, 0, 5+16*len(vols))
	out = bp.AppendBE32(out, macTime(time.Now()))
	out = append(out, byte(len(vols)))
	for _, v := range vols {
		out = append(out, 0) // flags: no password, no config info
		out = putPString(out, []byte(v.Name()))
	}
	return out
}

// --- FPOpenVol (spec/AFP_Connection_Flow §6). ---

// Volume-parameter bitmap bits (Inside Macintosh: Networking, "Volume bitmap").
const (
	volBitmapAttributes uint16 = 1 << 0
	volBitmapSignature  uint16 = 1 << 1
	volBitmapCreateDate uint16 = 1 << 2
	volBitmapModDate    uint16 = 1 << 3
	volBitmapBackupDate uint16 = 1 << 4
	volBitmapID         uint16 = 1 << 5
	volBitmapBytesFree  uint16 = 1 << 6
	volBitmapBytesTotal uint16 = 1 << 7
	volBitmapName       uint16 = 1 << 8
)

// AFP volume signature values (Inside Macintosh: Networking, "Volume signature"):
// 1 = Flat (no directories, not mountable by the Finder), 2 = Fixed Directory ID,
// 3 = Variable Directory ID. A mountable hierarchical volume advertises Fixed.
const volSignatureFixedDirID uint16 = 2

// NOTE: AFP 2.x has NO volume block-size field — the classic AppleShare client
// derives the HFS allocation block size itself from the reported BytesTotal
// with 16-bit block math (block ≈ total/65536, rounded up). The volume bitmap
// bits above bit 8 (Name) belong to AFP 3.x (ExtBytesFree/ExtBytesTotal/
// BlockSize) and are never requested by classic clients; an earlier revision
// served a "block size" under bit 9, which is actually AFP 3.x ExtBytesFree —
// dead and mislabeled, now removed. The ONLY lever over the Finder's per-file
// "size on disk" granularity is the reported volume size (see
// defaultVolumeSizeLimit).

// afpOpenVol opens a volume by name and packs the requested volume parameters.
//
// Request: cmd(1) pad(1) bitmap(2) pstring VolName [password...].
// Reply: bitmap(2) followed by the requested parameters in bitmap-bit order.
func (s *Service) afpOpenVol(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 4 {
		return nil, afpErrParamErr
	}
	reqBitmap := bp.BE16(block[2:4])
	name, _, ok := pString(block, 4)
	if !ok {
		return nil, afpErrParamErr
	}
	vol := s.volumeByName(string(name))
	if vol == nil {
		return nil, afpErrObjectNotFnd
	}
	// Gate on the session identity: a volume the logged-in user may not access is
	// reported as not-found (the same answer FPGetSrvrParms gave by omitting it),
	// so a client naming a restricted volume directly is still refused without
	// leaking that the volume exists.
	if !vol.allows(a.user) {
		return nil, afpErrObjectNotFnd
	}
	a.openVols[vol.ID()] = vol

	// Always answer at least the volume id so the client has a usable handle,
	// even if it asked for nothing (some clients send bitmap 0).
	bitmap := reqBitmap | volBitmapID
	out := make([]byte, 0, 64)
	out = bp.AppendBE16(out, bitmap)
	out = packVolParams(out, vol, bitmap)
	return out, afpNoErr
}

// packVolParams appends the volume parameters named by bitmap, in ascending
// bit order (the order AFP packs them). Dates default to the AFP epoch; free/
// total bytes come from the share's DiskUsage (0/0 when the backend can't report).
//
// The volume Name is a VARIABLE-length field: per AFP its fixed-section slot
// holds a 2-byte OFFSET (measured from the start of the parameters block, i.e.
// just after the reply bitmap) to a Pascal string appended after all the fixed
// fields. Writing the Pascal string inline — where the offset belongs — makes
// every real client mis-read the name pointer and truncates the reply.
func packVolParams(out []byte, vol *Volume, bitmap uint16) []byte {
	total, free := reportVolBytes(vol)
	// The name offset is relative to the parameters block, so it counts the
	// fixed fields only (the name's own 2-byte pointer included) but NOT the
	// bitmap word that precedes this block.
	fixedSize := volFixedParamsSize(bitmap)
	fixed := make([]byte, 0, fixedSize)
	var variable []byte

	if bitmap&volBitmapAttributes != 0 {
		fixed = bp.AppendBE16(fixed, 0)
	}
	if bitmap&volBitmapSignature != 0 {
		// Hierarchical, CNID-backed volumes advertise Fixed Directory ID so the
		// Finder will mount them (Flat volumes are not mountable). Matches the
		// legacy server's volumeType().
		fixed = bp.AppendBE16(fixed, volSignatureFixedDirID)
	}
	if bitmap&volBitmapCreateDate != 0 {
		fixed = bp.AppendBE32(fixed, macTime(afpEpoch))
	}
	if bitmap&volBitmapModDate != 0 {
		// Real root mod-date, not the constant epoch: classic Finders poll the
		// volume mod-date to decide when to re-read open windows (AFP has no
		// change push), and an observed AppleShare server reports a live date.
		mod := afpEpoch
		if fi, err := vol.FS().Stat(""); err == nil && fi.ModTime().After(afpEpoch) {
			mod = fi.ModTime()
		}
		fixed = bp.AppendBE32(fixed, macTime(mod))
	}
	if bitmap&volBitmapBackupDate != 0 {
		fixed = bp.AppendBE32(fixed, noBackupDate)
	}
	if bitmap&volBitmapID != 0 {
		fixed = bp.AppendBE16(fixed, vol.ID())
	}
	if bitmap&volBitmapBytesFree != 0 {
		fixed = bp.AppendBE32(fixed, sat32(free))
	}
	if bitmap&volBitmapBytesTotal != 0 {
		fixed = bp.AppendBE32(fixed, sat32(total))
	}
	if bitmap&volBitmapName != 0 {
		fixed = bp.AppendBE16(fixed, uint16(fixedSize+len(variable)))
		variable = putPString(variable, []byte(vol.Name()))
	}
	out = append(out, fixed...)
	out = append(out, variable...)
	return out
}

// volFixedParamsSize returns the byte size of the fixed section of a volume
// parameter block for bitmap — every field contributes its own width, and the
// variable-length Name contributes only its 2-byte offset pointer. Used to seed
// the name offset (see packVolParams).
func volFixedParamsSize(bitmap uint16) int {
	size := 0
	if bitmap&volBitmapAttributes != 0 {
		size += 2
	}
	if bitmap&volBitmapSignature != 0 {
		size += 2
	}
	if bitmap&volBitmapCreateDate != 0 {
		size += 4
	}
	if bitmap&volBitmapModDate != 0 {
		size += 4
	}
	if bitmap&volBitmapBackupDate != 0 {
		size += 4
	}
	if bitmap&volBitmapID != 0 {
		size += 2
	}
	if bitmap&volBitmapBytesFree != 0 {
		size += 4
	}
	if bitmap&volBitmapBytesTotal != 0 {
		size += 4
	}
	if bitmap&volBitmapName != 0 {
		size += 2 // offset pointer, not the string
	}
	return size
}

// afpMaxVolumeBytes is the largest free/total byte count we report in the
// 32-bit AFP 2.x BytesFree/BytesTotal fields: 2 GiB − 1, NOT the field's full
// 4 GiB − 1 range. The classic AppleShare workstation client derives an HFS
// allocation-block size from BytesTotal (≈ total/65536); at 0xFFFFFFFF that
// yields 0x10000, which overflows a 16-bit register to zero and the client's
// next division is the System 7.5 Finder's "divide by zero" crash at mount
// (observed e2e over LToUDP; see spec/errata.md). Capping at MaxInt32 also
// dodges clients that treat the count as signed, and matches main's
// known-good capAFPBytes32. (The 64-bit ExtBytesFree/Total fields are an AFP
// 3.x feature this server does not implement.)
const afpMaxVolumeBytes uint32 = 0x7FFFFFFF

// defaultVolumeSizeLimit is the volume size reported to AFP clients when a
// volume has no size_limit configured: 512 MiB. The classic AppleShare client
// derives the HFS allocation block size from the reported bytes with 16-bit
// block math (block ≈ bytes/65536, rounded up), so the reported size sets the
// Finder's per-file "size on disk" granularity: reporting the saturated 2 GiB
// cap yields 32 KiB blocks (a 1 KB file shows as "32K on disk"); 512 MiB
// yields 8 KiB — a period-typical hard disk. Presentation only: it does not
// limit what the host stores.
const defaultVolumeSizeLimit uint64 = 512 << 20

// reportVolBytes computes the BytesTotal/BytesFree PRESENTATION values for a
// volume: the host figures clamped to the volume's reported size (size_limit,
// default defaultVolumeSizeLimit). A backend that cannot report usage (memfs's
// 0/0, or a DiskUsage error) presents an empty virtual disk of the reported
// size, so the Finder still shows usable space. sat32 at the pack site remains
// the final wire guard for an operator limit above 2 GiB − 1.
func reportVolBytes(vol *Volume) (total, free uint64) {
	limit := vol.SizeLimit()
	total, free = limit, limit
	if t, f, err := vol.FS().DiskUsage(""); err == nil && t > 0 {
		total = min(t, limit)
		free = min(f, total)
	}
	return total, free
}

// sat32 SATURATES a 64-bit byte count to the reportable AFP volume field
// range: a real disk larger than the cap is reported as exactly the cap (a
// full, valid value) rather than a uint32 cast, which would WRAP and tell the
// client a 6 GiB disk has 2 GiB.
func sat32(v uint64) uint32 {
	if v > uint64(afpMaxVolumeBytes) {
		return afpMaxVolumeBytes
	}
	return uint32(v)
}

// afpCloseVol releases a volume handle held by the session.
//
// Request: cmd(1) pad(1) volID(2).
func (s *Service) afpCloseVol(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 4 {
		return nil, afpErrParamErr
	}
	delete(a.openVols, bp.BE16(block[2:4]))
	return nil, afpNoErr
}

// afpMapID maps a user or group id to a name (FPMapID, cmd 21). This is a
// compatibility server with no real user database, so it answers the two IDs the
// Finder cares about: the owner ("root") and the group ("wheel"). Ported from
// main's handleMapID.
//
// Request: cmd(1) function(1) id(4). Reply: pstring(name).
func (s *Service) afpMapID(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 6 {
		return nil, afpErrParamErr
	}
	function := block[1]
	name := "root"
	// Functions 2 (MapUGRGID→group) and 4 (kUserUUID variants) → the group name.
	if function == 2 || function == 4 {
		name = "wheel"
	}
	return putPString(nil, []byte(name)), afpNoErr
}

// afpMapName maps a user or group name to an id (FPMapName, cmd 22). With no user
// database every name maps to id 0. Ported from main's handleMapName.
//
// Request: cmd(1) function(1) pstring(name). Reply: id(4).
func (s *Service) afpMapName(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 3 {
		return nil, afpErrParamErr
	}
	return bp.AppendBE32(nil, 0), afpNoErr
}

// Server-message constants (FPGetSrvrMsg, cmd 38). From an observed capture of a
// real AppleShare server: the client fetches the login message (type 0)
// unprompted right after FPOpenVol, and the server message (type 1) after each
// SPAttention carrying the AspAttnMsg bit; the reply bitmap always carries the
// message-as-text bit.
const (
	srvrMsgTypeLogin  uint16 = 0      // login (greeting) message, fetched at mount
	srvrMsgTypeServer uint16 = 1      // server (operator) message, announced by attention
	srvrMsgBitmap     uint16 = 0x0001 // MessageBitmap bit 0: message as text (bit 1 = UTF-8, not served)
	// maxSrvrMsgLen is the AFP server-message length limit (199 bytes).
	maxSrvrMsgLen = 199
)

// srvrMsgBytes renders message text for the wire: MacRoman, truncated to the AFP
// limit. An unmappable rune degrades to '?' rather than failing the reply.
func srvrMsgBytes(text string) []byte {
	b, err := encoding.UTF8ToMacRoman(text)
	if err != nil {
		b = make([]byte, 0, len(text))
		for _, r := range text {
			if rb, rerr := encoding.UTF8ToMacRoman(string(r)); rerr == nil {
				b = append(b, rb...)
			} else {
				b = append(b, '?')
			}
		}
	}
	if len(b) > maxSrvrMsgLen {
		b = b[:maxSrvrMsgLen]
	}
	return b
}

// afpGetSrvrMsg returns a server or login message (FPGetSrvrMsg, cmd 38). Type 0
// is the configured login greeting the Finder fetches during mount; type 1 is
// the session's pending operator message a preceding SPAttention (AspAttnMsg)
// announced. An unconfigured/absent message answers with an empty string, the
// pre-message behaviour.
//
// Request: cmd(1) pad(1) messageType(2) bitmap(2).
// Reply:   messageType(2) bitmap(2) pstring(message).
func (s *Service) afpGetSrvrMsg(a *afpSession, block []byte) ([]byte, int32) {
	var msgType uint16
	if len(block) >= 6 {
		msgType = bp.BE16(block[2:4])
	}
	var text string
	switch msgType {
	case srvrMsgTypeLogin:
		text = s.loginMessage()
	case srvrMsgTypeServer:
		text = a.serverMessage()
	}
	msg := srvrMsgBytes(text)
	out := make([]byte, 0, 5+len(msg))
	out = bp.AppendBE16(out, msgType)
	out = bp.AppendBE16(out, srvrMsgBitmap)
	out = putPString(out, msg)
	return out, afpNoErr
}

// afpGetVolParms returns the parameters of an already-open volume. The Finder
// issues it during mount; the refactor's scratch rewrite dropped it entirely,
// so it answered kFPCallNotSupported (-5024) and the mount stalled.
//
// Request: cmd(1) pad(1) volID(2) bitmap(2).
// Reply:   bitmap(2) <packed volume params> — the same parameter block FPOpenVol
// returns, so it shares packVolParams (the volume Name is a trailing variable
// field addressed by a 2-byte offset).
func (s *Service) afpGetVolParms(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 6 {
		return nil, afpErrParamErr
	}
	vol, ok := a.openVols[bp.BE16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	// Echo exactly the requested bitmap. An observed AppleShare server answers a
	// GetVolParms bitmap 0x0048 with 0x0048 — it does NOT inject unrequested
	// fields; our earlier forced VolumeID (reply 0x0068) was a parity divergence.
	// (FPOpenVol keeps its forced ID: the mount handshake needs the id.)
	bitmap := bp.BE16(block[4:6])
	out := make([]byte, 0, 64)
	out = bp.AppendBE16(out, bitmap)
	out = packVolParams(out, vol, bitmap)
	return out, afpNoErr
}

// --- FPGetFileDirParms / FPEnumerate (catalog reads; spec/AFP_Connection_Flow
// §7). The requested file/dir parameters are packed by the volume's full
// bitmap packer (parms.go), in ascending bit order with variable-length names in
// a trailing area addressed by 2-byte offsets — the AFP 2.x parameter block. ---

// isDirFlag is the high bit of the per-entry "file/dir" byte in an Enumerate
// reply: set for a directory, clear for a file.
const isDirFlag uint8 = 0x80

// fileDirParmsHeader appends the fixed FPGetFileDirParms reply header to out and
// returns it: fileBitmap(2) dirBitmap(2) then the file/dir byte pair — isDirFlag
// (0x80) followed by a pad for a directory, or 0x00 0x00 for a file. It delegates
// to FPGetFileDirParmsRes.Marshal (the production path) so the golden test that
// pins this framing validates the same code the handler runs.
func fileDirParmsHeader(out []byte, fileBitmap, dirBitmap uint16, isDir bool) []byte {
	hdr := (&FPGetFileDirParmsRes{FileBitmap: fileBitmap, DirBitmap: dirBitmap, IsDir: isDir}).Marshal()
	return append(out, hdr...)
}

// afpGetFileDirParms stats one path and packs the requested file/dir parameters.
//
// Request: cmd(1) pad(1) volID(2) dirID(4) fileBitmap(2) dirBitmap(2) pathType(1)
//
//	pathname...
//
// Reply: fileBitmap(2) dirBitmap(2) isDir(1) pad(1) <packed params>.
func (s *Service) afpGetFileDirParms(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 13 {
		return nil, afpErrParamErr
	}
	vol, ok := a.openVols[bp.BE16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	dirID := bp.BE32(block[4:8])
	fileBitmap := bp.BE16(block[8:10])
	dirBitmap := bp.BE16(block[10:12])
	pathType := block[12]
	store, code := resolveBlockPath(vol, dirID, block, 13, pathType)
	if code != afpNoErr {
		return nil, code
	}

	info, err := vol.Stat(store)
	if err != nil {
		return nil, mapStatErr(err)
	}
	bitmap := dirBitmap
	if !info.IsDir() {
		bitmap = fileBitmap
	}
	// Reply echoes BOTH bitmaps (file then dir), then the type/pad byte pair,
	// then the packed params governed by the applicable bitmap. The DTO owns the
	// fixed header; fileDirParams packs the variable params.
	res := &FPGetFileDirParmsRes{
		FileBitmap: fileBitmap,
		DirBitmap:  dirBitmap,
		IsDir:      info.IsDir(),
		Params:     vol.fileDirParams(nil, store, info, bitmap, pathType),
	}
	return res.Marshal(), afpNoErr
}

// afpSetFileDirParms sets parameters common to files and directories. The Finder
// issues it during mount (e.g. to stamp folder Finder info); the refactor's
// scratch rewrite omitted it, so it answered kFPCallNotSupported (-5024) and the
// Finder treated the volume as faulty.
//
// Request: cmd(1) pad(1) volID(2) dirID(4) bitmap(2) pathType(1) pathname(pascal)
//
//	[pad to even] <params in bitmap order>
//
// Only the Finder-info parameter is persisted; other bits (dates/attributes) are
// accepted and acknowledged so the client proceeds. Reply: empty.
func (s *Service) afpSetFileDirParms(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 11 {
		return nil, afpErrParamErr
	}
	vol, ok := a.openVols[bp.BE16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	if vol.FS().Capabilities().ReadOnly {
		return nil, afpErrAccessDenied
	}
	dirID := bp.BE32(block[4:8])
	bitmap := bp.BE16(block[8:10])
	pathType := block[10]
	store, code := resolveBlockPath(vol, dirID, block, 11, pathType)
	if code != afpNoErr {
		return nil, code
	}
	// The parameter block follows the Pascal pathname, word-aligned to an even
	// offset from the start of the command block.
	nameLen := int(block[11])
	off := 12 + nameLen
	if off%2 != 0 {
		off++
	}
	if fi, okFI := setParamsFinderInfo(block, off, bitmap); okFI && store != "" {
		// The volume root ("") carries no per-object Finder-info sidecar; the Finder
		// still stamps it during mount, so that case is acknowledged without a write.
		if err := vol.SetFinderInfo(store, fi); err != nil {
			return nil, afpErrAccessDenied
		}
	}
	return nil, afpNoErr
}

// setParamsFinderInfo extracts the 32-byte Finder info from a Set*Parms parameter
// block at off, walking the fixed fields that precede FinderInfo (bit 5) in
// ascending bitmap-bit order. Returns ok=false if the FinderInfo bit is clear or
// the block is too short.
func setParamsFinderInfo(block []byte, off int, bitmap uint16) ([32]byte, bool) {
	var fi [32]byte
	if bitmap&fdBitmapFinderInfo == 0 {
		return fi, false
	}
	if bitmap&fdBitmapAttributes != 0 {
		off += 2
	}
	if bitmap&fdBitmapParentDID != 0 {
		off += 4
	}
	if bitmap&fdBitmapCreateDate != 0 {
		off += 4
	}
	if bitmap&fdBitmapModDate != 0 {
		off += 4
	}
	if bitmap&fdBitmapBackupDate != 0 {
		off += 4
	}
	if off+32 > len(block) {
		return fi, false
	}
	copy(fi[:], block[off:off+32])
	return fi, true
}

// afpEnumerate lists a directory's children, packing one entry per child with the
// requested file/dir parameters.
//
// Request: cmd(1) pad(1) volID(2) dirID(4) fileBitmap(2) dirBitmap(2) reqCount(2)
//
//	startIndex(2) maxReplySize(2) pathType(1) pathname...
//
// Reply: fileBitmap(2) dirBitmap(2) actualCount(2) {entryLen(1) isDir(1)
//
//	<packed params>} × actualCount, each entry padded to an even length.
func (s *Service) afpEnumerate(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 19 {
		return nil, afpErrParamErr
	}
	vol, ok := a.openVols[bp.BE16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	dirID := bp.BE32(block[4:8])
	fileBitmap := bp.BE16(block[8:10])
	dirBitmap := bp.BE16(block[10:12])
	reqCount := int(bp.BE16(block[12:14]))
	startIndex := int(bp.BE16(block[14:16]))
	// maxReplySize (AFP 2.x: 2 bytes) is the client's reply-buffer budget. The
	// server MUST NOT exceed it: an over-long reply is truncated by the transport,
	// leaving the client a partial final entry that desyncs its parse and discards
	// the whole listing (observed as "volume enumerates nothing"). enumReplyHeader
	// (fileBitmap+dirBitmap+actCount) counts against the budget.
	maxReply := int(bp.BE16(block[16:18]))
	pathType := block[18]
	store, code := resolveBlockPath(vol, dirID, block, 19, pathType)
	if code != afpNoErr {
		return nil, code
	}

	entries, err := vol.Enumerate(store)
	if err != nil {
		return nil, mapStatErr(err)
	}
	// AFP start index is 1-based.
	start := max(startIndex-1, 0)
	if start >= len(entries) {
		return nil, afpErrObjectNotFnd // kFPObjectNotFound == "no more entries"
	}

	const enumReplyHeader = 6 // fileBitmap(2) dirBitmap(2) actCount(2)
	var entries2 []byte
	actual := 0
	for i := start; i < len(entries) && actual < reqCount; i++ {
		de := entries[i]
		if isMetadataName(de.Name()) {
			continue // hide ._sidecars and EA/stream shadow paths
		}
		childStore := joinStore(store, de.Name())
		info, err := de.Info()
		if err != nil {
			continue
		}
		bitmap := dirBitmap
		if !de.IsDir() {
			bitmap = fileBitmap
		}
		// The packed params carry their own name-offset words, anchored at the
		// start of the params (byte 2 of the framed entry: length + type). enumEntry
		// frames them as [len][type][params] with even-length padding at the tail —
		// NO pad byte between the type byte and the params, or every client
		// mis-reads the name pointer.
		params := vol.fileDirParams(nil, childStore, info, bitmap, pathType)
		entry := enumEntry(de.IsDir(), params)
		// Stop before overflowing the client's reply budget (but always return at
		// least one entry, per the AFP convention, so a single over-large entry
		// still makes progress rather than looping).
		if maxReply > 0 && actual > 0 && enumReplyHeader+len(entries2)+len(entry) > maxReply {
			// Budget reached; the client re-requests from startIndex+actual for the
			// next page.
			break
		}
		entries2 = append(entries2, entry...)
		actual++
	}
	if actual == 0 {
		return nil, afpErrObjectNotFnd
	}
	res := &FPEnumerateRes{
		FileBitmap: fileBitmap,
		DirBitmap:  dirBitmap,
		ActCount:   uint16(actual),
		Entries:    entries2,
	}
	return res.Marshal(), afpNoErr
}

// resolveBlockPath resolves the pathname starting at off in an AFP command block
// (relative to the volume root in this spine — dir-id-relative resolution lands
// with FPOpenDir in a later slice) and maps codec errors to AFP result codes.
func resolveBlockPath(vol *Volume, dirID uint32, block []byte, off int, pathType uint8) (string, int32) {
	return resolveCatalogPath(vol, dirID, block, off, pathType)
}

// mapStatErr maps a store Stat/Enumerate error to an AFP result code.
func mapStatErr(err error) int32 {
	switch {
	case err == nil:
		return afpNoErr
	case isNotExist(err):
		return afpErrObjectNotFnd
	default:
		return afpErrMiscErr
	}
}

// isNotExist reports whether err is a store "not found" error.
func isNotExist(err error) bool { return errors.Is(err, stdfs.ErrNotExist) }

// splitStore splits a '/'-separated store path into its parent and final element.
func splitStore(path string) (dir, base string) {
	i := strings.LastIndexByte(path, '/')
	if i < 0 {
		return "", path
	}
	return path[:i], path[i+1:]
}

// isMetadataName reports whether a store-native name is a metadata shadow that
// must not surface as a catalog entry: an AppleDouble "._" sidecar, or the
// NUL-delimited EA / ":"-delimited stream shadow paths the fork engines address
// through the FileSystem. The data fork (the file itself) is never one of these.
func isMetadataName(name string) bool {
	if strings.HasPrefix(name, "._") {
		return true
	}
	// Netatalk-style metadata containers and the CNID database must never surface
	// as catalog entries (matches main's alwaysHiddenNames + isMetadataArtifact).
	// A stray visible ".AppleDouble"/".AppleDesktop" was cluttering the Finder and
	// (with the CNID .db) padding the listing.
	for _, hidden := range alwaysHiddenNames {
		if strings.EqualFold(name, hidden) {
			return true
		}
	}
	// xattr engine EA shadow ("name\x00ea\x00…") and ads engine stream shadow
	// ("name:AFP_Resource"/"name:AFP_AfpInfo") — neither is a real child name.
	if strings.Contains(name, "\x00ea\x00") {
		return true
	}
	if strings.Contains(name, ":AFP_") {
		return true
	}
	return false
}

// alwaysHiddenNames are metadata containers hidden from every enumeration,
// case-insensitively (Netatalk layout + the CNID sidecar db). Ported from main's
// service/afp alwaysHiddenNames.
var alwaysHiddenNames = []string{
	".appledesktop",
	".appledouble",
	".desktop.db", // legacy Desktop DB
}
