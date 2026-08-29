package smb

import (
	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// notify.go is the §10d SMB wire-push half: NT_TRANSACT NOTIFY_CHANGE. A client
// posts a change-notify request on an open directory handle; the server does NOT
// reply immediately — it holds the request open (a pendingNotify on the session)
// and completes it asynchronously when a change occurs under the watched tree. The
// completion is pushed back over the session's transport circuit (Conn.SetPushWriter),
// the server-initiated channel the transports install. This is what lets a Windows
// client refresh an Explorer window when a same-host-path AFP volume (or an external
// editor, via §10e) mutates the directory.
//
// NOTIFY_CHANGE is one-shot per [MS-CIFS] §2.2.7.4.2: each fired notification
// consumes the request; the client re-arms by posting a fresh one. We watch at the
// share (tree) granularity rather than the exact directory — coarser than Windows,
// but a faithful, safe superset for a compatibility server (a client re-reads and
// sees the actual change). The WatchTree flag and the specific directory FID are
// accepted and recorded but not used to narrow the match in this slice.

// ntTransactNotifyChange is the NT_TRANSACT subcommand (Function) for
// NT_TRANSACT_NOTIFY_CHANGE ([MS-CIFS] §2.2.7.4).
const ntTransactNotifyChange uint16 = 0x0004

// FILE_ACTION_* values in a FILE_NOTIFY_INFORMATION record ([MS-FSCC] §2.7.1).
// The CompletionFilter the client sends is recorded on the watch (pendingNotify.filter)
// but not used to narrow matching in this slice — the watch is share-coarse, so any
// change under the tree completes it and the client re-reads.
const (
	fileActionAdded          uint32 = 0x00000001
	fileActionRemoved        uint32 = 0x00000002
	fileActionModified       uint32 = 0x00000003
	fileActionRenamedNewName uint32 = 0x00000005
)

// handleNtTransact decodes an NT_TRANSACT (0xA0) and routes its Function. Only
// NOTIFY_CHANGE is served (it is the one async-completion subcommand the §10d push
// needs); other NT_TRANSACT functions answer STATUS_NOT_SUPPORTED, as before.
func (s *Service) handleNtTransact(sess *smbSession, h smb.Header, req []byte) []byte {
	fn, setup, ok := parseNtTransactSetup(req)
	if !ok {
		return buildErrorResponse(h, req, statusNotSupported)
	}
	if fn != ntTransactNotifyChange {
		return buildErrorResponse(h, req, statusNotSupported)
	}
	return s.handleNotifyChange(sess, h, setup)
}

// handleNotifyChange registers a held watch from an NT_TRANSACT_NOTIFY_CHANGE Setup
// (CompletionFilter(4) FID(2) WatchTree(1) Reserved(1)) and returns NIL — the server
// holds the request open rather than replying, completing it later from the reactor.
// A request on an unbound TID, or a session whose transport cannot push, is still
// registered (uniform bookkeeping); the latter simply never completes.
func (s *Service) handleNotifyChange(sess *smbSession, h smb.Header, setup []byte) []byte {
	if len(setup) < 8 {
		return buildErrorResponse(h, smbReqBytes(setup), statusUnsuccessful)
	}
	filter := bp.LE32(setup[0:4])

	tc, ok := sess.tree(h.TID)
	if !ok || tc.share == nil {
		// No real disk tree to watch (IPC$ or unbound). Reply NOT_SUPPORTED so the
		// client does not wait forever on a tree that can never notify.
		return buildErrorResponse(h, nil, statusNotSupported)
	}

	sess.addWatch(&pendingNotify{
		tid:    h.TID,
		uid:    h.UID,
		mid:    h.MID,
		pidLow: h.PIDLow,
		pidHi:  h.PIDHigh,
		flags2: h.Flags2,
		filter: filter,
		share:  tc.share,
	})
	return nil // held open — no immediate reply
}

// notifyFSChange is the §10d reactor sink for SMB: a foreign-origin FS mutation
// under one of this service's shares fired the reactor with the affected share name.
// It completes every held NOTIFY_CHANGE bound to that share by pushing a
// FILE_NOTIFY_INFORMATION completion over each session's circuit. One-shot: a fired
// watch is consumed (takeWatchesFor removes it). Sessions with no held watch, or a
// transport with no push channel, see nothing.
func (s *Service) notifyFSChange(shareName string, ev fs.Event) {
	sh, ok := s.ShareByName(shareName)
	if !ok {
		return
	}
	for _, sess := range s.liveSessions() {
		fired, push := sess.takeWatchesFor(sh)
		if push == nil {
			continue
		}
		for _, w := range fired {
			push(buildNotifyChangeCompletion(w, ev))
		}
	}
}

// buildNotifyChangeCompletion frames one NT_TRANSACT NOTIFY_CHANGE completion: an
// SMB header echoing the held request's ids, NT_TRANSACT response words, and an
// NT-parameter block holding one FILE_NOTIFY_INFORMATION record (NextEntryOffset=0,
// the FILE_ACTION_* for the op, and the changed leaf name in UTF-16LE). It carries
// the changed name's host leaf — the client treats it as a hint and re-reads.
func buildNotifyChangeCompletion(w *pendingNotify, ev fs.Event) []byte {
	info := buildFileNotifyInformation(actionForOp(ev.Op), hostLeaf(ev))

	rh := smb.Header{
		Command: smb.CommandNtTransact,
		Status:  statusSuccess,
		Flags:   smb.FlagReply,
		Flags2:  w.flags2 | smb.Flags2KnowsLongNames,
		PIDHigh: w.pidHi,
		TID:     w.tid,
		PIDLow:  w.pidLow,
		UID:     w.uid,
		MID:     w.mid,
	}
	out := rh.Encode(nil)

	// NT_TRANSACT response WordCount = 18 (0x12): 3 reserved bytes + 8 LE32 fields +
	// SetupCount(1). ParameterOffset is measured from the SMB header start.
	const wordCount = 18
	paramCount := uint32(len(info))
	// Header(32) + WCT(1) + 18 words(36) + BCC(2) = 71; pad ParameterOffset to it.
	paramOffset := uint32(smb.HeaderLen + 1 + wordCount*2 + 2)

	out = append(out, wordCount)
	out = append(out, 0, 0, 0)                  // Reserved1[3]
	out = bp.AppendLE32(out, paramCount)        // TotalParameterCount
	out = bp.AppendLE32(out, 0)                 // TotalDataCount
	out = bp.AppendLE32(out, paramCount)        // ParameterCount
	out = bp.AppendLE32(out, paramOffset)       // ParameterOffset
	out = bp.AppendLE32(out, 0)                 // ParameterDisplacement
	out = bp.AppendLE32(out, 0)                 // DataCount
	out = bp.AppendLE32(out, 0)                 // DataOffset
	out = bp.AppendLE32(out, 0)                 // DataDisplacement
	out = append(out, 0)                        // SetupCount
	out = bp.AppendLE16(out, uint16(len(info))) // ByteCount (BCC)
	out = append(out, info...)                  // the FILE_NOTIFY_INFORMATION block
	return out
}

// buildFileNotifyInformation packs one FILE_NOTIFY_INFORMATION record ([MS-FSCC]
// §2.7.1): NextEntryOffset(4)=0, Action(4), FileNameLength(4 bytes of UTF-16),
// FileName (UTF-16LE, no NUL terminator).
func buildFileNotifyInformation(action uint32, name string) []byte {
	nameUTF16 := utf16le(name)
	out := make([]byte, 0, 12+len(nameUTF16))
	out = bp.AppendLE32(out, 0)                      // NextEntryOffset (single record)
	out = bp.AppendLE32(out, action)                 // Action
	out = bp.AppendLE32(out, uint32(len(nameUTF16))) // FileNameLength (bytes)
	out = append(out, nameUTF16...)
	return out
}

// actionForOp maps an fs.Op to the FILE_ACTION_* a NOTIFY_CHANGE record reports.
func actionForOp(op fs.Op) uint32 {
	switch op {
	case fs.OpCreate:
		return fileActionAdded
	case fs.OpDelete:
		return fileActionRemoved
	case fs.OpRename:
		return fileActionRenamedNewName
	default: // OpModify, OpAttrChange
		return fileActionModified
	}
}

// hostLeaf returns the leaf name of the changed host path (the new path for a
// rename), for the notification's FileName field.
func hostLeaf(ev fs.Event) string {
	p := ev.HostPath
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}

// utf16le encodes s as little-endian UTF-16 (BMP only; supplementary planes are
// encoded as surrogate pairs by range). No BOM, no terminator.
func utf16le(s string) []byte {
	out := make([]byte, 0, len(s)*2)
	for _, r := range s {
		if r > 0xFFFF {
			r -= 0x10000
			hi := 0xD800 + (r >> 10)
			lo := 0xDC00 + (r & 0x3FF)
			out = append(out, byte(hi), byte(hi>>8), byte(lo), byte(lo>>8))
			continue
		}
		out = append(out, byte(r), byte(r>>8))
	}
	return out
}

// parseNtTransactSetup extracts the Function and Setup bytes from an NT_TRANSACT
// request. Layout after the 32-byte header: WordCount(1), then
// MaxSetupCount(1) Reserved1(2) TotalParameterCount(4) TotalDataCount(4)
// MaxParameterCount(4) MaxDataCount(4) ParameterCount(4) ParameterOffset(4)
// DataCount(4) DataOffset(4) SetupCount(1) Function(2) Setup[SetupCount*2].
func parseNtTransactSetup(req []byte) (fn uint16, setup []byte, ok bool) {
	if len(req) < smb.HeaderLen+1 {
		return 0, nil, false
	}
	p := smb.HeaderLen
	wct := int(req[p])
	p++
	// The NT_TRANSACT primary request has WordCount >= 19 (0x13): 18 fixed + Function.
	if wct < 19 || len(req) < p+wct*2 {
		return 0, nil, false
	}
	words := req[p : p+wct*2]
	// SetupCount is at word offset: skip MaxSetupCount(1)+Reserved1(2)=3 bytes, then
	// 8 LE32 fields (32 bytes) = byte 35; SetupCount(1) then Function(2).
	const setupCountOff = 3 + 8*4 // = 35
	if len(words) < setupCountOff+1+2 {
		return 0, nil, false
	}
	setupCount := int(words[setupCountOff])
	fn = bp.LE16(words[setupCountOff+1 : setupCountOff+3])
	setupStart := setupCountOff + 3
	need := setupStart + setupCount*2
	if len(words) < need {
		return 0, nil, false
	}
	return fn, words[setupStart:need], true
}

// smbReqBytes is a tiny shim so a malformed-setup error can reuse buildErrorResponse
// (which wants the request bytes only for length checks). The setup alone suffices.
func smbReqBytes(setup []byte) []byte { return setup }
