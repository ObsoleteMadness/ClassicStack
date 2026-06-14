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
	sess := newSession()
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

// TestNetServerEnum2NoProvider proves that with no browser wired, the pipe answers
// an empty success rather than dropping or erroring.
func TestNetServerEnum2NoProvider(t *testing.T) {
	svc := &Service{shares: []*Share{newTestShare(t)}}
	sess := newSession()
	tid := sess.allocTID(&treeConnect{ipc: true})
	reply := svc.Dispatch(sess, lanmanReq(tid, rapNetServerEnum2, 0))
	status, returned, _ := rapParams(t, reply)
	if status != 0 || returned != 0 {
		t.Fatalf("no-provider: status=%d returned=%d, want 0/0", status, returned)
	}
}

// TestTransactionOnNonIPCRefused proves a TRANSACTION on a disk tree (not IPC$) is
// refused with STATUS_NOT_SUPPORTED, not served as a RAP call.
func TestTransactionOnNonIPCRefused(t *testing.T) {
	svc := &Service{shares: []*Share{newTestShare(t)}}
	svc.SetBrowseProvider(&fakeBrowseProvider{available: true})
	sess := newSession()
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
	sess := newSession()
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

func trimNul(b []byte) []byte {
	if i := indexByte(b, 0); i >= 0 {
		return b[:i]
	}
	return b
}
