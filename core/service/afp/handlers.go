package afp

import (
	"errors"
	stdfs "io/fs"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// --- hand-rolled big-endian + Pascal-string helpers (core ring: no
// encoding/binary, which transitively imports reflect — §1 / archtest). ---

func putBE16(dst []byte, v uint16) []byte { return append(dst, byte(v>>8), byte(v)) }

func putBE32(dst []byte, v uint32) []byte {
	return append(dst, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func be16(b []byte) uint16 { return uint16(b[0])<<8 | uint16(b[1]) }

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
	out = putBE16(out, uint16(machineOff))
	out = putBE16(out, uint16(versionsOff))
	out = putBE16(out, uint16(uamsOff))
	out = putBE16(out, 0) // icon/mask offset — none
	out = putBE16(out, info.Flags)
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
	uam, _, ok := pString(args, off)
	if !ok {
		return nil, afpErrParamErr
	}
	if !s.supportsVersion(string(ver)) {
		return nil, afpErrBadVersNum
	}
	switch string(uam) {
	case "No User Authent", "Cleartxt Passwrd":
		a.loggedIn = true
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
func (s *Service) afpGetSrvrParms() []byte {
	// Snapshot under the lock: the share.Manager can mutate s.volumes at runtime.
	vols := s.Volumes()
	out := make([]byte, 0, 5+16*len(vols))
	out = putBE32(out, macTime(time.Now()))
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

// volSignatureFixed is the AFP volume signature for a fixed (non-variable) volume.
const volSignatureFixed uint16 = 1

// afpOpenVol opens a volume by name and packs the requested volume parameters.
//
// Request: cmd(1) pad(1) bitmap(2) pstring VolName [password...].
// Reply: bitmap(2) followed by the requested parameters in bitmap-bit order.
func (s *Service) afpOpenVol(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 4 {
		return nil, afpErrParamErr
	}
	reqBitmap := be16(block[2:4])
	name, _, ok := pString(block, 4)
	if !ok {
		return nil, afpErrParamErr
	}
	vol := s.volumeByName(string(name))
	if vol == nil {
		return nil, afpErrObjectNotFnd
	}
	a.openVols[vol.ID()] = vol

	// Always answer at least the volume id so the client has a usable handle,
	// even if it asked for nothing (some clients send bitmap 0).
	bitmap := reqBitmap | volBitmapID
	out := make([]byte, 0, 64)
	out = putBE16(out, bitmap)
	out = packVolParams(out, vol, bitmap)
	return out, afpNoErr
}

// packVolParams appends the volume parameters named by bitmap, in ascending
// bit order (the order AFP packs them). Dates default to the AFP epoch; free/
// total bytes come from the share's DiskUsage (0/0 when the backend can't report).
func packVolParams(out []byte, vol *Volume, bitmap uint16) []byte {
	var total, free uint64
	if t, f, err := vol.FS().DiskUsage(""); err == nil {
		total, free = t, f
	}
	if bitmap&volBitmapAttributes != 0 {
		out = putBE16(out, 0)
	}
	if bitmap&volBitmapSignature != 0 {
		out = putBE16(out, volSignatureFixed)
	}
	if bitmap&volBitmapCreateDate != 0 {
		out = putBE32(out, macTime(afpEpoch))
	}
	if bitmap&volBitmapModDate != 0 {
		out = putBE32(out, macTime(afpEpoch))
	}
	if bitmap&volBitmapBackupDate != 0 {
		out = putBE32(out, 0x80000000) // "no backup date" sentinel
	}
	if bitmap&volBitmapID != 0 {
		out = putBE16(out, vol.ID())
	}
	if bitmap&volBitmapBytesFree != 0 {
		out = putBE32(out, uint32(free))
	}
	if bitmap&volBitmapBytesTotal != 0 {
		out = putBE32(out, uint32(total))
	}
	if bitmap&volBitmapName != 0 {
		out = putPString(out, []byte(vol.Name()))
	}
	return out
}

// afpCloseVol releases a volume handle held by the session.
//
// Request: cmd(1) pad(1) volID(2).
func (s *Service) afpCloseVol(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 4 {
		return nil, afpErrParamErr
	}
	delete(a.openVols, be16(block[2:4]))
	return nil, afpNoErr
}

// --- FPGetFileDirParms / FPEnumerate (catalog reads; spec/AFP_Connection_Flow
// §7). These pack a deliberately small, faithful subset of the file/dir bitmaps —
// enough to prove the Volume seam end-to-end — leaving the full bitmap surface to
// a follow-up slice. ---

// File/dir parameter bitmap bits actually packed by this spine (Inside
// Macintosh: Networking, "File parameters" / "Directory parameters"). The full
// bitmap surface (Finder info, dates, CNID, short name, access rights, …) lands
// in a follow-up slice; this subset proves the Volume seam end-to-end:
//   - LongName exercises the FilenameCodec Encode path (store → wire charset);
//   - DataForkLen exercises the fork engine's ForkLen;
//   - OffspringCount exercises Enumerate via the FileSystem.
//
// LongName shares bit 4 between the file and directory bitmaps; the data-fork
// length (file, bit 9) and the offspring count (directory, bit 9) share bit 9.
const (
	fileDirBitmapLongName   uint16 = 1 << 4
	fileBitmapDataForkLen   uint16 = 1 << 9
	dirBitmapOffspringCount uint16 = 1 << 9
)

// isDirFlag is the high bit of the per-entry "file/dir" byte in an Enumerate
// reply: set for a directory, clear for a file.
const isDirFlag uint8 = 0x80

// afpGetFileDirParms stats one path and packs the requested file/dir parameters.
//
// Request: cmd(1) pad(1) volID(2) dirID(4) fileBitmap(2) dirBitmap(2) pathType(1)
//
//	pathname...
//
// Reply: fileDirBitmap(2) isDir(1) pad(1) <packed params>.
func (s *Service) afpGetFileDirParms(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 13 {
		return nil, afpErrParamErr
	}
	vol, ok := a.openVols[be16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	fileBitmap := be16(block[8:10])
	dirBitmap := be16(block[10:12])
	pathType := block[12]
	store, code := resolveBlockPath(vol, block, 13, pathType)
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
	out := make([]byte, 0, 32)
	out = putBE16(out, bitmap)
	if info.IsDir() {
		out = append(out, isDirFlag, 0)
	} else {
		out = append(out, 0, 0)
	}
	out = packEntryParams(out, vol, store, info, bitmap, pathType)
	return out, afpNoErr
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
	vol, ok := a.openVols[be16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	fileBitmap := be16(block[8:10])
	dirBitmap := be16(block[10:12])
	reqCount := int(be16(block[12:14]))
	startIndex := int(be16(block[14:16]))
	pathType := block[18]
	store, code := resolveBlockPath(vol, block, 19, pathType)
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

	out := make([]byte, 0, 256)
	out = putBE16(out, fileBitmap)
	out = putBE16(out, dirBitmap)
	countOff := len(out)
	out = putBE16(out, 0) // actualCount, patched below

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
		entry := make([]byte, 0, 64)
		isDir := byte(0)
		if de.IsDir() {
			isDir = isDirFlag
		}
		entry = append(entry, isDir, 0) // isDir + pad, after the length byte
		entry = packEntryParams(entry, vol, childStore, info, bitmap, pathType)
		// Each entry is prefixed by its own length byte (incl. the length byte)
		// and padded to an even total length.
		entryLen := len(entry) + 1
		if entryLen%2 != 0 {
			entry = append(entry, 0)
			entryLen++
		}
		out = append(out, byte(entryLen))
		out = append(out, entry...)
		actual++
	}
	if actual == 0 {
		return nil, afpErrObjectNotFnd
	}
	out[countOff] = byte(actual >> 8)
	out[countOff+1] = byte(actual)
	return out, afpNoErr
}

// packEntryParams appends the file/dir parameters named by bitmap for one catalog
// entry, in ascending bit order. Only the spine's subset is packed (LongName,
// DataForkLen for files / OffspringCount for directories); bits outside it are
// silently omitted, so the client reads exactly what the returned bitmap
// advertises. The name is encoded back to the request's wire charset through the
// volume's FilenameCodec — an unrepresentable name is omitted rather than
// mangled.
func packEntryParams(out []byte, vol *Volume, store string, info stdfs.FileInfo, bitmap uint16, pathType uint8) []byte {
	if bitmap&fileDirBitmapLongName != 0 {
		_, base := splitStore(store)
		if wire, err := vol.EncodeName(base, pathType); err == nil {
			out = putPString(out, wire)
		} else {
			out = putPString(out, nil)
		}
	}
	if info.IsDir() {
		if bitmap&dirBitmapOffspringCount != 0 {
			var count uint16
			if kids, err := vol.Enumerate(store); err == nil {
				for _, k := range kids {
					if !isMetadataName(k.Name()) {
						count++
					}
				}
			}
			out = putBE16(out, count)
		}
	} else {
		if bitmap&fileBitmapDataForkLen != 0 {
			n, _ := vol.ForkLen(store, fs.DataFork)
			out = putBE32(out, uint32(n))
		}
	}
	return out
}

// resolveBlockPath resolves the pathname starting at off in an AFP command block
// (relative to the volume root in this spine — dir-id-relative resolution lands
// with FPOpenDir in a later slice) and maps codec errors to AFP result codes.
func resolveBlockPath(vol *Volume, block []byte, off int, pathType uint8) (string, int32) {
	if off > len(block) {
		return "", afpErrParamErr
	}
	store, err := vol.ResolvePath("", string(block[off:]), pathType)
	if err != nil {
		return "", afpErrParamErr // unrepresentable name → "illegal name"
	}
	return store, afpNoErr
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
