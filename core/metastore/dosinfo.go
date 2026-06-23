package metastore

import (
	"errors"
	"time"
)

// XATTR_DOSINFO codec — the on-disk format Samba writes to the user.DOSATTRIB
// extended attribute, reused verbatim as the metastore value so the metastore KV,
// the Samba-compatible xattr backend, and the sidecar backend all share ONE wire
// format (a value written by one backend is readable by another, and by Samba).
//
// Layout (Samba source3/lib/xattr_tdb + librpc xattr_DOSAttrib, the version-3
// "info_compat" arm, all little-endian):
//
//	uint16  version          = 3
//	uint32  valid_flags      (which fields below are meaningful)
//	uint32  attrib           (the DOS attribute bitmask, FILE_ATTRIBUTE_*)
//	uint32  ext_attrib       (reserved; 0)
//	uint32  reserved         (0)
//	uint64  create_time      (NTTIME: 100-ns ticks since 1601-01-01)
//
// We emit exactly this 26-byte record. A reader accepts version 1–4 (older Samba
// arms are prefixes/subsets) by reading the fields it understands and ignoring
// the rest; an unknown or truncated blob is rejected so a corrupt value falls back
// to host-derived attributes rather than mis-decoding.
const (
	dosInfoVersion3 = 3

	// xattrDOSInfoValidAttrib marks the attrib field meaningful.
	xattrDOSInfoValidAttrib uint32 = 0x0001
	// xattrDOSInfoValidCreateTime marks the create_time field meaningful.
	xattrDOSInfoValidCreateTime uint32 = 0x0008

	// nttimeEpochOffset is the 100-ns interval count between the NTTIME epoch
	// (1601-01-01) and the Unix epoch (1970-01-01).
	nttimeEpochOffset = 116444736000000000
)

// ErrBadDOSInfo is returned by DecodeDOSInfo for a blob that is not a recognisable
// XATTR_DOSINFO record.
var ErrBadDOSInfo = errors.New("metastore: malformed XATTR_DOSINFO blob")

// EncodeDOSInfo renders attr as a version-3 XATTR_DOSINFO record (the
// user.DOSATTRIB payload). The valid-flags mark attrib always present and
// create_time present only when non-zero.
func EncodeDOSInfo(attr DOSAttr) []byte {
	valid := xattrDOSInfoValidAttrib
	var ct uint64
	if !attr.CreateTime.IsZero() {
		valid |= xattrDOSInfoValidCreateTime
		ct = unixToNTTIME(attr.CreateTime)
	}
	b := make([]byte, 26)
	putLE16(b[0:2], dosInfoVersion3)
	putLE32(b[2:6], valid)
	putLE32(b[6:10], uint32(attr.Attrs))
	putLE32(b[10:14], 0) // ext_attrib
	putLE32(b[14:18], 0) // reserved
	putLE64(b[18:26], ct)
	return b
}

// DecodeDOSInfo parses a XATTR_DOSINFO record written by EncodeDOSInfo or by
// Samba. It accepts the version-3 layout; a version it does not recognise, or a
// blob too short to hold the fields its valid-flags claim, is rejected.
func DecodeDOSInfo(b []byte) (DOSAttr, error) {
	if len(b) < 6 {
		return DOSAttr{}, ErrBadDOSInfo
	}
	version := le16(b[0:2])
	if version == 0 || version > 4 {
		return DOSAttr{}, ErrBadDOSInfo
	}
	valid := le32(b[2:6])
	var attr DOSAttr
	// attrib (offset 6, 4 bytes) — present in every version we accept.
	if valid&xattrDOSInfoValidAttrib != 0 {
		if len(b) < 10 {
			return DOSAttr{}, ErrBadDOSInfo
		}
		attr.Attrs = uint16(le32(b[6:10]) & 0xFFFF)
	}
	// create_time (offset 18, 8 bytes) — version-3 layout.
	if valid&xattrDOSInfoValidCreateTime != 0 {
		if len(b) < 26 {
			return DOSAttr{}, ErrBadDOSInfo
		}
		attr.CreateTime = nttimeToUnix(le64(b[18:26]))
	}
	return attr, nil
}

// unixToNTTIME converts a Go time to an NTTIME (100-ns ticks since 1601). A
// zero/pre-epoch time renders as 0 ("unknown").
func unixToNTTIME(t time.Time) uint64 {
	if t.IsZero() {
		return 0
	}
	ns := t.UTC().UnixNano()
	ticks := ns/100 + nttimeEpochOffset
	if ticks < 0 {
		return 0
	}
	return uint64(ticks)
}

// nttimeToUnix converts an NTTIME back to a Go time; 0 yields the zero time.
func nttimeToUnix(nt uint64) time.Time {
	if nt == 0 {
		return time.Time{}
	}
	ns := (int64(nt) - nttimeEpochOffset) * 100
	return time.Unix(0, ns).UTC()
}

// Little-endian helpers (metastore is CORE/stdlib-only; encoding/binary pulls in
// reflect, so the few LE codecs here are hand-rolled like core/binaryprimitives).
func le16(b []byte) uint16 { return uint16(b[0]) | uint16(b[1])<<8 }
func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
func le64(b []byte) uint64 {
	return uint64(le32(b[0:4])) | uint64(le32(b[4:8]))<<32
}
func putLE16(b []byte, v uint16) { b[0] = byte(v); b[1] = byte(v >> 8) }
func putLE32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}
func putLE64(b []byte, v uint64) {
	putLE32(b[0:4], uint32(v))
	putLE32(b[4:8], uint32(v>>32))
}
