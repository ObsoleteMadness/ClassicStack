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
	conn := svc.NewConn()

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
	conn := svc.NewConn()

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
	conn := svc.NewConn()
	if resp := conn.ServeMessage([]byte("garbage not smb ...............")); resp != nil {
		t.Fatalf("non-SMB message produced a response: %x", resp)
	}
}
