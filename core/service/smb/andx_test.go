package smb

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// treeConnectBlock builds the TREE_CONNECT_ANDX request block (WordCount +
// words + ByteCount + bytes, no header) for the given UNC path: WCT=4 words —
// AndXCommand(1) AndXReserved(1) AndXOffset(2) Flags(2) PasswordLength(2) —
// then password(1 NUL) + path + service "?????".
func treeConnectBlock(unc string) []byte {
	words := make([]byte, 8)
	words[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(words[6:8], 1) // PasswordLength = 1
	area := []byte{0x00}
	area = append(area, []byte(unc)...)
	area = append(area, 0)
	area = append(area, []byte("?????")...)
	area = append(area, 0)

	out := []byte{byte(len(words) / 2)}
	out = append(out, words...)
	out = append(out, byte(len(area)), byte(len(area)>>8))
	out = append(out, area...)
	return out
}

// chainedSetupTreeConnect builds the message NT 3.51 opens a share with
// (netbeui.pcap frame 174): SESSION_SETUP_ANDX whose AndXCommand chains a
// TREE_CONNECT_ANDX block, AndXOffset pointing at the chained WordCount
// ([smb6.0] 1008, rule 9: offset from the start of the SMB header).
func chainedSetupTreeConnect(flags2 uint16, unc string) []byte {
	// SESSION_SETUP_ANDX words (WCT=13, NT LM 0.12 form), password lengths zero.
	ssWords := make([]byte, 26)
	ssWords[0] = protocol.CommandTreeConnectAndX
	frame := smbReq(protocol.CommandSessionSetupAndX, flags2, 0, 0, ssWords, nil)
	// Patch the AndXOffset (words[2:4], i.e. frame[HeaderLen+3:HeaderLen+5]) to
	// the chained block appended at the current end of the frame.
	bp.PutLE16(frame[protocol.HeaderLen+3:protocol.HeaderLen+5], uint16(len(frame)))
	return append(frame, treeConnectBlock(unc)...)
}

// TestDispatch_AndXChain_SessionSetupTreeConnect proves a chained
// SESSION_SETUP_ANDX → TREE_CONNECT_ANDX message is served as one response
// carrying both blocks ([smb6.0] 996, rule 3), with the first block's AndX link
// patched to the second and the granted TID in the shared header (rule 5).
// This is the NT 3.51 share-open path: the redirector treats an un-processed
// chain (AndXCommand 0xFF in the reply) as a failed tree connect.
func TestDispatch_AndXChain_SessionSetupTreeConnect(t *testing.T) {
	svc, sess := newDispatchService(t)
	req := chainedSetupTreeConnect(protocol.Flags2NTStatus, "\\\\SERVER\\PUBLIC")

	reply := svc.Dispatch(sess, req)
	h := respHeader(t, reply)
	if h.Status != statusSuccess {
		t.Fatalf("chained setup+treeconnect status = %#x, want success", h.Status)
	}
	if h.TID == 0 {
		t.Fatal("chained TREE_CONNECT_ANDX granted no TID in the response header")
	}
	tc, ok := sess.tree(h.TID)
	if !ok || tc.share == nil || tc.share.Name() != "PUBLIC" {
		t.Fatalf("TID %d not bound to PUBLIC (tc=%+v ok=%v)", h.TID, tc, ok)
	}

	// First block: SESSION_SETUP_ANDX response with its AndX link patched to
	// the appended TREE_CONNECT_ANDX block.
	if got := reply[protocol.HeaderLen+1]; got != protocol.CommandTreeConnectAndX {
		t.Fatalf("first block AndXCommand = %#x, want TREE_CONNECT_ANDX", got)
	}
	off := int(bp.LE16(reply[protocol.HeaderLen+3 : protocol.HeaderLen+5]))
	if off <= protocol.HeaderLen || off >= len(reply) {
		t.Fatalf("first block AndXOffset = %d, out of range (len %d)", off, len(reply))
	}
	// Second block: TREE_CONNECT_ANDX response (WCT=3) terminating the chain.
	if wct := reply[off]; wct != 3 {
		t.Fatalf("chained block WCT = %d, want 3", wct)
	}
	if got := reply[off+1]; got != protocol.CommandNoAndXCommand {
		t.Fatalf("chained block AndXCommand = %#x, want none (0xFF)", got)
	}
}

// TestDispatch_AndXChain_ErrorStopsChain proves a chained command that fails
// puts its error in the single response header ([smb6.0] 1006, rule 8) while
// the successfully processed first block is still present.
func TestDispatch_AndXChain_ErrorStopsChain(t *testing.T) {
	svc, sess := newDispatchService(t)
	req := chainedSetupTreeConnect(protocol.Flags2NTStatus, "\\\\SERVER\\NOPE")

	reply := svc.Dispatch(sess, req)
	h := respHeader(t, reply)
	if h.Status != statusBadNetworkName {
		t.Fatalf("chained bad-share status = %#x, want STATUS_BAD_NETWORK_NAME", h.Status)
	}
	// The session setup itself succeeded: its block links to the failed one.
	if got := reply[protocol.HeaderLen+1]; got != protocol.CommandTreeConnectAndX {
		t.Fatalf("first block AndXCommand = %#x, want TREE_CONNECT_ANDX", got)
	}
	if sess.uid == 0 {
		t.Fatal("session setup before the failed chained command was not applied")
	}
}

// TestDispatch_AndXChain_TerminatorUnchanged proves a plain (unchained) AndX
// request — AndXCommand 0xFF — is answered exactly as before the chain walker.
func TestDispatch_AndXChain_TerminatorUnchanged(t *testing.T) {
	svc, sess := newDispatchService(t)
	ssWords := make([]byte, 26)
	ssWords[0] = protocol.CommandNoAndXCommand
	req := smbReq(protocol.CommandSessionSetupAndX, protocol.Flags2NTStatus, 0, 0, ssWords, nil)

	reply := svc.Dispatch(sess, req)
	h := respHeader(t, reply)
	if h.Status != statusSuccess {
		t.Fatalf("status = %#x, want success", h.Status)
	}
	if got := reply[protocol.HeaderLen+1]; got != protocol.CommandNoAndXCommand {
		t.Fatalf("AndXCommand = %#x, want none (0xFF)", got)
	}
}

// openAndXReadAndXBlock builds the message OS/2 sends to open-and-read a file
// in one round trip (netbeui.pcap frame 812): OPEN_ANDX (WCT=15) chaining a
// READ_ANDX (WCT=10) whose FID field is a placeholder — the client cannot
// know the real FID before the Open completes ([smb6.0] 1000, rule 5).
func openAndXReadAndXBlock(tid, uid uint16, path string, placeholderFID uint16) []byte {
	// OPEN_ANDX request words (WCT=15).
	openWords := make([]byte, 30)
	openWords[0] = protocol.CommandReadAndX
	bp.PutLE16(openWords[6:8], 0x0002)   // AccessMode: read/write
	bp.PutLE16(openWords[16:18], 0x0011) // OpenFunction: open-or-create
	area := append([]byte(path), 0)
	req := smbReq(protocol.CommandOpenAndX, protocol.Flags2NTStatus, tid, uid, openWords, area)

	// Chained READ_ANDX block (WCT=10), appended after the Open block.
	readWords := make([]byte, 20)
	readWords[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(readWords[4:6], placeholderFID)
	bp.PutLE16(readWords[10:12], 4096) // MaxCount
	readBlock := []byte{byte(len(readWords) / 2)}
	readBlock = append(readBlock, readWords...)
	readBlock = append(readBlock, 0, 0) // ByteCount = 0

	bp.PutLE16(req[protocol.HeaderLen+3:protocol.HeaderLen+5], uint16(len(req)))
	return append(req, readBlock...)
}

// TestDispatch_AndXChain_OpenReadFIDInheritance proves a chained
// OPEN_ANDX → READ_ANDX message ([smb6.0] 1000, rule 5) serves the READ_ANDX
// against the FID the OPEN_ANDX just granted, not the placeholder FID the
// client put on the wire — OS/2 chains open-then-read this way and got
// STATUS_INVALID_FID back before this was wired up (netbeui.pcap frames
// 812/813).
func TestDispatch_AndXChain_OpenReadFIDInheritance(t *testing.T) {
	svc, sess := newDispatchService(t)
	tid := sess.allocTID(&treeConnect{share: svc.shares[0]})

	req := openAndXReadAndXBlock(tid, 1, "\\Really long file name here.COM", 0xFFFF)

	reply := svc.Dispatch(sess, req)
	h := respHeader(t, reply)
	if h.Status != statusSuccess {
		t.Fatalf("chained open+read status = %#x, want success", h.Status)
	}

	// First block: OPEN_ANDX response (WCT=15), FID at words[4:6].
	if wct := reply[protocol.HeaderLen]; wct != 15 {
		t.Fatalf("first block WCT = %d, want 15", wct)
	}
	grantedFID := bp.LE16(reply[protocol.HeaderLen+1+4 : protocol.HeaderLen+1+6])
	if grantedFID == 0xFFFF {
		t.Fatal("OPEN_ANDX did not grant a real FID")
	}

	off := int(bp.LE16(reply[protocol.HeaderLen+3 : protocol.HeaderLen+5]))
	if off <= protocol.HeaderLen || off >= len(reply) {
		t.Fatalf("first block AndXOffset = %d, out of range (len %d)", off, len(reply))
	}
	if wct := reply[off]; wct != 12 {
		t.Fatalf("chained READ_ANDX response WCT = %d, want 12", wct)
	}
}

// TestDispatch_NtCreateOnIPCIsNotFound proves an NT_CREATE_ANDX open of an RPC
// pipe on the IPC$ tree (NT 3.51 probing \srvsvc, netbeui.pcap frames 189/190)
// answers STATUS_OBJECT_NAME_NOT_FOUND — "no such pipe", steering the client to
// its RAP fallback — not ACCESS_DENIED, which the NT redirector surfaces to the
// user as a share-access failure.
func TestDispatch_NtCreateOnIPCIsNotFound(t *testing.T) {
	svc, sess := newDispatchService(t)
	tid := sess.allocTID(&treeConnect{ipc: true})

	words := make([]byte, 48) // WCT=24 parameter block, contents irrelevant here
	words[0] = protocol.CommandNoAndXCommand
	area := append([]byte("\\srvsvc"), 0)
	req := smbReq(protocol.CommandNtCreateAndX, protocol.Flags2NTStatus, tid, 1, words, area)

	reply := svc.Dispatch(sess, req)
	h := respHeader(t, reply)
	if h.Status != statusObjectNameNotFound {
		t.Fatalf("IPC$ pipe open status = %#x, want STATUS_OBJECT_NAME_NOT_FOUND", h.Status)
	}
}
