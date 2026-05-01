//go:build afp || all

package afp

import (
	"encoding/binary"
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/pkg/binutil"
)

// FPOpenDT - open the Desktop Database for a volume.
// Per AFP spec: cmd(1), pad(1), VolID(2) -> DTRefNum(2).
type FPOpenDTReq struct {
	VolID uint16
}

func (req *FPOpenDTReq) Unmarshal(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("FPOpenDTReq: short packet (%d bytes)", len(data))
	}
	req.VolID = binary.BigEndian.Uint16(data[2:4])
	return nil
}

func (req *FPOpenDTReq) String() string { return fmt.Sprintf("FPOpenDTReq{VolID: %d}", req.VolID) }

type FPOpenDTRes struct {
	DTRefNum uint16
}

func (res *FPOpenDTRes) WireSize() int { return 2 }

func (res *FPOpenDTRes) MarshalWire(b []byte) (int, error) {
	return binutil.PutU16(b, res.DTRefNum)
}

func (res *FPOpenDTRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}

func (res *FPOpenDTRes) String() string {
	return fmt.Sprintf("FPOpenDTRes{DTRefNum: %d}", res.DTRefNum)
}

// FPCloseDT - close the Desktop Database; invalidate the DTRefNum.
// cmd(1), pad(1), DTRefNum(2)
type FPCloseDTReq struct {
	DTRefNum uint16
}

func (req *FPCloseDTReq) Unmarshal(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("FPCloseDTReq: short packet (%d bytes)", len(data))
	}
	req.DTRefNum = binary.BigEndian.Uint16(data[2:4])
	return nil
}

func (req *FPCloseDTReq) String() string {
	return fmt.Sprintf("FPCloseDTReq{DTRefNum: %d}", req.DTRefNum)
}

type FPCloseDTRes struct{}

func (res *FPCloseDTRes) Marshal() []byte { return nil }
func (res *FPCloseDTRes) String() string  { return "FPCloseDTRes{}" }

// FPGetIconInfo - get icon metadata from the Desktop Database.
type FPGetIconInfoReq struct {
	DTRefNum  uint16
	Creator   [4]byte
	IconIndex uint16
}

func (req *FPGetIconInfoReq) Unmarshal(data []byte) error {
	if len(data) < 10 {
		return fmt.Errorf("ErrParamErr")
	}
	req.DTRefNum = binary.BigEndian.Uint16(data[2:4])
	copy(req.Creator[:], data[4:8])
	req.IconIndex = binary.BigEndian.Uint16(data[8:10])
	return nil
}

func (req *FPGetIconInfoReq) String() string {
	return fmt.Sprintf("FPGetIconInfoReq{DTRefNum: %d, Creator: %q, IconIndex: %d}", req.DTRefNum, string(req.Creator[:]), req.IconIndex)
}

type FPGetIconInfoRes struct {
	Header [12]byte
}

func (res *FPGetIconInfoRes) Marshal() []byte { return res.Header[:] }
func (res *FPGetIconInfoRes) String() string  { return "FPGetIconInfoRes{HeaderLen:12}" }

// FPGetIcon - retrieve an application icon from the Desktop Database.
type FPGetIconReq struct {
	DTRefNum uint16
	Creator  [4]byte
	Type     [4]byte
	IType    byte
	Size     uint16
}

func (req *FPGetIconReq) Unmarshal(data []byte) error {
	if len(data) < 16 {
		return fmt.Errorf("ErrParamErr")
	}
	req.DTRefNum = binary.BigEndian.Uint16(data[2:4])
	copy(req.Creator[:], data[4:8])
	copy(req.Type[:], data[8:12])
	req.IType = data[12]
	req.Size = binary.BigEndian.Uint16(data[14:16])
	return nil
}

func (req *FPGetIconReq) String() string {
	return fmt.Sprintf("FPGetIconReq{DTRefNum:%d Creator:%q Type:%q IType:%d Size:%d}", req.DTRefNum, string(req.Creator[:]), string(req.Type[:]), req.IType, req.Size)
}

type FPGetIconRes struct {
	Data []byte
}

func (res *FPGetIconRes) Marshal() []byte { return res.Data }

func (res *FPGetIconRes) String() string {
	return fmt.Sprintf("FPGetIconRes{DataLen:%d}", len(res.Data))
}

// FPAddIcon - add an application icon to the Desktop Database.
type FPAddIconReq struct {
	DTRefNum uint16
	Creator  [4]byte
	Type     [4]byte
	IType    byte
	Tag      uint32
	Size     uint16
	Data     []byte
}

func (req *FPAddIconReq) Unmarshal(data []byte) error {
	if len(data) < 22 {
		return fmt.Errorf("ErrParamErr")
	}
	req.DTRefNum = binary.BigEndian.Uint16(data[2:4])
	copy(req.Creator[:], data[4:8])
	copy(req.Type[:], data[8:12])
	req.IType = data[12]
	req.Tag = binary.BigEndian.Uint32(data[14:18])
	req.Size = binary.BigEndian.Uint16(data[18:20])
	if len(data) < 20+int(req.Size) {
		return fmt.Errorf("ErrParamErr")
	}
	req.Data = append([]byte(nil), data[20:20+int(req.Size)]...)
	return nil
}

func (req *FPAddIconReq) String() string {
	return fmt.Sprintf("FPAddIconReq{DTRefNum:%d Creator:%q Type:%q IType:%d Tag:%d Size:%d}", req.DTRefNum, string(req.Creator[:]), string(req.Type[:]), req.IType, req.Tag, req.Size)
}

type FPAddIconRes struct{}

func (res *FPAddIconRes) Marshal() []byte { return nil }
func (res *FPAddIconRes) String() string  { return "FPAddIconRes{}" }

// FPAddAPPL - register an application mapping in the Desktop Database.
type FPAddAPPLReq struct {
	DTRefNum uint16
	DirID    uint32
	Creator  [4]byte
	Tag      uint32
	PathType uint8
	Path     string
}

func (req *FPAddAPPLReq) Unmarshal(data []byte) error {
	if len(data) < 18 {
		return fmt.Errorf("FPAddAPPLReq: short packet (%d bytes)", len(data))
	}
	req.DTRefNum = binary.BigEndian.Uint16(data[2:4])
	req.DirID = binary.BigEndian.Uint32(data[4:8])
	copy(req.Creator[:], data[8:12])
	req.Tag = binary.BigEndian.Uint32(data[12:16])
	req.PathType = data[16]
	pathLen := int(data[17])
	if len(data) < 18+pathLen {
		return fmt.Errorf("FPAddAPPLReq: path truncated")
	}
	req.Path = string(data[18 : 18+pathLen])
	return nil
}

func (req *FPAddAPPLReq) String() string {
	return fmt.Sprintf("FPAddAPPLReq{DTRefNum:%d DirID:%d Creator:%q Tag:%d Path:%q}", req.DTRefNum, req.DirID, string(req.Creator[:]), req.Tag, req.Path)
}

type FPAddAPPLRes struct{}

func (res *FPAddAPPLRes) Marshal() []byte { return nil }
func (res *FPAddAPPLRes) String() string  { return "FPAddAPPLRes{}" }

// FPRemoveAPPL - remove an application mapping from the Desktop Database.
type FPRemoveAPPLReq struct {
	DTRefNum uint16
	DirID    uint32
	Creator  [4]byte
	PathType uint8
	Path     string
}

func (req *FPRemoveAPPLReq) Unmarshal(data []byte) error {
	if len(data) < 14 {
		return fmt.Errorf("FPRemoveAPPLReq: short packet (%d bytes)", len(data))
	}
	req.DTRefNum = binary.BigEndian.Uint16(data[2:4])
	req.DirID = binary.BigEndian.Uint32(data[4:8])
	copy(req.Creator[:], data[8:12])
	req.PathType = data[12]
	pathLen := int(data[13])
	if len(data) < 14+pathLen {
		return fmt.Errorf("FPRemoveAPPLReq: path truncated")
	}
	req.Path = string(data[14 : 14+pathLen])
	return nil
}

func (req *FPRemoveAPPLReq) String() string {
	return fmt.Sprintf("FPRemoveAPPLReq{DTRefNum:%d DirID:%d Creator:%q Path:%q}", req.DTRefNum, req.DirID, string(req.Creator[:]), req.Path)
}

type FPRemoveAPPLRes struct{}

func (res *FPRemoveAPPLRes) Marshal() []byte { return nil }
func (res *FPRemoveAPPLRes) String() string  { return "FPRemoveAPPLRes{}" }

// FPGetAPPL - look up an application entry in the Desktop Database.
type FPGetAPPLReq struct {
	DTRefNum  uint16
	Creator   [4]byte
	APPLIndex uint16
	Bitmap    uint16
}

func (req *FPGetAPPLReq) Unmarshal(data []byte) error {
	if len(data) < 12 {
		return fmt.Errorf("FPGetAPPLReq: short packet (%d bytes)", len(data))
	}
	req.DTRefNum = binary.BigEndian.Uint16(data[2:4])
	copy(req.Creator[:], data[4:8])
	req.APPLIndex = binary.BigEndian.Uint16(data[8:10])
	req.Bitmap = binary.BigEndian.Uint16(data[10:12])
	return nil
}

func (req *FPGetAPPLReq) String() string {
	return fmt.Sprintf("FPGetAPPLReq{DTRefNum:%d Creator:%q APPLIndex:%d Bitmap:%04x}", req.DTRefNum, string(req.Creator[:]), req.APPLIndex, req.Bitmap)
}

// FPGetAPPLRes - Bitmap(2) + APPLTag(4) + file parameters (variable)
type FPGetAPPLRes struct {
	Bitmap  uint16
	APPLTag uint32
	Data    []byte
}

func (res *FPGetAPPLRes) WireSize() int { return 6 + len(res.Data) }

func (res *FPGetAPPLRes) MarshalWire(b []byte) (int, error) {
	off := 0
	n, err := binutil.PutU16(b[off:], res.Bitmap)
	if err != nil {
		return 0, err
	}
	off += n
	n, err = binutil.PutU32(b[off:], res.APPLTag)
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

func (res *FPGetAPPLRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}

func (res *FPGetAPPLRes) String() string {
	return fmt.Sprintf("FPGetAPPLRes{Bitmap:%04x APPLTag:%d DataLen:%d}", res.Bitmap, res.APPLTag, len(res.Data))
}

// FPAddComment - add a Finder comment to a file/dir.
type FPAddCommentReq struct {
	DTRefNum uint16
	DirID    uint32
	PathType uint8
	Path     string
	Comment  []byte
}

func (req *FPAddCommentReq) Unmarshal(data []byte) error {
	if len(data) < 12 {
		return fmt.Errorf("ErrParamErr")
	}
	req.DTRefNum = binary.BigEndian.Uint16(data[2:4])
	req.DirID = binary.BigEndian.Uint32(data[4:8])
	req.PathType = data[8]
	pathLen := int(data[9])
	pathStart := 10
	if len(data) < pathStart+pathLen {
		return fmt.Errorf("ErrParamErr")
	}
	req.Path = string(data[pathStart : pathStart+pathLen])
	idx := pathStart + pathLen
	if idx%2 != 0 {
		idx++
	}
	if idx >= len(data) {
		return fmt.Errorf("ErrParamErr")
	}
	clen := int(data[idx])
	idx++
	if clen > 199 {
		clen = 199
	}
	if len(data) < idx+clen {
		return fmt.Errorf("ErrParamErr")
	}
	req.Comment = append([]byte(nil), data[idx:idx+clen]...)
	return nil
}

func (req *FPAddCommentReq) String() string {
	return fmt.Sprintf("FPAddCommentReq{DTRefNum:%d DirID:%d PathType:%d Path:%q CommentLen:%d}", req.DTRefNum, req.DirID, req.PathType, req.Path, len(req.Comment))
}

type FPAddCommentRes struct{}

func (res *FPAddCommentRes) Marshal() []byte { return nil }
func (res *FPAddCommentRes) String() string  { return "FPAddCommentRes{}" }

// FPRemoveComment - remove a Finder comment from a file/dir.
type FPRemoveCommentReq struct {
	DTRefNum uint16
	DirID    uint32
	PathType uint8
	Path     string
}

func (req *FPRemoveCommentReq) Unmarshal(data []byte) error {
	if len(data) < 10 {
		return fmt.Errorf("ErrParamErr")
	}
	req.DTRefNum = binary.BigEndian.Uint16(data[2:4])
	req.DirID = binary.BigEndian.Uint32(data[4:8])
	req.PathType = data[8]
	pathLen := int(data[9])
	if len(data) < 10+pathLen {
		return fmt.Errorf("ErrParamErr")
	}
	req.Path = string(data[10 : 10+pathLen])
	return nil
}

func (req *FPRemoveCommentReq) String() string {
	return fmt.Sprintf("FPRemoveCommentReq{DTRefNum:%d DirID:%d PathType:%d Path:%q}", req.DTRefNum, req.DirID, req.PathType, req.Path)
}

type FPRemoveCommentRes struct{}

func (res *FPRemoveCommentRes) Marshal() []byte { return nil }
func (res *FPRemoveCommentRes) String() string  { return "FPRemoveCommentRes{}" }

// FPGetComment - retrieve a Finder comment for a file/dir.
type FPGetCommentReq struct {
	DTRefNum uint16
	DirID    uint32
	PathType uint8
	Path     string
}

func (req *FPGetCommentReq) Unmarshal(data []byte) error {
	if len(data) < 10 {
		return fmt.Errorf("ErrParamErr")
	}
	req.DTRefNum = binary.BigEndian.Uint16(data[2:4])
	req.DirID = binary.BigEndian.Uint32(data[4:8])
	req.PathType = data[8]
	pathLen := int(data[9])
	if len(data) < 10+pathLen {
		return fmt.Errorf("ErrParamErr")
	}
	req.Path = string(data[10 : 10+pathLen])
	return nil
}

func (req *FPGetCommentReq) String() string {
	return fmt.Sprintf("FPGetCommentReq{DTRefNum:%d DirID:%d PathType:%d Path:%q}", req.DTRefNum, req.DirID, req.PathType, req.Path)
}

type FPGetCommentRes struct {
	Comment []byte
}

func (res *FPGetCommentRes) commentLen() int {
	n := len(res.Comment)
	if n > 128 {
		n = 128
	}
	return n
}

func (res *FPGetCommentRes) WireSize() int { return 1 + res.commentLen() }

func (res *FPGetCommentRes) MarshalWire(b []byte) (int, error) {
	clen := res.commentLen()
	if len(b) < 1+clen {
		return 0, binutil.ErrShortBuffer
	}
	b[0] = byte(clen)
	copy(b[1:], res.Comment[:clen])
	return 1 + clen, nil
}

func (res *FPGetCommentRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}

func (res *FPGetCommentRes) String() string {
	return fmt.Sprintf("FPGetCommentRes{Len:%d}", len(res.Comment))
}
