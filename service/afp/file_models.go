//go:build afp || all

package afp

import (
	"encoding/binary"
	"fmt"
	"strings"
)

func formatFileBitmap(bitmap uint16) string {
	var flags []string
	if bitmap&FileBitmapAttributes != 0 {
		flags = append(flags, "Attributes")
	}
	if bitmap&FileBitmapParentDID != 0 {
		flags = append(flags, "ParentDID")
	}
	if bitmap&FileBitmapCreateDate != 0 {
		flags = append(flags, "CreateDate")
	}
	if bitmap&FileBitmapModDate != 0 {
		flags = append(flags, "ModDate")
	}
	if bitmap&FileBitmapBackupDate != 0 {
		flags = append(flags, "BackupDate")
	}
	if bitmap&FileBitmapFinderInfo != 0 {
		flags = append(flags, "FinderInfo")
	}
	if bitmap&FileBitmapLongName != 0 {
		flags = append(flags, "LongName")
	}
	if bitmap&FileBitmapShortName != 0 {
		flags = append(flags, "ShortName")
	}
	if bitmap&FileBitmapFileNum != 0 {
		flags = append(flags, "FileNum")
	}
	if bitmap&FileBitmapDataForkLen != 0 {
		flags = append(flags, "DataForkLen")
	}
	if bitmap&FileBitmapRsrcForkLen != 0 {
		flags = append(flags, "RsrcForkLen")
	}
	if bitmap&FileBitmapProDOSInfo != 0 {
		flags = append(flags, "ProDOSInfo")
	}
	return fmt.Sprintf("0x%04x [%s]", bitmap, strings.Join(flags, "|"))
}

// FPCreateFile request structure.
//
// Wire layout (bits): CreateFlag (8 bits), VolumeID (16), DirID (32), PathType (8), Pathname (variable).
// CreateFlag bit 7 selects hard-create (1) vs soft-create (0).
type FPCreateFileReq struct {
	// CreateFlag contains the 8-bit CreateFlag field from the wire. Bit 7
	// selects hard-create (1) vs soft-create (0). See FPCreateFileFlag* constants in types.go.
	CreateFlag uint8

	// VolumeID is the 16-bit identifier of the volume on which to create the file.
	VolumeID uint16

	// DirID is the 32-bit ancestor (parent) directory identifier for the new file.
	DirID uint32

	// PathType indicates the name encoding/format for Path: 1 for short names, 2 for long names.
	PathType uint8

	// Path is the pathname (file name) to create. This must not be empty/null.
	Path string
}

func (req *FPCreateFileReq) Unmarshal(data []byte) error {
	if len(data) < 10 {
		return fmt.Errorf("ErrParamErr")
	}
	req.CreateFlag = data[1]
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

func (req *FPCreateFileReq) String() string {
	return fmt.Sprintf("FPCreateFileReq{CreateFlag: 0x%02x, VolumeID: %d, DirID: %d, PathType: %d, Path: %q}", req.CreateFlag, req.VolumeID, req.DirID, req.PathType, req.Path)
}

// HasFlag returns true if the provided flag mask is set in the request's CreateFlag byte.
func (req *FPCreateFileReq) HasFlag(mask uint8) bool {
	return req.CreateFlag&mask != 0
}

type FPCreateFileRes struct{}

func (res *FPCreateFileRes) Marshal() []byte { return nil }
func (res *FPCreateFileRes) String() string  { return "FPCreateFileRes{}" }

// FPCopyFile - copy a file to another location, optionally renaming it (AFP 2.x section 5.1.5).
//
// Wire request:
//
//	cmd(0), pad(1), SrcVolumeID(2:4), SrcDirID(4:8), DstVolumeID(8:10), DstDirID(10:14),
//	SrcPathType(14), SrcPathLen(15), SrcName(16:16+srcLen),
//	[word-align], DstPathType, DstPathLen, DstDirName,
//	[word-align], NewPathType, NewPathLen, NewName
//
// DstDirName is the destination subdirectory path within DstDirID (may be empty).
// NewName is the filename for the copy; if empty, the source filename is used.
// Wire response: empty (NoErr on success).
type FPCopyFileReq struct {
	SrcVolumeID uint16
	SrcDirID    uint32
	DstVolumeID uint16
	DstDirID    uint32
	SrcPathType uint8
	SrcName     string
	DstPathType uint8
	DstDirName  string
	NewPathType uint8
	NewName     string
}

func (req *FPCopyFileReq) Unmarshal(data []byte) error {
	if len(data) < 16 {
		return fmt.Errorf("ErrParamErr")
	}
	req.SrcVolumeID = binary.BigEndian.Uint16(data[2:4])
	req.SrcDirID = binary.BigEndian.Uint32(data[4:8])
	req.DstVolumeID = binary.BigEndian.Uint16(data[8:10])
	req.DstDirID = binary.BigEndian.Uint32(data[10:14])
	req.SrcPathType = data[14]
	srcLen := int(data[15])
	if len(data) < 16+srcLen {
		return fmt.Errorf("ErrParamErr")
	}
	req.SrcName = string(data[16 : 16+srcLen])
	idx := 16 + srcLen
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
	req.DstDirName = string(data[idx+2 : idx+2+dstLen])
	idx += 2 + dstLen
	if dstLen%2 != 0 {
		idx++
	}
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

func (req *FPCopyFileReq) String() string {
	return fmt.Sprintf("FPCopyFileReq{SrcVol:%d SrcDir:%d DstVol:%d DstDir:%d Src:%q Dst:%q New:%q}",
		req.SrcVolumeID, req.SrcDirID, req.DstVolumeID, req.DstDirID, req.SrcName, req.DstDirName, req.NewName)
}

type FPCopyFileRes struct{}

func (res *FPCopyFileRes) Marshal() []byte { return nil }
func (res *FPCopyFileRes) String() string  { return "FPCopyFileRes{}" }

// FPSetFileParms - set file parameters (AFP 2.x section 5.1.30)
// Handles FinderInfo (bitmap bit 5); other bits are accepted but ignored.
type FPSetFileParmsReq struct {
	VolumeID   uint16
	DirID      uint32
	Bitmap     uint16
	PathType   uint8
	Path       string
	FinderInfo [32]byte
}

func (req *FPSetFileParmsReq) Unmarshal(data []byte) error {
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

func (req *FPSetFileParmsReq) String() string {
	return fmt.Sprintf("FPSetFileParmsReq{VolumeID: %d, DirID: %d, Bitmap: %s, PathType: %d, Path: %q}", req.VolumeID, req.DirID, formatFileBitmap(req.Bitmap), req.PathType, req.Path)
}

type FPSetFileParmsRes struct{}

func (res *FPSetFileParmsRes) Marshal() []byte { return nil }
func (res *FPSetFileParmsRes) String() string  { return "FPSetFileParmsRes{}" }

var (
	_ RequestModel = (*FPCreateFileReq)(nil)
	_ RequestModel = (*FPCopyFileReq)(nil)
	_ RequestModel = (*FPSetFileParmsReq)(nil)

	_ ResponseModel = (*FPCreateFileRes)(nil)
	_ ResponseModel = (*FPCopyFileRes)(nil)
	_ ResponseModel = (*FPSetFileParmsRes)(nil)
)
