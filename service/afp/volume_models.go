//go:build afp || all

package afp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/pkg/binutil"
)

func formatVolBitmap(bitmap uint16) string {
	var flags []string
	if bitmap&VolBitmapAttributes != 0 {
		flags = append(flags, "Attributes")
	}
	if bitmap&VolBitmapSignature != 0 {
		flags = append(flags, "Signature")
	}
	if bitmap&VolBitmapCreateDate != 0 {
		flags = append(flags, "CreateDate")
	}
	if bitmap&VolBitmapModDate != 0 {
		flags = append(flags, "ModDate")
	}
	if bitmap&VolBitmapBackupDate != 0 {
		flags = append(flags, "BackupDate")
	}
	if bitmap&VolBitmapVolID != 0 {
		flags = append(flags, "VolID")
	}
	if bitmap&VolBitmapBytesFree != 0 {
		flags = append(flags, "BytesFree")
	}
	if bitmap&VolBitmapBytesTotal != 0 {
		flags = append(flags, "BytesTotal")
	}
	if bitmap&VolBitmapName != 0 {
		flags = append(flags, "Name")
	}
	if bitmap&VolBitmapExtBytesFree != 0 {
		flags = append(flags, "ExtBytesFree")
	}
	if bitmap&VolBitmapExtBytesTotal != 0 {
		flags = append(flags, "ExtBytesTotal")
	}
	if bitmap&VolBitmapBlockSize != 0 {
		flags = append(flags, "BlockSize")
	}
	return fmt.Sprintf("0x%04x [%s]", bitmap, strings.Join(flags, "|"))
}

type FPOpenVolReq struct {
	// Bitmap is a bitmap specifying which volume parameters the client
	// requests to be returned in the reply. The corresponding bit for each
	// desired parameter should be set; this field must not be null.
	Bitmap uint16

	// VolName is the Pascal-style name of the volume to open. It should be
	// one of the names returned by FPGetSrvrParms or visible to the client.
	VolName string

	// Password is an optional cleartext password for volumes that are
	// password-protected. The password is up to 8 bytes long and, if
	// shorter than 8 bytes, is padded with NULs. Comparison is
	// case-sensitive by the server.
	Password string
}

func (req *FPOpenVolReq) String() string {
	return fmt.Sprintf("FPOpenVolReq{Bitmap: %s, VolName: %q}", formatVolBitmap(req.Bitmap), req.VolName)
}

func (req *FPOpenVolReq) Unmarshal(data []byte) error {
	if len(data) < 5 {
		return fmt.Errorf("ErrParamErr")
	}
	// Command Byte is data[0], Pad is data[1]
	req.Bitmap = binary.BigEndian.Uint16(data[2:4])

	name, nameBytes := ReadPascalString(data, 4)
	if nameBytes == 0 {
		return fmt.Errorf("ErrParamErr")
	}
	req.VolName = name

	passIdx := 4 + nameBytes
	if passIdx%2 != 0 {
		passIdx++
	}
	// AFP 2.x uses a fixed-size password field (VOLPASSLEN=8) padded with NULs.
	// Some non-conformant clients/tests may send a Pascal string instead, so we fall
	// back when we don't have 8 bytes available.
	if len(data) >= passIdx+8 {
		passBytes := data[passIdx : passIdx+8]
		req.Password = string(bytes.TrimRight(passBytes, "\x00"))
	} else {
		pass, _ := ReadPascalString(data, passIdx)
		req.Password = pass
	}
	return nil
}

type FPOpenVolRes struct {
	// Bitmap echoes the request Bitmap, indicating which parameters are
	// present in the returned Data block.
	Bitmap uint16

	// Data contains the requested volume parameters. Fixed-length fields
	// (offsets) appear first and variable-length fields (e.g. volume name)
	// are concatenated after them, with offsets measured from the start of
	// the parameters block.
	Data []byte
}

func (res *FPOpenVolRes) String() string {
	return fmt.Sprintf("FPOpenVolRes{Bitmap: %s, DataLen: %d}", formatVolBitmap(res.Bitmap), len(res.Data))
}

func (res *FPOpenVolRes) WireSize() int { return 2 + len(res.Data) }

func (res *FPOpenVolRes) MarshalWire(b []byte) (int, error) {
	off := 0
	n, err := binutil.PutU16(b[off:], res.Bitmap)
	if err != nil {
		return 0, err
	}
	off += n
	if len(b[off:]) < len(res.Data) {
		return 0, binutil.ErrShortBuffer
	}
	off += copy(b[off:], res.Data)
	return off, nil
}

func (res *FPOpenVolRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}

type FPCloseVolReq struct {
	// VolumeID is the identifier of the open volume to close. This ID is
	// the value previously returned by FPOpenVol and is invalidated by a
	// matching FPCloseVol.
	VolumeID uint16
}

func (req *FPCloseVolReq) Unmarshal(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("ErrParamErr")
	}
	req.VolumeID = binary.BigEndian.Uint16(data[2:4])
	return nil
}
func (req *FPCloseVolReq) String() string {
	return fmt.Sprintf("FPCloseVolReq{VolumeID: %d}", req.VolumeID)
}

type FPCloseVolRes struct{}

func (res *FPCloseVolRes) Marshal() []byte { return nil }
func (res *FPCloseVolRes) String() string  { return "FPCloseVolRes{}" }

type FPGetVolParmsReq struct {
	// VolumeID identifies the volume (as returned by FPOpenVol) for which
	// parameters are being requested. The client must have previously
	// opened this volume.
	VolumeID uint16

	// Bitmap specifies which volume parameters the server should return.
	// This field cannot be null and maps to the VolBitmap* flags.
	Bitmap uint16
}

func (req *FPGetVolParmsReq) String() string {
	return fmt.Sprintf("FPGetVolParmsReq{VolumeID: %d, Bitmap: %s}", req.VolumeID, formatVolBitmap(req.Bitmap))
}

func (req *FPGetVolParmsReq) Unmarshal(data []byte) error {
	if len(data) < 6 {
		return fmt.Errorf("ErrParamErr")
	}
	// Cmd: 0, Pad: 1, VolumeID: 2:4, Bitmap: 4:6
	req.VolumeID = binary.BigEndian.Uint16(data[2:4])
	req.Bitmap = binary.BigEndian.Uint16(data[4:6])
	return nil
}

type FPGetVolParmsRes struct {
	// Bitmap echoes which parameters are contained in Data.
	Bitmap uint16

	// Data holds returned volume parameter values in bitmap order. Variable
	// length fields are represented by offsets in the fixed portion and
	// the actual values are appended at the end of the block.
	Data []byte
}

func (res *FPGetVolParmsRes) String() string {
	return fmt.Sprintf("FPGetVolParmsRes{Bitmap: %s, DataLen: %d}", formatVolBitmap(res.Bitmap), len(res.Data))
}

func (res *FPGetVolParmsRes) WireSize() int { return 2 + len(res.Data) }

func (res *FPGetVolParmsRes) MarshalWire(b []byte) (int, error) {
	off := 0
	n, err := binutil.PutU16(b[off:], res.Bitmap)
	if err != nil {
		return 0, err
	}
	off += n
	if len(b[off:]) < len(res.Data) {
		return 0, binutil.ErrShortBuffer
	}
	off += copy(b[off:], res.Data)
	return off, nil
}

func (res *FPGetVolParmsRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}

// FPSetVolParms - set volume parameters (AFP 2.x section 5.1.32)
// Wire format: cmd(0), pad(1), VolID(2:4), Bitmap(4:6), aint(6:10)
type FPSetVolParmsReq struct {
	VolumeID   uint16
	Bitmap     uint16
	BackupDate uint32
}

func (req *FPSetVolParmsReq) Unmarshal(data []byte) error {
	if len(data) < 10 {
		return fmt.Errorf("ErrParamErr")
	}
	req.VolumeID = binary.BigEndian.Uint16(data[2:4])
	req.Bitmap = binary.BigEndian.Uint16(data[4:6])
	req.BackupDate = binary.BigEndian.Uint32(data[6:10])
	return nil
}

func (req *FPSetVolParmsReq) String() string {
	return fmt.Sprintf("FPSetVolParmsReq{VolumeID: %d, Bitmap: %s, BackupDate: %d}", req.VolumeID, formatVolBitmap(req.Bitmap), req.BackupDate)
}

type FPSetVolParmsRes struct{}

func (res *FPSetVolParmsRes) Marshal() []byte { return nil }
func (res *FPSetVolParmsRes) String() string  { return "FPSetVolParmsRes{}" }

var (
	_ RequestModel = (*FPOpenVolReq)(nil)
	_ RequestModel = (*FPCloseVolReq)(nil)
	_ RequestModel = (*FPGetVolParmsReq)(nil)
	_ RequestModel = (*FPSetVolParmsReq)(nil)

	_ ResponseModel = (*FPOpenVolRes)(nil)
	_ ResponseModel = (*FPCloseVolRes)(nil)
	_ ResponseModel = (*FPGetVolParmsRes)(nil)
	_ ResponseModel = (*FPSetVolParmsRes)(nil)
)
