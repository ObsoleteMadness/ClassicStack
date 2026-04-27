//go:build afp

package afp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/pgodw/omnitalk/pkg/binutil"
)

// FPGetSrvrInfoReq - request to obtain a block of descriptive information
// about a server. The request itself is empty in the AFP packet model used
// here; the server address (SAddr / EntityAddr) is provided by the caller
// (outside of this data block). This call may be made without an open
// AFP session.
type FPGetSrvrInfoReq struct{}

func (req *FPGetSrvrInfoReq) Unmarshal(data []byte) error { return nil }

func (req *FPGetSrvrInfoReq) String() string { return "FPGetSrvrInfoReq{}" }

type FPGetSrvrInfoRes struct {
	// MachineType: Pascal string describing the server's hardware and/or
	// operating system. In the reply block this field's offset is provided
	// in the 16-bit "Offset to Machine Type" header entry (offset from the
	// start of the information block).
	MachineType string

	// AFPVersions: slice of AFP version strings supported by the server.
	// Encoded in the packet as a 1-byte count followed by that many
	// Pascal strings packed back-to-back. The reply header contains an
	// offset to the count of AFP versions.
	AFPVersions []string

	// UAMs: slice of user authentication method strings supported by the
	// server. Encoded like AFPVersions (1-byte count then Pascal strings).
	// The reply header contains an offset to the count of UAMs.
	UAMs []string

	// ServerName: Pascal string containing the server's name. The Server
	// Name field always begins immediately after the 16-bit Flags field in
	// the reply block (i.e. no offset is needed to find it).
	ServerName string

	// Flags: 16-bit server capability flags. Bit layout (from MSB=bit15
	// down to LSB=bit0):
	//  - bit 15 (0x8000): SupportsCopyFile — set if the server supports
	//    the FPCopyFile call.
	//  - bit 14 (0x4000): SupportsChgPwd — set if the server supports the
	//    FPChangePassword call (AFP 2.0 only).
	//  - bits 0..13: reserved (must be 0).
	Flags uint16
}

// layout returns the offsets used by the GetSrvrInfo reply block, plus the
// total wire size. The fixed header is 4 × uint16 offsets + 1 × uint16 Flags
// = 10 bytes; the ServerName follows immediately as a Pascal string and is
// padded to an even boundary before the rest of the variable-length fields.
func (res *FPGetSrvrInfoRes) layout() (machineOff, versionsOff, uamsOff, total int) {
	const headerLen = 10 // 4 offsets + Flags
	baseOffset := headerLen + 1 + len(res.ServerName)
	if baseOffset%2 != 0 {
		baseOffset++
	}
	machineOff = baseOffset
	versionsOff = machineOff + 1 + len(res.MachineType)
	versionsLen := 1
	for _, v := range res.AFPVersions {
		versionsLen += 1 + len(v)
	}
	uamsOff = versionsOff + versionsLen
	uamsLen := 1
	for _, u := range res.UAMs {
		uamsLen += 1 + len(u)
	}
	total = uamsOff + uamsLen
	return
}

// WireSize returns the encoded length of the reply block.
func (res *FPGetSrvrInfoRes) WireSize() int {
	_, _, _, total := res.layout()
	return total
}

// MarshalWire encodes the reply block into b. Returns ErrShortBuffer if
// b is too small.
func (res *FPGetSrvrInfoRes) MarshalWire(b []byte) (int, error) {
	machineOff, versionsOff, uamsOff, total := res.layout()
	if len(b) < total {
		return 0, binutil.ErrShortBuffer
	}
	// Zero the buffer first so the gap before machineOff (caused by the
	// even-boundary pad after ServerName) is left as zero bytes.
	for i := 0; i < total; i++ {
		b[i] = 0
	}

	off := 0
	n, _ := binutil.PutU16(b[off:], uint16(machineOff))
	off += n
	n, _ = binutil.PutU16(b[off:], uint16(versionsOff))
	off += n
	n, _ = binutil.PutU16(b[off:], uint16(uamsOff))
	off += n
	n, _ = binutil.PutU16(b[off:], 0) // iconOffset
	off += n
	n, _ = binutil.PutU16(b[off:], res.Flags)
	off += n

	n, _ = binutil.PutPString(b[off:], []byte(res.ServerName))
	off += n

	// Skip pad bytes (already zeroed) up to machineOff.
	off = machineOff

	n, _ = binutil.PutPString(b[off:], []byte(res.MachineType))
	off += n

	b[off] = byte(len(res.AFPVersions))
	off++
	for _, v := range res.AFPVersions {
		n, _ = binutil.PutPString(b[off:], []byte(v))
		off += n
	}

	b[off] = byte(len(res.UAMs))
	off++
	for _, u := range res.UAMs {
		n, _ = binutil.PutPString(b[off:], []byte(u))
		off += n
	}

	return off, nil
}

// Marshal allocates a buffer and encodes the reply block. Prefer MarshalWire
// when the caller can supply a buffer.
func (res *FPGetSrvrInfoRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}

func (res *FPGetSrvrInfoRes) String() string {
	return fmt.Sprintf("FPGetSrvrInfoRes{ServerName: %q, MachineType: %q, AFPVersions: %v, UAMs: %v, Flags: %d}", res.ServerName, res.MachineType, res.AFPVersions, res.UAMs, res.Flags)
}

// FPGetSrvrParmsReq - request to retrieve server parameters. This call
// is made after a session is established and requires a valid session
// reference number (SRefNum) provided to the server by the caller; the
// packet body for this request is empty in this implementation.
type FPGetSrvrParmsReq struct{}

func (req *FPGetSrvrParmsReq) Unmarshal(data []byte) error { return nil }

func (req *FPGetSrvrParmsReq) String() string { return "FPGetSrvrParmsReq{}" }

type FPGetSrvrParmsRes struct {
	// ServerTime: 32-bit server clock time value (seconds since epoch
	// or server-specific epoch depending on implementation). Returned
	// as the first 4 bytes of the reply block.
	ServerTime uint32

	// Volumes: list of volumes managed by the server. The reply contains
	// a 1-byte count followed by, for each volume, a flags byte and a
	// Pascal string name (no padding between entries).
	Volumes []VolInfo
}

type VolInfo struct {
	// Flags: per-volume flags (1 byte). Bits include at least:
	//  - bit 0 (0x01): HasPassword — set if the volume is password-protected.
	//  - bit 1 (0x02): HasConfigInfo — AFP 2.0 only; set for the volume that
	//    contains Apple II configuration information.
	// Remaining bits are reserved.
	Flags uint8

	// Name: Pascal string containing the volume name. Encoded in the
	// reply as a 1-byte length followed by the name bytes.
	Name string
}

const (
	VolInfoFlagHasPassword uint8 = 1 << 0
)

// WireSize returns the encoded length: 4-byte ServerTime + 1-byte volume
// count + per-volume (1-byte flags + 1-byte name len + name bytes, name
// truncated to 255).
func (res *FPGetSrvrParmsRes) WireSize() int {
	n := 5
	for _, v := range res.Volumes {
		nameLen := len(v.Name)
		if nameLen > 255 {
			nameLen = 255
		}
		n += 2 + nameLen
	}
	return n
}

// MarshalWire encodes the reply block into b.
func (res *FPGetSrvrParmsRes) MarshalWire(b []byte) (int, error) {
	if len(b) < res.WireSize() {
		return 0, binutil.ErrShortBuffer
	}
	off := 0
	n, _ := binutil.PutU32(b[off:], res.ServerTime)
	off += n
	b[off] = uint8(len(res.Volumes))
	off++
	for _, v := range res.Volumes {
		nameLen := len(v.Name)
		if nameLen > 255 {
			nameLen = 255
		}
		b[off] = v.Flags
		off++
		n, _ = binutil.PutPString(b[off:], []byte(v.Name[:nameLen]))
		off += n
	}
	return off, nil
}

// Marshal allocates a buffer and encodes the reply block.
func (res *FPGetSrvrParmsRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}

func (res *FPGetSrvrParmsRes) Unmarshal(data []byte) error {
	if len(data) < 5 {
		return fmt.Errorf("ErrParamErr")
	}

	res.ServerTime = binary.BigEndian.Uint32(data[:4])
	count := int(data[4])
	offset := 5
	res.Volumes = make([]VolInfo, 0, count)

	for i := 0; i < count; i++ {
		if offset+2 > len(data) {
			return fmt.Errorf("ErrParamErr")
		}

		flags := data[offset]
		offset++
		nameLen := int(data[offset])
		offset++
		if offset+nameLen > len(data) {
			return fmt.Errorf("ErrParamErr")
		}

		res.Volumes = append(res.Volumes, VolInfo{
			Flags: flags,
			Name:  string(data[offset : offset+nameLen]),
		})
		offset += nameLen
	}

	return nil
}

func (res *FPGetSrvrParmsRes) String() string {
	return fmt.Sprintf("FPGetSrvrParmsRes{ServerTime: %d, VolumesCount: %d}", res.ServerTime, len(res.Volumes))
}

// FPLoginReq - request to log in to the server and establish a session.
type FPLoginReq struct {
	// AFPVersion: Pascal string indicating the AFP version requested by the
	// client. The server will return BadVersNum if it cannot support the
	// requested version.
	AFPVersion string

	// UAM: Pascal string naming the User Authentication Method to use for
	// this login (for example "Cleartxt Passwrd" or "Randnum Exchange").
	UAM string

	// Username and Password: fields populated for UAMs that require them
	// (for example, "Cleartxt Passwrd"). When using the cleared-text
	// password UAM the username is placed on an even boundary and the
	// password is padded with null bytes to an 8-byte field.
	Username string
	Password string
}

func (req *FPLoginReq) Unmarshal(data []byte) error {
	if len(data) < 2 {
		return fmt.Errorf("ErrParamErr")
	}
	offset := 0
	afpVerLen := int(data[offset])
	offset++
	if offset+afpVerLen > len(data) {
		return fmt.Errorf("ErrParamErr")
	}
	req.AFPVersion = string(data[offset : offset+afpVerLen])
	offset += afpVerLen
	if offset >= len(data) {
		return fmt.Errorf("ErrParamErr")
	}
	uamLen := int(data[offset])
	offset++
	if offset+uamLen > len(data) {
		return fmt.Errorf("ErrParamErr")
	}
	req.UAM = string(data[offset : offset+uamLen])
	offset += uamLen

	if req.UAM == "Cleartxt Passwrd" {
		if offset%2 != 0 {
			offset++
		}
		if offset >= len(data) {
			return fmt.Errorf("ErrParamErr")
		}
		usernameLen := int(data[offset])
		offset++
		if offset+usernameLen > len(data) {
			return fmt.Errorf("ErrParamErr")
		}
		req.Username = string(data[offset : offset+usernameLen])
		offset += usernameLen
		if offset%2 != 0 {
			offset++
		}
		if offset+8 > len(data) {
			return fmt.Errorf("ErrParamErr")
		}
		req.Password = string(bytes.TrimRight(data[offset:offset+8], "\x00"))
	}
	return nil
}

func (req *FPLoginReq) String() string {
	return fmt.Sprintf("FPLoginReq{AFPVersion: %q, UAM: %q, Username: %q}", req.AFPVersion, req.UAM, req.Username)
}

type FPLoginRes struct {
	// SRefNum: session reference number assigned by the server. Valid if
	// no error (or AuthContinue) is returned and used for subsequent
	// session calls.
	SRefNum uint16

	// IDNumber: an identifier returned by some UAMs (for example the
	// Randnum Exchange flow). Used by FPLoginCont to continue
	// authentication when AuthContinue is returned.
	IDNumber uint16
}

// WireSize returns the fixed 4-byte size of the FPLoginRes block.
func (res *FPLoginRes) WireSize() int { return 4 }

// MarshalWire encodes the reply block into b.
func (res *FPLoginRes) MarshalWire(b []byte) (int, error) {
	if len(b) < 4 {
		return 0, binutil.ErrShortBuffer
	}
	_, _ = binutil.PutU16(b[0:], res.SRefNum)
	_, _ = binutil.PutU16(b[2:], res.IDNumber)
	return 4, nil
}

// Marshal allocates a buffer and encodes the reply block.
func (res *FPLoginRes) Marshal() []byte {
	b := make([]byte, res.WireSize())
	_, _ = res.MarshalWire(b)
	return b
}

func (res *FPLoginRes) String() string {
	return fmt.Sprintf("FPLoginRes{SRefNum: %d, IDNumber: %d}", res.SRefNum, res.IDNumber)
}

type FPLogoutReq struct{}

func (req *FPLogoutReq) Unmarshal(data []byte) error { return nil }
func (req *FPLogoutReq) String() string              { return "FPLogoutReq{}" }

type FPLogoutRes struct{}

func (res *FPLogoutRes) Marshal() []byte { return nil }
func (res *FPLogoutRes) String() string  { return "FPLogoutRes{}" }

// FPLoginCont - second stage of a two-phase UAM login (AFP 2.x section 5.1.19).
// Not supported; server returns ErrCallNotSupported.
type FPLoginContReq struct{}

func (req *FPLoginContReq) Unmarshal(data []byte) error { return nil }
func (req *FPLoginContReq) String() string              { return "FPLoginContReq{}" }

type FPLoginContRes struct{}

func (res *FPLoginContRes) Marshal() []byte { return nil }
func (res *FPLoginContRes) String() string  { return "FPLoginContRes{}" }

// FPMapID - map a user or group ID to its name (AFP 2.x section 5.1.21).
type FPMapIDReq struct {
	Function uint8
	ID       uint32
}

func (req *FPMapIDReq) Unmarshal(data []byte) error {
	if len(data) < 6 {
		return fmt.Errorf("ErrParamErr")
	}
	req.Function = data[1]
	req.ID = binary.BigEndian.Uint32(data[2:6])
	return nil
}

func (req *FPMapIDReq) String() string {
	return fmt.Sprintf("FPMapIDReq{Function:%d ID:%d}", req.Function, req.ID)
}

type FPMapIDRes struct {
	Name string
}

func (res *FPMapIDRes) Marshal() []byte {
	b := make([]byte, 1+len(res.Name))
	b[0] = byte(len(res.Name))
	copy(b[1:], res.Name)
	return b
}

func (res *FPMapIDRes) String() string { return fmt.Sprintf("FPMapIDRes{Name:%q}", res.Name) }

// FPMapName - map a user or group name to its ID (AFP 2.x section 5.1.22).
type FPMapNameReq struct {
	Function uint8
	Name     string
}

func (req *FPMapNameReq) Unmarshal(data []byte) error {
	if len(data) < 3 {
		return fmt.Errorf("ErrParamErr")
	}
	req.Function = data[1]
	nameLen := int(data[2])
	if len(data) < 3+nameLen {
		return fmt.Errorf("ErrParamErr")
	}
	req.Name = string(data[3 : 3+nameLen])
	return nil
}

func (req *FPMapNameReq) String() string {
	return fmt.Sprintf("FPMapNameReq{Function:%d Name:%q}", req.Function, req.Name)
}

type FPMapNameRes struct {
	ID uint32
}

func (res *FPMapNameRes) Marshal() []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, res.ID)
	return b
}

func (res *FPMapNameRes) String() string { return fmt.Sprintf("FPMapNameRes{ID:%d}", res.ID) }

// FPGetSrvrMsg - retrieve the server login or server message.
type FPGetSrvrMsgReq struct {
	MessageType uint16
	Bitmap      uint16
}

func (req *FPGetSrvrMsgReq) Unmarshal(data []byte) error {
	if len(data) < 6 {
		return fmt.Errorf("ErrParamErr")
	}
	req.MessageType = binary.BigEndian.Uint16(data[2:4])
	req.Bitmap = binary.BigEndian.Uint16(data[4:6])
	return nil
}

func (req *FPGetSrvrMsgReq) String() string {
	return fmt.Sprintf("FPGetSrvrMsgReq{Type:%d Bitmap:%d}", req.MessageType, req.Bitmap)
}

type FPGetSrvrMsgRes struct {
	MessageType uint16
	Bitmap      uint16
	Message     string
}

func (res *FPGetSrvrMsgRes) Marshal() []byte {
	b := new(bytes.Buffer)
	binary.Write(b, binary.BigEndian, res.MessageType)
	binary.Write(b, binary.BigEndian, res.Bitmap)
	b.WriteByte(byte(len(res.Message)))
	b.WriteString(res.Message)
	return b.Bytes()
}

func (res *FPGetSrvrMsgRes) String() string {
	return fmt.Sprintf("FPGetSrvrMsgRes{Type:%d Bitmap:%d Msg:%q}", res.MessageType, res.Bitmap, res.Message)
}

type FPUnsupportedReq struct{}

func (req *FPUnsupportedReq) Unmarshal(data []byte) error { return nil }
func (req *FPUnsupportedReq) String() string              { return "FPUnsupportedReq{}" }

// FPCatSearch request (AFP 2.1).
type FPCatSearchReq struct {
	VolumeID            uint16
	ReqMatches          int32
	Reserved            uint32
	CatalogPosition     [16]byte
	FileRsltBitmap      uint16
	DirectoryRsltBitmap uint16
	ReqBitmap           uint32
	Parameters          []byte
}

func (req *FPCatSearchReq) Unmarshal(data []byte) error {
	if len(data) < 36 {
		return fmt.Errorf("ErrParamErr")
	}
	req.VolumeID = binary.BigEndian.Uint16(data[2:4])
	req.ReqMatches = int32(binary.BigEndian.Uint32(data[4:8]))
	req.Reserved = binary.BigEndian.Uint32(data[8:12])
	copy(req.CatalogPosition[:], data[12:28])
	req.FileRsltBitmap = binary.BigEndian.Uint16(data[28:30])
	req.DirectoryRsltBitmap = binary.BigEndian.Uint16(data[30:32])
	req.ReqBitmap = binary.BigEndian.Uint32(data[32:36])
	if len(data) > 36 {
		req.Parameters = append([]byte(nil), data[36:]...)
	} else {
		req.Parameters = nil
	}
	return nil
}

func (req *FPCatSearchReq) String() string {
	query := req.SearchQuery()
	printable := req.searchPrintableParameters()
	if len(printable) > 80 {
		printable = printable[:80] + "..."
	}
	return fmt.Sprintf("FPCatSearchReq{VolumeID:%d ReqMatches:%d FileRsltBitmap:%s DirectoryRsltBitmap:%s ReqBitmap:0x%08x ParamsLen:%d Query:%q Params:%q}",
		req.VolumeID,
		req.ReqMatches,
		formatFileBitmap(req.FileRsltBitmap),
		formatDirBitmap(req.DirectoryRsltBitmap),
		req.ReqBitmap,
		len(req.Parameters),
		query,
		printable,
	)
}

func (req *FPCatSearchReq) SearchQuery() string {
	if len(req.Parameters) == 0 {
		return ""
	}
	return req.searchPrintableParameters()
}

func (req *FPCatSearchReq) searchPrintableParameters() string {
	b := make([]byte, 0, len(req.Parameters))
	for _, c := range req.Parameters {
		if c >= 32 && c <= 126 {
			b = append(b, c)
			continue
		}
		if len(b) > 0 && b[len(b)-1] != ' ' {
			b = append(b, ' ')
		}
	}
	return strings.Join(strings.Fields(string(b)), " ")
}

type FPCatSearchRes struct {
	CatalogPosition     [16]byte
	FileRsltBitmap      uint16
	DirectoryRsltBitmap uint16
	ActualCount         int32
	Data                []byte
}

func (res *FPCatSearchRes) Marshal() []byte {
	b := new(bytes.Buffer)
	b.Write(res.CatalogPosition[:])
	binary.Write(b, binary.BigEndian, res.FileRsltBitmap)
	binary.Write(b, binary.BigEndian, res.DirectoryRsltBitmap)
	binary.Write(b, binary.BigEndian, res.ActualCount)
	b.Write(res.Data)
	return b.Bytes()
}

func (res *FPCatSearchRes) String() string {
	return fmt.Sprintf("FPCatSearchRes{FileRsltBitmap:%s DirectoryRsltBitmap:%s ActualCount:%d DataLen:%d}",
		formatFileBitmap(res.FileRsltBitmap),
		formatDirBitmap(res.DirectoryRsltBitmap),
		res.ActualCount,
		len(res.Data),
	)
}

var (
	_ RequestModel = (*FPGetSrvrInfoReq)(nil)
	_ RequestModel = (*FPGetSrvrParmsReq)(nil)
	_ RequestModel = (*FPLoginReq)(nil)
	_ RequestModel = (*FPLogoutReq)(nil)
	_ RequestModel = (*FPLoginContReq)(nil)
	_ RequestModel = (*FPMapIDReq)(nil)
	_ RequestModel = (*FPMapNameReq)(nil)
	_ RequestModel = (*FPGetSrvrMsgReq)(nil)
	_ RequestModel = (*FPUnsupportedReq)(nil)
	_ RequestModel = (*FPCatSearchReq)(nil)

	_ ResponseModel = (*FPGetSrvrInfoRes)(nil)
	_ ResponseModel = (*FPGetSrvrParmsRes)(nil)
	_ ResponseModel = (*FPLoginRes)(nil)
	_ ResponseModel = (*FPLogoutRes)(nil)
	_ ResponseModel = (*FPLoginContRes)(nil)
	_ ResponseModel = (*FPMapIDRes)(nil)
	_ ResponseModel = (*FPMapNameRes)(nil)
	_ ResponseModel = (*FPGetSrvrMsgRes)(nil)
	_ ResponseModel = (*FPCatSearchRes)(nil)
)
