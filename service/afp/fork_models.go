//go:build afp

package afp

import (
	"encoding/binary"
	"fmt"

	"github.com/pgodw/omnitalk/pkg/binutil"
)

// Fork type constants for FPOpenFork.
const (
	ForkData     = uint8(0x00)
	ForkResource = uint8(0x80)
)

type FPOpenForkReq struct {
	Fork       uint8
	VolumeID   uint16
	DirID      uint32
	Bitmap     uint16
	AccessMode uint16
	PathType   uint8
	Path       string
}

func (req *FPOpenForkReq) String() string {
	return fmt.Sprintf("FPOpenForkReq{Fork: %d, VolumeID: %d, DirID: %d, Bitmap: %s, AccessMode: %d, PathType: %d, Path: %q}", req.Fork, req.VolumeID, req.DirID, formatFileBitmap(req.Bitmap), req.AccessMode, req.PathType, req.Path)
}

func (req *FPOpenForkReq) Unmarshal(data []byte) error {
	if len(data) < 14 {
		return fmt.Errorf("ErrParamErr")
	}
	req.Fork = data[1]
	req.VolumeID = binary.BigEndian.Uint16(data[2:4])
	req.DirID = binary.BigEndian.Uint32(data[4:8])
	req.Bitmap = binary.BigEndian.Uint16(data[8:10])
	req.AccessMode = binary.BigEndian.Uint16(data[10:12])
	req.PathType = data[12]
	pathLen := int(data[13])
	if len(data) < 14+pathLen {
		return fmt.Errorf("ErrParamErr")
	}
	req.Path = string(data[14 : 14+pathLen])
	return nil
}

type FPOpenForkRes struct {
	Bitmap uint16
	ForkID uint16
	Data   []byte
}

func (res *FPOpenForkRes) String() string {
	return fmt.Sprintf("FPOpenForkRes{ForkID: %d, Bitmap: %s, DataLen: %d}", res.ForkID, formatFileBitmap(res.Bitmap), len(res.Data))
}

func (res *FPOpenForkRes) WireSize() int { return 4 + len(res.Data) }

func (res *FPOpenForkRes) MarshalWire(b []byte) (int, error) {
	off := 0
	n, err := binutil.PutU16(b[off:], res.Bitmap)
	if err != nil {
		return 0, err
	}
	off += n
	n, err = binutil.PutU16(b[off:], res.ForkID)
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

func (res *FPOpenForkRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}

type FPReadReq struct {
	ForkID   uint16
	Offset   int64
	ReqCount int
}

func (req *FPReadReq) Unmarshal(data []byte) error {
	if len(data) < 13 {
		return fmt.Errorf("ErrParamErr")
	}
	req.ForkID = binary.BigEndian.Uint16(data[2:4])
	req.Offset = int64(int32(binary.BigEndian.Uint32(data[4:8])))
	req.ReqCount = int(int32(binary.BigEndian.Uint32(data[8:12])))
	return nil
}

func (req *FPReadReq) String() string {
	return fmt.Sprintf("FPReadReq{ForkID: %d, Offset: %d, ReqCount: %d}", req.ForkID, req.Offset, req.ReqCount)
}

type FPReadRes struct {
	Data []byte
}

func (res *FPReadRes) Marshal() []byte {
	return res.Data
}

func (res *FPReadRes) String() string {
	return fmt.Sprintf("FPReadRes{DataLen: %d}", len(res.Data))
}

type FPWriteReq struct {
	FromEnd   bool
	ForkID    uint16
	Offset    int64
	ReqCount  uint32
	WriteData []byte
}

func (req *FPWriteReq) Unmarshal(data []byte) error {
	if len(data) < 12 {
		return fmt.Errorf("ErrParamErr")
	}
	req.FromEnd = (data[1] & 0x80) != 0
	req.ForkID = binary.BigEndian.Uint16(data[2:4])
	req.Offset = int64(int32(binary.BigEndian.Uint32(data[4:8])))
	req.ReqCount = binary.BigEndian.Uint32(data[8:12])
	available := len(data) - 12
	writeCount := int(req.ReqCount)
	if writeCount > available {
		writeCount = available
	}
	req.WriteData = data[12 : 12+writeCount]
	return nil
}

func (req *FPWriteReq) String() string {
	return fmt.Sprintf("FPWriteReq{ForkID: %d, Offset: %d, FromEnd: %t, ReqCount: %d, DataLen: %d}", req.ForkID, req.Offset, req.FromEnd, req.ReqCount, len(req.WriteData))
}

type FPWriteRes struct {
	LastWritten int64
}

func (res *FPWriteRes) WireSize() int { return 4 }

func (res *FPWriteRes) MarshalWire(b []byte) (int, error) {
	return binutil.PutU32(b, uint32(int32(res.LastWritten)))
}

func (res *FPWriteRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}

func (res *FPWriteRes) String() string {
	return fmt.Sprintf("FPWriteRes{LastWritten: %d}", res.LastWritten)
}

type FPCloseForkReq struct {
	OForkRefNum uint16
}

func (req *FPCloseForkReq) Unmarshal(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("ErrParamErr")
	}
	req.OForkRefNum = binary.BigEndian.Uint16(data[2:4])
	return nil
}

func (req *FPCloseForkReq) String() string {
	return fmt.Sprintf("FPCloseForkReq{OForkRefNum: %d}", req.OForkRefNum)
}

type FPCloseForkRes struct{}

func (res *FPCloseForkRes) Marshal() []byte { return nil }
func (res *FPCloseForkRes) String() string  { return "FPCloseForkRes{}" }

type FPFlushForkReq struct {
	OForkRefNum uint16
}

func (req *FPFlushForkReq) Unmarshal(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("ErrParamErr")
	}
	req.OForkRefNum = binary.BigEndian.Uint16(data[2:4])
	return nil
}

func (req *FPFlushForkReq) String() string {
	return fmt.Sprintf("FPFlushForkReq{OForkRefNum: %d}", req.OForkRefNum)
}

type FPFlushForkRes struct{}

func (res *FPFlushForkRes) Marshal() []byte { return nil }
func (res *FPFlushForkRes) String() string  { return "FPFlushForkRes{}" }

type FPFlushReq struct {
	VolumeID uint16
}

func (req *FPFlushReq) Unmarshal(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("ErrParamErr")
	}
	req.VolumeID = binary.BigEndian.Uint16(data[2:4])
	return nil
}

func (req *FPFlushReq) String() string { return fmt.Sprintf("FPFlushReq{VolumeID: %d}", req.VolumeID) }

type FPFlushRes struct{}

func (res *FPFlushRes) Marshal() []byte { return nil }
func (res *FPFlushRes) String() string  { return "FPFlushRes{}" }

// FPByteRangeLock - byte-range locking for concurrent file access (AFP 2.x section 5.1.1).
// Request: cmd(0), flags(1), forkRef(2:4), offset(4:8), length(8:12)
// Reply: offset(4:8)
type FPByteRangeLockReq struct {
	FromEnd bool
	Unlock  bool
	ForkID  uint16
	Offset  int64
	Length  int64
}

func (req *FPByteRangeLockReq) Unmarshal(data []byte) error {
	if len(data) < 12 {
		return fmt.Errorf("ErrParamErr")
	}
	flags := data[1]
	req.FromEnd = (flags & 0x80) != 0
	req.Unlock = (flags & 0x01) != 0
	req.ForkID = binary.BigEndian.Uint16(data[2:4])
	req.Offset = int64(int32(binary.BigEndian.Uint32(data[4:8])))
	req.Length = int64(int32(binary.BigEndian.Uint32(data[8:12])))
	return nil
}

func (req *FPByteRangeLockReq) String() string {
	return fmt.Sprintf("FPByteRangeLockReq{ForkID:%d, FromEnd:%t, Unlock:%t, Offset:%d, Length:%d}", req.ForkID, req.FromEnd, req.Unlock, req.Offset, req.Length)
}

type FPByteRangeLockRes struct {
	Offset int64
}

func (res *FPByteRangeLockRes) WireSize() int { return 4 }

func (res *FPByteRangeLockRes) MarshalWire(b []byte) (int, error) {
	return binutil.PutU32(b, uint32(int32(res.Offset)))
}

func (res *FPByteRangeLockRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}

func (res *FPByteRangeLockRes) String() string {
	return fmt.Sprintf("FPByteRangeLockRes{Offset:%d}", res.Offset)
}

// FPGetForkParms - cmd(0), pad(1), OForkRefNum(2:4), Bitmap(4:6)
// Bitmap uses FileBitmapDataForkLen (bit 9) and FileBitmapRsrcForkLen (bit 10).
type FPGetForkParmsReq struct {
	OForkRefNum uint16
	Bitmap      uint16
}

func (req *FPGetForkParmsReq) Unmarshal(data []byte) error {
	if len(data) < 6 {
		return fmt.Errorf("ErrParamErr")
	}
	req.OForkRefNum = binary.BigEndian.Uint16(data[2:4])
	req.Bitmap = binary.BigEndian.Uint16(data[4:6])
	return nil
}

func (req *FPGetForkParmsReq) String() string {
	return fmt.Sprintf("FPGetForkParmsReq{OForkRefNum: %d, Bitmap: %s}", req.OForkRefNum, formatFileBitmap(req.Bitmap))
}

type FPGetForkParmsRes struct {
	Bitmap uint16
	Data   []byte
}

func (res *FPGetForkParmsRes) WireSize() int { return 2 + len(res.Data) }

func (res *FPGetForkParmsRes) MarshalWire(b []byte) (int, error) {
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

func (res *FPGetForkParmsRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}

func (res *FPGetForkParmsRes) String() string {
	return fmt.Sprintf("FPGetForkParmsRes{Bitmap: %s, DataLen: %d}", formatFileBitmap(res.Bitmap), len(res.Data))
}

// FPSetForkParms - set open-fork parameters (AFP 2.x section 5.1.31)
// cmd(0), pad(1), OForkRefNum(2:4), Bitmap(4:6), DataForkLen(6:10 if bit9), RsrcForkLen(next 4 if bit10)
type FPSetForkParmsReq struct {
	OForkRefNum uint16
	Bitmap      uint16
	DataForkLen int64
	RsrcForkLen int64
}

func (req *FPSetForkParmsReq) Unmarshal(data []byte) error {
	if len(data) < 6 {
		return fmt.Errorf("ErrParamErr")
	}
	req.OForkRefNum = binary.BigEndian.Uint16(data[2:4])
	req.Bitmap = binary.BigEndian.Uint16(data[4:6])
	off := 6
	if req.Bitmap&FileBitmapDataForkLen != 0 {
		if len(data) < off+4 {
			return fmt.Errorf("ErrParamErr")
		}
		req.DataForkLen = int64(int32(binary.BigEndian.Uint32(data[off : off+4])))
		off += 4
	}
	if req.Bitmap&FileBitmapRsrcForkLen != 0 {
		if len(data) < off+4 {
			return fmt.Errorf("ErrParamErr")
		}
		req.RsrcForkLen = int64(int32(binary.BigEndian.Uint32(data[off : off+4])))
	}
	return nil
}

func (req *FPSetForkParmsReq) String() string {
	return fmt.Sprintf("FPSetForkParmsReq{OForkRefNum: %d, Bitmap: %s, DataForkLen: %d, RsrcForkLen: %d}", req.OForkRefNum, formatFileBitmap(req.Bitmap), req.DataForkLen, req.RsrcForkLen)
}

type FPSetForkParmsRes struct{}

func (res *FPSetForkParmsRes) Marshal() []byte { return nil }
func (res *FPSetForkParmsRes) String() string  { return "FPSetForkParmsRes{}" }

var (
	_ RequestModel = (*FPOpenForkReq)(nil)
	_ RequestModel = (*FPReadReq)(nil)
	_ RequestModel = (*FPWriteReq)(nil)
	_ RequestModel = (*FPCloseForkReq)(nil)
	_ RequestModel = (*FPFlushForkReq)(nil)
	_ RequestModel = (*FPFlushReq)(nil)
	_ RequestModel = (*FPByteRangeLockReq)(nil)
	_ RequestModel = (*FPGetForkParmsReq)(nil)
	_ RequestModel = (*FPSetForkParmsReq)(nil)

	_ ResponseModel = (*FPOpenForkRes)(nil)
	_ ResponseModel = (*FPReadRes)(nil)
	_ ResponseModel = (*FPWriteRes)(nil)
	_ ResponseModel = (*FPCloseForkRes)(nil)
	_ ResponseModel = (*FPFlushForkRes)(nil)
	_ ResponseModel = (*FPFlushRes)(nil)
	_ ResponseModel = (*FPByteRangeLockRes)(nil)
	_ ResponseModel = (*FPGetForkParmsRes)(nil)
	_ ResponseModel = (*FPSetForkParmsRes)(nil)
)
