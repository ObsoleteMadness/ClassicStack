package smb

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// TestConn_ServeMessageSharesSession proves a Conn carries one smbSession across
// successive messages: SESSION_SETUP grants a UID that a later TREE_CONNECT on the
// same Conn reuses, and the bound TID resolves on the same session — the
// per-virtual-circuit state the transport seam relies on.
func TestConn_ServeMessageSharesSession(t *testing.T) {
	sh := newTestShare(t) // share "PUBLIC"
	svc := &Service{shares: []*Share{sh}}
	conn := svc.NewConn("")

	// SESSION_SETUP_ANDX grants a guest UID on the circuit.
	setup := smbReq(protocol.CommandSessionSetupAndX, protocol.Flags2NTStatus, 0, 0, make([]byte, 26), nil)
	sh1 := respHeader(t, conn.ServeMessage(setup))
	if sh1.UID == 0 {
		t.Fatal("SESSION_SETUP granted UID 0")
	}

	// TREE_CONNECT_ANDX on the same circuit binds a TID against PUBLIC.
	words := make([]byte, 8)
	words[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(words[6:8], 1) // PasswordLength = 1
	area := []byte{0x00}
	area = append(area, []byte("\\\\SERVER\\PUBLIC")...)
	area = append(area, 0)
	area = append(area, []byte("?????")...)
	area = append(area, 0)
	tc := smbReq(protocol.CommandTreeConnectAndX, protocol.Flags2NTStatus, 0, sh1.UID, words, area)
	th := respHeader(t, conn.ServeMessage(tc))
	if th.Status != statusSuccess || th.TID == 0 {
		t.Fatalf("TREE_CONNECT on circuit failed: status=%#x tid=%d", th.Status, th.TID)
	}
	if tcn, ok := conn.sess.tree(th.TID); !ok || tcn.share == nil || tcn.share.Name() != "PUBLIC" {
		t.Fatalf("TID %d not bound to PUBLIC on the circuit", th.TID)
	}
}

// TestConn_CloseReleasesHandles proves Conn.Close drains open file handles, so a
// dropped circuit does not leak them.
func TestConn_CloseReleasesHandles(t *testing.T) {
	sh := newTestShare(t)
	svc := &Service{shares: []*Share{sh}}
	conn := svc.NewConn("")

	// Bind a tree directly and create a file, leaving its FID open.
	tid := conn.sess.allocTID(&treeConnect{share: sh})
	words := make([]byte, 6)
	create := smbReq(protocol.CommandCreate, protocol.Flags2NTStatus, tid, 1, words, ansiPathArea("open.bin"))
	rh := respHeader(t, conn.ServeMessage(create))
	if rh.Status != statusSuccess {
		t.Fatalf("CREATE on circuit status = %#x", rh.Status)
	}
	if len(conn.sess.fids) != 1 {
		t.Fatalf("expected 1 open FID, got %d", len(conn.sess.fids))
	}

	conn.Close()
	if len(conn.sess.fids) != 0 {
		t.Fatalf("Close left %d open FIDs", len(conn.sess.fids))
	}
}

// TestConn_NonSMBDropsSilently proves a non-SMB message on the circuit yields no
// response bytes (the transport sends nothing).
func TestConn_NonSMBDropsSilently(t *testing.T) {
	sh := newTestShare(t)
	svc := &Service{shares: []*Share{sh}}
	conn := svc.NewConn("")
	if resp := conn.ServeMessage([]byte("garbage not smb ...............")); resp != nil {
		t.Fatalf("non-SMB message produced a response: %x", resp)
	}
}

// TestSessions_TracksClientAndNegotiatedDialect proves the service tracks one live
// session per circuit keyed by the transport client label, and records the dialect
// the client negotiated (SMB_COM_NEGOTIATE) so the management view reports the
// per-client SMB version. Closing the circuit drops it from the tracked set.
func TestSessions_TracksClientAndNegotiatedDialect(t *testing.T) {
	sh := newTestShare(t)
	svc := &Service{shares: []*Share{sh}}

	conn := svc.NewConn("00:00:d8:72:e9:a4.0455")
	// Before NEGOTIATE the session is tracked with no dialect.
	if got := svc.Sessions(); len(got) != 1 || got[0].Client != "00:00:d8:72:e9:a4.0455" || got[0].Dialect != "" {
		t.Fatalf("pre-negotiate Sessions() = %+v, want one client with empty dialect", got)
	}

	// A WfW client negotiates: the session records the selected LANMAN dialect.
	neg := smbReq(protocol.CommandNegotiate, 0, 0, 0, nil,
		dialectListBytes(protocol.DialectPCNetwork1, protocol.DialectWfW311))
	conn.ServeMessage(neg)

	got := svc.Sessions()
	if len(got) != 1 {
		t.Fatalf("Sessions() len = %d, want 1", len(got))
	}
	if got[0].Client != "00:00:d8:72:e9:a4.0455" {
		t.Errorf("Client = %q, want the circuit's transport label", got[0].Client)
	}
	if got[0].Dialect != protocol.DialectWfW311 {
		t.Errorf("Dialect = %q, want %q", got[0].Dialect, protocol.DialectWfW311)
	}
	if got[0].NegotiatedAt.IsZero() {
		t.Error("NegotiatedAt is zero after a successful NEGOTIATE")
	}

	// Closing the circuit drops it from the tracked set.
	conn.Close()
	if got := svc.Sessions(); len(got) != 0 {
		t.Fatalf("post-close Sessions() = %+v, want empty", got)
	}
}
