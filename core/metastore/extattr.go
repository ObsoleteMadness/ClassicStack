package metastore

import "errors"

// ClassicStack-private extended-attribute codec: fields with no Samba
// XATTR_DOSINFO equivalent (today, just AccessTime). Kept out of
// EncodeDOSInfo/DecodeDOSInfo so that blob stays byte-identical to what Samba
// writes to user.DOSATTRIB — this is our own record, never shared with another
// implementation, so its layout is free to grow in later versions.
//
// Layout (little-endian):
//
//	uint16  version      = 1
//	uint32  valid_flags   (which fields below are meaningful)
//	uint64  access_time   (NTTIME: 100-ns ticks since 1601-01-01)
const (
	extAttrVersion1 = 1

	// extAttrValidAccessTime marks the access_time field meaningful.
	extAttrValidAccessTime uint32 = 0x0001
)

// ErrBadExtAttr is returned by DecodeExtAttr for a blob that is not a
// recognisable ClassicStack extended-attribute record.
var ErrBadExtAttr = errors.New("metastore: malformed ext-attr blob")

// EncodeExtAttr renders the ClassicStack-private fields of attr as a
// version-1 record.
func EncodeExtAttr(attr DOSAttr) []byte {
	valid := uint32(0)
	var at uint64
	if !attr.AccessTime.IsZero() {
		valid |= extAttrValidAccessTime
		at = unixToNTTIME(attr.AccessTime)
	}
	b := make([]byte, 14)
	putLE16(b[0:2], extAttrVersion1)
	putLE32(b[2:6], valid)
	putLE64(b[6:14], at)
	return b
}

// DecodeExtAttr parses a record written by EncodeExtAttr. An unrecognised
// version or truncated blob is rejected so a corrupt value is treated as
// absent rather than mis-decoded.
func DecodeExtAttr(b []byte) (DOSAttr, error) {
	if len(b) < 6 {
		return DOSAttr{}, ErrBadExtAttr
	}
	version := le16(b[0:2])
	if version != extAttrVersion1 {
		return DOSAttr{}, ErrBadExtAttr
	}
	valid := le32(b[2:6])
	var attr DOSAttr
	if valid&extAttrValidAccessTime != 0 {
		if len(b) < 14 {
			return DOSAttr{}, ErrBadExtAttr
		}
		attr.AccessTime = nttimeToUnix(le64(b[6:14]))
	}
	return attr, nil
}

// mergeExtAttr copies the ClassicStack-private fields from ext onto attr,
// leaving the Samba-interop fields (Attrs, CreateTime) untouched.
func mergeExtAttr(attr DOSAttr, ext DOSAttr) DOSAttr {
	attr.AccessTime = ext.AccessTime
	return attr
}
