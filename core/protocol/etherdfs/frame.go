package etherdfs

import (
	"errors"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// Ethernet framing constants. EtherDFS rides directly on Ethernet II with the
// custom EtherType; there is no 802.2/SNAP encapsulation.
const (
	// The EtherDFS header fields sit at fixed offsets within the Ethernet
	// payload (i.e. from the start of the frame). The 38 bytes between the
	// EtherType and offset 52 are padding so a minimal frame still meets the
	// 46-byte Ethernet minimum payload; they carry no protocol meaning.
	offSize     = 52 // 2 bytes LE: total frame length (0 ⇒ "use Ethernet length")
	offChecksum = 54 // 2 bytes LE: BSD checksum over [offVersion:] (if CKS set)
	offVersion  = 56 // 1 byte: low 7 bits = version, high bit = CKS flag
	offSequence = 57 // 1 byte: client request sequence (echoed)
	offDrive    = 58 // 1 byte: low 5 bits = drive number
	offOpcode   = 59 // 1 byte: AL_* function
	headerEnd   = 60 // payload begins here

	// cksFlag is the high bit of the version byte: when set, the frame carries a
	// BSD checksum at offChecksum guarding everything from offVersion onward.
	cksFlag = 0x80
	// versionMask isolates the protocol version in the low 7 bits.
	versionMask = 0x7F
)

// MinFrameLen is the smallest valid EtherDFS frame: the Ethernet header plus the
// full EtherDFS header up to (and including) the opcode byte.
const MinFrameLen = headerEnd

var (
	// ErrShort is returned by ParseFrame when the buffer is shorter than a full
	// EtherDFS header.
	ErrShort = errors.New("etherdfs: frame shorter than header")
	// ErrEtherType is returned when the frame's EtherType is not 0xEDF5.
	ErrEtherType = errors.New("etherdfs: not an EtherDFS frame (wrong EtherType)")
	// ErrVersion is returned when the protocol version does not match.
	ErrVersion = errors.New("etherdfs: unsupported protocol version")
	// ErrChecksum is returned when the CKS flag is set but the BSD checksum does
	// not validate.
	ErrChecksum = errors.New("etherdfs: bad BSD checksum")
)

// Frame is a decoded EtherDFS request or reply. DstMAC/SrcMAC are the Ethernet
// addresses; Sequence/Drive/Opcode and the CKS flag are the EtherDFS header
// fields; Payload is the per-opcode body at offset 60. A reply is built from a
// request by swapping the MACs and replacing the payload (see Reply).
type Frame struct {
	DstMAC   [6]byte
	SrcMAC   [6]byte
	Sequence uint8
	Drive    uint8
	Opcode   uint8
	CKS      bool // whether the BSD checksum is present/required
	Payload  []byte
}

// ParseFrame decodes an EtherDFS frame from a full Ethernet frame b. It verifies
// the EtherType (0xEDF5), the protocol version (2), and — when the CKS flag is
// set — the BSD checksum over [offVersion:]. The Payload slice aliases b. The
// trailing length honoured for the payload is the explicit size field when
// non-zero, else the whole buffer (a minimal padded frame sets size to 0).
func ParseFrame(b []byte) (Frame, error) {
	if len(b) < MinFrameLen {
		return Frame{}, ErrShort
	}
	if uint16(b[12])<<8|uint16(b[13]) != EtherType {
		return Frame{}, ErrEtherType
	}
	ver := b[offVersion]
	if ver&versionMask != ProtocolVersion {
		return Frame{}, ErrVersion
	}

	// The explicit size field bounds the meaningful frame when non-zero; a zero
	// size means the sender padded to the Ethernet minimum and the whole buffer
	// is in play.
	end := len(b)
	if size := int(bp.LE16(b[offSize : offSize+2])); size > 0 && size <= len(b) {
		end = size
	}

	cks := ver&cksFlag != 0
	if cks {
		want := bp.LE16(b[offChecksum : offChecksum+2])
		if BSDChecksum(b[offVersion:end]) != want {
			return Frame{}, ErrChecksum
		}
	}

	var f Frame
	copy(f.DstMAC[:], b[0:6])
	copy(f.SrcMAC[:], b[6:12])
	f.Sequence = b[offSequence]
	f.Drive = b[offDrive] & 0x1F
	f.Opcode = b[offOpcode]
	f.CKS = cks
	if end > headerEnd {
		f.Payload = b[headerEnd:end]
	}
	return f, nil
}

// Reply builds a reply Frame for this request: the MACs are swapped (the reply
// goes back to the requester from the server), the sequence/drive/opcode and CKS
// preference are preserved, and payload becomes the reply body. srcMAC is the
// server's own hardware address (the reply's source).
func (f Frame) Reply(srcMAC [6]byte, payload []byte) Frame {
	return Frame{
		DstMAC:   f.SrcMAC,
		SrcMAC:   srcMAC,
		Sequence: f.Sequence,
		Drive:    f.Drive,
		Opcode:   f.Opcode,
		CKS:      f.CKS,
		Payload:  payload,
	}
}

// Encode appends the wire form of the frame to dst and returns it (append-style →
// caller controls allocation). It emits the Ethernet header, the 38-byte
// padding, the size field (the total length), the version+CKS byte, the
// sequence/drive/opcode, and the payload; when CKS is set the BSD checksum over
// [offVersion:] is filled in. The frame is zero-padded to the 60-byte minimum so
// it is a valid Ethernet frame on the wire.
func (f Frame) Encode(dst []byte) []byte {
	total := headerEnd + len(f.Payload)
	out := make([]byte, max(total, MinFrameLen))
	copy(out[0:6], f.DstMAC[:])
	copy(out[6:12], f.SrcMAC[:])
	out[12] = byte(EtherType >> 8)
	out[13] = byte(EtherType & 0xFF)

	bp.PutLE16(out[offSize:offSize+2], uint16(total))
	ver := byte(ProtocolVersion)
	if f.CKS {
		ver |= cksFlag
	}
	out[offVersion] = ver
	out[offSequence] = f.Sequence
	out[offDrive] = f.Drive & 0x1F
	out[offOpcode] = f.Opcode
	copy(out[headerEnd:], f.Payload)

	if f.CKS {
		sum := BSDChecksum(out[offVersion:total])
		bp.PutLE16(out[offChecksum:offChecksum+2], sum)
	}
	return append(dst, out...)
}
