package smb

import (
	"bytes"
	"encoding/binary"
	"strings"
)

func isLANMANTransactionRequest(req []byte) bool {
	bytesArea, ok := transactionBytesArea(req)
	if !ok || len(bytesArea) == 0 {
		return false
	}
	return bytes.Contains(bytes.ToUpper(bytesArea), []byte("\\PIPE\\LANMAN"))
}

// transactionBytesArea returns the SMB_COM_TRANSACTION bytes area,
// regardless of request word-count shape.
func transactionBytesArea(req []byte) ([]byte, bool) {
	if len(req) < smbHeaderLen+3 || string(req[0:4]) != "\xffSMB" || req[4] != CommandTransaction {
		return nil, false
	}
	wct := int(req[smbHeaderLen])
	bytesOffset := smbHeaderLen + 1 + (wct * 2)
	if bytesOffset+2 > len(req) {
		return nil, false
	}
	byteCount := int(binary.LittleEndian.Uint16(req[bytesOffset : bytesOffset+2]))
	if byteCount < 0 || bytesOffset+2+byteCount > len(req) {
		return nil, false
	}
	return req[bytesOffset+2 : bytesOffset+2+byteCount], true
}

func parseLANMANFunctionCode(req []byte) (uint16, bool) {
	bytesArea, ok := transactionBytesArea(req)
	if !ok {
		return 0, false
	}
	pipe := []byte("\\PIPE\\LANMAN\x00")
	idx := bytes.Index(bytes.ToUpper(bytesArea), bytes.ToUpper(pipe))
	if idx < 0 {
		return 0, false
	}
	p := idx + len(pipe)
	if p+2 > len(bytesArea) {
		return 0, false
	}
	return binary.LittleEndian.Uint16(bytesArea[p : p+2]), true
}

// parseNetServerEnum2ServerType best-effort parses the server-type
// filter (SV_TYPE_*) from a RAP NetServerEnum2 request.
func parseNetServerEnum2ServerType(req []byte) (uint32, bool) {
	bytesArea, ok := transactionBytesArea(req)
	if !ok {
		return 0, false
	}
	pipe := []byte("\\PIPE\\LANMAN\x00")
	idx := bytes.Index(bytes.ToUpper(bytesArea), bytes.ToUpper(pipe))
	if idx < 0 {
		return 0, false
	}
	p := idx + len(pipe)
	if p+2 > len(bytesArea) || binary.LittleEndian.Uint16(bytesArea[p:p+2]) != rapNetServerEnum2 {
		return 0, false
	}
	p += 2
	// Skip ParamDesc and DataDesc (both NUL-terminated strings).
	for i := 0; i < 2; i++ {
		n := bytes.IndexByte(bytesArea[p:], 0)
		if n < 0 {
			return 0, false
		}
		p += n + 1
		if p > len(bytesArea) {
			return 0, false
		}
	}
	if p+2+4 > len(bytesArea) {
		return 0, false
	}
	p += 2 // ReceiveBufferLength
	return binary.LittleEndian.Uint32(bytesArea[p : p+4]), true
}

// parseNetServerEnum2Domain extracts the optional Domain filter string from a RAP
// NetServerEnum2 request. Returns ("", false) when the field is absent.
func parseNetServerEnum2Domain(req []byte) (string, bool) {
	bytesArea, ok := transactionBytesArea(req)
	if !ok {
		return "", false
	}
	pipe := []byte("\\PIPE\\LANMAN\x00")
	idx := bytes.Index(bytes.ToUpper(bytesArea), bytes.ToUpper(pipe))
	if idx < 0 {
		return "", false
	}
	p := idx + len(pipe)
	if p+2 > len(bytesArea) || binary.LittleEndian.Uint16(bytesArea[p:p+2]) != rapNetServerEnum2 {
		return "", false
	}
	p += 2
	// Skip ParamDesc and DataDesc (both NUL-terminated).
	for i := 0; i < 2; i++ {
		n := bytes.IndexByte(bytesArea[p:], 0)
		if n < 0 {
			return "", false
		}
		p += n + 1
	}
	if p+2+4 > len(bytesArea) {
		return "", false
	}
	p += 2 + 4 // ReceiveBufferLength + ServerType
	if p >= len(bytesArea) {
		return "", false
	}
	n := bytes.IndexByte(bytesArea[p:], 0)
	if n < 0 {
		return "", false
	}
	domain := string(bytesArea[p : p+n])
	if domain == "" {
		return "", false
	}
	return domain, true
}

// smbServerList returns the server entries for a NetServerEnum2 response:
// ClassicStack itself plus any servers observed via browser announcements.
func (s *Service) smbServerList() []netServerInfo1 {
	self := normalizeBrowserName(s.opts.ServerName)
	if self == "" {
		self = "CLASSICSTACK"
	}
	entries := []netServerInfo1{{
		Name: self,
		Type: browserServerTypeWorkstationMask,
	}}
	s.mu.Lock()
	for name, rec := range s.browserServers {
		if name == self {
			continue
		}
		entries = append(entries, netServerInfo1{Name: name, Type: rec.ServerType})
	}
	s.mu.Unlock()
	return entries
}

// netServerEnum2Entries returns the entries and a RAP status code (0 = success).
//
// Per MS-BRWS §3.3.5.6:
//   - Potential browsers MUST return ERROR_REQ_NOT_ACCEP (71).
//   - SV_TYPE_DOMAIN_ENUM with any other type bit MUST return ERROR_INVALID_FUNCTION (1).
//   - SV_TYPE_DOMAIN_ENUM alone → return all observed machine groups.
func (s *Service) netServerEnum2Entries(serverType uint32, workgroup, requestedDomain string) ([]netServerInfo1, uint16) {
	s.mu.Lock()
	role := s.browserRole
	s.mu.Unlock()

	if role == browserRolePotential {
		return nil, rapStatusErrReqNotAccepted
	}

	if serverType&browserServerTypeDomainEnumMask != 0 {
		if serverType != browserServerTypeDomainEnumMask {
			// DOMAIN_ENUM mixed with other type bits is invalid.
			return nil, rapStatusErrInvalidFunction
		}
		// Return our own workgroup plus any domains observed via DomainAnnouncement.
		ownDomain := normalizeBrowserName(workgroup)
		if ownDomain == "" {
			ownDomain = "WORKGROUP"
		}
		groups := []netServerInfo1{{Name: ownDomain, Type: browserServerTypeDomainEnumMask}}
		s.mu.Lock()
		for group, rec := range s.machineGroups {
			if group == ownDomain {
				continue
			}
			groups = append(groups, netServerInfo1{
				Name:    group,
				Type:    browserServerTypeDomainEnumMask,
				Comment: rec.MasterBrowser,
			})
		}
		s.mu.Unlock()
		return groups, 0
	}

	// Server-list request: if the client specifies a domain, only return
	// results when it matches our own workgroup.
	if requestedDomain != "" {
		ownDomain := normalizeBrowserName(workgroup)
		if ownDomain == "" {
			ownDomain = "WORKGROUP"
		}
		if !strings.EqualFold(requestedDomain, ownDomain) {
			return nil, 0 // empty success — we don't serve that domain
		}
	}

	return s.smbServerList(), 0
}

// buildNetServerEnum2RAPErrorResponse wraps a non-zero RAP status code in a
// minimal SMB_COM_TRANSACTION success frame (SMB status is SUCCESS; the error
// is conveyed in the 2-byte RAP Status field of the parameter block).
func buildNetServerEnum2RAPErrorResponse(req []byte, rapStatus uint16) []byte {
	if len(req) < smbHeaderLen {
		return nil
	}
	const paramLen = 8                       // Status(2)+Converter(2)+EntriesReturned(2)+EntriesAvailable(2)
	paramOffset := smbHeaderLen + 1 + 20 + 2 // = 55
	totalLen := paramOffset + paramLen

	out := make([]byte, totalLen)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80

	out[smbHeaderLen] = 10 // WCT
	w := out[smbHeaderLen+1:]
	binary.LittleEndian.PutUint16(w[0:2], uint16(paramLen)) // TotalParameterCount
	binary.LittleEndian.PutUint16(w[6:8], uint16(paramLen)) // ParameterCount
	binary.LittleEndian.PutUint16(w[8:10], uint16(paramOffset))
	binary.LittleEndian.PutUint16(w[20:22], uint16(paramLen)) // ByteCount

	p := out[paramOffset:]
	binary.LittleEndian.PutUint16(p[0:2], rapStatus)
	return out
}

// buildNetServerEnum2Response constructs an SMB_COM_TRANSACTION response
// carrying a RAP NetServerEnum2 reply with the supplied server entries.
// Converter is set to zero; CommentOffset fields are offsets from the
// start of the Transaction data block.
func buildNetServerEnum2Response(req []byte, entries []netServerInfo1) []byte {
	if len(req) < smbHeaderLen {
		return nil
	}
	const entrySize = 26 // SERVER_INFO_1: Name(16)+VMaj(1)+VMin(1)+Type(4)+CommentOff(4)

	commentBase := len(entries) * entrySize

	commentOff := commentBase
	commentData := make([]byte, 0, len(entries))
	commentOffsets := make([]int, len(entries))
	for i, e := range entries {
		commentOffsets[i] = commentOff
		commentData = append(commentData, []byte(e.Comment)...)
		commentData = append(commentData, 0)
		commentOff += len(e.Comment) + 1
	}

	paramLen := 8 // Status(2)+Converter(2)+EntriesReturned(2)+EntriesAvailable(2)
	dataLen := len(entries)*entrySize + len(commentData)

	// Layout: header(32) + WCT(1) + 10 words(20) + ByteCount(2) + params + data.
	paramOffset := smbHeaderLen + 1 + 20 + 2 // = 55
	dataOffset := paramOffset + paramLen     // = 63
	totalLen := dataOffset + dataLen

	out := make([]byte, totalLen)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80

	out[smbHeaderLen] = 10 // WCT
	w := out[smbHeaderLen+1:]
	binary.LittleEndian.PutUint16(w[0:2], uint16(paramLen))           // TotalParameterCount
	binary.LittleEndian.PutUint16(w[2:4], uint16(dataLen))            // TotalDataCount
	binary.LittleEndian.PutUint16(w[6:8], uint16(paramLen))           // ParameterCount
	binary.LittleEndian.PutUint16(w[8:10], uint16(paramOffset))       // ParameterOffset
	binary.LittleEndian.PutUint16(w[12:14], uint16(dataLen))          // DataCount
	binary.LittleEndian.PutUint16(w[14:16], uint16(dataOffset))       // DataOffset
	binary.LittleEndian.PutUint16(w[20:22], uint16(paramLen+dataLen)) // ByteCount

	p := out[paramOffset:]
	// p[0:2] Status = 0, p[2:4] Converter = 0 (already zero from make).
	binary.LittleEndian.PutUint16(p[4:6], uint16(len(entries))) // EntriesReturned
	binary.LittleEndian.PutUint16(p[6:8], uint16(len(entries))) // EntriesAvailable

	d := out[dataOffset:]
	for i, e := range entries {
		base := i * entrySize
		name := normalizeBrowserName(e.Name)
		if len(name) > 15 {
			name = name[:15]
		}
		copy(d[base:base+16], []byte(name)) // remaining bytes stay NUL
		d[base+16] = 4                      // sv1_version_major
		// d[base+17] = 0 sv1_version_minor (already zero)
		binary.LittleEndian.PutUint32(d[base+18:base+22], e.Type)
		binary.LittleEndian.PutUint32(d[base+22:base+26], uint32(commentOffsets[i]))
	}
	copy(d[commentBase:], commentData)

	return out
}

// shareInfo1Entry holds the data for one SHARE_INFO_1 record.
type shareInfo1Entry struct {
	Name    string
	Type    uint16
	Comment string
}

// netShareEnumEntries returns all configured disk shares plus the IPC$ share.
func (s *Service) netShareEnumEntries() []shareInfo1Entry {
	const stypeDisktree = uint16(0x0000)
	const stypeIPC = uint16(0x0003)
	entries := make([]shareInfo1Entry, 0, len(s.shares)+1)
	for _, sc := range s.shares {
		name := sc.Name
		if len(name) > 12 {
			name = name[:12]
		}
		entries = append(entries, shareInfo1Entry{Name: name, Type: stypeDisktree})
	}
	entries = append(entries, shareInfo1Entry{Name: ipcShareName, Type: stypeIPC})
	return entries
}

// buildNetShareEnumResponse builds an SMB_COM_TRANSACTION response containing
// a RAP NetShareEnum reply (info level 1). Each entry is a SHARE_INFO_1
// record: Name(13)+Pad(1)+Type(2)+RemarkOff(4) = 20 bytes.
func buildNetShareEnumResponse(req []byte, entries []shareInfo1Entry) []byte {
	if len(req) < smbHeaderLen {
		return nil
	}
	const entrySize = 20 // Name(13)+Pad(1)+Type(2)+RemarkOff(4)

	// Build remark-offset table; each name is stored as a NUL-terminated
	// string in the "heap" area that follows the fixed-size records.
	remarkBase := len(entries) * entrySize
	remarkOff := remarkBase
	remarkData := make([]byte, 0)
	remarkOffsets := make([]int, len(entries))
	for i, e := range entries {
		remarkOffsets[i] = remarkOff
		remarkData = append(remarkData, []byte(e.Comment)...)
		remarkData = append(remarkData, 0)
		remarkOff += len(e.Comment) + 1
	}

	paramLen := 8 // Status(2)+Converter(2)+EntriesReturned(2)+EntriesAvailable(2)
	dataLen := len(entries)*entrySize + len(remarkData)

	paramOffset := smbHeaderLen + 1 + 20 + 2 // = 55
	dataOffset := paramOffset + paramLen      // = 63
	totalLen := dataOffset + dataLen

	out := make([]byte, totalLen)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80

	out[smbHeaderLen] = 10 // WCT
	w := out[smbHeaderLen+1:]
	binary.LittleEndian.PutUint16(w[0:2], uint16(paramLen))
	binary.LittleEndian.PutUint16(w[2:4], uint16(dataLen))
	binary.LittleEndian.PutUint16(w[6:8], uint16(paramLen))           // ParameterCount
	binary.LittleEndian.PutUint16(w[8:10], uint16(paramOffset))       // ParameterOffset
	binary.LittleEndian.PutUint16(w[12:14], uint16(dataLen))          // DataCount
	binary.LittleEndian.PutUint16(w[14:16], uint16(dataOffset))       // DataOffset
	binary.LittleEndian.PutUint16(w[20:22], uint16(paramLen+dataLen)) // ByteCount

	p := out[paramOffset:]
	// p[0:2] Status=0, p[2:4] Converter=0 (already zero)
	binary.LittleEndian.PutUint16(p[4:6], uint16(len(entries)))
	binary.LittleEndian.PutUint16(p[6:8], uint16(len(entries)))

	d := out[dataOffset:]
	for i, e := range entries {
		base := i * entrySize
		name := e.Name
		if len(name) > 12 {
			name = name[:12]
		}
		copy(d[base:base+12], []byte(name)) // shi1_netname (12 chars + NUL)
		// d[base+12] = NUL already zero
		// d[base+13] = pad already zero
		binary.LittleEndian.PutUint16(d[base+14:base+16], e.Type)
		binary.LittleEndian.PutUint32(d[base+16:base+20], uint32(remarkOffsets[i]))
	}
	copy(d[remarkBase:], remarkData)

	return out
}

func buildSMBTransactionEmptySuccess(req []byte) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}
	out := make([]byte, smbHeaderLen+1+20+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[5:9], smbStatusSuccess)
	out[9] = req[9] | 0x80
	out[32] = 10 // TRANSACTION response word count
	// 20-byte parameter block left as zero (no params/data)
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1+20:smbHeaderLen+1+22], 0)
	return out
}

// HandleDatagram implements netbios.CommandHandler.
