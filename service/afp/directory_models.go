//go:build afp || all

package afp

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/pgodw/omnitalk/pkg/binutil"
)

func formatDirBitmap(bitmap uint16) string {
	var flags []string
	if bitmap&DirBitmapAttributes != 0 {
		flags = append(flags, "Attributes")
	}
	if bitmap&DirBitmapParentDID != 0 {
		flags = append(flags, "ParentDID")
	}
	if bitmap&DirBitmapCreateDate != 0 {
		flags = append(flags, "CreateDate")
	}
	if bitmap&DirBitmapModDate != 0 {
		flags = append(flags, "ModDate")
	}
	if bitmap&DirBitmapBackupDate != 0 {
		flags = append(flags, "BackupDate")
	}
	if bitmap&DirBitmapFinderInfo != 0 {
		flags = append(flags, "FinderInfo")
	}
	if bitmap&DirBitmapLongName != 0 {
		flags = append(flags, "LongName")
	}
	if bitmap&DirBitmapShortName != 0 {
		flags = append(flags, "ShortName")
	}
	if bitmap&DirBitmapDirID != 0 {
		flags = append(flags, "DirID")
	}
	if bitmap&DirBitmapOffspringCount != 0 {
		flags = append(flags, "OffspringCount")
	}
	if bitmap&DirBitmapOwnerID != 0 {
		flags = append(flags, "OwnerID")
	}
	if bitmap&DirBitmapGroupID != 0 {
		flags = append(flags, "GroupID")
	}
	if bitmap&DirBitmapAccessRights != 0 {
		flags = append(flags, "AccessRights")
	}
	if bitmap&DirBitmapProDOSInfo != 0 {
		flags = append(flags, "ProDOSInfo")
	}
	return fmt.Sprintf("0x%04x [%s]", bitmap, strings.Join(flags, "|"))
}

type FPCloseDirReq struct {
	VolumeID uint16
	DirID    uint32
}

func (req *FPCloseDirReq) Unmarshal(data []byte) error {
	if len(data) < 8 {
		return fmt.Errorf("ErrParamErr")
	}
	req.VolumeID = binary.BigEndian.Uint16(data[2:4])
	req.DirID = binary.BigEndian.Uint32(data[4:8])
	return nil
}
func (req *FPCloseDirReq) String() string {
	return fmt.Sprintf("FPCloseDirReq{VolumeID: %d, DirID: %d}", req.VolumeID, req.DirID)
}

type FPCloseDirRes struct{}

func (res *FPCloseDirRes) Marshal() []byte { return nil }
func (res *FPCloseDirRes) String() string  { return "FPCloseDirRes{}" }

type FPOpenDirReq struct {
	VolumeID uint16
	DirID    uint32
	PathType uint8
	Path     string
}

func (req *FPOpenDirReq) String() string {
	return fmt.Sprintf("FPOpenDirReq{VolumeID: %d, DirID: %d, PathType: %d, Path: %q}", req.VolumeID, req.DirID, req.PathType, req.Path)
}

func (req *FPOpenDirReq) Unmarshal(data []byte) error {
	if len(data) < 10 {
		return fmt.Errorf("ErrParamErr")
	}
	req.VolumeID = binary.BigEndian.Uint16(data[2:4])
	req.DirID = binary.BigEndian.Uint32(data[4:8])
	req.PathType = data[8]
	pathLen := int(data[9])
	if len(data) < 10+pathLen {
		return fmt.Errorf("ErrParamErr")
	}
	req.Path = string(data[10 : 10+pathLen])
	return nil
}

type FPOpenDirRes struct {
	DirID uint32
}

func (res *FPOpenDirRes) String() string {
	return fmt.Sprintf("FPOpenDirRes{DirID: %d}", res.DirID)
}

func (res *FPOpenDirRes) WireSize() int { return 4 }

func (res *FPOpenDirRes) MarshalWire(b []byte) (int, error) {
	return binutil.PutU32(b, res.DirID)
}

func (res *FPOpenDirRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}

type FPEnumerateReq struct {
	VolumeID   uint16
	DirID      uint32
	FileBitmap uint16
	DirBitmap  uint16
	ReqCount   uint16
	StartIndex uint16
	MaxReply   uint32
	PathType   uint8
	Path       string
}

func (req *FPEnumerateReq) String() string {
	return fmt.Sprintf("FPEnumerateReq{VolumeID: %d, DirID: %d, FileBitmap: %s, DirBitmap: %s, ReqCount: %d, StartIndex: %d, MaxReply: %d, PathType: %d, Path: %q}", req.VolumeID, req.DirID, formatFileBitmap(req.FileBitmap), formatDirBitmap(req.DirBitmap), req.ReqCount, req.StartIndex, req.MaxReply, req.PathType, req.Path)
}

func (req *FPEnumerateReq) Unmarshal(data []byte) error {
	if len(data) < 18 {
		return fmt.Errorf("ErrParamErr")
	}
	req.VolumeID = binary.BigEndian.Uint16(data[2:4])
	req.DirID = binary.BigEndian.Uint32(data[4:8])
	req.FileBitmap = binary.BigEndian.Uint16(data[8:10])
	req.DirBitmap = binary.BigEndian.Uint16(data[10:12])
	req.ReqCount = binary.BigEndian.Uint16(data[12:14])
	req.StartIndex = binary.BigEndian.Uint16(data[14:16])
	// AFP 2.x MaxReplySize is always 2 bytes (AFP 3.x uses 4 bytes over DSI).
	req.MaxReply = uint32(binary.BigEndian.Uint16(data[16:18]))
	// Parse optional path (PathType at [18], PathLen at [19], PathName at [20:]).
	if len(data) >= 20 {
		req.PathType = data[18]
		pathLen := int(data[19])
		if len(data) >= 20+pathLen {
			req.Path = string(data[20 : 20+pathLen])
		}
	}
	return nil
}

type FPEnumerateRes struct {
	FileBitmap uint16
	DirBitmap  uint16
	ActCount   uint16
	Data       []byte
}

func (res *FPEnumerateRes) String() string {
	return fmt.Sprintf("FPEnumerateRes{FileBitmap: %s, DirBitmap: %s, ActCount: %d, DataLen: %d}", formatFileBitmap(res.FileBitmap), formatDirBitmap(res.DirBitmap), res.ActCount, len(res.Data))
}

func (res *FPEnumerateRes) WireSize() int { return 6 + len(res.Data) }

func (res *FPEnumerateRes) MarshalWire(b []byte) (int, error) {
	off := 0
	n, err := binutil.PutU16(b[off:], res.FileBitmap)
	if err != nil {
		return 0, err
	}
	off += n
	n, err = binutil.PutU16(b[off:], res.DirBitmap)
	if err != nil {
		return 0, err
	}
	off += n
	n, err = binutil.PutU16(b[off:], res.ActCount)
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

func (res *FPEnumerateRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}

// FPCreateDir - cmd(0), pad(1), VolumeID(2:4), DirID(4:8), PathType(8), PathLen(9), PathName(10:...)
type FPCreateDirReq struct {
	VolumeID uint16
	DirID    uint32
	PathType uint8
	Path     string
}

func (req *FPCreateDirReq) Unmarshal(data []byte) error {
	if len(data) < 10 {
		return fmt.Errorf("ErrParamErr")
	}
	req.VolumeID = binary.BigEndian.Uint16(data[2:4])
	req.DirID = binary.BigEndian.Uint32(data[4:8])
	req.PathType = data[8]
	nameLen := int(data[9])
	if len(data) < 10+nameLen {
		return fmt.Errorf("ErrParamErr")
	}
	req.Path = string(data[10 : 10+nameLen])
	return nil
}
func (req *FPCreateDirReq) String() string {
	return fmt.Sprintf("FPCreateDirReq{VolumeID: %d, DirID: %d, PathType: %d, Path: %q}", req.VolumeID, req.DirID, req.PathType, req.Path)
}

type FPCreateDirRes struct {
	DirID uint32
}

func (res *FPCreateDirRes) WireSize() int { return 4 }

func (res *FPCreateDirRes) MarshalWire(b []byte) (int, error) {
	return binutil.PutU32(b, res.DirID)
}

func (res *FPCreateDirRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}
func (res *FPCreateDirRes) String() string {
	return fmt.Sprintf("FPCreateDirRes{DirID: %d}", res.DirID)
}

// FPSetDirParms - set directory parameters (AFP 2.x section 5.1.29)
// Handles FinderInfo (bitmap bit 5); other bits are accepted but ignored.
type FPSetDirParmsReq struct {
	VolumeID   uint16
	DirID      uint32
	Bitmap     uint16
	PathType   uint8
	Path       string
	FinderInfo [32]byte
}

func (req *FPSetDirParmsReq) Unmarshal(data []byte) error {
	volID, dirID, bitmap, pathType, path, paramsOff, err := parseSetParmsPath(data)
	if err != nil {
		return err
	}
	req.VolumeID, req.DirID, req.Bitmap, req.PathType, req.Path = volID, dirID, bitmap, pathType, path

	// Walk bitmap fields in bit order to find FinderInfo's actual offset.
	// Fields before DirBitmapFinderInfo (bit 5), in bit order:
	off := paramsOff
	if bitmap&DirBitmapAttributes != 0 {
		off += 2 // uint16 Attributes
	}
	if bitmap&DirBitmapParentDID != 0 {
		off += 4 // uint32 ParentDirID
	}
	if bitmap&DirBitmapCreateDate != 0 {
		off += 4 // uint32 CreateDate
	}
	if bitmap&DirBitmapModDate != 0 {
		off += 4 // uint32 ModDate
	}
	if bitmap&DirBitmapBackupDate != 0 {
		off += 4 // uint32 BackupDate
	}
	if bitmap&DirBitmapFinderInfo != 0 {
		if len(data) < off+32 {
			return fmt.Errorf("ErrParamErr")
		}
		copy(req.FinderInfo[:], data[off:off+32])
	}
	return nil
}
func (req *FPSetDirParmsReq) String() string {
	return fmt.Sprintf("FPSetDirParmsReq{VolumeID: %d, DirID: %d, Bitmap: %s, PathType: %d, Path: %q}", req.VolumeID, req.DirID, formatDirBitmap(req.Bitmap), req.PathType, req.Path)
}

type FPSetDirParmsRes struct{}

func (res *FPSetDirParmsRes) Marshal() []byte { return nil }
func (res *FPSetDirParmsRes) String() string  { return "FPSetDirParmsRes{}" }

var (
	_ RequestModel = (*FPCloseDirReq)(nil)
	_ RequestModel = (*FPOpenDirReq)(nil)
	_ RequestModel = (*FPEnumerateReq)(nil)
	_ RequestModel = (*FPCreateDirReq)(nil)
	_ RequestModel = (*FPSetDirParmsReq)(nil)

	_ ResponseModel = (*FPCloseDirRes)(nil)
	_ ResponseModel = (*FPOpenDirRes)(nil)
	_ ResponseModel = (*FPEnumerateRes)(nil)
	_ ResponseModel = (*FPCreateDirRes)(nil)
	_ ResponseModel = (*FPSetDirParmsRes)(nil)
)
