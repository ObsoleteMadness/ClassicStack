//go:build afp || all

package afp

import (
	"encoding/binary"
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/pkg/binutil"
)

type FPGetFileDirParmsReq struct {
	VolumeID   uint16
	DirID      uint32
	FileBitmap uint16
	DirBitmap  uint16
	PathType   uint8
	Path       string
}

func (req *FPGetFileDirParmsReq) String() string {
	return fmt.Sprintf("FPGetFileDirParmsReq{VolumeID: %d, DirID: %d, FileBitmap: %s, DirBitmap: %s, PathType: %d, Path: %q}", req.VolumeID, req.DirID, formatFileBitmap(req.FileBitmap), formatDirBitmap(req.DirBitmap), req.PathType, req.Path)
}

func (req *FPGetFileDirParmsReq) Unmarshal(data []byte) error {
	if len(data) < 14 {
		return fmt.Errorf("ErrParamErr")
	}
	req.VolumeID = binary.BigEndian.Uint16(data[2:4])
	req.DirID = binary.BigEndian.Uint32(data[4:8])
	req.FileBitmap = binary.BigEndian.Uint16(data[8:10])
	req.DirBitmap = binary.BigEndian.Uint16(data[10:12])
	req.PathType = data[12]
	pathLen := int(data[13])
	if len(data) < 14+pathLen {
		return fmt.Errorf("ErrParamErr")
	}
	req.Path = string(data[14 : 14+pathLen])
	return nil
}

type FPGetFileDirParmsRes struct {
	FileBitmap uint16
	DirBitmap  uint16
	IsFile     bool
	Data       []byte
}

func (res *FPGetFileDirParmsRes) String() string {
	return fmt.Sprintf("FPGetFileDirParmsRes{FileBitmap: %s, DirBitmap: %s, IsFile: %t, DataLen: %d}", formatFileBitmap(res.FileBitmap), formatDirBitmap(res.DirBitmap), res.IsFile, len(res.Data))
}

func (res *FPGetFileDirParmsRes) WireSize() int { return 6 + len(res.Data) }

func (res *FPGetFileDirParmsRes) MarshalWire(b []byte) (int, error) {
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
	flag := byte(0x80)
	if res.IsFile {
		flag = 0x00
	}
	n, err = binutil.PutU8(b[off:], flag)
	if err != nil {
		return 0, err
	}
	off += n
	n, err = binutil.PutU8(b[off:], 0x00)
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

func (res *FPGetFileDirParmsRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}

// FPMoveAndRename - atomically move and/or rename a file or directory (AFP 2.x section 5.1.23).
//
// Wire request:
//
//	cmd(0), pad(1), VolumeID(2:4), SrcDirID(4:8), DstDirID(8:12),
//	SrcPathType(12), SrcPathLen(13), SrcName(14:14+srcLen),
//	[word-align], DstPathType, DstPathLen, DstDirName,
//	[word-align], NewPathType, NewPathLen, NewName
//
// DstDirName is the destination subdirectory within DstDirID (may be empty, meaning DstDirID itself).
// NewName is the new filename; if empty, the source filename is preserved.
// Wire response: empty (NoErr on success).
type FPMoveAndRenameReq struct {
	VolumeID    uint16
	SrcDirID    uint32
	DstDirID    uint32
	SrcPathType uint8
	SrcName     string
	DstPathType uint8
	DstDirName  string
	NewPathType uint8
	NewName     string
}

func (req *FPMoveAndRenameReq) Unmarshal(data []byte) error {
	if len(data) < 14 {
		return fmt.Errorf("ErrParamErr")
	}
	req.VolumeID = binary.BigEndian.Uint16(data[2:4])
	req.SrcDirID = binary.BigEndian.Uint32(data[4:8])
	req.DstDirID = binary.BigEndian.Uint32(data[8:12])
	req.SrcPathType = data[12]
	srcLen := int(data[13])
	if len(data) < 14+srcLen {
		return fmt.Errorf("ErrParamErr")
	}
	req.SrcName = string(data[14 : 14+srcLen])
	idx := 14 + srcLen
	if idx+2 > len(data) {
		return nil
	}
	req.DstPathType = data[idx]
	dstLen := int(data[idx+1])
	if idx+2+dstLen > len(data) {
		return nil
	}
	req.DstDirName = string(data[idx+2 : idx+2+dstLen])
	idx += 2 + dstLen
	if idx+2 > len(data) {
		return nil
	}
	req.NewPathType = data[idx]
	newLen := int(data[idx+1])
	if idx+2+newLen > len(data) {
		return nil
	}
	req.NewName = string(data[idx+2 : idx+2+newLen])
	return nil
}

func (req *FPMoveAndRenameReq) String() string {
	return fmt.Sprintf("FPMoveAndRenameReq{Vol:%d SrcDir:%d DstDir:%d Src:%q DstDir:%q New:%q}",
		req.VolumeID, req.SrcDirID, req.DstDirID, req.SrcName, req.DstDirName, req.NewName)
}

type FPMoveAndRenameRes struct{}

func (res *FPMoveAndRenameRes) Marshal() []byte { return nil }
func (res *FPMoveAndRenameRes) String() string  { return "FPMoveAndRenameRes{}" }

// FPSetFileDirParms - set file or directory parameters (AFP 2.x section 5.1.35)
// Same wire format as FPSetDirParms/FPSetFileParms; handles FinderInfo.
type FPSetFileDirParmsReq struct {
	VolumeID   uint16
	DirID      uint32
	Bitmap     uint16
	PathType   uint8
	Path       string
	FinderInfo [32]byte
}

func (req *FPSetFileDirParmsReq) Unmarshal(data []byte) error {
	volID, dirID, bitmap, pathType, path, paramsOff, err := parseSetParmsPath(data)
	if err != nil {
		return err
	}
	req.VolumeID, req.DirID, req.Bitmap, req.PathType, req.Path = volID, dirID, bitmap, pathType, path

	off := paramsOff
	if bitmap&FileBitmapAttributes != 0 {
		off += 2
	}
	if bitmap&FileBitmapParentDID != 0 {
		off += 4
	}
	if bitmap&FileBitmapCreateDate != 0 {
		off += 4
	}
	if bitmap&FileBitmapModDate != 0 {
		off += 4
	}
	if bitmap&FileBitmapBackupDate != 0 {
		off += 4
	}
	if bitmap&FileBitmapFinderInfo != 0 {
		if len(data) < off+32 {
			return fmt.Errorf("ErrParamErr")
		}
		copy(req.FinderInfo[:], data[off:off+32])
	}
	return nil
}

func (req *FPSetFileDirParmsReq) String() string {
	return fmt.Sprintf("FPSetFileDirParmsReq{VolumeID: %d, DirID: %d, Bitmap: %s, PathType: %d, Path: %q}", req.VolumeID, req.DirID, formatFileBitmap(req.Bitmap), req.PathType, req.Path)
}

type FPSetFileDirParmsRes struct{}

func (res *FPSetFileDirParmsRes) Marshal() []byte { return nil }
func (res *FPSetFileDirParmsRes) String() string  { return "FPSetFileDirParmsRes{}" }

// FPRename - cmd(0), pad(1), VolumeID(2:4), DirID(4:8), old path then new path.
type FPRenameReq struct {
	VolumeID    uint16
	DirID       uint32
	PathType    uint8
	Name        string
	NewPathType uint8
	NewName     string
}

func (req *FPRenameReq) Unmarshal(data []byte) error {
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
	req.Name = string(data[10 : 10+nameLen])
	newNameIdx := 10 + nameLen
	if len(data) < newNameIdx+2 {
		return fmt.Errorf("ErrParamErr")
	}
	req.NewPathType = data[newNameIdx]
	newNameLen := int(data[newNameIdx+1])
	if len(data) < newNameIdx+2+newNameLen {
		return fmt.Errorf("ErrParamErr")
	}
	req.NewName = string(data[newNameIdx+2 : newNameIdx+2+newNameLen])
	return nil
}

func (req *FPRenameReq) String() string {
	return fmt.Sprintf("FPRenameReq{VolumeID: %d, DirID: %d, PathType: %d, Name: %q, NewName: %q}", req.VolumeID, req.DirID, req.PathType, req.Name, req.NewName)
}

type FPRenameRes struct{}

func (res *FPRenameRes) Marshal() []byte { return nil }
func (res *FPRenameRes) String() string  { return "FPRenameRes{}" }

// FPDelete - cmd(0), pad(1), VolumeID(2:4), DirID(4:8), PathType(8), PathLen(9), PathName(10:...)
type FPDeleteReq struct {
	VolumeID uint16
	DirID    uint32
	PathType uint8
	Path     string
}

func (req *FPDeleteReq) Unmarshal(data []byte) error {
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

func (req *FPDeleteReq) String() string {
	return fmt.Sprintf("FPDeleteReq{VolumeID: %d, DirID: %d, PathType: %d, Path: %q}", req.VolumeID, req.DirID, req.PathType, req.Path)
}

type FPDeleteRes struct{}

func (res *FPDeleteRes) Marshal() []byte { return nil }
func (res *FPDeleteRes) String() string  { return "FPDeleteRes{}" }

// FPGetDirParms - cmd(0), pad(1), VolumeID(2:4), DirID(4:8), Bitmap(8:10), PathType(10), PathLen(11), PathName(12:...)
type FPGetDirParmsReq struct {
	VolumeID uint16
	DirID    uint32
	Bitmap   uint16
	PathType uint8
	Path     string
}

func (req *FPGetDirParmsReq) Unmarshal(data []byte) error {
	if len(data) < 12 {
		return fmt.Errorf("ErrParamErr")
	}
	req.VolumeID = binary.BigEndian.Uint16(data[2:4])
	req.DirID = binary.BigEndian.Uint32(data[4:8])
	req.Bitmap = binary.BigEndian.Uint16(data[8:10])
	req.PathType = data[10]
	nameLen := int(data[11])
	if len(data) < 12+nameLen {
		return fmt.Errorf("ErrParamErr")
	}
	req.Path = string(data[12 : 12+nameLen])
	return nil
}

func (req *FPGetDirParmsReq) String() string {
	return fmt.Sprintf("FPGetDirParmsReq{VolumeID: %d, DirID: %d, Bitmap: %s, PathType: %d, Path: %q}", req.VolumeID, req.DirID, formatDirBitmap(req.Bitmap), req.PathType, req.Path)
}

type FPGetDirParmsRes struct {
	Bitmap uint16
	Data   []byte
}

func (res *FPGetDirParmsRes) WireSize() int { return 4 + len(res.Data) }

func (res *FPGetDirParmsRes) MarshalWire(b []byte) (int, error) {
	off := 0
	n, err := binutil.PutU16(b[off:], res.Bitmap)
	if err != nil {
		return 0, err
	}
	off += n
	n, err = binutil.PutU8(b[off:], 0x80)
	if err != nil {
		return 0, err
	}
	off += n
	n, err = binutil.PutU8(b[off:], 0x00)
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

func (res *FPGetDirParmsRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}

func (res *FPGetDirParmsRes) String() string {
	return fmt.Sprintf("FPGetDirParmsRes{Bitmap: %s, DataLen: %d}", formatDirBitmap(res.Bitmap), len(res.Data))
}

// FPGetFileParms - cmd(0), pad(1), VolumeID(2:4), DirID(4:8), Bitmap(8:10), PathType(10), PathLen(11), PathName(12:...)
type FPGetFileParmsReq struct {
	VolumeID uint16
	DirID    uint32
	Bitmap   uint16
	PathType uint8
	Path     string
}

func (req *FPGetFileParmsReq) Unmarshal(data []byte) error {
	if len(data) < 12 {
		return fmt.Errorf("ErrParamErr")
	}
	req.VolumeID = binary.BigEndian.Uint16(data[2:4])
	req.DirID = binary.BigEndian.Uint32(data[4:8])
	req.Bitmap = binary.BigEndian.Uint16(data[8:10])
	req.PathType = data[10]
	nameLen := int(data[11])
	if len(data) < 12+nameLen {
		return fmt.Errorf("ErrParamErr")
	}
	req.Path = string(data[12 : 12+nameLen])
	return nil
}

func (req *FPGetFileParmsReq) String() string {
	return fmt.Sprintf("FPGetFileParmsReq{VolumeID: %d, DirID: %d, Bitmap: %s, PathType: %d, Path: %q}", req.VolumeID, req.DirID, formatFileBitmap(req.Bitmap), req.PathType, req.Path)
}

type FPGetFileParmsRes struct {
	Bitmap uint16
	Data   []byte
}

func (res *FPGetFileParmsRes) WireSize() int { return 4 + len(res.Data) }

func (res *FPGetFileParmsRes) MarshalWire(b []byte) (int, error) {
	off := 0
	n, err := binutil.PutU16(b[off:], res.Bitmap)
	if err != nil {
		return 0, err
	}
	off += n
	n, err = binutil.PutU8(b[off:], 0x00)
	if err != nil {
		return 0, err
	}
	off += n
	n, err = binutil.PutU8(b[off:], 0x00)
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

func (res *FPGetFileParmsRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}

func (res *FPGetFileParmsRes) String() string {
	return fmt.Sprintf("FPGetFileParmsRes{Bitmap: %s, DataLen: %d}", formatFileBitmap(res.Bitmap), len(res.Data))
}

// FPExchangeFiles - swap the data/resource forks and Finder info of two files.
type FPExchangeFilesReq struct {
	VolumeID    uint16
	SrcDirID    uint32
	DstDirID    uint32
	SrcPathType uint8
	SrcName     string
	DstPathType uint8
	DstName     string
}

func (req *FPExchangeFilesReq) Unmarshal(data []byte) error {
	if len(data) < 14 {
		return fmt.Errorf("ErrParamErr")
	}
	req.VolumeID = binary.BigEndian.Uint16(data[2:4])
	req.SrcDirID = binary.BigEndian.Uint32(data[4:8])
	req.DstDirID = binary.BigEndian.Uint32(data[8:12])
	req.SrcPathType = data[12]
	srcLen := int(data[13])
	if len(data) < 14+srcLen {
		return fmt.Errorf("ErrParamErr")
	}
	req.SrcName = string(data[14 : 14+srcLen])
	idx := 14 + srcLen
	if srcLen%2 != 0 {
		idx++
	}
	if idx+2 > len(data) {
		return nil
	}
	req.DstPathType = data[idx]
	dstLen := int(data[idx+1])
	if idx+2+dstLen > len(data) {
		return nil
	}
	req.DstName = string(data[idx+2 : idx+2+dstLen])
	return nil
}

func (req *FPExchangeFilesReq) String() string {
	return fmt.Sprintf("FPExchangeFilesReq{Vol:%d SrcDir:%d DstDir:%d Src:%q Dst:%q}",
		req.VolumeID, req.SrcDirID, req.DstDirID, req.SrcName, req.DstName)
}

type FPExchangeFilesRes struct{}

func (res *FPExchangeFilesRes) Marshal() []byte { return nil }
func (res *FPExchangeFilesRes) String() string  { return "FPExchangeFilesRes{}" }

var (
	_ RequestModel = (*FPGetFileDirParmsReq)(nil)
	_ RequestModel = (*FPMoveAndRenameReq)(nil)
	_ RequestModel = (*FPSetFileDirParmsReq)(nil)
	_ RequestModel = (*FPRenameReq)(nil)
	_ RequestModel = (*FPDeleteReq)(nil)
	_ RequestModel = (*FPGetDirParmsReq)(nil)
	_ RequestModel = (*FPGetFileParmsReq)(nil)
	_ RequestModel = (*FPExchangeFilesReq)(nil)

	_ ResponseModel = (*FPGetFileDirParmsRes)(nil)
	_ ResponseModel = (*FPMoveAndRenameRes)(nil)
	_ ResponseModel = (*FPSetFileDirParmsRes)(nil)
	_ ResponseModel = (*FPRenameRes)(nil)
	_ ResponseModel = (*FPDeleteRes)(nil)
	_ ResponseModel = (*FPGetDirParmsRes)(nil)
	_ ResponseModel = (*FPGetFileParmsRes)(nil)
	_ ResponseModel = (*FPExchangeFilesRes)(nil)
)
