//go:build afp || all

package afp

import (
	"encoding/binary"
	"fmt"
)

// parseSetParmsPath parses the common path+params layout used by FPSetDirParms,
// FPSetFileParms, and FPSetFileDirParms:
//
//	cmd(0), pad(1), VolumeID(2:4), DirID(4:8), Bitmap(8:10),
//	PathType(10), PathLen(11), PathName(12:12+nameLen), [pad], params
//
// Returns the path name, a bitmap, the byte offset of the first param, and any error.
func parseSetParmsPath(data []byte) (volID uint16, dirID uint32, bitmap uint16, pathType uint8, path string, paramsOff int, err error) {
	if len(data) < 12 {
		err = fmt.Errorf("ErrParamErr")
		return
	}
	volID = binary.BigEndian.Uint16(data[2:4])
	dirID = binary.BigEndian.Uint32(data[4:8])
	bitmap = binary.BigEndian.Uint16(data[8:10])
	pathType = data[10]
	nameLen := int(data[11])
	if len(data) < 12+nameLen {
		err = fmt.Errorf("ErrParamErr")
		return
	}
	// Store raw bytes so resolvePath can decode MacRoman exactly once.
	path = string(data[12 : 12+nameLen])
	paramsOff = 12 + nameLen
	if nameLen%2 != 0 {
		paramsOff++ // word-align after path block
	}
	return
}
