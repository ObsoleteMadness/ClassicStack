package smb

// AndX chaining ([smb6.0] 988 "ANDX SMB Messages"): "LANMAN1.0 and later
// dialects of the SMB protocol allow multiple SMB requests to be sent in one
// message to the server." The embedded command "does not repeat the SMB header
// information. Rather the next SMB starts at the WordCount field" (rule 1), and
// each block "contains the offset (from the start of the SMB header) to the
// next chained request/response (in the AndXOffset field)" (rule 9).
//
// NT-family redirectors depend on this: NT 3.51 opens a share with one message
// chaining SESSION_SETUP_ANDX → TREE_CONNECT_ANDX and treats a reply whose
// AndXCommand is 0xFF (chain not processed) as a failed tree connect
// (netbeui.pcap frames 174/175 — the client fell back to IPC$ and reported
// "access denied" to the user).

import (
	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// isAndXRequest reports whether cmd is an "AndX" command — one whose parameter
// words begin AndXCommand / AndXReserved / AndXOffset so a secondary command
// may follow in the same message ([smb6.0] 988).
func isAndXRequest(cmd uint8) bool {
	switch cmd {
	case protocol.CommandLockingAndX,
		protocol.CommandOpenAndX,
		protocol.CommandReadAndX,
		protocol.CommandWriteAndX,
		protocol.CommandSessionSetupAndX,
		protocol.CommandLogoffAndX,
		protocol.CommandTreeConnectAndX,
		protocol.CommandNtCreateAndX:
		return true
	}
	return false
}

// maxAndXChain bounds how many chained blocks one message may carry. Real
// clients chain two, occasionally three commands; the bound only guards
// against a crafted frame whose offsets never terminate.
const maxAndXChain = 8

// fidGrantingCommands / fidConsumingCommands identify the AndX commands that
// hand out a FID in their response, and the AndX commands whose request block
// carries a FID at the same words[4:6] slot ([smb6.0] 1000, rule 5).
func fidGrantsFID(cmd uint8) bool {
	return cmd == protocol.CommandOpenAndX || cmd == protocol.CommandNtCreateAndX
}

func commandConsumesFID(cmd uint8) bool {
	switch cmd {
	case protocol.CommandReadAndX, protocol.CommandWriteAndX, protocol.CommandLockingAndX:
		return true
	}
	return false
}

// grantedFID extracts the FID a successful Open/NtCreate AndX response block
// just handed out. block is the response bytes starting at the block's
// WordCount byte. OpenAndX (WCT=15) carries FID at words[4:6]; NtCreateAndX
// (WCT=34) carries it one word later, at words[5:7].
func grantedFID(cmd uint8, block []byte) (uint16, bool) {
	var wordByteOff int // byte offset of the FID within the words area
	switch cmd {
	case protocol.CommandOpenAndX:
		wordByteOff = 4
	case protocol.CommandNtCreateAndX:
		wordByteOff = 5
	default:
		return 0, false
	}
	fidOff := 1 + wordByteOff // skip WordCount byte
	if len(block) < fidOff+2 {
		return 0, false
	}
	return bp.LE16(block[fidOff : fidOff+2]), true
}

// processAndXChain serves the secondary commands chained after an AndX request
// and splices their response blocks onto resp, returning the combined message.
// resp is the already-built response to the primary command in req.
//
//   - "There is one message sent containing the chained requests and there is
//     one response message to the chained requests" ([smb6.0] 996, rule 3).
//   - "The server will implicitly use the result of the first command in the
//     'X' command" (rule 5): the UID granted by SESSION_SETUP_ANDX and the TID
//     granted by TREE_CONNECT_ANDX are carried into each chained dispatch and
//     ride out in the single response header. Likewise "the Fid obtained in the
//     SMB_COM_OPEN_ANDX would be used in the embedded SMB_COM_READ" ([smb6.0]
//     1000): OS/2 chains OPEN_ANDX → READ_ANDX in one message (netbeui.pcap
//     frame 812), and its Read AndX block carries a placeholder FID the client
//     cannot know in advance — the FID field of a chained Read/Write/Locking
//     AndX request is overwritten with the FID just granted by a preceding
//     Open/NtCreate AndX before that chained command is dispatched.
//   - "The first Command to encounter an error will stop all further
//     processing of embedded commands" (rule 7); "In all cases the error
//     information are returned in the SMB header at the start of the response
//     buffer" (rule 8).
func (s *Service) processAndXChain(sess *smbSession, h protocol.Header, req, resp []byte) []byte {
	curCmd := h.Command
	reqOff := protocol.HeaderLen  // WordCount offset of the current request block
	respOff := protocol.HeaderLen // WordCount offset of the last response block

	for i := 0; i < maxAndXChain; i++ {
		if !isAndXRequest(curCmd) || resp == nil {
			return resp
		}
		rh, err := protocol.DecodeHeader(resp)
		if err != nil || rh.Status != statusSuccess {
			// Rule 7: an error stops the chain (zero is success in both the
			// NTSTATUS and the DOS class/code wire forms).
			return resp
		}
		// The current request block must carry the AndX link words and the
		// current response block must have the slot to point onward from.
		if len(req) < reqOff+5 || int(req[reqOff]) < 2 ||
			len(resp) < respOff+5 || int(resp[respOff]) < 2 {
			return resp
		}
		next := req[reqOff+1]
		nextOff := int(bp.LE16(req[reqOff+3 : reqOff+5]))
		if next == protocol.CommandNoAndXCommand {
			return resp
		}
		// AndXOffset is "from the start of the SMB header to the next chained
		// request" (rule 9). It must advance past the current block and land
		// inside the message, or the chain is malformed — stop.
		if nextOff <= reqOff || nextOff >= len(req) {
			return resp
		}

		// Synthesize a standalone request for the chained command: the shared
		// header followed by the chained block, so every handler parses it at
		// the usual reqBody position. Rule 5: the granted UID/TID accumulated
		// in the response header so far feed the chained command.
		chained := make([]byte, 0, protocol.HeaderLen+len(req)-nextOff)
		chained = append(chained, req[:protocol.HeaderLen]...)
		chained = append(chained, req[nextOff:]...)
		ch := h
		ch.Command = next
		ch.TID = rh.TID
		ch.UID = rh.UID

		// Rule 5 FID inheritance: a chained Read/Write/Locking AndX carries a
		// placeholder FID the client filled in before the Open/NtCreate it
		// follows had actually granted one — overwrite it with the real FID
		// from the response block just built for that Open/NtCreate.
		if fidGrantsFID(curCmd) && commandConsumesFID(next) {
			if fid, ok := grantedFID(curCmd, resp[respOff:]); ok {
				const chainedFIDOff = protocol.HeaderLen + 1 + 4 // WCT byte + words[4:6]
				if len(chained) >= chainedFIDOff+2 {
					bp.PutLE16(chained[chainedFIDOff:chainedFIDOff+2], fid)
				}
			}
		}

		chainResp := s.dispatchOne(sess, ch, chained)
		if len(chainResp) <= protocol.HeaderLen {
			return resp // silent-drop or malformed — leave the chain as answered so far
		}

		// Splice: point the last block's AndX link at the appended block, then
		// append the chained response body (header stripped, rule 1). The
		// chained response's header fields — status (rule 8) and any newly
		// granted TID/UID — become the single response header's; byte 4 (the
		// Command, which must stay the primary command's) is excluded.
		newBlock := len(resp)
		resp[respOff+1] = next
		bp.PutLE16(resp[respOff+3:respOff+5], uint16(newBlock))
		resp = append(resp, chainResp[protocol.HeaderLen:]...)
		copy(resp[5:protocol.HeaderLen], chainResp[5:protocol.HeaderLen])

		curCmd = next
		reqOff = nextOff
		respOff = newBlock
	}
	return resp
}
