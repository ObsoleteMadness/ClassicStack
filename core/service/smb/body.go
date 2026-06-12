package smb

import (
	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// --- request-body parsing + reply assembly shared by the FS command engine.
// Every SMB1 message after the 32-byte header is a WordCount-prefixed parameter
// block followed by a ByteCount-prefixed data area; these helpers slice that
// uniformly so each handler reads its own fields without re-deriving offsets, and
// assemble the reply with the same framing. ---

// reqBody splits a request frame into its parameter words and byte area. words is
// the WCT*2 parameter bytes; area is the BCC data bytes. ok is false if the frame
// is truncated (a malformed packet the caller refuses rather than mis-parses).
func reqBody(req []byte) (words, area []byte, ok bool) {
	if len(req) < protocol.HeaderLen+1 {
		return nil, nil, false
	}
	wct := int(req[protocol.HeaderLen])
	wStart := protocol.HeaderLen + 1
	bccOff := wStart + 2*wct
	if len(req) < bccOff+2 {
		return nil, nil, false
	}
	bcc := int(bp.LE16(req[bccOff : bccOff+2]))
	dataOff := bccOff + 2
	if len(req) < dataOff+bcc {
		return nil, nil, false
	}
	return req[wStart:bccOff], req[dataOff : dataOff+bcc], true
}

// reply assembles an SMB1 response: the request header echoed back with the reply
// flag + wire status, then a WordCount-prefixed words block and a
// ByteCount-prefixed area. wordCount is the WCT value to stamp (words must be
// exactly 2*wordCount bytes). The wire status is derived from the request's
// flags2 (NT vs DOS form) by toWireStatus.
func reply(h protocol.Header, status uint32, wordCount int, words, area []byte) []byte {
	rh := responseHeader(h, toWireStatus(h.Flags2, status))
	out := rh.Encode(nil)
	out = append(out, byte(wordCount))
	out = append(out, words...)
	out = append(out, byte(len(area)), byte(len(area)>>8))
	out = append(out, area...)
	return out
}

// successNoData builds the canonical header-only success reply (WCT=0, BCC=0)
// many path operations return (DELETE, RENAME, CREATE_DIRECTORY, …).
func successNoData(h protocol.Header) []byte {
	return reply(h, statusSuccess, 0, nil, nil)
}

// errResponse builds a header-only error reply carrying the given NTSTATUS in the
// request's wire form.
func errResponse(h protocol.Header, status uint32) []byte {
	return reply(h, status, 0, nil, nil)
}
