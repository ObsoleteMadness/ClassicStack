package smb

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// fakeBrowseProvider is a test BrowseProvider with controllable availability and a
// fixed server list.
type fakeBrowseProvider struct {
	available bool
	entries   []BrowseServer
}

func (f *fakeBrowseProvider) Available() bool               { return f.available }
func (f *fakeBrowseProvider) ServerEntries() []BrowseServer { return f.entries }

// lanmanReq builds an SMB_COM_TRANSACTION request to \PIPE\LANMAN carrying a RAP
// call: the byte area is "\PIPE\LANMAN\0" + function(2) + ParamDesc\0 + DataDesc\0
// + ReceiveBufferLength(2) + ServerType(4). The transaction words are zeroed (the
// handler reads only the byte area).
func lanmanReq(tid uint16, fn uint16, serverType uint32) []byte {
	area := append([]byte("\\PIPE\\LANMAN"), 0)
	fnb := make([]byte, 2)
	bp.PutLE16(fnb, fn)
	area = append(area, fnb...)
	area = append(area, []byte("WrLeh")...) // ParamDesc
	area = append(area, 0)
	area = append(area, []byte("B16BBDz")...) // DataDesc
	area = append(area, 0)
	rb := make([]byte, 2)
	bp.PutLE16(rb, 0xFFFF) // ReceiveBufferLength
	area = append(area, rb...)
	st := make([]byte, 4)
	bp.PutLE32(st, serverType)
	area = append(area, st...)

	// TRANSACTION request: WCT=14 (SetupCount=0 form is fine; handler ignores words).
	// NT_STATUS flag set so an error reply carries the raw NTSTATUS (not DOS-mapped).
	words := make([]byte, 28)
	return smbReq(protocol.CommandTransaction, protocol.Flags2NTStatus, tid, 1, words, area)
}

// ipcSession returns a service (with the given provider) and a session whose TID is
// bound to the IPC$ pipe tree.
func ipcSession(t *testing.T, p BrowseProvider) (*Service, *smbSession, uint16) {
	t.Helper()
	svc := &Service{shares: []*Share{newTestShare(t)}}
	svc.SetBrowseProvider(p)
	sess := newSession("")
	tid := sess.allocTID(&treeConnect{ipc: true})
	return svc, sess, tid
}

// rapParams decodes the RAP parameter block (Status/Converter/Returned/Available)
// from a TRANSACTION response.
func rapParams(t *testing.T, reply []byte) (status, returned, available uint16) {
	t.Helper()
	respHeader(t, reply) // assert reply flag
	paramOffset := protocol.HeaderLen + 1 + 20 + 2
	if len(reply) < paramOffset+8 {
		t.Fatalf("response too short for RAP params: %d bytes", len(reply))
	}
	p := reply[paramOffset:]
	return bp.LE16(p[0:2]), bp.LE16(p[4:6]), bp.LE16(p[6:8])
}

// TestNetServerEnum2ReturnsBrowseList proves a NetServerEnum2 over IPC$ returns the
// provider's server entries, with the SERVER_INFO_1 names packed in the data block.
func TestNetServerEnum2ReturnsBrowseList(t *testing.T) {
	provider := &fakeBrowseProvider{
		available: true,
		entries: []BrowseServer{
			{Name: "CLASSICSTACK", Type: 0x00402003},
			{Name: "OTHERBOX", Type: 0x00000002},
		},
	}
	svc, sess, tid := ipcSession(t, provider)

	reply := svc.Dispatch(sess, lanmanReq(tid, rapNetServerEnum2, 0))
	status, returned, available := rapParams(t, reply)
	if status != 0 {
		t.Fatalf("RAP status = %d, want success", status)
	}
	if returned != 2 || available != 2 {
		t.Fatalf("entries returned=%d available=%d, want 2/2", returned, available)
	}

	// The data block holds the SERVER_INFO_1 records; the first name is CLASSICSTACK.
	dataOffset := protocol.HeaderLen + 1 + 20 + 2 + 8
	name := string(trimNul(reply[dataOffset : dataOffset+16]))
	if name != "CLASSICSTACK" {
		t.Errorf("first server name = %q, want CLASSICSTACK", name)
	}
}

// TestNetServerEnum2PotentialBrowser proves a potential browser (Available=false)
// answers ERROR_REQ_NOT_ACCEP in the RAP Status field (SMB status still success).
func TestNetServerEnum2PotentialBrowser(t *testing.T) {
	svc, sess, tid := ipcSession(t, &fakeBrowseProvider{available: false})
	reply := svc.Dispatch(sess, lanmanReq(tid, rapNetServerEnum2, 0))
	status, _, _ := rapParams(t, reply)
	if status != rapStatusReqNotAccepted {
		t.Fatalf("RAP status = %d, want ERROR_REQ_NOT_ACCEP(71)", status)
	}
}

// TestNetServerEnum2DomainEnumMixed proves DOMAIN_ENUM mixed with another type bit
// is rejected with ERROR_INVALID_FUNCTION.
func TestNetServerEnum2DomainEnumMixed(t *testing.T) {
	svc, sess, tid := ipcSession(t, &fakeBrowseProvider{available: true})
	reply := svc.Dispatch(sess, lanmanReq(tid, rapNetServerEnum2, svTypeDomainEnum|0x00000002))
	status, _, _ := rapParams(t, reply)
	if status != rapStatusInvalidFunction {
		t.Fatalf("RAP status = %d, want ERROR_INVALID_FUNCTION(1)", status)
	}
}

// TestNetServerEnum2NoProvider proves that with no browser wired (e.g. a NetBIOS-less
// direct-TCP :445 deployment), the pipe still reports the server ITSELF — one entry
// carrying the §4-bis server name — so a client browsing \\server sees a named entry
// rather than an empty list.
func TestNetServerEnum2NoProvider(t *testing.T) {
	svc := &Service{shares: []*Share{newTestShare(t)}}
	svc.SetServerName("MYSERVER")
	svc.SetDescription("the test server")
	sess := newSession("")
	tid := sess.allocTID(&treeConnect{ipc: true})
	reply := svc.Dispatch(sess, lanmanReq(tid, rapNetServerEnum2, 0))
	status, returned, _ := rapParams(t, reply)
	if status != 0 || returned != 1 {
		t.Fatalf("no-provider self-report: status=%d returned=%d, want 0/1", status, returned)
	}
}

// TestTransactionOnNonIPCRefused proves a TRANSACTION on a disk tree (not IPC$) is
// refused with STATUS_NOT_SUPPORTED, not served as a RAP call.
func TestTransactionOnNonIPCRefused(t *testing.T) {
	svc := &Service{shares: []*Share{newTestShare(t)}}
	svc.SetBrowseProvider(&fakeBrowseProvider{available: true})
	sess := newSession("")
	tid := sess.allocTID(&treeConnect{share: svc.shares[0]}) // disk tree, not IPC$
	reply := svc.Dispatch(sess, lanmanReq(tid, rapNetServerEnum2, 0))
	h := respHeader(t, reply)
	if h.Status != statusNotSupported {
		t.Fatalf("status = %#x, want STATUS_NOT_SUPPORTED", h.Status)
	}
}

// TestNetShareEnumListsSharesAndIPC proves NetShareEnum over IPC$ returns every
// bound disk share plus the virtual IPC$ pipe, with the SHARE_INFO_1 names + types
// packed in the data block. NetShareEnum needs no browser.
func TestNetShareEnumListsSharesAndIPC(t *testing.T) {
	svc := &Service{shares: []*Share{newTestShare(t)}} // one share named PUBLIC
	sess := newSession("")
	tid := sess.allocTID(&treeConnect{ipc: true})

	reply := svc.Dispatch(sess, lanmanReq(tid, rapNetShareEnum, 0))
	status, returned, available := rapParams(t, reply)
	if status != 0 {
		t.Fatalf("RAP status = %d, want success", status)
	}
	if returned != 2 || available != 2 {
		t.Fatalf("shares returned=%d available=%d, want 2 (PUBLIC + IPC$)", returned, available)
	}

	// Walk the two SHARE_INFO_1 records (20 bytes each): Name(13)+Pad(1)+Type(2)+RemarkOff(4).
	const entrySize = 20
	dataOffset := protocol.HeaderLen + 1 + 20 + 2 + 8
	names := make([]string, 0, 2)
	types := make([]uint16, 0, 2)
	for i := range 2 {
		base := dataOffset + i*entrySize
		names = append(names, string(trimNul(reply[base:base+13])))
		types = append(types, bp.LE16(reply[base+14:base+16]))
	}
	if names[0] != "PUBLIC" {
		t.Errorf("first share name = %q, want PUBLIC", names[0])
	}
	if names[1] != ipcShareName || types[1] != shareTypeIPC {
		t.Errorf("last entry = %q type %#x, want IPC$ / STYPE_IPC", names[1], types[1])
	}
	if types[0] != shareTypeDisktree {
		t.Errorf("disk share type = %#x, want STYPE_DISKTREE", types[0])
	}
}

// TestUnknownRAPFunctionEmptySuccess proves an unrecognised RAP function over IPC$ —
// including NetServerGetInfo (0x000D), the call Win98 issues when opening \\server —
// answers empty-success (SMB status SUCCESS, WCT=10, zero params/data), NOT
// ERRDOS/ERRbadfunc and NOT a synthesized info record. Returning "Invalid function"
// made the client abandon the server (refactor regression); returning a hand-built
// SERVER_INFO_1 record bluescreened Win98. Empty-success is the legacy
// buildSMBTransactionEmptySuccess behaviour the client tolerates. (captures/ipx.pcap.)
//
// NetWkstaGetInfo is deliberately NOT in this list: at level 10 it now returns a real
// WKSTA_INFO_10 record (a NetBEUI client rejects the empty form — see
// TestNetWkstaGetInfoReturnsIdentity). A NON-level-10 WkstaGetInfo still falls through
// to empty-success, which lanmanReq (no level word) exercises via someOtherFn.
func TestUnknownRAPFunctionEmptySuccess(t *testing.T) {
	svc := &Service{shares: []*Share{newTestShare(t)}}
	sess := newSession("")
	tid := sess.allocTID(&treeConnect{ipc: true})

	const (
		rapNetServerGetInfo = 0x000D
		someOtherFn         = 0x00FE
	)
	for _, fn := range []uint16{rapNetServerGetInfo, someOtherFn} {
		reply := svc.Dispatch(sess, lanmanReq(tid, fn, 0))
		h := respHeader(t, reply)
		if h.Status != statusSuccess {
			t.Fatalf("fn %#x: status = %#x, want SUCCESS (empty-success, not an error)", fn, h.Status)
		}
		if wct := reply[protocol.HeaderLen]; wct != 10 {
			t.Errorf("fn %#x: WordCount = %d, want 10 (TRANSACTION response shape)", fn, wct)
		}
		// Empty-success carries no RAP params/data: ParameterCount and DataCount are 0.
		wordsOff := protocol.HeaderLen + 1
		if pc := bp.LE16(reply[wordsOff+6 : wordsOff+8]); pc != 0 {
			t.Errorf("fn %#x: ParameterCount = %d, want 0 (no synthesized record)", fn, pc)
		}
		if dc := bp.LE16(reply[wordsOff+12 : wordsOff+14]); dc != 0 {
			t.Errorf("fn %#x: DataCount = %d, want 0 (no synthesized record)", fn, dc)
		}
		// ...and ParameterOffset/DataOffset MUST also be 0, matching the legacy
		// buildSMBTransactionEmptySuccess. A zero-count reply with a non-zero offset
		// makes the Win98 RAP receive path compute a buffer past the frame end, reject
		// the reply, and loop NetWkstaGetInfo forever without opening \\CLASSICSTACK
		// (captures/ipx.pcap). The whole 20-byte word block must be zero.
		if po := bp.LE16(reply[wordsOff+8 : wordsOff+10]); po != 0 {
			t.Errorf("fn %#x: ParameterOffset = %d, want 0 (empty-success block must be all-zero)", fn, po)
		}
		if do := bp.LE16(reply[wordsOff+14 : wordsOff+16]); do != 0 {
			t.Errorf("fn %#x: DataOffset = %d, want 0 (empty-success block must be all-zero)", fn, do)
		}
		if bcc := bp.LE16(reply[wordsOff+20 : wordsOff+22]); bcc != 0 {
			t.Errorf("fn %#x: ByteCount = %d, want 0 (empty-success has no trailing bytes)", fn, bcc)
		}
	}
}

// wkstaGetInfoReq builds a RAP NetWkstaGetInfo request at the given detail level,
// matching captures/netbeui.pcap: "\PIPE\LANMAN\0" + function(2) + ParamDesc "WrLh\0"
// + ReturnDesc "zzzBBzz\0" + Level(2) + ReceiveBufferLength(2).
func wkstaGetInfoReq(tid uint16, level uint16) []byte {
	area := append([]byte("\\PIPE\\LANMAN"), 0)
	fnb := make([]byte, 2)
	bp.PutLE16(fnb, rapNetWkstaGetInfo)
	area = append(area, fnb...)
	area = append(area, []byte("WrLh")...) // ParamDesc
	area = append(area, 0)
	area = append(area, []byte("zzzBBzz")...) // ReturnDesc (WKSTA_INFO_10)
	area = append(area, 0)
	lvl := make([]byte, 2)
	bp.PutLE16(lvl, level)
	area = append(area, lvl...)
	rb := make([]byte, 2)
	bp.PutLE16(rb, 0x005B) // ReceiveBufferLength (91, as in the capture)
	area = append(area, rb...)

	words := make([]byte, 28)
	return smbReq(protocol.CommandTransaction, protocol.Flags2NTStatus, tid, 1, words, area)
}

// TestNetWkstaGetInfoReturnsIdentity proves a level-10 NetWkstaGetInfo returns a real
// WKSTA_INFO_10 record (not empty-success): a Win98/WfW NetBEUI client rejects the
// empty form and loops the call forever, hanging Explorer (captures/netbeui.pcap
// frames 128→363). The record carries the server's own computer name, the session
// user, and the workgroup, packed as data-relative "z" string pointers.
func TestNetWkstaGetInfoReturnsIdentity(t *testing.T) {
	svc := &Service{shares: []*Share{newTestShare(t)}} // serverName→CLASSICSTACK, workgroup→WORKGROUP
	sess := newSession("")
	sess.user = "GUEST"
	tid := sess.allocTID(&treeConnect{ipc: true})

	reply := svc.Dispatch(sess, wkstaGetInfoReq(tid, wkstaInfoLevel10))
	h := respHeader(t, reply)
	if h.Status != statusSuccess {
		t.Fatalf("status = %#x, want SUCCESS", h.Status)
	}

	wordsOff := protocol.HeaderLen + 1
	if wct := reply[protocol.HeaderLen]; wct != 10 {
		t.Fatalf("WordCount = %d, want 10", wct)
	}
	// Empty-success is the bug we're fixing: assert real params AND data came back.
	pc := bp.LE16(reply[wordsOff+6 : wordsOff+8])
	dc := bp.LE16(reply[wordsOff+12 : wordsOff+14])
	po := bp.LE16(reply[wordsOff+8 : wordsOff+10])
	do := bp.LE16(reply[wordsOff+14 : wordsOff+16])
	if pc == 0 || dc == 0 {
		t.Fatalf("ParameterCount=%d DataCount=%d, want both non-zero (WKSTA_INFO_10 record, not empty-success)", pc, dc)
	}

	// RAP status word must be success.
	if status := bp.LE16(reply[int(po) : int(po)+2]); status != 0 {
		t.Errorf("RAP status = %d, want 0", status)
	}

	// Walk the WKSTA_INFO_10 fixed part: three z-pointers (computername/username/
	// langroup), ver_major/minor, then two more z-pointers. Each z is a 4-byte
	// data-relative offset (low word used). Resolve the strings from the data block.
	data := reply[int(do) : int(do)+int(dc)]
	readZ := func(fieldOff int) string {
		off := int(bp.LE16(data[fieldOff : fieldOff+2]))
		if off < 0 || off >= len(data) {
			t.Fatalf("z-pointer at %d = %d out of range (data len %d)", fieldOff, off, len(data))
		}
		end := indexByte(data[off:], 0)
		if end < 0 {
			t.Fatalf("unterminated string at data offset %d", off)
		}
		return string(data[off : off+end])
	}
	if got := readZ(0); got != "CLASSICSTACK" {
		t.Errorf("wki10_computername = %q, want CLASSICSTACK", got)
	}
	if got := readZ(4); got != "GUEST" {
		t.Errorf("wki10_username = %q, want GUEST", got)
	}
	if got := readZ(8); got != "WORKGROUP" {
		t.Errorf("wki10_langroup = %q, want WORKGROUP", got)
	}
	if maj := data[16]; maj != smbVerMajor {
		t.Errorf("wki10_ver_major = %d, want %d", maj, smbVerMajor)
	}
}

// TestNetWkstaGetInfoNonLevel10EmptySuccess proves a WkstaGetInfo at any other detail
// level still falls through to empty-success (we only synthesize the level-10 record).
func TestNetWkstaGetInfoNonLevel10EmptySuccess(t *testing.T) {
	svc := &Service{shares: []*Share{newTestShare(t)}}
	sess := newSession("")
	tid := sess.allocTID(&treeConnect{ipc: true})

	reply := svc.Dispatch(sess, wkstaGetInfoReq(tid, 1)) // level 1, unsupported
	wordsOff := protocol.HeaderLen + 1
	if pc := bp.LE16(reply[wordsOff+6 : wordsOff+8]); pc != 0 {
		t.Errorf("level-1 ParameterCount = %d, want 0 (empty-success)", pc)
	}
	if dc := bp.LE16(reply[wordsOff+12 : wordsOff+14]); dc != 0 {
		t.Errorf("level-1 DataCount = %d, want 0 (empty-success)", dc)
	}
}

func trimNul(b []byte) []byte {
	if i := indexByte(b, 0); i >= 0 {
		return b[:i]
	}
	return b
}
