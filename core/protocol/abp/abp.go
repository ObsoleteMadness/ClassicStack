// Package abp holds the AppleTalk Boot Protocol (ABP) codec, plus Elliot Nunn's
// ChainBoot EBP extension commands.
//
// ABP is the wire protocol the `.netBOOT`/`.ATBOOT` ROM drivers speak to download
// a boot payload over DDP type 10. This package is wire-format only — no I/O, no
// goroutines, no session state. Constants and struct names follow Apple's source
// (SuperMario os/netboot: BootDefines.h, ATBootEqu.h); the Chain* commands are
// Elliot Nunn's NetBoot-project extension (not Apple protocol).
//
// Ring: CORE (stdlib only, reflection-free). Big-endian integer codecs come from
// core/binaryprimitives, because encoding/binary transitively imports reflect.
//
// Reference: spec/19-netboot.md.
package abp

import (
	"errors"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

const (
	// DDPType is the ABP DDP protocol type (BOOTDDPTYPE, ATBootEqu.h).
	DDPType = 10
	// ClientSocket is the DDP socket the booting client listens on (BOOTSOCKET,
	// hardcoded in the ROM client). The server socket is NBP-advertised.
	ClientSocket = 10
	// Version is the ABP protocol version (thispversion, BootDefines.h). Clients
	// trash packets with a greater version.
	Version = 1
	// MachineMac is the osID a BootPktRply must carry (MACHINE_MAC, NetBoot.h):
	// the client validates osID == 1 regardless of the request's machineID.
	MachineMac = 1
	// DiskSector is the classic block size for rbImageData (disksector,
	// BootDefines.h). ChainLoader payloads use 256 (ATBOOT_BLOCK_SIZE).
	DiskSector = 512
	// BitmapSize is the maximum request bitmap length in bytes (bitmapsize,
	// BootDefines.h); it caps an ABP payload at BitmapSize*8 blocks.
	BitmapSize = 512
	// MaxImageBlocks is the largest payload, in blocks, the client accepts.
	// GetServer.c rejects lastBlockNo/8 >= BITMAP_BYTES-1, i.e. imageSize
	// > 4088 — slightly stricter than the raw 4096-bit bitmap.
	MaxImageBlocks = (BitmapSize - 1) * 8
	// DDPMaxData is the DDP maximum payload; a BootPktRply is exactly this long
	// (the client's socket listener reads ddpMaxData for a user reply).
	DDPMaxData = 586
	// UserNameLength is the userName field width in a UserRecordRequest
	// (userNameLength, ATBootEqu.h — Pascal string in a fixed 34-byte field).
	UserNameLength = 34
	// UserRecordLength is the userRecord tail of a BootPktRply (568 bytes,
	// zero-filled by this server; the ROM boots without it).
	UserRecordLength = 568
)

// ABP command bytes (BootDefines.h; rb* aliases from NetBoot.py's dump of the
// .netBOOT equates). 128–131 are ChainBoot EBP (Elliot Nunn).
const (
	CmdNullCommand       = 0 // rbNullCommand (ignore)
	CmdUserRecordRequest = 1 // User_record_request / rbMapUser (wks → srv)
	CmdUserRecordReply   = 2 // User_record_reply / rbUserReply (srv → wks)
	CmdBootImageRequest  = 3 // Boot_image_request / rbImageRequest (wks → srv)
	CmdBootImageReply    = 4 // Boot_image_reply / rbImageData (srv → wks)
	CmdImageDone         = 5 // Image_done (unused by the boot path)
	CmdUserRecordUpdate  = 6 // User_record_update (unused)
	CmdUserUpdateReply   = 7 // User_update_reply (unused)

	CmdChainRead     = 128 // EBP chunk read request (wks → srv)
	CmdChainReadData = 129 // EBP chunk read data (srv → wks)
	CmdChainWrite    = 130 // EBP chunk write block (wks → srv)
	CmdChainWriteAck = 131 // EBP chunk write ack (srv → wks)
)

// ChainBoot EBP framing (Elliot Nunn's ChainBoot.py / Client.a).
const (
	// ChainBlockSize is the EBP transfer block size (always 512).
	ChainBlockSize = 512
	// ChunkBlocks is the maximum blocks per EBP chunk (32 × 512 = 16 KB).
	ChunkBlocks = 32
	// ChainLastFlag marks the final block of a chunk in the blkIndex byte.
	ChainLastFlag = 0x80
)

// Codec errors.
var (
	ErrShort   = errors.New("abp: packet too short")
	ErrCommand = errors.New("abp: unexpected command byte")
	ErrVersion = errors.New("abp: unsupported protocol version")
)

// Command peeks the command byte of an ABP packet (0 if too short to carry one).
func Command(b []byte) uint8 {
	if len(b) == 0 {
		return 0
	}
	return b[0]
}

// checkHeader validates the leading {command, version} pair.
func checkHeader(b []byte, cmd uint8) error {
	if len(b) < 2 {
		return ErrShort
	}
	if b[0] != cmd {
		return ErrCommand
	}
	if b[1] > Version {
		return ErrVersion
	}
	return nil
}

// UserRecordRequest is the rbMapUser packet a booting workstation sends
// (UserRecordRequest, ATBootEqu.h): 42 bytes on the wire.
type UserRecordRequest struct {
	MachineID uint16 // carries the client's PRAM osType
	Timestamp uint32 // client TickCount at send; echoed back as userData
	UserName  []byte // Pascal string content (≤ 33 bytes, no length byte here)
}

// userRecordRequestLen is the fixed wire length: cmd+version+machineID+timestamp+userName[34].
const userRecordRequestLen = 2 + 2 + 4 + UserNameLength

// Unmarshal parses a full ABP payload (command byte first) into r.
func (r *UserRecordRequest) Unmarshal(b []byte) error {
	if err := checkHeader(b, CmdUserRecordRequest); err != nil {
		return err
	}
	if len(b) < userRecordRequestLen {
		return ErrShort
	}
	r.MachineID = bp.BE16(b[2:4])
	r.Timestamp = bp.BE32(b[4:8])
	n := min(int(b[8]), UserNameLength-1)
	r.UserName = append([]byte(nil), b[9:9+n]...)
	return nil
}

// Marshal renders the 42-byte wire form (used by tests and client tooling).
func (r UserRecordRequest) Marshal() []byte {
	out := make([]byte, 0, userRecordRequestLen)
	out = append(out, CmdUserRecordRequest, Version)
	out = bp.AppendBE16(out, r.MachineID)
	out = bp.AppendBE32(out, r.Timestamp)
	name := r.UserName
	if len(name) > UserNameLength-1 {
		name = name[:UserNameLength-1]
	}
	out = append(out, byte(len(name)))
	out = append(out, name...)
	for len(out) < userRecordRequestLen {
		out = append(out, 0)
	}
	return out
}

// BootPktRply is the rbUserReply the server answers a UserRecordRequest with
// (BootPktRply, ATBootEqu.h). Marshal emits exactly DDPMaxData (586) bytes with
// a zero-filled userRecord — the proven-bootable form.
type BootPktRply struct {
	OSID      uint16 // MUST be MachineMac (1); the client validates it
	UserData  uint32 // MUST echo the request Timestamp (client RTT source)
	BlockSize uint16 // bytes per rbImageData block
	ImageID   uint16 // echoed by the client in image requests
	Result    int16  // 0 = success
	ImageSize uint32 // payload length in blocks
}

// Marshal renders the 586-byte wire form.
func (r BootPktRply) Marshal() []byte {
	out := make([]byte, 0, DDPMaxData)
	out = append(out, CmdUserRecordReply, Version)
	out = bp.AppendBE16(out, r.OSID)
	out = bp.AppendBE32(out, r.UserData)
	out = bp.AppendBE16(out, r.BlockSize)
	out = bp.AppendBE16(out, r.ImageID)
	out = bp.AppendBE16(out, uint16(r.Result))
	out = bp.AppendBE32(out, r.ImageSize)
	out = append(out, make([]byte, DDPMaxData-len(out))...) // zero userRecord
	return out
}

// Unmarshal parses the fixed header of a reply (tests / client tooling); the
// zero userRecord tail is not decoded.
func (r *BootPktRply) Unmarshal(b []byte) error {
	if err := checkHeader(b, CmdUserRecordReply); err != nil {
		return err
	}
	if len(b) < 18 {
		return ErrShort
	}
	r.OSID = bp.BE16(b[2:4])
	r.UserData = bp.BE32(b[4:8])
	r.BlockSize = bp.BE16(b[8:10])
	r.ImageID = bp.BE16(b[10:12])
	r.Result = int16(bp.BE16(b[12:14]))
	r.ImageSize = bp.BE32(b[14:18])
	return nil
}

// BootImageRequest is the rbImageRequest a workstation sends for image blocks
// (bir, ATBootEqu.h): 8-byte header + variable-length bitmap. The bitmap is
// buggy on real clients (spec/19 errata) and servers must ignore it — it is
// still captured for diagnostics.
type BootImageRequest struct {
	ImageID    uint16
	Section    uint8 // always 0 (multi-section unimplemented client-side)
	Flags      uint8
	ReplyDelay uint16
	Bitmap     []byte // ≤ BitmapSize; possibly empty or truncated
}

// Unmarshal parses a full ABP payload into r, tolerating any bitmap length.
func (r *BootImageRequest) Unmarshal(b []byte) error {
	if err := checkHeader(b, CmdBootImageRequest); err != nil {
		return err
	}
	if len(b) < 8 {
		return ErrShort
	}
	r.ImageID = bp.BE16(b[2:4])
	r.Section = b[4]
	r.Flags = b[5]
	r.ReplyDelay = bp.BE16(b[6:8])
	r.Bitmap = append([]byte(nil), b[8:]...)
	return nil
}

// Marshal renders the wire form (tests / client tooling).
func (r BootImageRequest) Marshal() []byte {
	out := make([]byte, 0, 8+len(r.Bitmap))
	out = append(out, CmdBootImageRequest, Version)
	out = bp.AppendBE16(out, r.ImageID)
	out = append(out, r.Section, r.Flags)
	out = bp.AppendBE16(out, r.ReplyDelay)
	out = append(out, r.Bitmap...)
	return out
}

// BootBlock is one rbImageData packet (BootBlock, ATBootEqu.h): 6-byte header +
// one payload block. BlockNo is 0-BASED on the wire (spec/19 errata — the
// struct comment "starts with 1" in Apple's header is wrong).
type BootBlock struct {
	ImageID uint16
	BlockNo uint16
	Data    []byte
}

// Marshal renders the wire form.
func (r BootBlock) Marshal() []byte {
	out := make([]byte, 0, 6+len(r.Data))
	out = append(out, CmdBootImageReply, Version)
	out = bp.AppendBE16(out, r.ImageID)
	out = bp.AppendBE16(out, r.BlockNo)
	out = append(out, r.Data...)
	return out
}

// Unmarshal parses a full ABP payload into r (tests / client tooling).
func (r *BootBlock) Unmarshal(b []byte) error {
	if err := checkHeader(b, CmdBootImageReply); err != nil {
		return err
	}
	if len(b) < 6 {
		return ErrShort
	}
	r.ImageID = bp.BE16(b[2:4])
	r.BlockNo = bp.BE16(b[4:6])
	r.Data = append([]byte(nil), b[6:]...)
	return nil
}

// ChainReadRequest is an EBP chunk read (cmd 128, ChainBoot.py / Client.a
// DrvrSendRead): the chain-loaded driver asks for BlockCount 512-byte blocks
// starting at BlockOffset.
type ChainReadRequest struct {
	Seq         uint16
	ImageNum    uint32
	BlockOffset uint32 // in ChainBlockSize blocks
	BlockCount  uint32 // server clamps to ChunkBlocks
}

// Unmarshal parses a full ABP payload into r. Byte 1 is a client flag byte
// (not a version) and is not validated. The wire form is exactly 16 bytes
// (observed live from ChainLoader, ltoudp-netboot capture 2026-07-16); any
// trailing bytes are tolerated.
func (r *ChainReadRequest) Unmarshal(b []byte) error {
	if len(b) < 16 {
		return ErrShort
	}
	if b[0] != CmdChainRead {
		return ErrCommand
	}
	r.Seq = bp.BE16(b[2:4])
	r.ImageNum = bp.BE32(b[4:8])
	r.BlockOffset = bp.BE32(b[8:12])
	r.BlockCount = bp.BE32(b[12:16])
	return nil
}

// Marshal renders the 16-byte wire form (tests / client tooling), matching
// ChainBoot.py's `>HLLL` layout read from offset 2.
func (r ChainReadRequest) Marshal() []byte {
	out := make([]byte, 0, 16)
	out = append(out, CmdChainRead, 0)
	out = bp.AppendBE16(out, r.Seq)
	out = bp.AppendBE32(out, r.ImageNum)
	out = bp.AppendBE32(out, r.BlockOffset)
	out = bp.AppendBE32(out, r.BlockCount)
	return out
}

// ChainReadData is one EBP read-reply block (cmd 129): BlkIndex is the block's
// plain index within the chunk (reads carry NO ChainLastFlag — the client
// tracks completion in its own progress bitmap; only write blocks flag the
// last block).
type ChainReadData struct {
	BlkIndex uint8
	Seq      uint16
	Data     []byte
}

// Marshal renders the wire form.
func (r ChainReadData) Marshal() []byte {
	out := make([]byte, 0, 4+len(r.Data))
	out = append(out, CmdChainReadData, r.BlkIndex)
	out = bp.AppendBE16(out, r.Seq)
	out = append(out, r.Data...)
	return out
}

// Unmarshal parses a full ABP payload into r (tests / client tooling).
func (r *ChainReadData) Unmarshal(b []byte) error {
	if len(b) < 4 {
		return ErrShort
	}
	if b[0] != CmdChainReadData {
		return ErrCommand
	}
	r.BlkIndex = b[1]
	r.Seq = bp.BE16(b[2:4])
	r.Data = append([]byte(nil), b[4:]...)
	return nil
}

// ChainWriteBlock is one EBP write block (cmd 130): the client streams a chunk
// block-by-block; the block flagged ChainLastFlag commits the chunk at
// HunkStart*ChainBlockSize.
type ChainWriteBlock struct {
	BlkIndex  uint8 // index within the chunk; ChainLastFlag set on the last
	Seq       uint16
	ImageNum  uint32
	HunkStart uint32 // first block of this chunk
	Data      []byte // ≤ ChainBlockSize
}

// Unmarshal parses a full ABP payload into r.
func (r *ChainWriteBlock) Unmarshal(b []byte) error {
	if len(b) < 12 {
		return ErrShort
	}
	if b[0] != CmdChainWrite {
		return ErrCommand
	}
	r.BlkIndex = b[1]
	r.Seq = bp.BE16(b[2:4])
	r.ImageNum = bp.BE32(b[4:8])
	r.HunkStart = bp.BE32(b[8:12])
	r.Data = append([]byte(nil), b[12:]...)
	return nil
}

// Marshal renders the wire form (tests / client tooling).
func (r ChainWriteBlock) Marshal() []byte {
	out := make([]byte, 0, 12+len(r.Data))
	out = append(out, CmdChainWrite, r.BlkIndex)
	out = bp.AppendBE16(out, r.Seq)
	out = bp.AppendBE32(out, r.ImageNum)
	out = bp.AppendBE32(out, r.HunkStart)
	out = append(out, r.Data...)
	return out
}

// ChainWriteAck is the EBP write acknowledgement (cmd 131) sent after a chunk
// commits.
type ChainWriteAck struct {
	Seq uint16
}

// Marshal renders the 4-byte wire form.
func (r ChainWriteAck) Marshal() []byte {
	out := make([]byte, 0, 4)
	out = append(out, CmdChainWriteAck, 0)
	out = bp.AppendBE16(out, r.Seq)
	return out
}

// Unmarshal parses a full ABP payload into r (tests / client tooling).
func (r *ChainWriteAck) Unmarshal(b []byte) error {
	if len(b) < 4 {
		return ErrShort
	}
	if b[0] != CmdChainWriteAck {
		return ErrCommand
	}
	r.Seq = bp.BE16(b[2:4])
	return nil
}
