package smb

import (
	"bytes"
	"encoding/binary"
	"time"
)

func buildSMBErrorResponse(req []byte, status uint32) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}
	out := make([]byte, smbHeaderLen+3)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[5:9], status)
	out[9] = req[9] | 0x80
	out[32] = 0
	binary.LittleEndian.PutUint16(out[33:35], 0)
	return out
}

// findNegotiateDialect scans the dialect list in a SMB_COM_NEGOTIATE
// request and returns the 0-based index of the named dialect, or -1
// if not found.
func findNegotiateDialect(req []byte, name string) int {
	if len(req) < smbHeaderLen+3 {
		return -1
	}
	byteCount := int(binary.LittleEndian.Uint16(req[smbHeaderLen+1 : smbHeaderLen+3]))
	if len(req) < smbHeaderLen+3+byteCount {
		return -1
	}
	rest := req[smbHeaderLen+3 : smbHeaderLen+3+byteCount]
	idx := 0
	for len(rest) >= 2 {
		if rest[0] != 0x02 {
			break
		}
		rest = rest[1:]
		nul := bytes.IndexByte(rest, 0)
		if nul < 0 {
			break
		}
		if string(rest[:nul]) == name {
			return idx
		}
		rest = rest[nul+1:]
		idx++
	}
	return -1
}

// buildNegotiateResponse constructs an SMB_COM_NEGOTIATE response
// accepting the NT LM 0.12 dialect (WCT=17). SecurityMode is set to
// user-level without challenge so Win98 can send plain-text credentials
// and proceed to the guest session path.
func buildNegotiateResponse(req []byte, workgroup string) []byte {
	if len(req) < smbHeaderLen {
		return nil
	}
	dialectIdx := findNegotiateDialect(req, dialectNTLM)
	if dialectIdx < 0 {
		dialectIdx = 0
	}
	domain := normalizeBrowserName(workgroup)
	domainBytes := append([]byte(domain), 0)

	paramLen := 34 // 17 words
	out := make([]byte, smbHeaderLen+1+paramLen+2+len(domainBytes))
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 17 // WCT
	w := out[smbHeaderLen+1:]
	binary.LittleEndian.PutUint16(w[0:2], uint16(dialectIdx)) // DialectIndex
	w[2] = 0x01                                               // SecurityMode: user-level, no encryption
	binary.LittleEndian.PutUint16(w[3:5], 50)                 // MaxMpxCount
	binary.LittleEndian.PutUint16(w[5:7], 1)                  // MaxNumberVcs
	binary.LittleEndian.PutUint32(w[7:11], 0x4000)            // MaxBufferSize
	binary.LittleEndian.PutUint32(w[11:15], 0)                // MaxRawSize
	binary.LittleEndian.PutUint32(w[15:19], 0)                         // SessionKey
	binary.LittleEndian.PutUint32(w[19:23], capNTSMBs|capStatus32)     // Capabilities
	ft := uint64(time.Now().UTC().UnixNano()/100) + windowsFiletimeOffset
	binary.LittleEndian.PutUint32(w[23:27], uint32(ft))                // SystemTimeLow
	binary.LittleEndian.PutUint32(w[27:31], uint32(ft>>32))            // SystemTimeHigh
	binary.LittleEndian.PutUint16(w[31:33], 0)                         // ServerTimeZone
	w[33] = 0                                                 // EncryptionKeyLength = 0 (no challenge)
	binary.LittleEndian.PutUint16(w[34:36], uint16(len(domainBytes)))
	copy(w[36:], domainBytes)
	return out
}

// buildSessionSetupResponse constructs an SMB_COM_SESSION_SETUP_ANDX
// response granting a guest session (UID=1, Action=0x0001).
func buildSessionSetupResponse(req []byte, uid uint16) []byte {
	if len(req) < smbHeaderLen {
		return nil
	}
	// WCT=3: AndXCommand(1b)+AndXReserved(1b)+AndXOffset(2b)+Action(2b).
	// ByteCount=2: empty NativeOS and NativeLM strings (two NULs).
	out := make([]byte, smbHeaderLen+1+6+2+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	binary.LittleEndian.PutUint16(out[smbOffUID:smbOffUID+2], uid) // guest session UID
	out[smbHeaderLen] = 3                                          // WCT
	w := out[smbHeaderLen+1:]
	w[0] = 0xFF                                   // AndXCommand = no chaining
	w[1] = 0x00                                   // AndXReserved
	binary.LittleEndian.PutUint16(w[2:4], 0)      // AndXOffset
	binary.LittleEndian.PutUint16(w[4:6], 0x0001) // Action = guest logon
	binary.LittleEndian.PutUint16(w[6:8], 2)      // ByteCount
	w[8] = 0x00                                   // NativeOS = ""
	w[9] = 0x00                                   // NativeLM = ""
	return out
}

// buildTreeConnectResponse constructs an SMB_COM_TREE_CONNECT_ANDX
// response assigning TID=1 with IPC$ service type.
func buildTreeConnectResponse(req []byte) []byte {
	if len(req) < smbHeaderLen {
		return nil
	}
	service := []byte("IPC\x00")
	nativeFS := []byte("\x00")
	byteCount := len(service) + len(nativeFS)
	// WCT=3: AndXCommand(1b)+AndXReserved(1b)+AndXOffset(2b)+OptionalSupport(2b).
	out := make([]byte, smbHeaderLen+1+6+2+byteCount)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	binary.LittleEndian.PutUint16(out[smbOffTID:smbOffTID+2], 1) // TID = 1
	out[smbHeaderLen] = 3                                        // WCT
	w := out[smbHeaderLen+1:]
	w[0] = 0xFF                              // AndXCommand = no chaining
	w[1] = 0x00                              // AndXReserved
	binary.LittleEndian.PutUint16(w[2:4], 0) // AndXOffset
	binary.LittleEndian.PutUint16(w[4:6], 0) // OptionalSupport
	binary.LittleEndian.PutUint16(w[6:8], uint16(byteCount))
	copy(w[8:], service)
	copy(w[8+len(service):], nativeFS)
	return out
}

// buildEchoResponse constructs a base SMB_COM_ECHO response that mirrors
// the request body and sets SequenceNumber to 1.
func buildEchoResponse(req []byte) []byte {
	if len(req) < smbHeaderLen+5 || string(req[0:4]) != "\xffSMB" {
		return nil
	}
	if req[smbHeaderLen] != 1 {
		return nil
	}
	echoCount := binary.LittleEndian.Uint16(req[smbHeaderLen+1 : smbHeaderLen+3])
	if echoCount == 0 {
		return nil
	}
	byteCount := int(binary.LittleEndian.Uint16(req[smbHeaderLen+3 : smbHeaderLen+5]))
	if byteCount < 0 || len(req) < smbHeaderLen+5+byteCount {
		return nil
	}
	data := req[smbHeaderLen+5 : smbHeaderLen+5+byteCount]
	out := make([]byte, smbHeaderLen+1+2+2+len(data))
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 1                                                // WCT
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1:smbHeaderLen+3], 1) // SequenceNumber = 1
	binary.LittleEndian.PutUint16(out[smbHeaderLen+3:smbHeaderLen+5], uint16(len(data)))
	copy(out[smbHeaderLen+5:], data)
	return out
}

func isValidEchoTID(req []byte, conn *connState) bool {
	if len(req) < smbHeaderLen {
		return false
	}
	tid := binary.LittleEndian.Uint16(req[smbOffTID : smbOffTID+2])
	if tid == 0xFFFF || tid == 1 {
		return true
	}
	if conn == nil {
		return false
	}
	conn.mu.Lock()
	_, ok := conn.tids[tid]
	conn.mu.Unlock()
	return ok
}

func (s *Service) handleTreeConnectAndX(req []byte, conn *connState) []byte {
	if conn == nil {
		return buildTreeConnectResponse(req)
	}
	shareName, ok := parseTreeConnectShareName(req)
	if !ok {
		return buildTreeConnectResponse(req)
	}

	normalized := normalizeBrowserName(shareName)
	s.mu.Lock()
	shareIdx, found := s.shareNameToIndex[normalized]
	s.mu.Unlock()
	if !found {
		return buildTreeConnectResponse(req)
	}

	conn.mu.Lock()
	conn.nextTID++
	tid := conn.nextTID
	if tid == 0 {
		conn.nextTID++
		tid = conn.nextTID
	}
	conn.tids[tid] = treeSlot{shareIdx: shareIdx}
	conn.mu.Unlock()

	return buildTreeConnectResponseWithTID(req, tid)
}

// handleQueryInformationDisk (0x80) reports disk geometry and free space
// for the share associated with the request's TID.

func buildTreeConnectResponseWithTID(req []byte, tid uint16) []byte {
	if len(req) < smbHeaderLen {
		return nil
	}
	service := []byte("A:\x00")
	nativeFS := []byte("\x00")
	byteCount := len(service) + len(nativeFS)
	out := make([]byte, smbHeaderLen+1+6+2+byteCount)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	binary.LittleEndian.PutUint16(out[smbOffTID:smbOffTID+2], tid)
	out[smbHeaderLen] = 3
	w := out[smbHeaderLen+1:]
	w[0] = 0xFF
	w[1] = 0x00
	binary.LittleEndian.PutUint16(w[2:4], 0)
	binary.LittleEndian.PutUint16(w[4:6], 0)
	binary.LittleEndian.PutUint16(w[6:8], uint16(byteCount))
	copy(w[8:], service)
	copy(w[8+len(service):], nativeFS)
	return out
}

// handleCheckDirectory (0x10) verifies a path is a directory.
