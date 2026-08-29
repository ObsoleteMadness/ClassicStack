package smb

import (
	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// --- The multiplexed / raw transfer commands Win9x probes for and DOS clients
// occasionally issue: READ_MPX / WRITE_MPX / WRITE_RAW, plus SMB_COM_SEEK. We do
// not advertise CAP_RAW_MODE (MaxRawSize=0 in NEGOTIATE), so READ_MPX and
// WRITE_RAW answer the spec-mandated fall-back forms that steer the client back
// to plain READ / WRITE; WRITE_MPX is served for real because Win9x uses it for
// bulk copies and a wrong reply silently corrupts the file. Ported from the
// legacy service/smb command_file_io.go. ---

// handleReadMPX answers SMB_COM_READ_MPX (0x1B) with STATUS_USE_STANDARD
// (ERRSRV/ERRuseSTD), which tells the client to fall back to SMB_COM_READ. We
// never advertise CAP_RAW_MODE, so multiplexed read is not offered.
func (s *Service) handleReadMPX(sess *smbSession, h protocol.Header, req []byte) []byte {
	_ = req
	return errResponse(h, statusUseStandard)
}

// handleWriteRaw answers SMB_COM_WRITE_RAW (0x1D) with the spec-mandated Final
// Server Response carrying Count=0 (WCT=1, BCC=0). [MS-CIFS] §3.3.5.26 requires
// CAP_RAW_MODE before honouring raw write; we do not advertise it, so this
// zero-count response steers Win9x to plain SMB_COM_WRITE.
func (s *Service) handleWriteRaw(sess *smbSession, h protocol.Header, req []byte) []byte {
	_ = req
	w := make([]byte, 2) // Count = 0
	return reply(h, statusSuccess, 1, w, nil)
}

// handleWriteMPX serves SMB_COM_WRITE_MPX (0x1E) per [MS-CIFS] §2.2.4.26 /
// §3.3.5.27. The client sends a sequence of WRITE_MPX requests sharing a MID/CID,
// each carrying a data chunk at its own ByteOffsetToBeginWrite and a unique
// RequestMask bit. The server writes each chunk and ORs the RequestMask into a
// per-FID accumulator, replying ONLY to the final request (marked by a non-zero
// SequenceNumber in the header's SecurityFeatures), whose reply echoes the
// accumulated ResponseMask. Acking non-final chunks would corrupt the client's
// window arithmetic, so those return nil (no reply).
//
// Request words (WCT=12): FID(2) TotalByteCount(2) Reserved(2)
// ByteOffsetToBeginWrite(4) Timeout(4) WriteMode(2) RequestMask(4) DataLength(2)
// DataOffset(2). The data sits at a header-relative DataOffset.
func (s *Service) handleWriteMPX(sess *smbSession, h protocol.Header, req []byte) []byte {
	isFinal := h.SequenceNumber() != 0

	words, _, ok := reqBody(req)
	if !ok || len(words) < 24 {
		if isFinal {
			return errResponse(h, statusUnsuccessful)
		}
		return nil
	}
	fid := bp.LE16(words[0:2])
	offset := int64(bp.LE32(words[6:10]))
	requestMask := bp.LE32(words[16:20])
	dataLen := int(bp.LE16(words[20:22]))
	dataOff := int(bp.LE16(words[22:24]))
	if dataOff < 0 || dataOff+dataLen > len(req) || dataOff > dataOff+dataLen {
		if isFinal {
			return errResponse(h, statusUnsuccessful)
		}
		return nil
	}
	data := req[dataOff : dataOff+dataLen]

	if len(data) > 0 {
		if _, st := s.writeAt(sess, fid, offset, data); st != statusSuccess {
			// [MS-CIFS] §3.3.5.27 defers pre-final errors to the final response;
			// we cannot easily defer, so surface only on the sequenced request.
			if isFinal {
				return errResponse(h, st)
			}
			return nil
		}
	}

	// Accumulate this request's RequestMask; reply only on the final request,
	// then reset the accumulator for the next sequence.
	sess.mu.Lock()
	hnd, ok := sess.fids[fid]
	if !ok || hnd == nil {
		sess.mu.Unlock()
		if isFinal {
			return errResponse(h, statusInvalidHandle)
		}
		return nil
	}
	hnd.mpxAccum |= requestMask
	accumulated := hnd.mpxAccum
	if isFinal {
		hnd.mpxAccum = 0
	}
	sess.mu.Unlock()

	if !isFinal {
		return nil
	}
	w := make([]byte, 4)
	bp.PutLE32(w[0:4], accumulated) // ResponseMask
	return reply(h, statusSuccess, 2, w, nil)
}

// handleSeek answers SMB_COM_SEEK (0x12). Request words (WCT=4): FID(2) Mode(2)
// Offset(4, signed). We do positional I/O and hold no per-handle seek cursor, so
// SEEK_SET/SEEK_CUR echo the requested offset (current position is treated as 0)
// and SEEK_END resolves against the file's live size. Reply WCT=2: Offset(4).
func (s *Service) handleSeek(sess *smbSession, h protocol.Header, req []byte) []byte {
	words, _, ok := reqBody(req)
	if !ok || len(words) < 8 {
		return errResponse(h, statusNotSupported)
	}
	fid := bp.LE16(words[0:2])
	mode := bp.LE16(words[2:4])
	delta := int64(int32(bp.LE32(words[4:8])))

	hnd, ok := sess.fileByFID(fid)
	if !ok || hnd == nil || hnd.file == nil {
		return errResponse(h, statusInvalidHandle)
	}

	var base int64
	switch mode {
	case 0, 1: // SEEK_SET / SEEK_CUR — no tracked cursor, current position is 0
		base = 0
	case 2: // SEEK_END
		info, err := hnd.file.Stat()
		if err != nil {
			return errResponse(h, statusUnsuccessful)
		}
		base = info.Size()
	default:
		return errResponse(h, statusUnsuccessful)
	}
	pos := base + delta
	if pos < 0 {
		return errResponse(h, statusUnsuccessful)
	}

	w := make([]byte, 4)
	bp.PutLE32(w[0:4], uint32(pos))
	return reply(h, statusSuccess, 2, w, nil)
}
